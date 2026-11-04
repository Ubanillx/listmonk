package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/bestedm"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/knadh/listmonk/internal/providerautomation"
	"github.com/labstack/echo/v4"
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

func TestProviderAutomationManualHTTPContract(t *testing.T) {
	db := newAdminTestDB(t)
	if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('integration.bestedm','{"username":"stub","password":"stub"}');
        INSERT INTO customers(uuid,email,name) VALUES(gen_random_uuid(),'http@example.test','HTTP customer')`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, bestEDMClient: stubBestEDM{failures: []bestedm.Row{{"email": "http@example.test", "error_type": "2"}}}}
	rule, err := a.providerAutomation().Rule(context.Background(), "private")
	if err != nil {
		t.Fatal(err)
	}
	config := rule.Config
	config.Types = []int{2}
	request := func(action string, body map[string]any) ([]byte, error) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		c, recorder := providerContext(t, "/api/delivery-provider/automation/private/"+action, "scope", "private", "action", action)
		req := httptest.NewRequest(http.MethodPost, c.Request().URL.String(), bytes.NewReader(raw))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.SetRequest(req)
		err = a.ChangeProviderAutomation(c)
		return recorder.Body.Bytes(), err
	}
	if _, err := request("preview", map[string]any{"version": rule.Version}); err == nil {
		t.Fatal("missing check config accepted")
	}
	result, err := request("preview", map[string]any{"version": rule.Version, "config": config})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data struct {
			Run providerautomation.Run `json:"run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Data.Run.Counts.ToBlock != 1 {
		t.Fatalf("check response: %s %v", result, err)
	}
	// Omitting config keeps the existing saved-rule contract, which cannot
	// reuse a check for different manual conditions.
	if _, err := request("run", map[string]any{"version": rule.Version, "preview_id": response.Data.Run.ID}); err == nil {
		t.Fatal("manual check authorized saved conditions")
	}
	result, err = request("run", map[string]any{"version": rule.Version, "preview_id": response.Data.Run.ID, "config": config})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Data.Run.Status != "success" || response.Data.Run.Counts.ToBlock != 1 {
		t.Fatalf("execution response: %s %v", result, err)
	}
	after, err := a.providerAutomation().Rule(context.Background(), "private")
	if err != nil || !reflect.DeepEqual(rule, after) {
		t.Fatal("HTTP manual execution saved or rescheduled the rule")
	}
}

func TestProviderAutomationManualConditionsDoNotSaveSchedule(t *testing.T) {
	db := newAdminTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(migrations.ProviderAutomationSQL); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 4, 4, 0, 0, 0, time.FixedZone("CST", 8*3600))
	fail := false
	service := &providerautomation.Service{DB: db, Now: func() time.Time { return clock }, Fetch: func(_ context.Context, _ url.Values) ([]bestedm.Row, error) {
		if fail {
			return nil, errors.New("provider unavailable")
		}
		return []bestedm.Row{{"email": "manual@example.test", "error_type": "2"}, {"email": "excluded@example.test", "error_type": "2"}}, nil
	}}
	for _, scope := range []string{"private", "pool"} {
		t.Run(scope, func(t *testing.T) {
			table, initial := "customers", "enabled"
			if scope == "pool" {
				table, initial = "pool_contacts", "active"
				if _, err := db.Exec(`INSERT INTO customer_lists(uuid,name,type) VALUES(gen_random_uuid(),'Manual pool','pool')`); err != nil {
					t.Fatal(err)
				}
			}
			for _, email := range []string{"manual@example.test", "excluded@example.test"} {
				query := `INSERT INTO customers(uuid,email,name) VALUES(gen_random_uuid(),$1,'Manual customer')`
				if scope == "pool" {
					query = `WITH contact AS (INSERT INTO pool_contacts(email) VALUES($1) RETURNING id)
                    INSERT INTO pool_members(pool_id,contact_id) SELECT l.id,c.id FROM customer_lists l,contact c WHERE l.name='Manual pool'`
				}
				if _, err := db.Exec(query, email); err != nil {
					t.Fatal(err)
				}
			}
			rule, err := service.Rule(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if scope == "pool" {
				// Also preserve the timestamps of an already enabled schedule.
				enabled := rule.Config
				enabled.Enabled = true
				check, err := service.Process(ctx, scope, rule.Version, 0, &enabled, false)
				if err != nil {
					t.Fatal(err)
				}
				rule, err = service.Save(ctx, scope, rule.Version, check.ID, enabled)
				if err != nil {
					t.Fatal(err)
				}
			}
			// These are unsaved conditions, including an enabled draft switch.
			// Applying them must never enable or overwrite the recurring rule.
			config := rule.Config
			config.Types = []int{2}
			config.Enabled = true
			config.LookbackDays = 9
			config.ExcludedEmails = []string{"excluded@example.test"}
			preview, err := service.Process(ctx, scope, rule.Version, 0, &config, false)
			if err != nil || preview.Counts.ToBlock != 1 || preview.Counts.Excluded != 1 {
				t.Fatalf("check: %+v %v", preview, err)
			}
			assertStatus := func(email, want string) {
				t.Helper()
				var got string
				if err := db.Get(&got, `SELECT status FROM `+table+` WHERE email=$1`, email); err != nil || got != want {
					t.Fatalf("%s status %s, want %s: %v", email, got, want, err)
				}
			}
			assertStatus("manual@example.test", initial)
			changed := config
			changed.LookbackDays++
			if _, err := service.Execute(ctx, scope, rule.Version, preview.ID, &changed); !errors.Is(err, providerautomation.ErrPreview) {
				t.Fatalf("check authorized different conditions: %v", err)
			}
			if _, err := service.Execute(ctx, scope, rule.Version+1, preview.ID, &config); !errors.Is(err, providerautomation.ErrConflict) {
				t.Fatalf("stale version accepted: %v", err)
			}
			clock = clock.Add(16 * time.Minute)
			if _, err := service.Execute(ctx, scope, rule.Version, preview.ID, &config); !errors.Is(err, providerautomation.ErrPreview) {
				t.Fatalf("expired check accepted: %v", err)
			}
			preview, err = service.Process(ctx, scope, rule.Version, 0, &config, false)
			if err != nil {
				t.Fatal(err)
			}
			fail = true
			if _, err := service.Execute(ctx, scope, rule.Version, preview.ID, &config); err == nil {
				t.Fatal("failed provider request succeeded")
			}
			assertStatus("manual@example.test", initial)
			fail = false
			run, err := service.Execute(ctx, scope, rule.Version, preview.ID, &config)
			if err != nil || run.Trigger != "manual" || run.Status != "success" || run.Counts.ToBlock != 1 {
				t.Fatalf("manual run: %+v %v", run, err)
			}
			assertStatus("manual@example.test", "blocklisted")
			assertStatus("excluded@example.test", initial)
			after, err := service.Rule(ctx, scope)
			if err != nil || !reflect.DeepEqual(rule, after) {
				t.Fatalf("manual operation changed schedule: before %+v after %+v: %v", rule, after, err)
			}
		})
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
