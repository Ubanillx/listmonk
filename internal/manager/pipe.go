package manager

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/schedule"
	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

const (
	stopReasonNone int32 = iota
	stopReasonPause
	stopReasonDeferred
	stopReasonCancelled
	stopReasonPersonalSMTP
)

type pipe struct {
	camp       *models.Campaign
	messenger  Messenger
	rate       *ratecounter.RateCounter
	wg         *sync.WaitGroup
	sent       atomic.Int64
	errors     atomic.Uint64
	stopped    atomic.Bool
	deferred   atomic.Bool
	deferMut   sync.Mutex
	withErrors atomic.Bool
	stopReason atomic.Int32

	m *Manager
}

// newPipe adds a campaign to the process queue.
func (m *Manager) newPipe(c *models.Campaign) (*pipe, error) {
	// Validate messenger.
	if _, ok := m.messengers[c.Messenger]; !ok && !email.IsMessengerName(c.Messenger) {
		m.store.UpdateCampaignStatus(c.ID, models.CampaignStatusCancelled)
		return nil, fmt.Errorf("unknown messenger %s on campaign %s", c.Messenger, c.Name)
	}
	// Every e-mail campaign is account-owned. Reject malformed/legacy rows
	// without an owner before resolving a messenger so they can never probe or
	// borrow the platform system SMTP pool.
	if email.IsMessengerName(c.Messenger) &&
		(!c.OwnerUserID.Valid || c.OwnerUserID.Int < 1) {
		m.markCampaignSMTPUnavailable(c)
		return nil, fmt.Errorf("campaign %s cannot send: %w: campaign has no account owner", c.Name, ErrPersonalSMTPUnavailable)
	}
	msg := models.Message{Messenger: c.Messenger, OwnerUserID: c.OwnerUserID.Int}
	msgr, err := m.resolveMessenger(msg)
	if err != nil {
		// A campaign without an account SMTP must never silently use the platform
		// SMTP. Preserve the prior scheduler state when the store supports the
		// atomic strict transition; this pauses an already-running campaign while
		// returning a scheduled/deferred claim to draft.
		if errors.Is(err, ErrPersonalSMTPUnavailable) {
			m.markCampaignSMTPUnavailable(c)
		} else {
			m.markCampaignStartFailure(c)
		}
		return nil, fmt.Errorf("campaign %s cannot send: %w", c.Name, err)
	}

	// Load the template.
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		// The scheduler may already have changed a scheduled/deferred campaign to
		// running before template compilation.  Always leave a failed claim in a
		// retryable state instead of allowing it to remain running forever.
		m.markCampaignStartFailure(c)
		return nil, err
	}

	// Load any media/attachments.
	if err := m.attachMedia(c); err != nil {
		// A campaign can remain in the scheduler's result set while one of its
		// media/template rows is transferred or removed.  Do not leave a running
		// row retrying forever with an invalid attachment graph; pause it so the
		// owner can repair the content, while a not-yet-started campaign returns
		// to draft as it does for missing personal SMTP.
		m.markCampaignStartFailure(c)
		return nil, err
	}

	// Add the campaign to the active map.
	p := &pipe{
		camp: c,
		// Personal SMTP is resolved for every campaign message so a running
		// campaign observes account configuration changes immediately. Other
		// messengers are immutable for the lifetime of the pipe.
		messenger: msgr,
		rate:      ratecounter.NewRateCounter(time.Minute),
		wg:        &sync.WaitGroup{},
		m:         m,
	}
	if email.IsMessengerName(c.Messenger) {
		p.messenger = nil
	}

	// Increment the waitgroup so that Wait() blocks immediately. This is necessary
	// as a campaign pipe is created first and subscribers/messages under it are
	// fetched asynchronolusly later. The messages each add to the wg and that
	// count is used to determine the exhaustion/completion of all messages.
	p.wg.Add(1)

	go func() {
		// Wait for all the messages in the campaign to be processed
		// (successfully or skipped after errors or cancellation).
		p.wg.Wait()

		p.cleanup()
	}()

	m.pipesMut.Lock()
	m.pipes[c.ID] = p
	m.pipesMut.Unlock()
	return p, nil
}

