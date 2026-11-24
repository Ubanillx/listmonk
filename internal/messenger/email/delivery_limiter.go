package email

import (
	"strconv"
	"sync"
	"time"

	"github.com/knadh/listmonk/models"
)

// DeliveryLimiter is shared by every SMTP pool in one sender process,
// including direct system notifications. Waiting never consumes an allowance;
// all applicable limits are acquired together immediately before SMTP Send.
type DeliveryLimiter struct {
	mu             sync.Mutex
	perSecond      int
	windowLimit    int
	windowDuration time.Duration
	secondStarts   []time.Time
	windowStarts   []time.Time
	campaignNext   map[string]time.Time
	// campaignActive counts admitted messages whose delivery result is not
	// persisted yet. Bounding it keeps a congested queue from turning into a
	// burst of mail for one campaign once capacity returns.
	campaignActive map[string]int
	lastPrune      time.Time
	shutdown       <-chan struct{}
	maxConcurrent  int
	active         int
	changed        chan struct{}
	// campaignChanged wakes producers waiting on a campaign's outstanding cap
	// without disturbing platform-concurrency waiters on changed.
	campaignChanged chan struct{}
}

// SetConcurrency configures the platform-wide number of active SMTP attempts.
// Configure it before any senders use the limiter.
func (l *DeliveryLimiter) SetConcurrency(limit int) {
	l.maxConcurrent = limit
	l.changed = make(chan struct{})
}

// Release frees the SMTP attempt acquired by Wait and wakes queued senders.
func (l *DeliveryLimiter) Release() {
	if l == nil || l.maxConcurrent < 1 {
		return
	}
	l.mu.Lock()
	l.active--
	close(l.changed)
	l.changed = make(chan struct{})
	l.mu.Unlock()
}

func NewDeliveryLimiter(perSecond, windowLimit int, windowDuration time.Duration, shutdown <-chan struct{}) *DeliveryLimiter {
	return &DeliveryLimiter{
		perSecond: perSecond, windowLimit: windowLimit, windowDuration: windowDuration,
		campaignNext: make(map[string]time.Time), campaignActive: make(map[string]int),
		shutdown: shutdown, changed: make(chan struct{}), campaignChanged: make(chan struct{}),
	}
}

func trimStarts(starts []time.Time, cutoff time.Time) []time.Time {
	n := 0
	for n < len(starts) && !starts[n].After(cutoff) {
		n++
	}
	return starts[n:]
}

func campaignKey(c *models.Campaign) string {
	if c == nil {
		return ""
	}
	if c.UUID != "" {
		return c.UUID
	}
	if c.ID > 0 {
		return "id:" + strconv.Itoa(c.ID)
	}
	return ""
}

// CampaignInterval returns the minimum spacing between two sends of the
// campaign, derived from its per-minute limit and the source default.
func CampaignInterval(c *models.Campaign) time.Duration {
	limit := c.SMTPRateLimit
	if limit < 1 {
		limit = models.DefaultPersonalSMTPRateLimit
		if c.SMTPSource == "organization" {
			limit = models.DefaultOrganizationSMTPRateLimit
		}
	}
	return (time.Minute + time.Duration(limit) - 1) / time.Duration(limit)
}

// campaignWait returns how long the campaign has to wait for its own send
// slot. The caller holds mu. A key is returned even when no wait is needed so
// the caller can reserve the slot in the same critical section.
func (l *DeliveryLimiter) campaignWait(now time.Time, c *models.Campaign) (string, time.Duration) {
	key := campaignKey(c)
	if key == "" {
		return "", 0
	}
	if next := l.campaignNext[key]; next.After(now) {
		return key, next.Sub(now)
	}
	return key, 0
}

// reserveCampaign records the campaign's next allowed send. The caller holds
// mu.
func (l *DeliveryLimiter) reserveCampaign(now time.Time, key string, interval time.Duration) {
	if key == "" {
		return
	}
	if now.Sub(l.lastPrune) >= time.Minute {
		for id, next := range l.campaignNext {
			if !next.After(now) {
				delete(l.campaignNext, id)
			}
		}
		l.lastPrune = now
	}
	l.campaignNext[key] = now.Add(interval)
}

