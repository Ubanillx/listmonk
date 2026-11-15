package manager

import (
	"io"
	"log"
	"testing"
	"time"

	apptz "github.com/knadh/listmonk/internal/tz"
	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

type notificationCapture struct {
	extra   []string
	subject string
	data    map[string]any
}

func newNotificationManager(ownerEmail func(int) (string, error)) (*Manager, *[]notificationCapture) {
	m := New(Config{OwnerEmail: ownerEmail}, nil, nil, log.New(io.Discard, "", 0))
	got := make([]notificationCapture, 0, 1)
	m.fnNotify = func(extra []string, subject string, data any) error {
		got = append(got, notificationCapture{extra: extra, subject: subject, data: data.(map[string]any)})
		return nil
	}
	return m, &got
}

func TestSendNotifIncludesOwnerAndExtraFields(t *testing.T) {
	m, got := newNotificationManager(func(id int) (string, error) {
		if id != 5 {
			t.Fatalf("owner id = %d", id)
		}
		return "owner@example.test", nil
	})
	c := &models.Campaign{
		Base:          models.Base{ID: 7},
		Name:          "launch",
		Status:        models.CampaignStatusDeferred,
		ResourceScope: models.ResourceScope{OwnerUserID: null.IntFrom(5)},
		CampaignMeta:  models.CampaignMeta{Sent: 10, ToSend: 20},
	}
	if err := m.sendNotif(c, models.CampaignStatusDeferred, "Daily limit reached",
		map[string]any{"ResumeAt": "2026-11-14 08:00"}); err != nil {
		t.Fatal(err)
	}

	if len(*got) != 1 {
		t.Fatalf("notifications = %d", len(*got))
	}
	n := (*got)[0]
	if len(n.extra) != 1 || n.extra[0] != "owner@example.test" {
		t.Fatalf("owner recipients = %v", n.extra)
	}
	if n.subject != "Deferred: launch" {
		t.Fatalf("subject = %q", n.subject)
	}
	if n.data["ResumeAt"] != "2026-11-14 08:00" || n.data["Reason"] != "Daily limit reached" {
		t.Fatalf("data = %#v", n.data)
	}
	if n.data["Sent"] != 10 || n.data["ToSend"] != 20 {
		t.Fatalf("counters = %#v", n.data)
	}
}

func TestSendNotifWithoutOwnerResolver(t *testing.T) {
	m, got := newNotificationManager(nil)
	c := &models.Campaign{Base: models.Base{ID: 8}, Name: "no-owner", ResourceScope: models.ResourceScope{OwnerUserID: null.IntFrom(5)}}
	if err := m.sendNotif(c, models.CampaignStatusFinished, "", nil); err != nil {
		t.Fatal(err)
	}
	if len((*got)[0].extra) != 0 {
		t.Fatalf("owner recipients = %v", (*got)[0].extra)
	}
}

type notifDeferralStore struct {
	Store
	camp *models.Campaign
}

func (s *notifDeferralStore) GetCampaign(int) (*models.Campaign, error) {
	return s.camp, nil
}

func TestNotifyDeferredUsesPersistedResumeTime(t *testing.T) {
	resumeAt := time.Date(2026, 11, 14, 8, 30, 0, 0, apptz.Location())
	m, got := newNotificationManager(nil)
	m.store = &notifDeferralStore{camp: &models.Campaign{
		Base:          models.Base{ID: 9},
		Name:          "daily",
		Status:        models.CampaignStatusDeferred,
		ResourceScope: models.ResourceScope{OwnerUserID: null.IntFrom(5)},
		NextResumeAt:  null.TimeFrom(resumeAt),
	}}
	p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 9}, Name: "daily"}, m: m}
	p.notifyDeferred()

	if len(*got) != 1 {
		t.Fatalf("notifications = %d", len(*got))
	}
	n := (*got)[0]
	if n.data["Status"] != models.CampaignStatusDeferred {
		t.Fatalf("status = %v", n.data["Status"])
	}
	if n.data["ResumeAt"] != resumeAt.Format("2006-01-02 15:04") {
		t.Fatalf("resume at = %v", n.data["ResumeAt"])
	}
	if n.data["Reason"] != "Daily limit reached" {
		t.Fatalf("reason = %v", n.data["Reason"])
	}
}