// NextSubscribers processes the next batch of subscribers in a given campaign.
// It returns a bool indicating whether any subscribers were processed
// in the current batch or not. A false indicates that all subscribers
// have been processed, or that a campaign has been paused or cancelled.
func (p *pipe) NextSubscribers() (bool, error) {
	// A worker can discover an unavailable personal SMTP pool while this
	// goroutine is fetching subscribers. Do not claim another batch after the
	// pipe has been stopped; cleanup will reset any already queued recipients.
	if p.stopped.Load() {
		return false, nil
	}

	// Fetch the next batch of subscribers from a 'running' campaign.
	subs, err := p.m.store.NextSubscribers(p.camp.ID, p.m.cfg.BatchSize)
	if errors.Is(err, ErrCampaignDeferred) {
		p.Defer()
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error fetching campaign subscribers (%s): %v", p.camp.Name, err)
	}

	// There are no subscribers from the query. Either all subscribers on the campaign
	// have been processed, or the campaign has changed from 'running' to 'paused' or 'cancelled'.
	if len(subs) == 0 {
		return false, nil
	}

	// Is there a sliding window limit configured?
	hasSliding := p.m.cfg.SlidingWindow &&
		p.m.cfg.SlidingWindowRate > 0 &&
		p.m.cfg.SlidingWindowDuration.Seconds() > 1

	// Push messages.
	for _, s := range subs {
		if p.stopped.Load() {
			return false, nil
		}

		msg, err := p.newMessage(s)
		if err != nil {
			p.m.log.Printf("error rendering message (%s) (%s): %v", p.camp.Name, s.Email, err)
			continue
		}
		// Stop may race with rendering. The message has already incremented the
		// pipe wait group, so release that increment when it is intentionally not
		// handed to a worker.
		if p.stopped.Load() {
			p.wg.Done()
			return false, nil
		}

		// Push the message to the queue while blocking and waiting until
		// the queue is drained. During a reload the manager's done signal can
		// race with this send; select on it so the producer cannot remain stuck
		// (or write into a queue whose workers have already exited).
		select {
		case p.m.campMsgQ <- msg:
		case <-p.m.done:
			p.Stop(stopReasonPause, false)
			p.wg.Done()
			return false, nil
		}

		// Check if the sliding window is active.
		if hasSliding {
			diff := time.Since(p.m.slidingStart)

			// Window has expired. Reset the clock.
			if diff >= p.m.cfg.SlidingWindowDuration {
				p.m.slidingStart = time.Now()
				p.m.slidingCount = 0
			}

			// Have the messages exceeded the limit?
			p.m.slidingCount++
			if p.m.slidingCount >= p.m.cfg.SlidingWindowRate {
				wait := p.m.cfg.SlidingWindowDuration - diff

				p.m.log.Printf("messages exceeded (%d) for the window (%v since %s). Sleeping for %s.",
					p.m.slidingCount,
					p.m.cfg.SlidingWindowDuration,
					p.m.slidingStart.Format(time.RFC822Z),
					wait.Round(time.Second)*1)

				p.m.slidingCount = 0
				time.Sleep(wait)
			}
		}
	}

	return true, nil
}

// OnError keeps track of the number of errors that occur while sending messages
// and pauses the campaign if the error threshold is met.
func (p *pipe) OnError() {
	if p.m.cfg.MaxSendErrors < 1 {
		return
	}

	// If the error threshold is met, pause the campaign.
	count := p.errors.Add(1)
	if int(count) < p.m.cfg.MaxSendErrors {
		return
	}

	p.Stop(stopReasonPause, true)
	p.m.log.Printf("error count exceeded %d. pausing campaign %s", p.m.cfg.MaxSendErrors, p.camp.Name)
}

func (p *pipe) Defer() {
	p.deferCampaign(false)
}

// DeferImmediately prevents further queued messages from being delivered.
// It is used when the SMTP pool rejects a message for its own daily quota.
// In contrast, a campaign-level cap has already reserved its final batch, so
// Defer lets that batch drain before the campaign is resumed the next day.
func (p *pipe) DeferImmediately() {
	p.deferCampaign(true)
}

func (p *pipe) deferCampaign(stopQueuedMessages bool) {
	if p.stopped.Load() {
		return
	}

	p.deferMut.Lock()
	if !p.deferred.Load() {
		next := schedule.NextDailyResumeAt(p.camp.DailyResumeTime, time.Now())
		if err := p.m.store.DeferCampaign(p.camp.ID, next); err != nil {
			p.deferMut.Unlock()
			p.m.log.Printf("error deferring campaign (%s): %v", p.camp.Name, err)
			return
		}
		p.deferred.Store(true)
	}
	p.deferMut.Unlock()

	if stopQueuedMessages {
		p.Stop(stopReasonDeferred, false)
	}
}

// Stop "marks" a campaign as stopped. It doesn't actually stop the processing
// of messages. That happens when every queued message in the campaign is processed,
// marking .wg, the waitgroup counter as done. That triggers cleanup().
func (p *pipe) Stop(reason int32, withErrors bool) {
	// Already stopped.
	if p.stopped.Load() {
		return
	}

	if withErrors {
		p.withErrors.Store(true)
	}

	p.stopReason.Store(reason)
	p.stopped.Store(true)
}

// newMessage returns a campaign message while internally incrementing the
// number of messages in the pipe wait group so that the status of every
// message can be atomically tracked.
func (p *pipe) newMessage(s models.CampaignSubscriber) (CampaignMessage, error) {
	msg, err := p.m.NewCampaignMessage(p.camp, s.Subscriber)
	if err != nil {
		return msg, err
	}

	msg.pipe = p
	p.wg.Add(1)

	return msg, nil
}

