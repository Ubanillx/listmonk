package main

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

type templateLang struct{}

func (templateLang) T(key string) string               { return key }
func (templateLang) Ts(key string, _ ...string) string { return key }
func (templateLang) Tn(key string, _ int) string       { return key }

func parseEmailTemplates(t *testing.T) *template.Template {
	t.Helper()
	files, err := filepath.Glob("../static/email-templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("template files: %v %v", files, err)
	}
	tpl, err := template.New("").Funcs(template.FuncMap{
		"L":       func() templateLang { return templateLang{} },
		"RootURL": func() string { return "https://example.test" },
		"SiteName": func() string {
			return "Listmonk"
		},
		"LogoURL": func() string { return "" },
	}).ParseFiles(files...)
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	return tpl
}

func executeTemplate(t *testing.T, tpl *template.Template, name string, data map[string]any) string {
	t.Helper()
	var b strings.Builder
	if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("executing %s: %v", name, err)
	}
	return b.String()
}

func TestSendErrorAlertTemplateRenders(t *testing.T) {
	tpl := parseEmailTemplates(t)
	body := executeTemplate(t, tpl, "send-error-alert", map[string]any{
		"ID": 9, "Name": "burst", "Window": "5m0s", "WindowErrors": 60,
		"TotalErrors": 120, "AffectedRecipients": 12, "LastErrorAt": "2026-11-13 10:30",
		"RootURL": "https://example.test", "SiteName": "Listmonk", "LogoURL": "",
	})
	for _, want := range []string{"burst", "60", "120", "12", "2026-11-13 10:30"} {
		if !strings.Contains(body, want) {
			t.Fatalf("alert template missing %q:\n%s", want, body)
		}
	}
}

func TestCampaignStatusTemplateOptionalFields(t *testing.T) {
	tpl := parseEmailTemplates(t)
	base := map[string]any{
		"ID": 7, "Name": "launch", "Status": "paused", "Sent": 1, "ToSend": 2, "Reason": "manual",
		"RootURL": "https://example.test", "SiteName": "Listmonk", "LogoURL": "",
	}
	withExtrasData := map[string]any{"Actor": "ops", "ResumeAt": "2026-11-14 08:00"}
	for k, v := range base {
		withExtrasData[k] = v
	}
	withExtras := executeTemplate(t, tpl, "campaign-status", withExtrasData)
	if !strings.Contains(withExtras, "ops") || !strings.Contains(withExtras, "2026-11-14 08:00") {
		t.Fatalf("optional fields missing:\n%s", withExtras)
	}
	plain := executeTemplate(t, tpl, "campaign-status", base)
	if strings.Contains(plain, "<no value>") {
		t.Fatalf("missing optional fields rendered incorrectly:\n%s", plain)
	}
	withoutReason := map[string]any{}
	for k, v := range base {
		if k != "Reason" {
			withoutReason[k] = v
		}
	}
	noReason := executeTemplate(t, tpl, "campaign-status", withoutReason)
	if strings.Contains(noReason, "<no value>") || strings.Contains(noReason, "email.status.campaignReason") {
		t.Fatalf("missing reason rendered incorrectly:\n%s", noReason)
	}
}

type fakeAlertSource struct {
	rows          []models.CampaignSendErrorWindow
	windowSeconds float64
	threshold     int
	claimCalls    int
	claimOK       bool
	claimErr      error
	released      []time.Time
}

func (f *fakeAlertSource) WindowCounts(windowSeconds float64, threshold int) ([]models.CampaignSendErrorWindow, error) {
	f.windowSeconds = windowSeconds
	f.threshold = threshold
	return f.rows, nil
}

func (f *fakeAlertSource) Claim(int, float64) (time.Time, bool, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return time.Time{}, false, f.claimErr
	}
	return time.Unix(123, 0), f.claimOK, nil
}

func (f *fakeAlertSource) Release(_ int, at time.Time) error {
	f.released = append(f.released, at)
	return nil
}

type alertNotification struct {
	extra   []string
	subject string
	tpl     string
	data    map[string]any
	err     error
}

func newAlertRow() models.CampaignSendErrorWindow {
	return models.CampaignSendErrorWindow{
		CampaignID: 9, Name: "burst", UUID: "uuid-9", OwnerUserID: null.IntFrom(4),
		SendErrors: 120, WindowErrors: 60, AffectedRecipients: 12,
		LastErrorAt: time.Date(2026, 11, 13, 10, 30, 0, 0, time.UTC),
	}
}

func runAlertTickForTest(t *testing.T, src *fakeAlertSource, owner func(int) (string, error),
	notify func([]string, string, string, any) error) {
	t.Helper()
	runSendErrorAlertTick(context.Background(), src, owner, notify, nil,
		log.New(io.Discard, "", 0), sendErrorAlertConfig{
			Window: 5 * time.Minute, Threshold: 50, Cooldown: 30 * time.Minute,
		})
}