// take computes the next available slot and records only an immediately
// available send. The caller holds mu. now is explicit for deterministic tests.
// campaignReserved skips the campaign pacing for messages that consumed their
// campaign slot at queue admission.
func (l *DeliveryLimiter) take(now time.Time, campaign *models.Campaign, campaignReserved bool) time.Duration {
	var wait time.Duration
	if l.maxConcurrent > 0 && l.active >= l.maxConcurrent {
		// Completion wakes the waiter immediately; the timer only bounds a wait
		// if a caller fails to release its attempt.
		wait = time.Hour
	}
	key := ""
	interval := time.Duration(0)
	if campaign != nil && !campaignReserved {
		var campaignDelay time.Duration
		key, campaignDelay = l.campaignWait(now, campaign)
		wait = max(wait, campaignDelay)
		interval = CampaignInterval(campaign)
	}
	// Apply the campaign cap first, then the hard platform ceilings. A campaign
	// never reserves platform capacity while it is waiting for its own slot.
	if l.perSecond > 0 {
		l.secondStarts = trimStarts(l.secondStarts, now.Add(-time.Second))
		if len(l.secondStarts) >= l.perSecond {
			wait = max(wait, l.secondStarts[0].Add(time.Second).Sub(now))
		}
	}
	if l.windowLimit > 0 && l.windowDuration > 0 {
		l.windowStarts = trimStarts(l.windowStarts, now.Add(-l.windowDuration))
		if len(l.windowStarts) >= l.windowLimit {
			wait = max(wait, l.windowStarts[0].Add(l.windowDuration).Sub(now))
		}
	}
	if wait > 0 {
		return wait
	}
	l.reserveCampaign(now, key, interval)
	if l.maxConcurrent > 0 {
		l.active++
	}
	if l.perSecond > 0 {
		l.secondStarts = append(l.secondStarts, now)
	}
	if l.windowLimit > 0 && l.windowDuration > 0 {
		l.windowStarts = append(l.windowStarts, now)
	}
	return 0
}

// WaitCampaign blocks until the campaign's per-minute allowance has a free
// slot and, when maxOutstanding > 0, fewer than that many admitted messages
// are still waiting for their delivery result. It then reserves the slot and
// counts the message as outstanding until ReleaseCampaign.
//
// Campaign messages call this before they are admitted to the delivery queue,
// so waiting for a slow campaign never occupies a worker or an SMTP connection
// that another running campaign could use. Bounding outstanding messages keeps
// the campaign's smoothing intact during queue congestion: without the bound,
// a paused drain would later release a whole batch of pre-admitted messages at
// the platform rate instead of the campaign rate.
func (l *DeliveryLimiter) WaitCampaign(c *models.Campaign, maxOutstanding int, closed <-chan struct{}) error {
	key := campaignKey(c)
	if l == nil || key == "" {
		return nil
	}
	for {
		select {
		case <-closed:
			return ErrSendCancelled
		case <-l.shutdown:
			return ErrSendCancelled
		default:
		}

		l.mu.Lock()
		now := time.Now()
		wait := l.campaignNext[key].Sub(now)
		if wait <= 0 && (maxOutstanding < 1 || l.campaignActive[key] < maxOutstanding) {
			l.reserveCampaign(now, key, CampaignInterval(c))
			l.campaignActive[key]++
			l.mu.Unlock()
			return nil
		}
		changed := l.campaignChanged
		l.mu.Unlock()
		if wait <= 0 {
			// Only the outstanding cap is blocking. ReleaseCampaign wakes this
			// wait; the timer only guards against a lost wakeup.
			wait = time.Second
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-changed:
			timer.Stop()
		case <-closed:
			timer.Stop()
			return ErrSendCancelled
		case <-l.shutdown:
			timer.Stop()
			return ErrSendCancelled
		}
	}
}

// ReleaseCampaign frees the outstanding message counted by WaitCampaign after
// the delivery result (sent or failed) has been persisted.
func (l *DeliveryLimiter) ReleaseCampaign(c *models.Campaign) {
	key := campaignKey(c)
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	if n := l.campaignActive[key]; n > 1 {
		l.campaignActive[key] = n - 1
	} else {
		delete(l.campaignActive, key)
	}
	close(l.campaignChanged)
	l.campaignChanged = make(chan struct{})
	l.mu.Unlock()
}

func (l *DeliveryLimiter) Wait(m models.Message, closed <-chan struct{}) error {
	if l == nil {
		return nil
	}
	for {
		select {
		case <-closed:
			return ErrSMTPUnavailable
		case <-l.shutdown:
			return ErrSendCancelled
		case <-m.SendCancel:
			return ErrSendCancelled
		case <-m.CampaignCancel:
			return ErrSendCancelled
		default:
		}
		l.mu.Lock()
		wait := l.take(time.Now(), m.Campaign, m.CampaignRateReserved)
		changed := l.changed
		l.mu.Unlock()
		if wait == 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-changed:
			timer.Stop()
		case <-closed:
			timer.Stop()
			return ErrSMTPUnavailable
		case <-l.shutdown:
			timer.Stop()
			return ErrSendCancelled
		case <-m.SendCancel:
			timer.Stop()
			return ErrSendCancelled
		case <-m.CampaignCancel:
			timer.Stop()
			return ErrSendCancelled
		}
	}
}