// cleanup finishes the campaign and updates the campaign status in the DB
// and also triggers a notification to the admin. This only triggers once
// a pipe's wg counter is fully exhausted, draining all messages in its queue.
func (p *pipe) cleanup() {
	defer func() {
		p.m.pipesMut.Lock()
		delete(p.m.pipes, p.camp.ID)
		p.m.pipesMut.Unlock()
	}()

	// The campaign was auto-paused due to errors.
	if p.withErrors.Load() {
		if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
			p.m.log.Printf("error resetting queued recipients (%s): %v", p.camp.Name, err)
		}
		if err := p.m.store.UpdateCampaignStatus(p.camp.ID, models.CampaignStatusPaused); err != nil {
			p.m.log.Printf("error updating campaign (%s) status to %s: %v", p.camp.Name, models.CampaignStatusPaused, err)
		} else {
			p.m.log.Printf("set campaign (%s) to %s", p.camp.Name, models.CampaignStatusPaused)
		}

		_ = p.m.sendNotif(p.camp, models.CampaignStatusPaused, "Too many errors")
		return
	}

	// The campaign was manually stopped (pause, cancel).
	if p.stopped.Load() {
		switch p.stopReason.Load() {
		case stopReasonPause:
			if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
				p.m.log.Printf("error resetting queued recipients (%s): %v", p.camp.Name, err)
			}
		case stopReasonPersonalSMTP:
			// Persist the stop atomically with recipient reset and scheduling
			// timestamp cleanup when the database store supports it. This closes
			// the race in which a scanner could observe a still-running row after
			// the account SMTP pool was disabled. Lightweight test stores retain
			// the historical two-call fallback below.
			if strict, ok := p.m.store.(PersonalSMTPUnavailableStore); ok {
				if err := strict.MarkCampaignSMTPUnavailable(p.camp.ID, models.CampaignStatusRunning); err != nil {
					p.m.log.Printf("error marking campaign (%s) SMTP unavailable: %v", p.camp.Name, err)
				}
			} else {
				if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
					p.m.log.Printf("error resetting queued recipients (%s): %v", p.camp.Name, err)
				}
				if err := p.m.store.UpdateCampaignStatus(p.camp.ID, models.CampaignStatusPaused); err != nil {
					p.m.log.Printf("error pausing campaign (%s) after personal SMTP failure: %v", p.camp.Name, err)
				}
			}
			// Do not overwrite a concurrent manual pause/cancel. Fetch the final
			// state only for logging/notification after the atomic transition.
			if current, err := p.m.store.GetCampaign(p.camp.ID); err != nil {
				p.m.log.Printf("error fetching campaign (%s) after personal SMTP failure: %v", p.camp.Name, err)
			} else if current.Status == models.CampaignStatusPaused {
				p.m.log.Printf("paused campaign (%s): personal SMTP unavailable", p.camp.Name)
				_ = p.m.sendNotif(current, models.CampaignStatusPaused, "Personal SMTP unavailable")
			}
		case stopReasonDeferred:
			if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusDeferred); err != nil {
				p.m.log.Printf("error deferring queued recipients (%s): %v", p.camp.Name, err)
			}
		case stopReasonCancelled:
			if err := p.m.store.UpdateCampaignRecipientStatuses(p.camp.ID, models.CampaignRecipientStatusCancelled, []string{
				models.CampaignRecipientStatusPending,
				models.CampaignRecipientStatusDeferred,
				models.CampaignRecipientStatusQueued,
			}); err != nil {
				p.m.log.Printf("error cancelling queued recipients (%s): %v", p.camp.Name, err)
			}
		}
		p.m.log.Printf("stop processing campaign (%s)", p.camp.Name)
		return
	}

	// The final batch is allowed to drain when the campaign cap is reached.
	// The campaign remains deferred and all unsent recipients are already in
	// deferred state, ready for the next scheduler claim.
	if p.deferred.Load() {
		if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusDeferred); err != nil {
			p.m.log.Printf("error deferring queued recipients (%s): %v", p.camp.Name, err)
		}
		p.m.log.Printf("deferred campaign (%s) until the next daily resume time", p.camp.Name)
		return
	}

	// Campaign wasn't manually stopped and subscribers were naturally exhausted.
	// Fetch the up-to-date campaign status from the DB.
	c, err := p.m.store.GetCampaign(p.camp.ID)
	if err != nil {
		p.m.log.Printf("error fetching campaign (%s) for ending: %v", p.camp.Name, err)
		return
	}

	// If a running campaign has exhausted subscribers, it's finished.
	if c.Status == models.CampaignStatusRunning || c.Status == models.CampaignStatusScheduled || c.Status == models.CampaignStatusDeferred {
		c.Status = models.CampaignStatusFinished
		if err := p.m.store.UpdateCampaignStatus(p.camp.ID, models.CampaignStatusFinished); err != nil {
			p.m.log.Printf("error finishing campaign (%s): %v", p.camp.Name, err)
		} else {
			p.m.log.Printf("campaign (%s) finished", p.camp.Name)
		}
	} else {
		p.m.log.Printf("finish processing campaign (%s)", p.camp.Name)
	}

	// Notify admin.
	_ = p.m.sendNotif(c, c.Status, "")
}
