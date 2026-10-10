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
	done       chan struct{}
	stopOnce   sync.Once

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
	// Platform-level public-pool campaigns resolve their SMTP per message
	// from the target organization's member SMTP pool, so the campaign
	// owner's own personal SMTP is not required here; availability was
	// validated when the campaign started and per-message resolution failure
	// pauses the campaign.
	poolScoped := c.PoolScope == models.CampaignPoolScopeAllOrganizations
	var msgr Messenger
	if poolScoped {
		// A platform-level campaign must not borrow any messenger that is
		// not the organization pool; keep the pipe messengerless for e-mail.
		if !email.IsMessengerName(c.Messenger) {
			msg := models.Message{Messenger: c.Messenger}
			var err error
			msgr, err = m.resolveMessenger(msg)
			if err != nil {
				m.markCampaignStartFailure(c)
				return nil, fmt.Errorf("campaign %s cannot send: %w", c.Name, err)
			}
		}
	} else {
		msg := models.Message{Messenger: c.Messenger, OwnerUserID: c.OwnerUserID.Int, Campaign: c}
		var err error
		msgr, err = m.resolveMessenger(msg)
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
		done: make(chan struct{}),
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
	// as a campaign pipe is created first and customers/messages under it are
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

// NextCustomers processes the next batch of customers in a given campaign.
// It returns a bool indicating whether any customers were processed
// in the current batch or not. A false indicates that all customers
// have been processed, or that a campaign has been paused or cancelled.
func (p *pipe) NextCustomers() (bool, error) {
	// A worker can discover an unavailable personal SMTP pool while this
	// goroutine is fetching customers. Do not claim another batch after the
	// pipe has been stopped; cleanup will reset any already queued recipients.
	if p.stopped.Load() {
		return false, nil
	}

	// Fetch the next batch of customers from a 'running' campaign.
	subs, err := p.m.store.NextCustomers(p.camp.ID, p.m.cfg.BatchSize)
	if errors.Is(err, ErrCampaignDeferred) {
		p.Defer()
		return false, nil
	}
	if errors.Is(err, ErrPoolSMTPUnavailable) {
		// A target organization's member SMTP pool has no eligible SMTP
		// account. Pause the campaign strictly (no owner/platform fallback)
		// so an operator can repair the pool and resume.
		p.Stop(stopReasonPersonalSMTP, false)
		p.m.log.Printf("paused campaign (%s): %v", p.camp.Name, err)
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("error fetching campaign customers (%s): %v", p.camp.Name, err)
	}

	// There are no customers from the query. Either all customers on the campaign
	// have been processed, or the campaign has changed from 'running' to 'paused' or 'cancelled'.
	if len(subs) == 0 {
		return false, nil
	}

	// Push messages.
	for _, s := range subs {
		if p.stopped.Load() {
			return false, nil
		}

		msg, err := p.newMessage(s)
		if err != nil {
			p.m.log.Printf("error rendering message (%s) (%s): %v", p.camp.Name, s.Email, err)
			p.m.recordCampaignSendError(p.camp, s.Customer, s.PoolContactID, int(s.PoolOrganizationID), "render", err)
			var resetErr error
			if s.PoolContactID > 0 {
				if ps, ok := p.m.store.(PoolRecipientStore); ok {
					resetErr = ps.MarkPoolCampaignRecipientStatus(p.camp.ID, s.PoolContactID, models.CampaignRecipientStatusPending)
				}
			} else {
				resetErr = p.m.store.MarkCampaignRecipientStatus(p.camp.ID, s.ID, models.CampaignRecipientStatusPending)
			}
			if resetErr != nil {
				p.m.log.Printf("error resetting campaign recipient (%s:%d): %v", p.camp.Name, s.ID, resetErr)
			}
			p.OnError()
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

	}

	return true, nil
}

// OnError keeps track of the number of errors that occur while sending messages
// and pauses the campaign if the error threshold is met.
func (p *pipe) OnError() {
	count := p.errors.Add(1)
	if p.m.cfg.MaxSendErrors < 1 {
		return
	}

	// If the error threshold is met, pause the campaign.
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
		err := p.m.WithCampaignStatusChange(func() error {
			if p.stopped.Load() {
				return ErrCampaignNotRunning
			}
			return p.m.store.DeferCampaign(p.camp.ID, next)
		})
		if err != nil {
			p.deferMut.Unlock()
			if errors.Is(err, ErrCampaignNotRunning) {
				p.Stop(stopReasonPause, false)
				return
			}
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

// Stop marks a campaign as stopped and cancels pending SMTP waits. Queued
// messages still finish their waitgroup accounting before cleanup() runs.
func (p *pipe) Stop(reason int32, withErrors bool) {
	p.stopOnce.Do(func() {
		if withErrors {
			p.withErrors.Store(true)
		}
		p.stopReason.Store(reason)
		p.stopped.Store(true)
		if p.done != nil {
			close(p.done)
		}
	})
}

// newMessage returns a campaign message while internally incrementing the
// number of messages in the pipe wait group so that the status of every
// message can be atomically tracked.
func (p *pipe) newMessage(s models.CampaignCustomer) (CampaignMessage, error) {
	msg, err := p.m.NewCampaignMessage(p.camp, s.Customer)
	if err != nil {
		return msg, err
	}

	msg.pipe = p
	msg.PoolContactID = s.PoolContactID
	msg.PoolID = s.PoolID
	msg.OrgPoolAllocationID = s.OrgPoolAllocationID
	msg.PoolReplyMailboxID = s.ReplyMailboxID
	msg.PoolReplyMailboxEmail = s.PoolReplyMailboxEmail
	msg.PrivateReplyTo = s.PrivateReplyTo
	msg.PoolOrganizationID = s.PoolOrganizationID
	msg.PoolSenderSMTPUUID = s.PoolSenderSMTPUUID
	msg.PoolSenderUserID = s.PoolSenderUserID
	msg.PoolSenderFrom = s.PoolSenderFrom
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
	for {

		// The campaign was auto-paused due to errors.
		if p.withErrors.Load() {
			var changed bool
			err := p.m.WithCampaignStatusChange(func() error {
				if !p.withErrors.Load() {
					return nil
				}
				if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
					return err
				}
				var err error
				changed, err = p.m.updateRunningCampaignStatus(p.camp.ID, models.CampaignStatusPaused)
				return err
			})
			if !p.withErrors.Load() {
				continue
			}
			if err != nil {
				p.m.log.Printf("error updating campaign (%s) status to %s: %v", p.camp.Name, models.CampaignStatusPaused, err)
			} else if changed {
				p.m.log.Printf("set campaign (%s) to %s", p.camp.Name, models.CampaignStatusPaused)
				p.m.auditCampaign("campaign.paused", p.camp, map[string]any{"reason": "send_errors"})
				_ = p.m.sendNotif(p.camp, models.CampaignStatusPaused, "Too many errors")
			}
			return
		}

		// The campaign was manually stopped (pause, cancel).
		if p.stopped.Load() {
			switch p.stopReason.Load() {
			case stopReasonPause:
				if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
					p.m.log.Printf("error resetting queued recipients (%s): %v", p.camp.Name, err)
				}
				p.m.auditCampaign("campaign.paused", p.camp, map[string]any{"reason": "manual"})
			case stopReasonPersonalSMTP:
				// Persist the stop atomically with recipient reset and scheduling
				// timestamp cleanup when the database store supports it. This closes
				// the race in which a scanner could observe a still-running row after
				// the account SMTP pool was disabled. Lightweight test stores retain
				// the historical two-call fallback below.
				var interrupted bool
				err := p.m.WithCampaignStatusChange(func() error {
					if p.stopReason.Load() != stopReasonPersonalSMTP {
						interrupted = true
						return nil
					}
					if strict, ok := p.m.store.(PersonalSMTPUnavailableStore); ok {
						return strict.MarkCampaignSMTPUnavailable(p.camp.ID, models.CampaignStatusRunning)
					}
					if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusPending); err != nil {
						return err
					}
					_, err := p.m.updateRunningCampaignStatus(p.camp.ID, models.CampaignStatusPaused)
					return err
				})
				if interrupted {
					continue
				}
				if err != nil {
					p.m.log.Printf("error pausing campaign (%s) after personal SMTP failure: %v", p.camp.Name, err)
				}
				// Do not overwrite a concurrent manual pause/cancel. Fetch the final
				// state only for logging/notification after the atomic transition.
				if current, err := p.m.store.GetCampaign(p.camp.ID); err != nil {
					p.m.log.Printf("error fetching campaign (%s) after personal SMTP failure: %v", p.camp.Name, err)
				} else if current.Status == models.CampaignStatusPaused {
					p.m.log.Printf("paused campaign (%s): personal SMTP unavailable", p.camp.Name)
					p.m.auditCampaign("campaign.paused", current, map[string]any{"reason": "personal_smtp_unavailable"})
					_ = p.m.sendNotif(current, models.CampaignStatusPaused, "Personal SMTP unavailable")
				}
			case stopReasonDeferred:
				if err := p.m.store.ResetCampaignQueuedRecipients(p.camp.ID, models.CampaignRecipientStatusDeferred); err != nil {
					p.m.log.Printf("error deferring queued recipients (%s): %v", p.camp.Name, err)
				}
				p.m.auditCampaign("campaign.deferred", p.camp, map[string]any{"reason": "daily_limit"})
			case stopReasonCancelled:
				if err := p.m.store.UpdateCampaignRecipientStatuses(p.camp.ID, models.CampaignRecipientStatusCancelled, []string{
					models.CampaignRecipientStatusPending,
					models.CampaignRecipientStatusDeferred,
					models.CampaignRecipientStatusQueued,
				}); err != nil {
					p.m.log.Printf("error cancelling queued recipients (%s): %v", p.camp.Name, err)
				}
				p.m.auditCampaign("campaign.cancelled", p.camp, map[string]any{"reason": "manual"})
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
			p.m.auditCampaign("campaign.deferred", p.camp, map[string]any{"reason": "daily_limit"})
			return
		}

		// Campaign wasn't manually stopped and customers were naturally exhausted.
		// Fetch the up-to-date campaign status from the DB.
		var c *models.Campaign
		var interrupted, changed bool
		err := p.m.WithCampaignStatusChange(func() error {
			var err error
			c, err = p.m.store.GetCampaign(p.camp.ID)
			if err != nil {
				return err
			}
			// A pause/resume may have happened since the first stopped check.
			// The old pipe must clean up its queue and let the scanner reload it.
			if p.stopped.Load() {
				interrupted = true
				return nil
			}
			if c.Status == models.CampaignStatusRunning {
				changed, err = p.m.updateRunningCampaignStatus(p.camp.ID, models.CampaignStatusFinished)
			}
			return err
		})
		if err != nil {
			p.m.log.Printf("error fetching campaign (%s) for ending: %v", p.camp.Name, err)
			return
		}
		if interrupted {
			continue
		}

		// If a running campaign has exhausted customers, it's finished.
		if changed {
			c.Status = models.CampaignStatusFinished
			p.m.log.Printf("campaign (%s) finished", p.camp.Name)
			p.m.auditCampaign("campaign.finished", c, map[string]any{"status": models.CampaignStatusFinished})
			_ = p.m.sendNotif(c, c.Status, "")
		} else {
			p.m.log.Printf("finish processing campaign (%s)", p.camp.Name)
		}

		return
	}
}
