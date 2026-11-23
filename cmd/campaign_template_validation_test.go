package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func newCampaignTemplateTestApp(t *testing.T) *App {
	t.Helper()

	langB, err := os.ReadFile(filepath.Join("..", "i18n", "en.json"))
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	logger := log.New(io.Discard, "", 0)

	return &App{
		i18n: translator,
		log:  logger,
		manager: manager.New(manager.Config{
			RootURL:         "https://example.test",
			UnsubURL:        "https://example.test/unsub/%s/%s",
			ViewTrackURL:    "https://example.test/view/%s/%s",
			LinkTrackURL:    "https://example.test/link/%s/%s",
			OptinURL:        "https://example.test/optin/%s?%s",
			DisableTracking: true,
		}, nil, translator, logger),
	}
}

func TestValidateCampaignRenderCatchesTemplateErrors(t *testing.T) {
	a := newCampaignTemplateTestApp(t)
	wrap := func(body string) models.Campaign {
		return models.Campaign{
			Name:         "Render test",
			Messenger:    "email",
			ContentType:  models.CampaignContentTypeHTML,
			TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
			Body:         body,
		}
	}

	for _, tc := range []struct {
		name     string
		body     string
		wantKey  string
		wantCode int
	}{
		{
			name:     "unknown subscriber context",
			body:     `<p>{{ .Subscriber.Attribs.contact_name }}</p>`,
			wantKey:  "rendering message",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "unparsable body",
			body:     `<p>{{ if .Customer }}</p>`,
			wantKey:  "compiling template",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "customer context renders",
			body: `<p>{{ .Customer.Attribs.city }}</p>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.validateCampaignRender(wrap(tc.body))
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("valid template was rejected: %v", err)
				}
				return
			}
			var httpErr *echo.HTTPError
			if !errors.As(err, &httpErr) || httpErr.Code != tc.wantCode {
				t.Fatalf("error = %v, want HTTP %d", err, tc.wantCode)
			}
			if got := httpErr.Message.(string); !strings.Contains(strings.ToLower(got), tc.wantKey) {
				t.Fatalf("error message %q does not mention %q", got, tc.wantKey)
			}
		})
	}

	// Non-email messengers never compile the campaign template.
	sms := wrap(`<p>{{ .Subscriber.Attribs.contact_name }}</p>`)
	sms.Messenger = "sms"
	if err := a.validateCampaignRender(sms); err != nil {
		t.Fatalf("non-email messenger was render-validated: %v", err)
	}
}

func TestPreviewNewCampaignRendersRequestPayload(t *testing.T) {
	a := newCampaignTemplateTestApp(t)
	post := func(form url.Values) (*httptest.ResponseRecorder, error) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/campaigns/preview", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetPath("/api/campaigns/preview")
		c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID})
		return rec, a.PreviewNewCampaign(c)
	}

	t.Run("html body", func(t *testing.T) {
		rec, err := post(url.Values{
			"content_type": {models.CampaignContentTypeHTML},
			"body":         {`<p>Hello {{ .Customer.Attribs.city }}</p>`},
		})
		if err != nil {
			t.Fatalf("preview failed: %v", err)
		}
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bengaluru") {
			t.Fatalf("unexpected preview response %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("plain body", func(t *testing.T) {
		rec, err := post(url.Values{
			"content_type": {models.CampaignContentTypePlain},
			"body":         {`plain preview`},
		})
		if err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "plain preview") {
			t.Fatalf("unexpected plain preview %d: %v %s", rec.Code, err, rec.Body.String())
		}
	})

	t.Run("broken template", func(t *testing.T) {
		_, err := post(url.Values{
			"content_type": {models.CampaignContentTypeHTML},
			"body":         {`<p>{{ .Subscriber.Attribs.contact_name }}</p>`},
		})
		var httpErr *echo.HTTPError
		if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
			t.Fatalf("broken template preview = %v, want HTTP 400", err)
		}
	})
}
