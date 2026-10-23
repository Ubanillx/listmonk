package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

func TestCampaignBodyMediaReconcilesUnderOwnerBoundary(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	access := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true, PlatformAdmin: true}, UserID: 1}
	var ownID, foreignID, campaignID int
	for _, fixture := range []struct {
		owner  int
		result *int
	}{{1, &ownID}, {2, &foreignID}} {
		if err := a.db.Get(fixture.result, `INSERT INTO media(uuid,filename,content_type,thumb,owner_user_id)
			VALUES(gen_random_uuid(),'signature.png','image/png','',$1) RETURNING id`, fixture.owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,messenger,status,owner_user_id)
		VALUES(gen_random_uuid(),'Image validation','Subject','sender@example.test','Old body','html','email','paused',1) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	current, err := a.core.GetWorkspaceCampaign(access, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	current.Body = fmt.Sprintf(`<img src="/api/media/file/%d/signature.png">`, ownID)
	updated, err := a.core.UpdateCampaignInWorkspace(access, campaignID, current, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var associated []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(updated.Media, &associated); err != nil {
		t.Fatal(err)
	}
	if len(associated) != 1 || associated[0].ID != ownID {
		t.Fatalf("body image was not atomically associated: %+v", updated.Media)
	}
	for _, body := range []string{
		fmt.Sprintf(`<img src="/api/media/file/%d/signature.png">`, foreignID),
		fmt.Sprintf(`<img src="/api/media/file/%d/wrong.png">`, ownID),
		`<img src="/api/media/file/999999/missing.png">`,
	} {
		bad := current
		bad.Body = body
		if _, err := a.core.UpdateCampaignInWorkspace(access, campaignID, bad, nil, nil, ""); err == nil {
			t.Fatalf("admin inspection rights allowed foreign/stale image: %s", body)
		}
	}
	var saved string
	if err := a.db.Get(&saved, `SELECT body FROM campaigns WHERE id=$1`, campaignID); err != nil || saved != current.Body {
		t.Fatalf("invalid image mutation did not roll back: %s %v", saved, err)
	}
	// A historical valid body with a missing edge is repaired on start, without
	// resetting its existing sent count or adding a recipient.
	if _, err := a.db.Exec(`DELETE FROM campaign_media WHERE campaign_id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.UpdateCampaignStatusInWorkspace(access, campaignID, models.CampaignStatusRunning); err != nil {
		t.Fatal(err)
	}
	var ids pq.Int64Array
	if err := a.db.Get(&ids, `SELECT array_agg(media_id) FROM campaign_media WHERE campaign_id=$1`, campaignID); err != nil || len(ids) != 1 || int(ids[0]) != ownID {
		t.Fatalf("start did not reconcile saved body: %v %v", ids, err)
	}
	// An invalid historical body cannot start and cannot leave partial
	// associations behind when validation rolls the transition back.
	if _, err := a.db.Exec(`UPDATE campaigns SET status='paused',body='<img src="/api/media/file/999999/missing.png">' WHERE id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.UpdateCampaignStatusInWorkspace(access, campaignID, models.CampaignStatusRunning); err == nil {
		t.Fatal("start accepted deleted body media")
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, campaignID); err != nil || status != models.CampaignStatusPaused {
		t.Fatalf("failed media validation changed paused status: %s %v", status, err)
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET body=$2 WHERE id=$1`, campaignID, current.Body); err != nil {
		t.Fatal(err)
	}
	// Template authors get the same protection when the editor omits media IDs.
	scope := models.ResourceScope{OwnerUserID: null.Int{Int: 1, Valid: true}, Visibility: models.ResourceVisibilityPrivate}
	tpl, err := a.core.CreateTemplateInWorkspace(access, "Image template", "campaign", "", []byte(current.Body), null.String{}, nil, scope, nil)
	if err == nil {
		err = json.Unmarshal(tpl.Media, &associated)
	}
	if err != nil || len(associated) != 1 || associated[0].ID != ownID {
		t.Fatalf("template body association missing: %+v %v", tpl.Media, err)
	}
	// A wrapper template can acquire a missing dependency after a campaign
	// was saved. Starting must validate that wrapper, too.
	if _, err := a.db.Exec(`UPDATE templates SET body='<html>{{ template "content" . }}<img src="/api/media/file/999999/missing.png"></html>' WHERE id=$1`, tpl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET template_id=$1 WHERE id=$2`, tpl.ID, campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.UpdateCampaignStatusInWorkspace(access, campaignID, models.CampaignStatusRunning); err == nil {
		t.Fatal("start ignored deleted image in the template wrapper")
	}
}
