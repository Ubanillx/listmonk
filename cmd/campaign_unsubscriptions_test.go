package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

type unsubscribeFixture struct {
	a              *App
	id, listID     int
	uuid, listUUID string
	customers      []models.Customer
}

func newUnsubscribeFixture(t *testing.T) unsubscribeFixture {
	t.Helper()
	a := newOrgPoolAllocationTestApp(t)
	a.cfg = &Config{}
	seedOrgPoolAllocationFixtures(t, a.db)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 100, PageParam: "page", PerPageParam: "per_page"})
	f := unsubscribeFixture{a: a}
	if err := a.db.QueryRow(`INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
		VALUES(gen_random_uuid(),'Opt-out audience','public',3,101) RETURNING id,uuid`).Scan(&f.listID, &f.listUUID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,messenger,status,owner_user_id,organization_id,to_send,sent)
		VALUES(gen_random_uuid(),'Opt-out campaign','Subject','sender@example.test','Body','plain','email','running',3,101,2,2) RETURNING id,uuid`).Scan(&f.id, &f.uuid); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id) VALUES($1,$2)`, f.id, f.listID); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		var customer models.Customer
		customer.Email = fmt.Sprintf("optout%d@example.test", i)
		if err := a.db.QueryRow(`INSERT INTO customers(uuid,email,name,customer_code,owner_user_id,organization_id)
			VALUES(gen_random_uuid(),$1,$2,$3,3,101) RETURNING id,uuid`, customer.Email, fmt.Sprintf("Recipient %d", i), fmt.Sprintf("OPT-%d", i)).Scan(&customer.ID, &customer.UUID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($1,$2,'confirmed')`, customer.ID, f.listID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,sent_at) VALUES($1,$2,'sent',$3,NOW())`, f.id, customer.ID, customer.Email); err != nil {
			t.Fatal(err)
		}
		f.customers = append(f.customers, customer)
	}
	return f
}

type unsubscribeRenderer struct{}

func (unsubscribeRenderer) Render(w io.Writer, name string, _ interface{}, _ echo.Context) error {
	_, err := io.WriteString(w, name)
	return err
}

