package manager

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

type pauseDuringCleanupStore struct {
	lifecycleAuditStore
	onGet func()
	reset string
}

func (s *pauseDuringCleanupStore) GetCampaign(int) (*models.Campaign, error) {
	s.onGet()
	return s.campaign, nil
}

func (s *pauseDuringCleanupStore) ResetCampaignQueuedRecipients(_ int, status string) error {
	s.reset = status
	return nil
}

func TestPauseAndImmediateResumeWhilePipeIsFinishing(t *testing.T) {
	s := &pauseDuringCleanupStore{lifecycleAuditStore: lifecycleAuditStore{campaign: &models.Campaign{
		Base: models.Base{ID: 27}, Status: models.CampaignStatusRunning, UnsentCount: 2,
	}}}
	m := newTestManager()
	defer m.Close()
	m.store = s
	m.fnNotify = func(string, any) error { return nil }
	p := &pipe{camp: s.campaign, m: m, done: make(chan struct{})}
	m.pipes[p.camp.ID] = p
	s.onGet = func() {
		// The previous empty batch was caused by pause. The caller has already
		// resumed by the time the old pipe reads the current campaign snapshot.
		m.StopCampaign(p.camp.ID, models.CampaignStatusPaused)
	}
	p.cleanup()
	if len(s.updated) != 0 || s.campaign.Status != models.CampaignStatusRunning || s.reset != models.CampaignRecipientStatusPending {
		t.Fatalf("old pipe finished a resumed campaign: updates=%v status=%s reset=%s", s.updated, s.campaign.Status, s.reset)
	}
}

func TestManualPauseSupersedesAutomaticStop(t *testing.T) {
	for _, reason := range []int32{stopReasonPersonalSMTP, stopReasonPause} {
		m := newTestManager()
		p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 28}}, m: m, done: make(chan struct{})}
		m.pipes[p.camp.ID] = p
		p.Stop(reason, true)
		m.StopCampaign(p.camp.ID, models.CampaignStatusPaused)
		if !p.stopped.Load() || p.withErrors.Load() || p.stopReason.Load() != stopReasonPause {
			t.Fatalf("manual pause retained automatic stop: reason=%d errors=%v", p.stopReason.Load(), p.withErrors.Load())
		}
		m.Close()
	}
}

type stoppedDeferralStore struct{ deferCaptureStore }

func (*stoppedDeferralStore) DeferCampaign(int, time.Time) error { return ErrCampaignNotRunning }

func TestStaleDeferralStopsOldPipeWithoutScheduling(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.store = &stoppedDeferralStore{}
	p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 29}}, m: m, done: make(chan struct{})}
	p.DeferImmediately()
	if !p.stopped.Load() || p.deferred.Load() || p.stopReason.Load() != stopReasonPause {
		t.Fatalf("manual stop became scheduled: stopped=%v deferred=%v reason=%d", p.stopped.Load(), p.deferred.Load(), p.stopReason.Load())
	}
}
