package core

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestCloneCampaignHeadersStripsDeliveryIdentity(t *testing.T) {
	original := models.Headers{
		{
			"From":       "source@example.com",
			"Reply-To":   "reply@example.com",
			"X-Campaign": "summer",
		},
		{
			"sender":      "sender@example.com",
			"RETURN-PATH": "bounce@example.com",
			"X-Trace":     "trace-id",
		},
	}

	cloned := cloneCampaignHeaders(original)
	if len(cloned) != 2 {
		t.Fatalf("cloneCampaignHeaders() returned %d maps, want 2", len(cloned))
	}
	if got := cloned[0]["X-Campaign"]; got != "summer" {
		t.Fatalf("preserved header = %q, want summer", got)
	}
	if got := cloned[1]["X-Trace"]; got != "trace-id" {
		t.Fatalf("preserved header = %q, want trace-id", got)
	}
	for _, header := range cloned {
		for key := range header {
			switch strings.ToLower(key) {
			case "from", "reply-to", "sender", "return-path":
				t.Fatalf("cloned headers retained %q: %#v", key, header)
			}
		}
	}
	if original[0]["From"] != "source@example.com" || original[1]["sender"] != "sender@example.com" {
		t.Fatal("cloneCampaignHeaders() modified the source headers")
	}
}
