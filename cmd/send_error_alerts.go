package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/notifs"
	apptz "github.com/knadh/listmonk/internal/tz"
	"github.com/knadh/listmonk/models"
)

// campaignSendErrorAlertSource supplies the window counts and the durable
// per-campaign cooldown used by the alert monitor.
type campaignSendErrorAlertSource interface {
	WindowCounts(windowSeconds float64, threshold int) ([]models.CampaignSendErrorWindow, error)
	Claim(campaignID int, cooldownSeconds float64) (time.Time, bool, error)
	Release(campaignID int, claimedAt time.Time) error
}

type campaignSendErrorAlertStore struct {
	queries *models.Queries
}

func newCampaignSendErrorAlertStore(q *models.Queries) *campaignSendErrorAlertStore {
	return &campaignSendErrorAlertStore{queries: q}
}

func (s *campaignSendErrorAlertStore) WindowCounts(windowSeconds float64, threshold int) ([]models.CampaignSendErrorWindow, error) {
	var out []models.CampaignSendErrorWindow
	err := s.queries.GetRunningCampaignSendErrorWindow.Select(&out, windowSeconds, threshold)
	return out, err
}

func (s *campaignSendErrorAlertStore) Claim(campaignID int, cooldownSeconds float64) (time.Time, bool, error) {
	var claimedAt time.Time
	err := s.queries.ClaimCampaignSendErrorAlert.Get(&claimedAt, campaignID, cooldownSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return claimedAt, true, nil
}

func (s *campaignSendErrorAlertStore) Release(campaignID int, claimedAt time.Time) error {
	_, err := s.queries.ReleaseCampaignSendErrorAlert.Exec(campaignID, claimedAt)
	return err
}

type sendErrorAlertConfig struct {
	Window    time.Duration
	Threshold int
	Cooldown  time.Duration
}

func sendErrorAlertConfigFromKo(ko *koanf.Koanf) (sendErrorAlertConfig, error) {
	window, err := time.ParseDuration(strings.TrimSpace(ko.String("app.send_error_alert_window")))
	if err != nil {
		return sendErrorAlertConfig{}, fmt.Errorf("invalid send error alert window: %w", err)
	}
	cooldown, err := time.ParseDuration(strings.TrimSpace(ko.String("app.send_error_alert_cooldown")))
	if err != nil {
		return sendErrorAlertConfig{}, fmt.Errorf("invalid send error alert cooldown: %w", err)
	}
	cfg := sendErrorAlertConfig{
		Window:    window,
		Threshold: ko.Int("app.send_error_alert_threshold"),
		Cooldown:  cooldown,
	}
	if err := validateSendErrorAlertSettings(cfg.Window.String(), cfg.Cooldown.String(), cfg.Threshold); err != nil {
		return sendErrorAlertConfig{}, err
	}
	return cfg, nil
}

// alertTickInterval samples frequently enough to catch short bursts without
// querying the error table more than once a minute.
func alertTickInterval(window time.Duration) time.Duration {
	interval := window / 5
	if interval < 15*time.Second {
		interval = 15 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	return interval
}

// formatAlertWindow renders the configured duration the same way it is stored
// (5m instead of Go's 5m0s) so alert e-mails match the settings page.
func formatAlertWindow(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") || strings.HasSuffix(s, "h0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// runSendErrorAlerts periodically checks running campaigns for window error
// spikes and notifies the campaign owner and system notification list. It never
// changes campaign state; the existing max_send_errors threshold remains the
// only automatic pause mechanism.
func runSendErrorAlerts(ctx context.Context, src campaignSendErrorAlertSource,
	ownerEmail func(int) (string, error),
	notify func(extra []string, subject, tplName string, data any) error,
	i *i18n.I18n, lo *log.Logger, cfg sendErrorAlertConfig) {
	ticker := time.NewTicker(alertTickInterval(cfg.Window))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSendErrorAlertTick(ctx, src, ownerEmail, notify, i, lo, cfg)
		}
	}
}

func runSendErrorAlertTick(ctx context.Context, src campaignSendErrorAlertSource,
	ownerEmail func(int) (string, error),
	notify func(extra []string, subject, tplName string, data any) error,
	i *i18n.I18n, lo *log.Logger, cfg sendErrorAlertConfig) {
	if notify == nil {
		return
	}

	rows, err := src.WindowCounts(cfg.Window.Seconds(), cfg.Threshold)
	if err != nil {
		lo.Printf("error querying campaign send-error window: %v", err)
		return
	}
	systemEmails := notifs.SystemEmails()

	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}

		var owner []string
		if ownerEmail != nil && row.OwnerUserID.Valid && row.OwnerUserID.Int > 0 {
			email, err := ownerEmail(int(row.OwnerUserID.Int))
			if err != nil {
				lo.Printf("error resolving owner e-mail for campaign (%s) alert: %v", row.Name, err)
			} else if email != "" {
				owner = []string{email}
			}
		}
		// Without any recipient there is nothing to send; leave the cooldown
		// untouched so a later recipient change can still alert.
		if len(systemEmails) == 0 && len(owner) == 0 {
			continue
		}

		claimedAt, ok, err := src.Claim(row.CampaignID, cfg.Cooldown.Seconds())
		if err != nil {
			lo.Printf("error claiming send-error alert for campaign (%s): %v", row.Name, err)
			continue
		}
		if !ok {
			continue
		}

		windowText := formatAlertWindow(cfg.Window)
		data := map[string]any{
			"ID":                 row.CampaignID,
			"Name":               row.Name,
			"Status":             "alert",
			"Window":             windowText,
			"WindowErrors":       row.WindowErrors,
			"TotalErrors":        row.SendErrors,
			"AffectedRecipients": row.AffectedRecipients,
			"LastErrorAt":        row.LastErrorAt.In(apptz.Location()).Format("2006-01-02 15:04"),
		}
		subject := fmt.Sprintf("Send error alert: %s (%d errors in %s)", row.Name, row.WindowErrors, windowText)
		if i != nil {
			if s := i.Ts("email.alerts.subject", "name", row.Name, "count", strconv.Itoa(row.WindowErrors), "window", windowText); s != "email.alerts.subject" {
				subject = s
			}
		}

		if err := notify(owner, subject, notifs.TplSendErrorAlert, data); err != nil {
			lo.Printf("error sending send-error alert for campaign (%s): %v", row.Name, err)
			if releaseErr := src.Release(row.CampaignID, claimedAt); releaseErr != nil {
				lo.Printf("error releasing send-error alert claim for campaign (%s): %v", row.Name, releaseErr)
			}
			continue
		}
		lo.Printf("sent send-error alert for campaign (%s): %d errors in %s", row.Name, row.WindowErrors, windowText)
	}
}

// campaignOwnerEmail resolves the enabled e-mail address of a campaign owner.
func (a *App) campaignOwnerEmail(userID int) (string, error) {
	user, err := a.core.GetUser(userID, "", "")
	if err != nil {
		return "", err
	}
	if user.Status != auth.UserStatusEnabled || !user.Email.Valid {
		return "", nil
	}
	return strings.TrimSpace(user.Email.String), nil
}