func (f unsubscribeFixture) publicRequest(method, campaignUUID, recipientUUID string, form url.Values) *httptest.ResponseRecorder {
	e := echo.New()
	e.Renderer = unsubscribeRenderer{}
	req := httptest.NewRequest(method, "/subscription/"+campaignUUID+"/"+recipientUUID, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/subscription/:campUUID/:subUUID")
	c.SetParamNames("campUUID", "subUUID")
	c.SetParamValues(campaignUUID, recipientUUID)
	var err error
	if method == http.MethodGet {
		err = f.a.SubscriptionPage(c)
	} else {
		err = f.a.SubscriptionPrefs(c)
	}
	if err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

func (f unsubscribeFixture) count(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.a.db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscriptions WHERE campaign_id=$1`, f.id); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCampaignUnsubscribesConfirmAndDeduplicate(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if rec := f.publicRequest(http.MethodGet, f.uuid, f.customers[0].UUID, nil); rec.Code != 200 || f.count(t) != 0 {
		t.Fatalf("GET counted an opt-out: %d", rec.Code)
	}
	if rec := f.publicRequest(http.MethodPost, "00000000-0000-0000-0000-000000000001", f.customers[0].UUID, nil); rec.Code != 404 || f.count(t) != 0 {
		t.Fatalf("unrelated campaign accepted: %d", rec.Code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- f.publicRequest(http.MethodPost, f.uuid, f.customers[0].UUID, url.Values{"List-Unsubscribe": {"One-Click"}}).Code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("unsubscribe failed: %d", code)
		}
	}
	if f.count(t) != 1 {
		t.Fatalf("duplicate opt-outs counted %d times", f.count(t))
	}
	var status string
	if err := f.a.db.Get(&status, `SELECT status FROM customer_list_memberships WHERE customer_id=$1 AND customer_list_id=$2`, f.customers[0].ID, f.listID); err != nil || status != "unsubscribed" {
		t.Fatalf("membership=%s err=%v", status, err)
	}
	campaign, err := f.a.core.GetCampaign(f.id, "", "")
	if err != nil || campaign.Unsubscribes != 1 {
		t.Fatalf("campaign counter=%+v err=%v", campaign.CampaignMeta, err)
	}
	running, err := f.a.core.GetRunningCampaignStats()
	if err != nil || len(running) != 1 || running[0].Unsubscribes != 1 {
		t.Fatalf("running stats=%+v err=%v", running, err)
	}
	// A bulk/admin opt-out is not evidence of a click on this campaign.
	if err := f.a.core.UnsubscribeLists([]int{f.customers[1].ID}, []int{f.listID}, nil); err != nil {
		t.Fatal(err)
	}
	if f.count(t) != 1 {
		t.Fatal("administrative unsubscribe changed campaign click attribution")
	}
	// A different recipient counts separately, even after a prior admin opt-out.
	if rec := f.publicRequest(http.MethodPost, f.uuid, f.customers[1].UUID, nil); rec.Code != 200 || f.count(t) != 2 {
		t.Fatalf("second recipient not counted: status=%d count=%d", rec.Code, f.count(t))
	}
	if _, err := f.a.db.Exec(`UPDATE campaigns SET status='paused' WHERE id=$1`, f.id); err != nil {
		t.Fatal(err)
	}
	u := sendErrorReportUser(auth.PermListGetAll, auth.PermCampaignsGetAll)
	for _, handler := range []echo.HandlerFunc{f.a.GetCampaign, f.a.GetCampaigns} {
		c, rec := unsubscribeAPIContext(u, f.id, "")
		if err := handler(c); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.Body.String(), `"unsubscribes":2`) {
			t.Fatalf("list/detail lost paused count: %s", rec.Body.String())
		}
	}
}

func TestCampaignUnsubscribeIsAtomicAndPreferenceAware(t *testing.T) {
	f := newUnsubscribeFixture(t)
	f.a.cfg.Privacy.AllowPreferences = true
	// Saving an unchanged profile/list selection does not count.
	form := url.Values{"manage": {"true"}, "name": {"Changed name"}, "l": {f.listUUID}}
	if rec := f.publicRequest(http.MethodPost, f.uuid, f.customers[0].UUID, form); rec.Code != 200 || f.count(t) != 0 {
		t.Fatalf("profile edit counted: status=%d", rec.Code)
	}
	form.Del("l")
	if rec := f.publicRequest(http.MethodPost, f.uuid, f.customers[0].UUID, form); rec.Code != 200 || f.count(t) != 1 {
		t.Fatalf("list opt-out not recorded: status=%d count=%d", rec.Code, f.count(t))
	}
	if _, err := f.a.db.Exec(`ALTER TABLE campaign_unsubscriptions ADD CONSTRAINT reject_optout_test CHECK(false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if rec := f.publicRequest(http.MethodPost, f.uuid, f.customers[1].UUID, nil); rec.Code != 500 {
		t.Fatalf("counter write failure returned %d", rec.Code)
	}
	var status string
	if err := f.a.db.Get(&status, `SELECT status FROM customer_list_memberships WHERE customer_id=$1 AND customer_list_id=$2`, f.customers[1].ID, f.listID); err != nil || status != "confirmed" {
		t.Fatalf("failed event write committed unsubscribe: status=%s err=%v", status, err)
	}
}

func unsubscribeAPIContext(user auth.User, id int, query string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/campaigns/%d/unsubscribes%s", id, query), nil)
	req.Header.Set(workspaceHeader, "101")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetPath("/api/campaigns/:id/unsubscribes")
	c.Set("id", id)
	c.Set(auth.UserHTTPCtxKey, user)
	return c, rec
}

func TestCampaignUnsubscribeReportPermissionsPrivacyAndRetention(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if rec := f.publicRequest(http.MethodPost, f.uuid, f.customers[0].UUID, nil); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	u := sendErrorReportUser(auth.PermListGetAll)
	c, rec := unsubscribeAPIContext(u, f.id, "?per_page=1")
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), f.customers[0].Email) || strings.Contains(rec.Body.String(), f.customers[0].UUID) {
		t.Fatal("recipient email or bearer UUID leaked")
	}
	var report struct {
		Data struct {
			Results []models.CampaignUnsubscription `json:"results"`
			Total   int                             `json:"total"`
			PerPage int                             `json:"per_page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil || report.Data.Total != 1 || report.Data.PerPage != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	c, rec = unsubscribeAPIContext(u, f.id, "?search="+url.QueryEscape(f.customers[0].Email))
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil || report.Data.Total != 0 {
		t.Fatal("masked email was searchable")
	}
	u.PermissionsMap[auth.PermCustomersSensitiveRead] = struct{}{}
	c, rec = unsubscribeAPIContext(u, f.id, "?search="+url.QueryEscape(f.customers[0].Email))
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), f.customers[0].Email) {
		t.Fatal("authorized email missing")
	}
	denied := sendErrorReportUser(auth.PermListGetAll)
	delete(denied.PermissionsMap, auth.PermCampaignsRecipients)
	c, _ = unsubscribeAPIContext(denied, f.id, "")
	if err := f.a.GetCampaignUnsubscriptions(c); err == nil {
		t.Fatal("missing recipient permission was accepted")
	}
	c, _ = unsubscribeAPIContext(u, f.id, "")
	c.Request().Header.Set(workspaceHeader, "102")
	if err := f.a.GetCampaignUnsubscriptions(c); err == nil {
		t.Fatal("cross-workspace detail access was accepted")
	}
	c, _ = unsubscribeAPIContext(u, f.id, "")
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal,
		WorkspaceOrganizationID: null.Int{Int: 101, Valid: true}, Scopes: pq.StringArray{apiKeyScopeCampaignsRecipients}})
	if err := f.a.GetCampaignUnsubscriptions(c); err == nil {
		t.Fatal("API key without customer source scope was accepted")
	}
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal,
		WorkspaceOrganizationID: null.Int{Int: 101, Valid: true}, Scopes: pq.StringArray{apiKeyScopeCampaignsRecipients, apiKeyScopeCustomersRead}})
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatalf("authorized API key failed: %v", err)
	}
	// Moving a customer hides identifying details but preserves campaign totals.
	if _, err := f.a.db.Exec(`UPDATE customers SET owner_user_id=4 WHERE id=$1`, f.customers[0].ID); err != nil {
		t.Fatal(err)
	}
	c, rec = unsubscribeAPIContext(u, f.id, "")
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil || report.Data.Total != 0 {
		t.Fatal("moved customer history leaked")
	}
	if _, err := f.a.db.Exec(`DELETE FROM customers WHERE id=$1`, f.customers[0].ID); err != nil {
		t.Fatal(err)
	}
	campaign, err := f.a.core.GetCampaign(f.id, "", "")
	if err != nil || campaign.Unsubscribes != 1 {
		t.Fatalf("customer deletion erased opt-out history: %+v %v", campaign.CampaignMeta, err)
	}
}

func TestCampaignPoolUnsubscribeDeduplicatesAndPreservesDelivery(t *testing.T) {
	for _, initialStatus := range []string{"sent", "queued"} {
		t.Run(initialStatus, func(t *testing.T) {
			f := newUnsubscribeFixture(t)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.a.db.Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			id := func(query string, args ...any) int64 {
				t.Helper()
				var value int64
				if err := f.a.db.Get(&value, query, args...); err != nil {
					t.Fatal(err)
				}
				return value
			}
			pool := seedOrgPoolAllocationPool(t, f.a.db, "Opt-out pool")
			allocationList := id(`INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id) VALUES(gen_random_uuid(),'Opt-out allocation','org_pool_allocation',3,101) RETURNING id`)
			allocation := id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,101) RETURNING id`, allocationList, pool)
			var contactID int64
			var contactUUID string
			if err := f.a.db.QueryRow(`INSERT INTO pool_contacts(email,name) VALUES('pool-optout@example.test','Pool opt-out') RETURNING id,uuid`).Scan(&contactID, &contactUUID); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, contactID)
			exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, allocation, contactID)
			otherList := id(`INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id) VALUES(gen_random_uuid(),'Other allocation','org_pool_allocation',4,102) RETURNING id`)
			otherAllocation := id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,102) RETURNING id`, otherList, pool)
			exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, otherAllocation, contactID)
			exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,status,email_snapshot,sent_at)
				VALUES($1,$2,$3,$4,101,$5::campaign_recipient_status,'pool-optout@example.test',CASE WHEN $5='sent' THEN NOW() ELSE NULL END)`, f.id, contactID, pool, allocation, initialStatus)
			exec(`ALTER TABLE campaign_unsubscriptions ADD CONSTRAINT reject_pool_optout_test CHECK(false) NOT VALID`)
			if rec := f.publicRequest(http.MethodPost, f.uuid, contactUUID, nil); rec.Code != 500 || f.count(t) != 0 {
				t.Fatalf("pool failed event write returned %d, count=%d", rec.Code, f.count(t))
			}
			var exclusions int
			if err := f.a.db.Get(&exclusions, `SELECT COUNT(*) FROM org_pool_allocation_exclusions WHERE contact_id=$1`, contactID); err != nil || exclusions != 0 {
				t.Fatalf("failed event write committed pool exclusion: %d %v", exclusions, err)
			}
			exec(`ALTER TABLE campaign_unsubscriptions DROP CONSTRAINT reject_pool_optout_test`)
			for range 2 {
				if rec := f.publicRequest(http.MethodPost, f.uuid, contactUUID, nil); rec.Code != 200 {
					t.Fatalf("pool unsubscribe status=%d", rec.Code)
				}
			}
			if f.count(t) != 1 {
				t.Fatal("pool repeat click was not deduplicated")
			}
			var status string
			if err := f.a.db.Get(&status, `SELECT status FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, f.id, contactID); err != nil {
				t.Fatal(err)
			}
			wantStatus := "cancelled"
			if initialStatus == "sent" {
				wantStatus = "sent"
			}
			if status != wantStatus {
				t.Fatalf("unsubscribe rewrote delivery history: %s, want %s", status, wantStatus)
			}
			var sentAt bool
			if err := f.a.db.Get(&sentAt, `SELECT sent_at IS NOT NULL FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, f.id, contactID); err != nil || sentAt != (initialStatus == "sent") {
				t.Fatalf("sent timestamp changed: %v %v", sentAt, err)
			}
			var valid bool
			if err := f.a.db.Get(&valid, `SELECT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions WHERE pool_id=$1 AND contact_id=$2 AND organization_id=101 AND restored_at IS NULL)
				AND NOT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions WHERE pool_id=$1 AND contact_id=$2 AND organization_id=102)
				AND (SELECT status='active' FROM pool_contacts WHERE id=$2)`, pool, contactID); err != nil || !valid {
				t.Fatalf("pool unsubscribe crossed its source organization: %v %v", valid, err)
			}
			u := sendErrorReportUser(auth.PermListGetAll)
			c, rec := unsubscribeAPIContext(u, f.id, "")
			if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rec.Body.String(), "pool-optout@example.test") || strings.Contains(rec.Body.String(), contactUUID) {
				t.Fatal("public-pool identity leaked")
			}
			if !strings.Contains(rec.Body.String(), `"recipient_type":"pool"`) {
				t.Fatal("pool opt-out missing from detail API")
			}
		})
	}
}
