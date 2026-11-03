package main

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/bestedm"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/knadh/listmonk/internal/providerautomation"
)

func TestProviderAutomationAdminBoundary(t *testing.T) {
	a := &App{}
	for _, scope := range []string{"private", "pool"} {
		c, _ := providerContext(t, "/api/delivery-provider/automation", "scope", scope)
		c.Set(auth.UserHTTPCtxKey, auth.User{PermissionsMap: permissionSet(auth.PermDeliveryProviderGet, auth.PermDeliveryProviderBlocklist, auth.PermCustomersBlocklist, auth.PermPoolsMasterManage)})
		if err := a.GetProviderAutomation(c); err != auth.ErrPermDenied {
			t.Fatalf("reader accessed automation: %v", err)
		}
		if err := a.ChangeProviderAutomation(c); err != auth.ErrPermDenied {
			t.Fatalf("reader changed automation: %v", err)
		}
		if err := a.GetProviderAutomationItems(c); err != auth.ErrPermDenied {
			t.Fatalf("reader accessed details: %v", err)
		}
	}
}

func TestProviderAutomationDatabaseLifecycle(t *testing.T) {
	db := newAdminTestDB(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(migrations.ProviderAutomationSQL); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var customerID, poolID, contactID int64
	if err := db.Get(&customerID, `INSERT INTO customers(uuid,email,name) VALUES(gen_random_uuid(),'same@example.test','Private') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&poolID, `INSERT INTO customer_lists(uuid,name,type) VALUES(gen_random_uuid(),'Pool','pool') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&contactID, `INSERT INTO pool_contacts(email,name) VALUES('same@example.test','Shared') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, poolID, contactID)
	exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($1,$2,'confirmed')`, customerID, poolID)
	exec(`INSERT INTO customers(uuid,email,name,transfer_pending_at) VALUES(gen_random_uuid(),'transfer@example.test','Moving',NOW())`)
	exec(`INSERT INTO pool_contacts(email,status) VALUES('archived@example.test','archived')`)
	clock := time.Date(2026, 11, 3, 4, 0, 0, 0, time.FixedZone("CST", 8*3600))
	fail := false
	service := &providerautomation.Service{DB: db, Now: func() time.Time { return clock }, Fetch: func(_ context.Context, q url.Values) ([]bestedm.Row, error) {
		if q.Get("type") != "-1" || q.Get("date") != "2026-10-28" || q.Get("date_end") != "2026-11-03" || q.Has("task_ident") {
			t.Fatalf("unexpected provider query: %v", q)
		}
		if fail {
			return nil, &bestedm.Error{Code: "invalid_xml", Message: "invalid XML"}
		}
		return []bestedm.Row{{"email": "same@example.test", "error_type": "1"}, {"email": "SAME@example.test", "error_type": "9"}, {"email": "missing@example.test", "error_type": "1"}, {"email": "transfer@example.test", "error_type": "1"}, {"email": "archived@example.test", "error_type": "1"}}, nil
	}}
	private, err := service.Rule(ctx, "private")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := service.Rule(ctx, "pool")
	if err != nil {
		t.Fatal(err)
	}
	if private.Config.Enabled || pool.Config.Enabled {
		t.Fatal("automation enabled by default")
	}
	config := private.Config
	config.Enabled = true
	if _, err = service.Save(ctx, "private", private.Version, 0, config); !errors.Is(err, providerautomation.ErrPreview) {
		t.Fatalf("enabled without preview: %v", err)
	}
	preview, err := service.Process(ctx, "private", private.Version, 0, &config, false)
	if err != nil || preview.Counts.ToBlock != 1 || preview.Counts.Skipped != 1 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	var status string
	db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID)
	if status != "enabled" {
		t.Fatal("preview changed customer")
	}
	private, err = service.Save(ctx, "private", private.Version, preview.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, "private", private.Version-1, preview.ID, config); !errors.Is(err, providerautomation.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err = service.Process(ctx, "private", private.Version, preview.ID, nil, false); !errors.Is(err, providerautomation.ErrPreview) {
		t.Fatalf("old version preview reused: %v", err)
	}
	preview, err = service.Process(ctx, "private", private.Version, 0, &private.Config, false)
	if err != nil {
		t.Fatal(err)
	}
	changed := private.Config
	changed.LookbackDays = 8
	if _, err = service.Save(ctx, "private", private.Version, preview.ID, changed); !errors.Is(err, providerautomation.ErrPreview) {
		t.Fatalf("preview authorized different settings: %v", err)
	}
	// Cross-scope previews cannot authorize a public-pool mutation.
	if _, err = service.Process(ctx, "pool", pool.Version, preview.ID, nil, false); !errors.Is(err, providerautomation.ErrPreview) {
		t.Fatalf("cross scope preview: %v", err)
	}
	run, err := service.Process(ctx, "private", private.Version, preview.ID, nil, false)
	if err != nil || run.Counts.ToBlock != 1 {
		t.Fatalf("run %+v %v", run, err)
	}
	db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID)
	if status != "blocklisted" {
		t.Fatal("private not blocked")
	}
	db.Get(&status, `SELECT status FROM customer_list_memberships WHERE customer_id=$1`, customerID)
	if status != "unsubscribed" {
		t.Fatal("private memberships not unsubscribed")
	}
	db.Get(&status, `SELECT status FROM pool_contacts WHERE id=$1`, contactID)
	if status != "active" {
		t.Fatal("private rule changed pool contact")
	}
	items, err := service.Items(ctx, "private", run.ID, 1)
	if err != nil || len(items) != 4 || items[0].Evidence.Count != 2 {
		t.Fatalf("details missing: %+v %v", items, err)
	}
	preview, err = service.Process(ctx, "private", private.Version, 0, &private.Config, false)
	if err != nil || preview.Counts.Already != 1 || preview.Counts.ToBlock != 0 {
		t.Fatalf("idempotence %+v %v", preview, err)
	}
	// Exclusion protects a manually restored customer independently of pools.
	exec(`UPDATE customers SET status='enabled' WHERE id=$1`, customerID)
	// Failure while writing details rolls back the blocklist mutation as well.
	exec(`CREATE FUNCTION reject_automation_detail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.outcome='blocked' THEN RAISE EXCEPTION 'detail unavailable'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER reject_automation_detail BEFORE INSERT ON provider_blocklist_items FOR EACH ROW EXECUTE FUNCTION reject_automation_detail()`)
	preview, err = service.Process(ctx, "private", private.Version, 0, &private.Config, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Process(ctx, "private", private.Version, preview.ID, nil, false); err == nil {
		t.Fatal("partial transaction succeeded")
	}
	db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID)
	if status != "enabled" {
		t.Fatal("failed detail write committed blocklist mutation")
	}
	exec(`DROP TRIGGER reject_automation_detail ON provider_blocklist_items`)
	config = private.Config
	config.ExcludedEmails = []string{"SAME@example.test"}
	config.Validate()
	preview, err = service.Process(ctx, "private", private.Version, 0, &config, false)
	if err != nil || preview.Counts.Excluded != 1 || preview.Counts.ToBlock != 0 {
		t.Fatalf("exclusion %+v %v", preview, err)
	}
	// A provider failure commits no customer changes and is persisted as failed.
	fail = true
	failed, err := service.Process(ctx, "private", private.Version, 0, &private.Config, false)
	if err == nil || failed.Status != "failed" {
		t.Fatalf("failed request hidden: %+v %v", failed, err)
	}
	db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID)
	if status != "enabled" {
		t.Fatal("failed fetch mutated customer")
	}
	fail = false
	// A due scheduled pool rule runs once, persists its next execution, and
	// never changes private customers sharing the same address.
	config = pool.Config
	config.Enabled = true
	preview, err = service.Process(ctx, "pool", pool.Version, 0, &config, false)
	if err != nil {
		t.Fatal(err)
	}
	pool, err = service.Save(ctx, "pool", pool.Version, preview.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE provider_blocklist_rules SET next_run_at=$1 WHERE scope='pool'`, clock.Add(-time.Minute))
	run, err = service.Process(ctx, "pool", 0, 0, nil, true)
	if err != nil || run.ID == 0 || run.Counts.ToBlock != 1 {
		t.Fatalf("scheduled pool run: %+v %v", run, err)
	}
	second, err := service.Process(ctx, "pool", 0, 0, nil, true)
	if err != nil || second.ID != 0 {
		t.Fatalf("scheduled twice: %+v %v", second, err)
	}
	db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID)
	if status != "enabled" {
		t.Fatal("pool rule changed private customer")
	}
	// Locks span worker instances, and an abandoned run is recovered.
	conn, err := db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(740702)`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Process(ctx, "pool", 0, 0, nil, true); !errors.Is(err, providerautomation.ErrBusy) {
		t.Fatalf("concurrent worker admitted: %v", err)
	}
	conn.ExecContext(ctx, `SELECT pg_advisory_unlock(740702)`)
	exec(`INSERT INTO provider_blocklist_runs(scope,version,config,trigger,status,date_start,date_end) VALUES('pool',1,'{}','scheduled','running','2026-11-01','2026-11-03')`)
	if _, err = service.Process(ctx, "pool", 0, 0, nil, true); err != nil {
		t.Fatal(err)
	}
	var interrupted int
	db.Get(&interrupted, `SELECT COUNT(*) FROM provider_blocklist_runs WHERE scope='pool' AND status='interrupted'`)
	if interrupted != 1 {
		t.Fatal("crashed run not recovered")
	}
	preview, err = service.Process(ctx, "pool", pool.Version, 0, &pool.Config, false)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(16 * time.Minute)
	if _, err = service.Process(ctx, "pool", pool.Version, preview.ID, nil, false); !errors.Is(err, providerautomation.ErrPreview) {
		t.Fatalf("expired preview accepted: %v", err)
	}
}