func TestNotifyDeferredSkipsWhenStatusWasChanged(t *testing.T) {
	m, got := newNotificationManager(nil)
	m.store = &notifDeferralStore{camp: &models.Campaign{
		Base:   models.Base{ID: 10},
		Name:   "superseded",
		Status: models.CampaignStatusPaused,
	}}
	p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 10}, Name: "superseded"}, m: m}
	p.notifyDeferred()

	if len(*got) != 0 {
		t.Fatalf("notifications = %d, want none for a campaign that is no longer deferred", len(*got))
	}
}

func TestPipeCleanupNotifiesDeferralWhenErrorsHitAfterDeferral(t *testing.T) {
	resumeAt := time.Date(2026, 11, 14, 8, 30, 0, 0, apptz.Location())
	store := &lifecycleAuditStore{campaign: &models.Campaign{
		Base:         models.Base{ID: 11},
		Name:         "quota-then-errors",
		Status:       models.CampaignStatusDeferred,
		NextResumeAt: null.TimeFrom(resumeAt),
	}}
	m, got := newNotificationManager(func(int) (string, error) { return "owner@example.test", nil })
	m.store = store
	var auditAction string
	m.cfg.AuditCampaign = func(action string, _ *models.Campaign, _ map[string]any) {
		auditAction = action
	}
	p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 11}, Name: "quota-then-errors"}, m: m}
	p.deferred.Store(true)
	p.withErrors.Store(true)

	p.cleanup()

	if len(*got) != 1 {
		t.Fatalf("notifications = %d, want 1 (%v)", len(*got), *got)
	}
	n := (*got)[0]
	if n.data["Status"] != models.CampaignStatusDeferred {
		t.Fatalf("status = %v, want deferred", n.data["Status"])
	}
	if n.data["ResumeAt"] != resumeAt.Format("2006-01-02 15:04") {
		t.Fatalf("resume at = %v", n.data["ResumeAt"])
	}
	if auditAction != "campaign.deferred" {
		t.Fatalf("audit action = %q, want campaign.deferred", auditAction)
	}
}

func TestPipeCleanupNotifiesDeferralWhenSMTPFailsAfterDeferral(t *testing.T) {
	for _, tc := range []struct {
		reason int32
	}{
		{stopReasonPersonalSMTP},
		{stopReasonPoolSMTP},
	} {
		resumeAt := time.Date(2026, 11, 14, 8, 30, 0, 0, apptz.Location())
		store := &lifecycleAuditStore{campaign: &models.Campaign{
			Base:         models.Base{ID: 13},
			Name:         "quota-then-smtp",
			Status:       models.CampaignStatusDeferred,
			NextResumeAt: null.TimeFrom(resumeAt),
		}}
		m, got := newNotificationManager(nil)
		m.store = store
		p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 13}, Name: "quota-then-smtp"}, m: m}
		p.deferred.Store(true)
		p.Stop(tc.reason, false)

		p.cleanup()

		if len(*got) != 1 {
			t.Fatalf("reason %d notifications = %d, want 1", tc.reason, len(*got))
		}
		if (*got)[0].data["Status"] != models.CampaignStatusDeferred {
			t.Fatalf("reason %d status = %v, want deferred", tc.reason, (*got)[0].data["Status"])
		}
	}
}

func TestPipeCleanupUsesPoolSMTPReason(t *testing.T) {
	for _, tc := range []struct {
		reason int32
		want   string
	}{
		{stopReasonPersonalSMTP, "Personal SMTP unavailable"},
		{stopReasonPoolSMTP, "Organization SMTP pool unavailable"},
	} {
		store := &lifecycleAuditStore{campaign: &models.Campaign{
			Base:   models.Base{ID: 12},
			Name:   "smtp-unavailable",
			Status: models.CampaignStatusRunning,
		}}
		m, got := newNotificationManager(nil)
		m.store = store
		p := &pipe{camp: &models.Campaign{Base: models.Base{ID: 12}, Name: "smtp-unavailable"}, m: m}
		p.Stop(tc.reason, false)

		p.cleanup()

		if len(*got) != 1 {
			t.Fatalf("reason %d notifications = %d", tc.reason, len(*got))
		}
		if got := (*got)[0].data["Reason"]; got != tc.want {
			t.Fatalf("reason %d text = %v, want %q", tc.reason, got, tc.want)
		}
	}
}
