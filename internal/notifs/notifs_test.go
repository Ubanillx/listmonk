package notifs

import (
	"errors"
	"html/template"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/knadh/listmonk/models"
)

type fakePusher func(models.Message) error

func (f fakePusher) Push(m models.Message) error { return f(m) }

func withTestNotifs(t *testing.T, system []string, push func(models.Message) error) {
	t.Helper()
	oldTpls, oldNo := Tpls, no
	t.Cleanup(func() {
		Tpls, no = oldTpls, oldNo
	})
	Tpls = template.Must(template.New("").Parse(`{{ define "test" }}<p>notification</p>{{ end }}`))
	no = &Notifs{
		em:  fakePusher(push),
		lo:  log.New(io.Discard, "", 0),
		opt: Opt{FromEmail: "noreply@example.test", SystemEmails: system, ContentType: "text/html"},
	}
}

func TestUnlistedEmailsDeduplicatesAndTrims(t *testing.T) {
	got := unlistedEmails(
		[]string{" Admin@Example.test ", "ops@example.test"},
		[]string{"admin@example.test", " Owner@Example.test", "owner@example.test", "  "},
	)
	if len(got) != 1 || got[0] != "Owner@Example.test" {
		t.Fatalf("unlisted emails = %#v", got)
	}
}

func TestNotifySystemAndSendsSeparateMessages(t *testing.T) {
	var sent [][]string
	withTestNotifs(t, []string{"admin@example.test", "second-admin@example.test"}, func(m models.Message) error {
		sent = append(sent, append([]string{}, m.To...))
		return nil
	})

	err := NotifySystemAnd(
		[]string{"owner@example.test", " Admin@Example.test ", "second-owner@example.test"},
		"subject", "test", map[string]any{"Name": "launch"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(sent) != 3 {
		t.Fatalf("messages = %d, want 3 (%v)", len(sent), sent)
	}
	if got := sent[0]; len(got) != 2 || got[0] != "admin@example.test" || got[1] != "second-admin@example.test" {
		t.Fatalf("system recipients = %v", got)
	}
	if got := sent[1]; len(got) != 1 || got[0] != "owner@example.test" {
		t.Fatalf("first owner recipients = %v", got)
	}
	if got := sent[2]; len(got) != 1 || got[0] != "second-owner@example.test" {
		t.Fatalf("second owner recipients = %v", got)
	}
}

func TestNotifySystemAndSkipsDuplicateOwner(t *testing.T) {
	var sent [][]string
	withTestNotifs(t, []string{"admin@example.test"}, func(m models.Message) error {
		sent = append(sent, append([]string{}, m.To...))
		return nil
	})

	if err := NotifySystemAnd([]string{" ADMIN@example.test "}, "subject", "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || len(sent[0]) != 1 || sent[0][0] != "admin@example.test" {
		t.Fatalf("messages = %v, want only the system message", sent)
	}
}

func TestNotifySystemAndPartialFailureIsNotAnError(t *testing.T) {
	attempts := 0
	withTestNotifs(t, []string{"admin@example.test"}, func(m models.Message) error {
		attempts++
		if m.To[0] == "owner@example.test" {
			return errors.New("owner mailbox unavailable")
		}
		return nil
	})

	if err := NotifySystemAnd([]string{"owner@example.test"}, "subject", "test", nil, nil); err != nil {
		t.Fatalf("partial failure returned %v", err)
	}
	if attempts != 2 {
		t.Fatalf("delivery attempts = %d, want 2", attempts)
	}
}

func TestNotifySystemAndReturnsErrorWhenNothingWasSent(t *testing.T) {
	withTestNotifs(t, []string{"admin@example.test"}, func(models.Message) error {
		return errors.New("smtp down")
	})

	if err := NotifySystemAnd([]string{"owner@example.test"}, "subject", "test", nil, nil); err == nil {
		t.Fatal("zero successful deliveries did not return an error")
	}
}

func TestNotifySystemAndWithoutRecipientsIsANoop(t *testing.T) {
	calls := 0
	withTestNotifs(t, nil, func(models.Message) error {
		calls++
		return nil
	})

	if err := NotifySystemAnd(nil, "subject", "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("deliveries = %d, want 0", calls)
	}
}

func TestUninitializedNotifsReturnsError(t *testing.T) {
	old := no
	no = nil
	t.Cleanup(func() { no = old })

	if err := NotifySystem("subject", "tpl", nil, nil); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("NotifySystem error = %v", err)
	}
	if err := NotifySystemAnd([]string{"owner@example.test"}, "subject", "tpl", nil, nil); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("NotifySystemAnd error = %v", err)
	}
}

func TestInitializeWithNilEmailerReportsNotInitialized(t *testing.T) {
	oldTpls, oldNo := Tpls, no
	Tpls, no = nil, nil
	t.Cleanup(func() { Tpls, no = oldTpls, oldNo })

	Initialize(
		Opt{SystemEmails: []string{"admin@example.test"}},
		template.Must(template.New("").Parse(`{{ define "test" }}x{{ end }}`)),
		nil,
		log.New(io.Discard, "", 0),
	)
	if err := NotifySystem("subject", "test", nil, nil); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("NotifySystem with nil emailer = %v, want ErrNotInitialized", err)
	}
}

func TestMaskEmailsRedactsLocalPart(t *testing.T) {
	got := maskEmails([]string{"owner@example.test", " @example.test", "no-at-sign", ""})
	want := []string{"***@example.test", "***", "***", "***"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("maskEmails = %#v, want %#v", got, want)
		}
	}
}

func TestRedactEmailsFromSMTPError(t *testing.T) {
	got := redactEmails(
		"550 5.1.1 <owner@example.test> recipient rejected; also admin@example.test",
		[]string{"owner@example.test", "admin@example.test"},
	)
	if got != "550 5.1.1 <***@example.test> recipient rejected; also ***@example.test" {
		t.Fatalf("redacted error = %q", got)
	}
}

type testTemplateLang struct{}

func (testTemplateLang) T(string) string             { return "" }
func (testTemplateLang) Ts(string, ...string) string { return "" }
func (testTemplateLang) Tn(string, int) string       { return "" }

func TestNotificationTemplateNamesResolve(t *testing.T) {
	files, err := filepath.Glob("../../static/email-templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("template files: %v %v", files, err)
	}
	tpl, err := template.New("").Funcs(template.FuncMap{
		"L":       func() testTemplateLang { return testTemplateLang{} },
		"RootURL": func() string { return "" },
		"SiteName": func() string {
			return ""
		},
		"LogoURL": func() string { return "" },
	}).ParseFiles(files...)
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	for _, name := range []string{
		TplImport, TplCampaignStatus, TplSendErrorAlert, TplCustomerOptin, TplCustomerData, TplForgotPassword,
	} {
		if tpl.Lookup(name) == nil {
			t.Fatalf("notification template %q is not defined", name)
		}
	}
}
