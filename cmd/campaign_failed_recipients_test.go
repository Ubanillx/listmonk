package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestCampaignFailedRecipientsSelectsOnlyCurrentUnsentFailures(t *testing.T) {
	a, _, id, ids := newSendErrorReportFixture(t)
	queries := prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	s := newManagerStore(queries, a.core, nil, a.db)
	var listID int
	if err := a.db.Get(&listID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
		VALUES(gen_random_uuid(),'Retry audience','private',3,101) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id) VALUES($1,$2)`, id, listID); err != nil {
		t.Fatal(err)
	}
	for i, customerID := range ids[:3] {
		if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($1,$2,'confirmed')`, customerID, listID); err != nil {
			t.Fatal(err)
		}
		status := "pending"
		if i == 2 {
			status = "sent"
		}
		if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,name_snapshot,attribs_snapshot)
			VALUES($1,$2,$3::campaign_recipient_status,$4,'Recipient','{}')`, id, customerID, status, fmt.Sprintf("recipient%d@example.test", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET status='paused',messenger='test' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}, UserID: 3}
	f := models.CampaignSendErrorFilters{IncludePrivate: true, IncludePool: true, SensitivePrivate: true, PrivateDiagnostics: true}
	report, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 2 || len(report.Results) != 2 {
		t.Fatalf("current failures: %+v", report)
	}
	f.OrganizationID = 101
	filteredPrivate, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil || filteredPrivate.Total != 0 {
		t.Fatalf("organization filter included private recipients: %+v %v", filteredPrivate, err)
	}
	f.OrganizationID = 0
	for _, row := range report.Results {
		if !row.Retryable || row.RecipientID == int64(ids[2]) {
			t.Fatalf("incorrect retry candidate: %+v", row)
		}
	}
	overview, err := a.core.GetCampaignDeliveryOverview(access, id)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Planned != 3 || overview.Sent != 1 || overview.Failed != 2 || overview.Unsent != 2 {
		t.Fatalf("overview: %+v", overview)
	}
	if _, err := a.core.RetryCampaignFailedRecipients(access, id, f, []models.CampaignRetryRecipient{{Type: "private", ID: int64(ids[0])}, {Type: "private", ID: int64(ids[2])}}); err == nil {
		t.Fatal("sent recipient was accepted for retry")
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, id); err != nil || status != "paused" {
		t.Fatalf("failed selection changed campaign: %s %v", status, err)
	}
	count, err := a.core.RetryCampaignFailedRecipients(access, id, f, []models.CampaignRetryRecipient{{Type: "private", ID: int64(ids[0])}})
	if err != nil || count != 1 {
		t.Fatalf("start selected retry: %d %v", count, err)
	}
	if _, err := a.core.RetryCampaignFailedRecipients(access, id, f, []models.CampaignRetryRecipient{{Type: "private", ID: int64(ids[1])}}); err == nil {
		t.Fatal("concurrent retry activation was accepted")
	}
	customers, err := s.NextCustomers(id, 10)
	if err != nil || len(customers) != 1 || customers[0].ID != ids[0] {
		t.Fatalf("sender claimed wrong recipients: %+v %v", customers, err)
	}
	if err := s.MarkCampaignMessageSent(id, ids[0]); err != nil {
		t.Fatal(err)
	}
	changed, err := s.UpdateRunningCampaignStatus(id, models.CampaignStatusFinished)
	if err != nil || !changed {
		t.Fatalf("settle selected retry: %v %v", changed, err)
	}
	var state struct {
		Status string `db:"status"`
		Retry  bool   `db:"retry_mode"`
	}
	if err := a.db.Get(&state, `SELECT status,retry_mode FROM campaigns WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if state.Status != "paused" || state.Retry {
		t.Fatalf("other unsent recipient was lost: %+v", state)
	}
	if remaining, err := s.NextCustomers(id, 10); err != nil || len(remaining) != 0 {
		t.Fatalf("paused campaign still claimed recipients: %+v %v", remaining, err)
	}
	remaining, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil || remaining.Total != 1 || remaining.Results[0].RecipientID != int64(ids[1]) {
		t.Fatalf("successful retry remained failed: %+v %v", remaining, err)
	}
	if _, err := a.core.UpdateCampaignStatusInWorkspace(access, id, models.CampaignStatusRunning); err != nil {
		t.Fatal(err)
	}
	customers, err = s.NextCustomers(id, 10)
	if err != nil || len(customers) != 1 || customers[0].ID != ids[1] {
		t.Fatalf("normal resume did not restore other recipients: %+v %v", customers, err)
	}
}

