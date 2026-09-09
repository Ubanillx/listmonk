package dataexport

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/xuri/excelize/v2"
)

func TestSafeCSVCell(t *testing.T) {
	for _, s := range []string{"=1+1", " @SUM(A1)", "\t-2", "+cmd"} {
		if !strings.HasPrefix(SafeCSVCell(s), "'") {
			t.Fatal(s)
		}
	}
	if SafeCSVCell("客户001") != "客户001" {
		t.Fatal("changed safe text")
	}
}

func TestRequestValidation(t *testing.T) {
	for _, r := range []Request{{Type: "bad", Format: "csv"}, {Type: "pool_private", Format: "csv"}, {Type: "customers", Format: "html"}, {Type: "pools", Format: "xlsx", IDs: []int64{-1}}} {
		if r.Validate() == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	if _, _, err := BuildQuery(Request{Type: "activity", Format: "csv"}, Access{}); err == nil {
		t.Fatal("individual tracking bypass")
	}
}

func TestFilterExpressions(t *testing.T) {
	for _, bad := range []string{"true); SELECT 1", "email IN (SELECT email FROM users)", "pg_sleep(10)=1", "true --comment", "other.email='x'", "id=$1", "name='oops"} {
		q := &query{}
		if _, err := q.expression(bad); err == nil {
			t.Fatalf("accepted unsafe expression %s", bad)
		}
	}
	q := &query{}
	sql, args, err := BuildQuery(Request{Type: "customers", Format: "csv", Query: "customers.status='blocklisted' AND (name ILIKE '%test%' OR id IN (1,2))"}, Access{OrganizationID: 7})
	if err != nil || !strings.Contains(sql, "c.status = $") || strings.Contains(sql, "%test%") || len(args) < 5 {
		t.Fatalf("expression not parameterized: %s %v %v", sql, args, err)
	}
	if _, err = q.expression("attribs->>'country' = 'CN'"); err != nil {
		t.Fatal(err)
	}
}

// Run with EXPORT_TEST_DSN against a development PostgreSQL server. Every test
// object lives in a new isolated schema; no existing rows are read or changed.
func TestExportIntegration(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := "export_test_" + time.Now().Format("20060102150405_000000000")
	if _, err = db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	testDB, err := sqlx.Connect("postgres", dsn+" search_path="+schema+",public")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Existing public objects must not be resolved by destructive schema DDL.
	if _, err = testDB.Exec(`SET search_path TO ` + schema); err != nil {
		t.Fatal(err)
	}
	testDB.SetMaxOpenConns(1)
	if _, err = testDB.Exec(string(schemaBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err = testDB.Exec(migrations.ExportSchema); err != nil {
		t.Fatal(err)
	}
	if _, err = testDB.Exec(migrations.ExportSchema); err != nil {
		t.Fatal("idempotent migration:", err)
	}
	// Set startup search_path for all connections before testing the worker.
	testDB.Close()
	testDB, err = sqlx.Connect("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	fixture := `
 INSERT INTO roles(id,type,name) VALUES(1,'user','admin'),(2,'user','member');
 INSERT INTO users(id,username,email,name,user_role_id,status) VALUES
 (1,'admin','admin@test.invalid','Admin',1,'enabled'),(2,'manager','manager@test.invalid','Manager',2,'enabled'),(3,'member','member@test.invalid','Member',2,'enabled');
 INSERT INTO organizations(id,name,created_by_user_id) VALUES(1,'Org A',1),(2,'Org B',1);
 INSERT INTO organization_members(organization_id,user_id,role) VALUES(1,2,'manager'),(1,3,'member');
 INSERT INTO customers(id,uuid,email,name,customer_code,organization_id,owner_user_id,status) VALUES
 (1,gen_random_uuid(),'longcustomer@test.invalid','客户甲','C001',1,3,'blocklisted'),
 (2,gen_random_uuid(),'other@test.invalid','客户乙','C001',2,1,'enabled'),
 (3,gen_random_uuid(),'wrong@test.invalid','同编码不同邮箱','C001',1,3,'enabled');
 INSERT INTO customer_lists(id,uuid,name,type,organization_id,owner_user_id,mask_emails) VALUES
 (1,gen_random_uuid(),'Private A','private',1,3,true),(2,gen_random_uuid(),'Private B','private',2,1,false),
 (3,gen_random_uuid(),'Primary','pool',NULL,1,false),(4,gen_random_uuid(),'Secondary A','pool_segment',1,2,false),
 (5,gen_random_uuid(),'Private A2','private',1,3,true);
 INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES(1,1,'confirmed'),(1,5,'confirmed'),(2,2,'confirmed'),(3,1,'confirmed');
 INSERT INTO pool_contacts(id,customer_code,company_name,email) VALUES(1,'C001','公司甲','LONGCUSTOMER@test.invalid'),(2,'C001','公司乙','other@test.invalid');
 INSERT INTO pool_members(pool_id,contact_id) VALUES(3,1),(3,2);
 INSERT INTO pool_segments(id,list_id,pool_id,organization_id) VALUES(1,4,3,1);
 INSERT INTO pool_segment_members(segment_id,contact_id,status) VALUES(1,1,'removed'),(1,2,'active');
 INSERT INTO pool_segment_exclusions(pool_id,organization_id,contact_id,segment_id,source,reason) VALUES(3,1,1,1,'segment','转入私有');
 INSERT INTO bounces(customer_id,type,meta) VALUES(1,'hard','{"diagnostic":"longcustomer@test.invalid"}'),(1,'soft','{}'),(2,'hard','{}');
 INSERT INTO campaigns(id,uuid,name,subject,from_email,body,messenger,organization_id,owner_user_id) VALUES
 (1,gen_random_uuid(),'Campaign A','Subject','test@test.invalid','','email',1,3),(2,gen_random_uuid(),'Campaign B','Subject','test@test.invalid','','email',2,1);
 INSERT INTO campaign_recipients(campaign_id,customer_id,status,sent_at) VALUES(1,1,'sent',NOW());
 INSERT INTO campaign_views(campaign_id,customer_id) VALUES(1,1),(1,1),(2,2);
 INSERT INTO link_clicks(campaign_id,link_id,customer_id) SELECT 1,id,1 FROM links LIMIT 1;
 UPDATE settings SET value='true' WHERE key='privacy.individual_tracking';`
	if _, err = testDB.Exec(fixture); err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: testDB, Log: log.New(io.Discard, "", 0)}
	ctx := context.Background()
	a, _, err := s.Access(ctx, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Access(ctx, 3, 1); err == nil {
		t.Fatal("ordinary member exported")
	}
	if _, _, err = s.Access(ctx, 2, 2); err == nil {
		t.Fatal("manager crossed org")
	}
	for kind := range Types {
		t.Run(kind, func(t *testing.T) {
			if kind == "pool_contacts" {
				if _, _, e := BuildQuery(Request{Type: kind, Format: "csv"}, a); e == nil {
					t.Fatal("manager exported primary contacts")
				}
				admin := a
				admin.PlatformAdmin = true
				q, args, e := BuildQuery(Request{Type: kind, Format: "csv"}, admin)
				if e != nil {
					t.Fatal(e)
				}
				rows, e := testDB.Queryx(q, args...)
				if e != nil {
					t.Fatal(e)
				}
				rows.Close()
				return
			}
			q, args, e := BuildQuery(Request{Type: kind, Format: "csv"}, a)
			if e != nil {
				t.Fatal(e)
			}
			rows, e := testDB.Queryx(q, args...)
			if e != nil {
				t.Fatal(e)
			}
			var b bytes.Buffer
			n, e := WriteRows(ctx, &b, "csv", rows, nil)
			rows.Close()
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(b.String(), "客户乙") || strings.Contains(b.String(), "Campaign B") {
				t.Fatal("cross-organization leak")
			}
			if strings.Contains(b.String(), "longcustomer@test.invalid") {
				t.Fatal("plaintext leaked")
			}
			if kind == "pools" && (n != 2 || !strings.Contains(b.String(), "移除原因") || !strings.Contains(b.String(), "转入私有")) {
				t.Fatalf("pool removal reason missing: %s", b.String())
			}
			if kind == "bounce_customers" && n != 1 {
				t.Fatalf("bounce dedup: %d", n)
			}
		})
	}
	job, err := s.Create(ctx, a, Request{Type: "pools", Format: "xlsx"}, "Org A")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.tick(ctx); err != nil {
		t.Fatal(err)
	}
	job, chunks, err := s.Download(ctx, a, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for chunks.Next() {
		var x []byte
		if err = chunks.Scan(&x); err != nil {
			t.Fatal(err)
		}
		b.Write(x)
	}
	chunks.Close()
	book, err := excelize.OpenReader(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	data, err := book.GetRows("Sheet1")
	if err != nil || len(data) != 3 {
		t.Fatalf("xlsx content: %v %v", data, err)
	}
	if job.RowCount != 2 {
		t.Fatal("incorrect audit count")
	}
	if _, err = testDB.Exec(`UPDATE customer_lists SET mask_emails=false WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Download(ctx, a, job.ID); err == nil {
		t.Fatal("stale privacy stamp allowed")
	}
	if _, err = testDB.Exec(`UPDATE pool_segment_exclusions SET restored_at=NOW();UPDATE pool_segment_members SET status='active'`); err != nil {
		t.Fatal(err)
	}
	q, args, _ := BuildQuery(Request{Type: "pools", Format: "csv"}, a)
	rows, err := testDB.Queryx(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	n, err := WriteRows(ctx, &b, "csv", rows, nil)
	rows.Close()
	if err != nil || n != 2 || strings.Contains(b.String(), "转入私有") {
		t.Fatal("restored contacts retain removal reason", err)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(b.String(), "\ufeff"))).ReadAll()
	if err != nil || len(records) != 3 {
		t.Fatal("pool export rows", err)
	}
	if _, err = testDB.Exec(`UPDATE data_export_jobs SET expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.tick(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = testDB.Get(&remaining, `SELECT count(*) FROM data_export_chunks`); err != nil || remaining != 0 {
		t.Fatal("expired artifact retained", err)
	}
}
