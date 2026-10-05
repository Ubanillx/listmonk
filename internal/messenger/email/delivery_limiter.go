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
	lastPrune      time.Time
	shutdown       <-chan struct{}
	maxConcurrent  int
	active         int
	changed        chan struct{}
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
		campaignNext: make(map[string]time.Time), shutdown: shutdown,
	}
}

func trimStarts(starts []time.Time, cutoff time.Time) []time.Time {
	n := 0
	for n < len(starts) && !starts[n].After(cutoff) {
		n++
	}
	return starts[n:]
}

// take computes the next available slot and records only an immediately
// available send. The caller holds mu. now is explicit for deterministic tests.
func (l *DeliveryLimiter) take(now time.Time, campaign *models.Campaign) time.Duration {
	var wait time.Duration
	if l.maxConcurrent > 0 && l.active >= l.maxConcurrent {
		// Completion wakes the waiter immediately; the timer only bounds a wait
		// if a caller fails to release its attempt.
		wait = time.Hour
	}
	key := ""
	var interval time.Duration
	if campaign != nil {
		key = campaign.UUID
		if key == "" && campaign.ID > 0 {
			key = "id:" + strconv.Itoa(campaign.ID)
		}
		if key != "" {
			limit := campaign.SMTPRateLimit
			if limit < 1 {
				limit = models.DefaultPersonalSMTPRateLimit
				if campaign.SMTPSource == "organization" {
					limit = models.DefaultOrganizationSMTPRateLimit
				}
			}
			interval = (time.Minute + time.Duration(limit) - 1) / time.Duration(limit)
			wait = max(wait, l.campaignNext[key].Sub(now))
		}
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
	if now.Sub(l.lastPrune) >= time.Minute {
		for id, next := range l.campaignNext {
			if !next.After(now) {
				delete(l.campaignNext, id)
			}
		}
		l.lastPrune = now
	}
	if key != "" {
		l.campaignNext[key] = now.Add(interval)
	}
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
		wait := l.take(time.Now(), m.Campaign)
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