func TestCampaignFailedRecipientsHandlerRedactsAndExports(t *testing.T) {
	a, _, id, ids := newSendErrorReportFixture(t)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,name_snapshot)
		VALUES($1,$2,'pending','recipient0@example.test','Recipient')`, id, ids[0]); err != nil {
		t.Fatal(err)
	}
	u := sendErrorReportUser(auth.PermCustomersExport)
	c, rec := sendErrorReportContext(u, id, "", false)
	if err := a.GetCampaignFailedRecipients(c); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data models.CampaignFailedRecipientReport `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.Total != 1 || len(out.Data.Results) != 1 || !out.Data.CanExport ||
		strings.Contains(out.Data.Results[0].Email, "recipient0@example.test") || out.Data.Results[0].Error != "" {
		t.Fatalf("failure identity/diagnostic leaked or export unavailable: %+v", out.Data)
	}
	c, rec = sendErrorReportContext(u, id, "", true)
	if err := a.ExportCampaignFailedRecipients(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "failed-recipients.xlsx") {
		t.Fatal("failed-recipient Excel was not downloaded")
	}
	for _, filter := range []string{"?organization_id=0", "?organization_id=-1", "?organization_id=invalid"} {
		c, _ = sendErrorReportContext(u, id, filter, false)
		err := a.GetCampaignFailedRecipients(c)
		if httpErr, ok := err.(*echo.HTTPError); !ok || httpErr.Code != http.StatusBadRequest {
			t.Fatalf("invalid filter %q accepted: %v", filter, err)
		}
	}
}

func TestCampaignFailedRecipientsPublicPoolSelection(t *testing.T) {
	a, _, id, _ := newSendErrorReportFixture(t)
	queries := prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	s := newManagerStore(queries, a.core, nil, a.db)
	pool := seedOrgPoolAllocationPool(t, a.db, "Retry public pool")
	var listID int
	if err := a.db.Get(&listID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
		VALUES(gen_random_uuid(),'Retry allocation','org_pool_allocation',3,101) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	var allocationID int64
	if err := a.db.Get(&allocationID, `INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id)
		VALUES($1,$2,101,3) RETURNING id`, listID, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_contacts(id,email,name,customer_code) VALUES(41,'pool-secret@example.test','Pool recipient','POOL-41')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_contacts(id,email,name,customer_code) VALUES(42,'pool-other@example.test','Other recipient','POOL-42')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,41)`, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,41)`, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,101)`, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,42)`, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,42)`, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,email_snapshot,name_snapshot)
		VALUES($1,41,$2,$3,101,'pool-secret@example.test','Pool recipient')`, id, pool, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,email_snapshot,name_snapshot)
		VALUES($1,42,$2,$3,101,'pool-other@example.test','Other recipient')`, id, pool, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET status='paused',messenger='test' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}, UserID: 3}
	f := models.CampaignSendErrorFilters{IncludePool: true}
	report, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil || report.Total != 1 || !report.Results[0].Retryable || report.Results[0].RecipientType != "pool" {
		t.Fatalf("public pool candidate: %+v %v", report, err)
	}
	f.OrganizationID = 101
	orgReport, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil || orgReport.Total != 1 {
		t.Fatalf("organization filter excluded matching pool recipient: %+v %v", orgReport, err)
	}
	f.OrganizationID = 102
	otherOrg, err := a.core.GetCampaignFailedRecipients(access, id, f, 0, 20)
	if err != nil || otherOrg.Total != 0 {
		t.Fatalf("organization filter leaked other recipient: %+v %v", otherOrg, err)
	}
	f.OrganizationID = 0
	overview, err := a.core.GetCampaignDeliveryOverview(access, id)
	if err != nil || overview.Source != "pool" || overview.Failed != 1 || len(overview.Organizations) != 1 || overview.Organizations[0].Failed != 1 {
		t.Fatalf("pool overview: %+v %v", overview, err)
	}
	if n, err := a.core.RetryCampaignFailedRecipients(access, id, f, []models.CampaignRetryRecipient{{Type: "pool", ID: 41}}); err != nil || n != 1 {
		t.Fatalf("pool retry activation: %d %v", n, err)
	}
	customers, err := s.NextCustomers(id, 10)
	if err != nil || len(customers) != 1 || customers[0].PoolContactID != 41 {
		t.Fatalf("pool sender claim: %+v %v", customers, err)
	}
}