func TestSendErrorAlertNotifiesOwnerOnce(t *testing.T) {
	src := &fakeAlertSource{rows: []models.CampaignSendErrorWindow{newAlertRow()}, claimOK: true}
	var got []alertNotification
	notify := func(extra []string, subject, tpl string, data any) error {
		got = append(got, alertNotification{extra: extra, subject: subject, tpl: tpl, data: data.(map[string]any)})
		return nil
	}

	runAlertTickForTest(t, src, func(int) (string, error) { return "owner@example.test", nil }, notify)

	if len(got) != 1 || src.claimCalls != 1 {
		t.Fatalf("notifications=%d claims=%d, want 1/1", len(got), src.claimCalls)
	}
	if src.windowSeconds != 300 || src.threshold != 50 {
		t.Fatalf("window query parameters = (%v, %d), want (300, 50)", src.windowSeconds, src.threshold)
	}
	if len(got[0].extra) != 1 || got[0].extra[0] != "owner@example.test" {
		t.Fatalf("recipients = %v", got[0].extra)
	}
	if got[0].tpl != "send-error-alert" {
		t.Fatalf("template = %q", got[0].tpl)
	}
	if got[0].data["Window"] != "5m" {
		t.Fatalf("window = %v", got[0].data["Window"])
	}
	if got[0].subject != "Send error alert: burst (60 errors in 5m)" {
		t.Fatalf("subject = %q", got[0].subject)
	}
	if got[0].data["WindowErrors"] != 60 || got[0].data["ID"] != 9 {
		t.Fatalf("data = %#v", got[0].data)
	}
	if len(src.released) != 0 {
		t.Fatalf("successful alert released claim: %v", src.released)
	}
}

func TestSendErrorAlertSkipsClaimWithoutRecipients(t *testing.T) {
	src := &fakeAlertSource{rows: []models.CampaignSendErrorWindow{newAlertRow()}, claimOK: true}
	runAlertTickForTest(t, src, func(int) (string, error) { return "", nil },
		func([]string, string, string, any) error { return nil })
	if src.claimCalls != 0 {
		t.Fatalf("claim called %d times without recipients", src.claimCalls)
	}
}

func TestSendErrorAlertRespectsCooldown(t *testing.T) {
	src := &fakeAlertSource{rows: []models.CampaignSendErrorWindow{newAlertRow()}, claimOK: false}
	calls := 0
	runAlertTickForTest(t, src, func(int) (string, error) { return "owner@example.test", nil },
		func([]string, string, string, any) error { calls++; return nil })
	if calls != 0 || src.claimCalls != 1 {
		t.Fatalf("notifications=%d claims=%d, want 0/1", calls, src.claimCalls)
	}
}

func TestSendErrorAlertReleasesClaimOnDeliveryFailure(t *testing.T) {
	src := &fakeAlertSource{rows: []models.CampaignSendErrorWindow{newAlertRow()}, claimOK: true}
	runAlertTickForTest(t, src, func(int) (string, error) { return "owner@example.test", nil },
		func([]string, string, string, any) error { return errors.New("smtp down") })
	if len(src.released) != 1 || !src.released[0].Equal(time.Unix(123, 0)) {
		t.Fatalf("released = %v", src.released)
	}
}

func TestFormatAlertWindow(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{30 * time.Minute, "30m"},
		{90 * time.Second, "1m30s"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{24 * time.Hour, "24h"},
	} {
		if got := formatAlertWindow(tc.in); got != tc.want {
			t.Fatalf("formatAlertWindow(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAlertTickIntervalClamps(t *testing.T) {
	for _, tc := range []struct {
		window time.Duration
		want   time.Duration
	}{
		{time.Minute, 15 * time.Second},
		{5 * time.Minute, time.Minute},
		{24 * time.Hour, time.Minute},
	} {
		if got := alertTickInterval(tc.window); got != tc.want {
			t.Fatalf("alertTickInterval(%s) = %s, want %s", tc.window, got, tc.want)
		}
	}
}

func TestValidateSendErrorAlertSettings(t *testing.T) {
	if err := validateSendErrorAlertSettings("5m", "30m", 50); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		window, cooldown string
		threshold        int
	}{
		{"0s", "30m", 50},
		{"25h", "30m", 50},
		{"bad", "30m", 50},
		{"5m", "0s", 50},
		{"5m", "30m", 0},
		{"5m", "30m", 1000001},
	} {
		if err := validateSendErrorAlertSettings(tc.window, tc.cooldown, tc.threshold); err == nil {
			t.Fatalf("accepted invalid settings %+v", tc)
		}
	}
}

func TestSendErrorAlertConfigValidation(t *testing.T) {
	ko := koanf.New(".")
	ko.Set("app.send_error_alert_window", "5m")
	ko.Set("app.send_error_alert_threshold", 50)
	ko.Set("app.send_error_alert_cooldown", "30m")
	cfg, err := sendErrorAlertConfigFromKo(ko)
	if err != nil || cfg.Threshold != 50 || cfg.Window != 5*time.Minute {
		t.Fatalf("config = %+v, err = %v", cfg, err)
	}

	ko.Set("app.send_error_alert_window", "0s")
	if _, err := sendErrorAlertConfigFromKo(ko); err == nil {
		t.Fatal("invalid window accepted")
	}
	ko.Set("app.send_error_alert_window", "5m")
	ko.Set("app.send_error_alert_threshold", 0)
	if _, err := sendErrorAlertConfigFromKo(ko); err == nil {
		t.Fatal("invalid threshold accepted")
	}
}
