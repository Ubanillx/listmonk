package manager

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
)

func templateTestManager(t *testing.T) *Manager {
	t.Helper()

	raw, err := os.ReadFile("../../i18n/en.json")
	if err != nil {
		t.Fatal(err)
	}
	lang, err := i18n.New(raw)
	if err != nil {
		t.Fatal(err)
	}

	return New(Config{
		Concurrency: 1,
		RootURL:     "https://listmonk.example.invalid",
		SiteName:    "Example site",
		LogoURL:     "https://listmonk.example.invalid/logo.png",
		ArchiveURL:  "https://listmonk.example.invalid/archive",
	}, nil, lang, log.New(io.Discard, "", 0))
}

// TestSharedFooterTemplatesCompile guards the bundled templates that reuse the
// campaign footer: the transaction and archive samples render {{ RootURL }},
// {{ SiteName }} and {{ LogoURL }}, so those must be part of the generic func
// map that transactional templates compile with, not only the campaign map.
func TestSharedFooterTemplatesCompile(t *testing.T) {
	m := templateTestManager(t)

	for _, tc := range []struct {
		file     string
		isTx     bool
		rendered []string
	}{
		{"sample-tx.tpl", true, []string{"https://listmonk.example.invalid", "Example site"}},
		{"default-archive.tpl", false, []string{"https://listmonk.example.invalid", "Example site"}},
		{"default.tpl", false, nil},
	} {
		raw, err := os.ReadFile("../../static/email-templates/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}

		if tc.isTx {
			tpl := models.Template{Body: string(raw), Subject: "Subject"}
			if err := tpl.Compile(m.GenericTemplateFuncs()); err != nil {
				t.Fatalf("%s: compile with the generic func map: %v", tc.file, err)
			}
			tx := models.TxMessage{Subject: tpl.Subject}
			if err := tx.Render(models.Customer{Email: "customer@example.invalid"}, &tpl, m.GenericTemplateFuncs()); err != nil {
				t.Fatalf("%s: render: %v", tc.file, err)
			}
			for _, want := range tc.rendered {
				if !strings.Contains(string(tx.Body), want) {
					t.Fatalf("%s: rendered body is missing %q", tc.file, want)
				}
			}
			continue
		}

		c := &models.Campaign{TemplateBody: string(raw), ContentType: models.CampaignContentTypeHTML, Subject: "Subject"}
		if err := c.CompileTemplate(m.TemplateFuncs(nil)); err != nil {
			t.Fatalf("%s: compile with the campaign func map: %v", tc.file, err)
		}
	}
}

// TestGenericTemplateFuncsCoverSharedBranding pins the individual functions so a
// future refactor cannot silently move them back into the campaign-only map.
func TestGenericTemplateFuncsCoverSharedBranding(t *testing.T) {
	m := templateTestManager(t)

	funcs := m.GenericTemplateFuncs()
	for name, want := range map[string]string{
		"RootURL":  "https://listmonk.example.invalid",
		"SiteName": "Example site",
		"LogoURL":  "https://listmonk.example.invalid/logo.png",
	} {
		fn, ok := funcs[name].(func() string)
		if !ok {
			t.Fatalf("%s is not registered as func() string in the generic func map", name)
		}
		if got := fn(); got != want {
			t.Fatalf("%s() = %q, want %q", name, got, want)
		}
	}
}
