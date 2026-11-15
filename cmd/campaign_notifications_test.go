package main

import (
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/models"
)

type manualNotification struct {
	owner   []string
	subject string
	tpl     string
	data    map[string]any
}

func TestNotifyCampaignStatusChange(t *testing.T) {
	for _, tc := range []struct {
		status      string
		wantReason  string
		wantSubject string
	}{
		{models.CampaignStatusPaused, "Paused manually by 42", "Paused: launch"},
		{models.CampaignStatusCancelled, "Cancelled manually by 42", "Cancelled: launch"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			a := &App{log: log.New(io.Discard, "", 0)}
			got := make(chan manualNotification, 1)
			a.notifyCampaign = func(owner []string, subject, tpl string, data any) error {
				got <- manualNotification{owner: owner, subject: subject, tpl: tpl, data: data.(map[string]any)}
				return nil
			}

			a.notifyCampaignStatusChange(models.Campaign{
				Base:         models.Base{ID: 7},
				Name:         "launch",
				Status:       tc.status,
				CampaignMeta: models.CampaignMeta{Sent: 1, ToSend: 2},
			}, auth.User{Base: auth.Base{ID: 42}})

			select {
			case n := <-got:
				if n.tpl != notifs.TplCampaignStatus {
					t.Fatalf("template = %q", n.tpl)
				}
				if n.subject != tc.wantSubject {
					t.Fatalf("subject = %q, want %q", n.subject, tc.wantSubject)
				}
				if n.data["Status"] != tc.status || n.data["Reason"] != tc.wantReason || n.data["Actor"] != "42" {
					t.Fatalf("data = %#v", n.data)
				}
			case <-time.After(time.Second):
				t.Fatal("notification was not sent")
			}
		})
	}
}

func TestNotifyCampaignStatusChangeIgnoresNonStoppedStatus(t *testing.T) {
	a := &App{log: log.New(io.Discard, "", 0)}
	called := make(chan struct{}, 1)
	a.notifyCampaign = func([]string, string, string, any) error {
		called <- struct{}{}
		return nil
	}
	a.notifyCampaignStatusChange(models.Campaign{
		Base:   models.Base{ID: 7},
		Name:   "launch",
		Status: models.CampaignStatusRunning,
	}, auth.User{Base: auth.Base{ID: 42}})
	select {
	case <-called:
		t.Fatal("notification sent for a running campaign")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotifyCampaignStatusChangeWithoutNotifierIsSafe(t *testing.T) {
	a := &App{}
	a.notifyCampaignStatusChange(models.Campaign{
		Base:   models.Base{ID: 7},
		Name:   "launch",
		Status: models.CampaignStatusPaused,
	}, auth.User{Base: auth.Base{ID: 42}})
}

func TestNotifyCampaignStatusChangeUsesUsernameAndLogsErrors(t *testing.T) {
	a := &App{log: log.New(io.Discard, "", 0)}
	got := make(chan manualNotification, 1)
	a.notifyCampaign = func(owner []string, subject, tpl string, data any) error {
		got <- manualNotification{owner: owner, subject: subject, tpl: tpl, data: data.(map[string]any)}
		return errors.New("smtp down")
	}

	a.notifyCampaignStatusChange(models.Campaign{
		Base:   models.Base{ID: 7},
		Name:   "launch",
		Status: models.CampaignStatusPaused,
	}, auth.User{Base: auth.Base{ID: 42}, Username: "operator"})

	select {
	case n := <-got:
		if n.data["Actor"] != "operator" || n.data["Reason"] != "Paused manually by operator" {
			t.Fatalf("data = %#v", n.data)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not sent")
	}
}
