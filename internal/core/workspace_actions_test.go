package core

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

func TestCanReuseCampaignTemplate(t *testing.T) {
	scope := func(orgID, ownerID int, visibility string) models.ResourceScope {
		out := models.ResourceScope{OwnerUserID: null.IntFrom(ownerID), Visibility: visibility}
		if orgID > 0 {
			out.OrganizationID = null.IntFrom(orgID)
		}
		return out
	}
	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 10}
	member := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 7}, UserID: 10}
	manager := member
	manager.Role = models.OrganizationMemberRoleManager
	admin := member
	admin.PlatformAdmin = true
	pending := scope(7, 10, models.ResourceVisibilityPrivate)
	pending.TransferPendingAt.Valid = true
	archived := scope(7, 10, models.ResourceVisibilityPrivate)
	archived.OrganizationArchived = true

	for _, tc := range []struct {
		name             string
		source, template models.ResourceScope
		target           models.WorkspaceAccess
		want             bool
	}{
		{"personal own template", scope(0, 10, "private"), scope(0, 10, "private"), personal, true},
		{"different personal owner", scope(0, 20, "global"), scope(0, 20, "global"), personal, false},
		{"organization own template", scope(7, 10, "private"), scope(7, 10, "private"), member, true},
		{"organization shared template", scope(7, 10, "private"), scope(7, 20, "organization"), member, true},
		{"global template from another workspace", scope(7, 10, "private"), scope(9, 20, "global"), member, true},
		{"manager copying private template", scope(7, 20, "private"), scope(7, 20, "private"), manager, false},
		{"manager copying shared template", scope(7, 20, "private"), scope(7, 20, "organization"), manager, true},
		{"admin copying private template", scope(7, 20, "private"), scope(7, 20, "private"), admin, false},
		{"other organization", scope(9, 10, "private"), scope(9, 10, "global"), member, false},
		{"organization to personal", scope(7, 10, "private"), scope(7, 10, "global"), personal, false},
		{"personal to organization", scope(0, 10, "private"), scope(0, 10, "global"), member, false},
		{"pending template", scope(7, 10, "private"), pending, member, false},
		{"archived template", scope(7, 10, "private"), archived, member, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&Core{}).canReuseCampaignTemplate(tc.source, tc.template, tc.target); got != tc.want {
				t.Fatalf("canReuseCampaignTemplate() = %v, want %v", got, tc.want)
			}
		})
	}
}

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
