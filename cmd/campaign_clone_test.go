package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// Use the real schema and clone transaction in a disposable database. Run with
// ADMIN_AUTH_TEST_DSN; no templates or campaigns in the development DB change.
func TestCampaignCloneTemplateReference(t *testing.T) {
	access := func(orgID, userID int, admin bool) models.WorkspaceAccess {
		return models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: orgID, Personal: orgID == 0, PlatformAdmin: admin},
			UserID:    userID,
		}
	}
	personal := access(0, orgPoolAllocationTestMemberUser, false)
	member := access(orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestMemberUser, false)
	manager := access(orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestManagerUser, false)
	manager.Role = models.OrganizationMemberRoleManager
	admin := access(orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestAdminUser, true)
	personalAdmin := access(0, orgPoolAllocationTestAdminUser, true)
	otherOrgAdmin := access(orgPoolAllocationTestOtherOrgID, orgPoolAllocationTestAdminUser, true)

	for _, tc := range []struct {
		name                       string
		source, target             models.WorkspaceAccess
		campaignOwner              int
		templateOrg, templateOwner int
		visibility, contentType    string
		reuse, noTemplate, pending bool
		overlap                    bool
		wantStatus                 int
	}{
		{name: "personal richtext", source: personal, target: personal, campaignOwner: 3, templateOwner: 3, visibility: "private", contentType: "richtext", reuse: true},
		{name: "overlapping campaign and template media", source: personal, target: personal, campaignOwner: 3, templateOwner: 3, visibility: "private", contentType: "html", reuse: true, overlap: true},
		{name: "organization html", source: member, target: member, campaignOwner: 3, templateOrg: 101, templateOwner: 3, visibility: "private", contentType: "html", reuse: true},
		{name: "shared organization template", source: member, target: member, campaignOwner: 3, templateOrg: 101, templateOwner: 2, visibility: "organization", contentType: "html", reuse: true},
		{name: "global template", source: member, target: member, campaignOwner: 3, templateOrg: 102, templateOwner: 2, visibility: "global", contentType: "html", reuse: true},
		{name: "manager private dependency", source: manager, target: manager, campaignOwner: 3, templateOrg: 101, templateOwner: 3, visibility: "private", contentType: "html"},
		{name: "admin private dependency", source: admin, target: admin, campaignOwner: 3, templateOrg: 101, templateOwner: 3, visibility: "private", contentType: "html"},
		{name: "cross organization", source: admin, target: otherOrgAdmin, campaignOwner: 1, templateOrg: 101, templateOwner: 1, visibility: "private", contentType: "html"},
		{name: "personal to organization", source: personalAdmin, target: admin, campaignOwner: 1, templateOwner: 1, visibility: "private", contentType: "html"},
		{name: "organization to personal", source: admin, target: personalAdmin, campaignOwner: 1, templateOrg: 101, templateOwner: 1, visibility: "private", contentType: "html"},
		{name: "visual without template", source: personal, target: personal, campaignOwner: 3, contentType: "visual", noTemplate: true},
		{name: "forged cross organization template", source: member, target: member, campaignOwner: 3, templateOrg: 102, templateOwner: 3, visibility: "private", contentType: "html", wantStatus: http.StatusConflict},
		{name: "pending template", source: member, target: member, campaignOwner: 3, templateOrg: 101, templateOwner: 3, visibility: "private", contentType: "html", pending: true, wantStatus: http.StatusConflict},
		{name: "archived template", source: member, target: member, campaignOwner: 3, templateOrg: 103, templateOwner: 3, visibility: "global", contentType: "html", wantStatus: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newOrgPoolAllocationTestApp(t)
			seedOrgPoolAllocationFixtures(t, a.db)
			orgValue := func(id int) any {
				if id == 0 {
					return nil
				}
				return id
			}
			media := func(name string, orgID, ownerID int) int {
				t.Helper()
				var id int
				if err := a.db.Get(&id, `INSERT INTO media(uuid,filename,content_type,thumb,organization_id,owner_user_id)
					VALUES(gen_random_uuid(),$1,'image/png','',$2,$3) RETURNING id`, name, orgValue(orgID), ownerID); err != nil {
					t.Fatal(err)
				}
				return id
			}
			mediaURL := func(id int, name string) string { return fmt.Sprintf("/api/media/file/%d/%s", id, name) }
			campaignMediaID := media("campaign.png", tc.source.OrganizationID, tc.campaignOwner)
			campaignBody := `<img src="` + mediaURL(campaignMediaID, "campaign.png") + `">`
			var templateID any
			var originalTemplateID, templateMediaID int
			var templateBody string
			templateFilename := "template.png"
			if !tc.noTemplate {
				if tc.overlap {
					templateMediaID, templateFilename = campaignMediaID, "campaign.png"
				} else {
					templateMediaID = media(templateFilename, tc.templateOrg, tc.templateOwner)
				}
				templateBody = `<img src="` + mediaURL(templateMediaID, templateFilename) + `">{{ template "content" . }}`
				if err := a.db.Get(&originalTemplateID, `INSERT INTO templates(name,type,subject,body,body_source,name_fallback,
					organization_id,owner_user_id,visibility,transfer_pending_at)
					VALUES('Clone layout','campaign','',$1,$1,'{"enabled":true,"value":"Team"}'::jsonb,$2,$3,$4,
					CASE WHEN $5 THEN NOW() ELSE NULL END) RETURNING id`, templateBody, orgValue(tc.templateOrg), tc.templateOwner, tc.visibility, tc.pending); err != nil {
					t.Fatal(err)
				}
				if _, err := a.db.Exec(`INSERT INTO template_media(template_id,media_id,filename) VALUES($1,$2,$3)`, originalTemplateID, templateMediaID, templateFilename); err != nil {
					t.Fatal(err)
				}
				templateID = originalTemplateID
			}
			var campaignID int
			if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,body_source,altbody,
				content_type,messenger,organization_id,owner_user_id,template_id,send_errors)
				VALUES(gen_random_uuid(),'Source campaign','Hello','sender@example.test',$1,$1,$1,$2,'email',$3,$4,$5,9) RETURNING id`,
				campaignBody, tc.contentType, orgValue(tc.source.OrganizationID), tc.campaignOwner, templateID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`INSERT INTO campaign_media(campaign_id,media_id,filename) VALUES($1,$2,'campaign.png')`, campaignID, campaignMediaID); err != nil {
				t.Fatal(err)
			}
			beforeTemplates := countRows(t, a.db, `SELECT count(*) FROM templates`)
			beforeMedia := countRows(t, a.db, `SELECT count(*) FROM media`)
			cloned, err := a.core.CloneCampaignForWorkspaceWithSource(campaignID, tc.source, tc.target, "Cloned campaign")
			if tc.wantStatus != 0 {
				var httpErr *echo.HTTPError
				if !errors.As(err, &httpErr) || httpErr.Code != tc.wantStatus {
					t.Fatalf("clone error = %v, want HTTP %d", err, tc.wantStatus)
				}
				if countRows(t, a.db, `SELECT count(*) FROM campaigns`) != 1 || countRows(t, a.db, `SELECT count(*) FROM templates`) != beforeTemplates || countRows(t, a.db, `SELECT count(*) FROM media`) != beforeMedia {
					t.Fatal("rejected clone left partial resources")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cloned.Status != models.CampaignStatusDraft || cloned.SendErrors != 0 || cloned.OwnerUserID.Int != tc.target.UserID {
				t.Fatalf("unexpected cloned draft: %+v", cloned)
			}
			wantNewTemplates, wantNewMedia := 1, 2
			if tc.reuse || tc.noTemplate {
				wantNewTemplates, wantNewMedia = 0, 1
			}
			if got := countRows(t, a.db, `SELECT count(*) FROM templates`) - beforeTemplates; got != wantNewTemplates {
				t.Fatalf("new templates = %d, want %d", got, wantNewTemplates)
			}
			if got := countRows(t, a.db, `SELECT count(*) FROM media`) - beforeMedia; got != wantNewMedia {
				t.Fatalf("new media = %d, want %d (no orphan template media)", got, wantNewMedia)
			}
			var clonedMediaID int
			if err := a.db.Get(&clonedMediaID, `SELECT media_id FROM campaign_media WHERE campaign_id=$1`, cloned.ID); err != nil {
				t.Fatal(err)
			}
			wantBody := strings.ReplaceAll(campaignBody, mediaURL(campaignMediaID, "campaign.png"), mediaURL(clonedMediaID, "campaign.png"))
			if clonedMediaID == campaignMediaID || cloned.Body != wantBody || cloned.BodySource.String != wantBody || cloned.AltBody.String != wantBody {
				t.Fatal("campaign media or body references were not independently copied")
			}
			if tc.noTemplate {
				if cloned.TemplateID.Valid {
					t.Fatal("visual clone unexpectedly gained a template")
				}
				return
			}
			if !cloned.TemplateID.Valid || (cloned.TemplateID.Int == originalTemplateID) != tc.reuse || cloned.TemplateNameFallback.Resolve("") != "Team" {
				t.Fatalf("unexpected template reference or name fallback: %+v", cloned)
			}
			if tc.reuse {
				if cloned.TemplateBody != templateBody {
					t.Fatal("reused template body changed")
				}
				// Cloning the clone must not start accumulating template copies either.
				second, err := a.core.CloneCampaignForWorkspaceWithSource(cloned.ID, tc.target, tc.target, "Second clone")
				if err != nil || second.TemplateID != cloned.TemplateID {
					t.Fatalf("repeated clone changed template: %v", err)
				}
				if countRows(t, a.db, `SELECT count(*) FROM templates`) != beforeTemplates {
					t.Fatal("repeated clone created another template")
				}
			} else {
				var copiedMediaID int
				if err := a.db.Get(&copiedMediaID, `SELECT media_id FROM template_media WHERE template_id=$1`, cloned.TemplateID.Int); err != nil {
					t.Fatal(err)
				}
				wantTemplate := strings.ReplaceAll(templateBody, mediaURL(templateMediaID, templateFilename), mediaURL(copiedMediaID, templateFilename))
				if copiedMediaID == templateMediaID || cloned.TemplateBody != wantTemplate {
					t.Fatal("template snapshot lost independent media references")
				}
				if countRows(t, a.db, `SELECT count(*) FROM templates WHERE id=$1 AND owner_user_id=$2
					AND organization_id IS NOT DISTINCT FROM $3::bigint AND visibility='private' AND is_default=FALSE`,
					cloned.TemplateID.Int, tc.target.UserID, orgValue(tc.target.OrganizationID)) != 1 {
					t.Fatal("template snapshot does not belong to the target owner/workspace")
				}
			}
			var unchangedBody string
			if err := a.db.Get(&unchangedBody, `SELECT body FROM templates WHERE id=$1`, originalTemplateID); err != nil || unchangedBody != templateBody {
				t.Fatalf("original template changed: %v", err)
			}
			if tc.reuse {
				updatedBody := `<p>Updated shared layout</p>{{ template "content" . }}`
				if _, err := a.db.Exec(`UPDATE templates SET body=$2 WHERE id=$1`, originalTemplateID, updatedBody); err != nil {
					t.Fatal(err)
				}
				updated, err := a.core.GetWorkspaceCampaign(tc.target, cloned.ID)
				if err != nil || updated.TemplateBody != updatedBody {
					t.Fatalf("cloned campaign no longer follows its selected template: %v", err)
				}
			}
		})
	}
}
