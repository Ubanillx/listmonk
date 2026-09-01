package manager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

type linkCaptureStore struct {
	Store
	urls []string
}

func (s *linkCaptureStore) CreateLink(_ string, url string) (string, error) {
	s.urls = append(s.urls, url)
	return fmt.Sprintf("link-%d", len(s.urls)), nil
}

func TestNewCampaignMessageUsesOwnerAccountAttribs(t *testing.T) {
	m := newTestManager()
	c := &models.Campaign{
		UUID:             "campaign",
		Subject:          "subject",
		Body:             `{{ .Subscriber.Attribs.whatsapp }}`,
		ContentType:      models.CampaignContentTypeHTML,
		OwnerUserAttribs: models.JSON{"whatsapp": "+8613800000000"},
	}
	if err := c.CompileTemplate(m.GenericTemplateFuncs()); err != nil {
		t.Fatalf("compile campaign template: %v", err)
	}

	msg, err := m.NewCampaignMessage(c, models.Subscriber{
		Email:   "recipient@example.com",
		Name:    "Recipient",
		UUID:    "subscriber",
		Attribs: models.JSON{"whatsapp": "subscriber-value"},
	})
	if err != nil {
		t.Fatalf("new campaign message: %v", err)
	}
	if got := string(msg.Body()); !strings.Contains(got, "8613800000000") {
		t.Fatalf("message body = %q, account field was not injected", got)
	} else if strings.Contains(got, "subscriber-value") {
		t.Fatalf("message body = %q, subscriber field overrode account field", got)
	}
}

func TestNewCampaignMessageAutoTracksRenderedTemplateAndCustomFieldLinks(t *testing.T) {
	store := &linkCaptureStore{}
	m := newTestManager()
	m.store = store
	m.cfg.IndividualTracking = true
	m.cfg.LinkTrackURL = "https://listmonk.test/link/%s/%s/%s"
	c := &models.Campaign{
		UUID:           "campaign",
		Subject:        "subject",
		ContentType:    models.CampaignContentTypeHTML,
		AutoTrackLinks: true,
		TemplateBody:   `<section><a href="https://template.example/path">Template</a>{{ template "content" . }}</section>`,
		Body: `<a href="{{ .Subscriber.Attribs.customURL }}">Custom field</a>
<a href="https://body.example/path">Body</a>
<a href="https://explicit.example/path@TrackLink">Explicit</a>`,
	}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatalf("compile campaign template: %v", err)
	}

	msg, err := m.NewCampaignMessage(c, models.Subscriber{
		Email:   "recipient@example.com",
		UUID:    "subscriber",
		Attribs: models.JSON{"customURL": "https://custom.example/path"},
	})
	if err != nil {
		t.Fatalf("new campaign message: %v", err)
	}

	wantURLs := []string{
		"https://body.example/path",
		"https://explicit.example/path",
		"https://template.example/path",
		"https://custom.example/path",
	}
	if len(store.urls) != len(wantURLs) {
		t.Fatalf("registered links = %v, want %v", store.urls, wantURLs)
	}
	for _, want := range wantURLs {
		if !containsString(store.urls, want) {
			t.Fatalf("registered links = %v, missing %q", store.urls, want)
		}
	}

	body := string(msg.Body())
	for _, original := range wantURLs {
		if strings.Contains(body, original) {
			t.Fatalf("rendered message retained untracked URL %q: %q", original, body)
		}
	}
	for i := 1; i <= len(wantURLs); i++ {
		tracked := fmt.Sprintf("https://listmonk.test/link/link-%d/campaign/subscriber", i)
		if !strings.Contains(body, tracked) {
			t.Fatalf("rendered message missing tracked URL %q: %q", tracked, body)
		}
	}
}

func TestNewCampaignMessageDoesNotAutoTrackWhenDisabled(t *testing.T) {
	store := &linkCaptureStore{}
	m := newTestManager()
	m.store = store
	m.cfg.IndividualTracking = true
	m.cfg.LinkTrackURL = "https://listmonk.test/link/%s/%s/%s"
	c := &models.Campaign{
		UUID:         "campaign",
		Subject:      "subject",
		ContentType:  models.CampaignContentTypeHTML,
		TemplateBody: `<section><a href="https://template.example/path">Template</a>{{ template "content" . }}</section>`,
		Body:         `<a href="{{ .Subscriber.Attribs.customURL }}">Custom field</a>`,
	}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatalf("compile campaign template: %v", err)
	}

	msg, err := m.NewCampaignMessage(c, models.Subscriber{
		Email:   "recipient@example.com",
		UUID:    "subscriber",
		Attribs: models.JSON{"customURL": "https://custom.example/path"},
	})
	if err != nil {
		t.Fatalf("new campaign message: %v", err)
	}
	if len(store.urls) != 0 {
		t.Fatalf("automatic links were registered while disabled: %v", store.urls)
	}
	for _, original := range []string{"https://template.example/path", "https://custom.example/path"} {
		if !strings.Contains(string(msg.Body()), original) {
			t.Fatalf("rendered message did not retain original URL %q: %q", original, msg.Body())
		}
	}
}

func TestNewCampaignMessageAutoTracksTemplateLinksWithAggregateTracking(t *testing.T) {
	store := &linkCaptureStore{}
	m := newTestManager()
	m.store = store
	m.cfg.LinkTrackURL = "https://listmonk.test/link/%s/%s/%s"
	c := &models.Campaign{
		UUID:           "campaign",
		Subject:        "subject",
		ContentType:    models.CampaignContentTypeHTML,
		AutoTrackLinks: true,
		TemplateBody:   `<a href="https://template.example/path">Template</a>{{ template "content" . }}`,
	}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatalf("compile campaign template: %v", err)
	}

	msg, err := m.NewCampaignMessage(c, models.Subscriber{
		Email: "recipient@example.com",
		UUID:  "subscriber",
	})
	if err != nil {
		t.Fatalf("new campaign message: %v", err)
	}

	if got, want := store.urls, []string{"https://template.example/path"}; !equalStrings(got, want) {
		t.Fatalf("registered links = %v, want %v", got, want)
	}
	wantURL := fmt.Sprintf("https://listmonk.test/link/link-1/campaign/%s", dummyUUID)
	if !strings.Contains(string(msg.Body()), wantURL) {
		t.Fatalf("rendered message missing aggregate tracking URL %q: %q", wantURL, msg.Body())
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
