package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/migrations"
)

func recordUnsubscribeTestClick(t *testing.T, f unsubscribeFixture, destination, recipientUUID string) {
	t.Helper()
	var uuid string
	if err := f.a.db.QueryRow(`INSERT INTO links(uuid,url) VALUES(gen_random_uuid(),$1)
		ON CONFLICT(url) DO UPDATE SET url=EXCLUDED.url RETURNING uuid`, destination).Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO campaign_links(campaign_id,link_id) SELECT $1,id FROM links WHERE uuid=$2 ON CONFLICT DO NOTHING`, f.id, uuid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.core.RegisterCampaignLinkClick(uuid, f.uuid, recipientUUID); err != nil {
		t.Fatal(err)
	}
}

func TestCampaignUnsubscribeClickAndConfirmationAlign(t *testing.T) {
	f := newUnsubscribeFixture(t)
	const root = "https://mail.example.test"
	if _, err := f.a.db.Exec(`UPDATE settings SET value=to_jsonb($1::TEXT) WHERE key='app.root_url'`, root); err != nil {
		t.Fatal(err)
	}
	var linkUUID string
	destination := fmt.Sprintf("%s/subscription/%s/%s", root, f.uuid, f.customers[0].UUID)
	if err := f.a.db.QueryRow(`INSERT INTO links(uuid,url) VALUES(gen_random_uuid(),$1) RETURNING uuid`, destination).Scan(&linkUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO campaign_links(campaign_id,link_id) SELECT $1,id FROM links WHERE uuid=$2`, f.id, linkUUID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.a.core.RegisterCampaignLinkClick(linkUUID, f.uuid, f.customers[0].UUID); err != nil {
			t.Fatal(err)
		}
	}
	u := sendErrorReportUser(auth.PermListGetAll, auth.PermCampaignsGetAll)
	c, rec := unsubscribeAPIContext(u, f.id, "")
	if err := f.a.GetCampaign(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"unsubscribe_clicks":1`) || !strings.Contains(rec.Body.String(), `"unsubscribes":0`) {
		t.Fatalf("unsubscribe link clicks are absent from campaign statistics: %s", rec.Body.String())
	}
	for _, status := range []string{"all", "clicked", "confirmed"} {
		c, rec = unsubscribeAPIContext(u, f.id, "?status="+status)
		if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
			t.Fatal(err)
		}
		want := `"total":1`
		if status == "confirmed" {
			want = `"total":0`
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("status %s: %s", status, rec.Body.String())
		}
	}
	if result := f.publicRequest(http.MethodPost, f.uuid, f.customers[0].UUID, nil); result.Code != 200 {
		t.Fatalf("confirmation status=%d", result.Code)
	}
	c, rec = unsubscribeAPIContext(u, f.id, "?status=all")
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"total":1`, `"status":"confirmed"`, `"click_count":2`, `"clicked_at":`, `"confirmed_at":`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("activity report lost %s: %s", want, rec.Body.String())
		}
	}
	rows, err := f.a.core.GetRunningCampaignStats()
	if err != nil || len(rows) != 1 || rows[0].UnsubscribeClicks != 1 || rows[0].Unsubscribes != 1 {
		t.Fatalf("live stats mismatch: %+v %v", rows, err)
	}
	c, _ = unsubscribeAPIContext(u, f.id, "?status=invalid")
	if err := f.a.GetCampaignUnsubscriptions(c); err == nil {
		t.Fatal("invalid status accepted")
	}
}

func TestCampaignUnsubscribeClicksRejectMismatchedDestinations(t *testing.T) {
	f := newUnsubscribeFixture(t)
	const root = "https://mail.example.test"
	if _, err := f.a.db.Exec(`UPDATE settings SET value=to_jsonb($1::TEXT) WHERE key='app.root_url'`, root); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{
		fmt.Sprintf("https://other.example.test/subscription/%s/%s", f.uuid, f.customers[0].UUID),
		fmt.Sprintf("%s/subscription/%s/%s", root, f.uuid, f.customers[1].UUID),
		fmt.Sprintf("%s/subscription/00000000-0000-0000-0000-000000000001/%s", root, f.customers[0].UUID),
		root + "/ordinary?next=/subscription/",
	} {
		recordUnsubscribeTestClick(t, f, destination, f.customers[0].UUID)
	}
	var count int
	if err := f.a.db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscription_activity WHERE campaign_id=$1`, f.id); err != nil || count != 0 {
		t.Fatalf("false attribution: %d %v", count, err)
	}
	var alias string
	if err := f.a.db.QueryRow(`INSERT INTO customer_uuid_aliases(uuid,customer_id) VALUES(gen_random_uuid(),$1) RETURNING uuid`, f.customers[0].ID).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	recordUnsubscribeTestClick(t, f, fmt.Sprintf("%s/subscription/%s/%s?manage=true#preferences", root, f.uuid, alias), alias)
	if err := f.a.db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscription_activity WHERE campaign_id=$1 AND status='clicked'`, f.id); err != nil || count != 1 {
		t.Fatalf("valid legacy alias lost: %d %v", count, err)
	}
	// Click-only rows use the same sensitive-field and source permissions as confirmations.
	u := sendErrorReportUser(auth.PermListGetAll)
	c, rec := unsubscribeAPIContext(u, f.id, "?status=all&search="+f.customers[0].Email)
	if err := f.a.GetCampaignUnsubscriptions(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Fatal("masked email searchable through click report")
	}
}

