package manager

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

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