func TestCampaignUnsubscribeActivityUpgradeAndAuditBackfill(t *testing.T) {
	f := newUnsubscribeFixture(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.a.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Membership state by itself never proves campaign attribution.
	exec(`UPDATE customer_list_memberships SET status='unsubscribed',updated_at=NOW() WHERE customer_list_id=$1`, f.listID)
	proof := `INSERT INTO audit_events(actor_type,action,object_type,object_id,occurred_at,result,metadata)
		VALUES('customer','subscription.unsubscribed','customer',$1,NOW(),$3,
		jsonb_build_object('campaign_id',$2::TEXT,'channel','public_self_service','http_method','POST','http_status',200,'route','/subscription/:campUUID/:subUUID'))`
	exec(proof, f.customers[0].UUID, f.uuid, "success")
	exec(proof, f.customers[1].UUID, f.uuid, "failed")
	pool := seedOrgPoolAllocationPool(t, f.a.db, "Historical unsubscribe pool")
	var poolID int64
	var poolUUID string
	if err := f.a.db.QueryRow(`INSERT INTO pool_contacts(email,name) VALUES('historical-pool@example.test','Historical pool') RETURNING id,uuid`).Scan(&poolID, &poolUUID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, poolID)
	exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,organization_id,status,email_snapshot) VALUES($1,$2,$3,101,'sent','historical-pool@example.test')`, f.id, poolID, pool)
	var root string
	if err := f.a.db.Get(&root, `SELECT value#>>'{}' FROM settings WHERE key='app.root_url'`); err != nil {
		t.Fatal(err)
	}
	recordUnsubscribeTestClick(t, f, fmt.Sprintf("%s/subscription/%s/%s", root, f.uuid, poolUUID), poolUUID)
	exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,source) VALUES($1,101,$2,'unsubscribe')`, pool, poolID)
	exec(proof, poolUUID, f.uuid, "success")
	var before string
	if err := f.a.db.Get(&before, `SELECT pg_get_viewdef('campaign_unsubscription_activity'::regclass,TRUE)`); err != nil {
		t.Fatal(err)
	}
	exec(`DROP VIEW campaign_unsubscription_activity`)
	for range 2 {
		if err := migrations.V6_68_0(f.a.db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	if err := f.a.db.Get(&after, `SELECT pg_get_viewdef('campaign_unsubscription_activity'::regclass,TRUE)`); err != nil || after != before {
		t.Fatalf("fresh/upgrade view drift: %v", err)
	}
	if f.count(t) != 2 {
		t.Fatalf("historical confirmations=%d, want 2 (private + pool)", f.count(t))
	}
	var poolAligned bool
	if err := f.a.db.Get(&poolAligned, `SELECT clicked_at IS NOT NULL AND confirmed_at IS NOT NULL AND click_count=1 AND status='confirmed'
		AND recipient_organization_id=101 FROM campaign_unsubscription_activity WHERE campaign_id=$1 AND recipient_type='pool' AND recipient_id=$2`, f.id, poolID); err != nil || !poolAligned {
		t.Fatalf("pool click and historical confirmation failed to align: %v %v", poolAligned, err)
	}
	var confirmedAt time.Time
	if err := f.a.db.Get(&confirmedAt, `SELECT created_at FROM campaign_unsubscriptions WHERE campaign_id=$1 AND recipient_type='pool'`, f.id); err != nil {
		t.Fatal(err)
	}
	// Rerunning repair preserves the original proof time and delivery history.
	if err := migrations.V6_68_0(f.a.db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var stable bool
	if err := f.a.db.Get(&stable, `SELECT u.created_at=$2 AND pr.status='sent' FROM campaign_unsubscriptions u
		JOIN campaign_pool_recipients pr ON pr.campaign_id=u.campaign_id AND pr.pool_contact_id=u.recipient_id
		WHERE u.campaign_id=$1 AND u.recipient_type='pool'`, f.id, confirmedAt); err != nil || !stable {
		t.Fatalf("repair changed historical evidence: %v %v", stable, err)
	}
}
