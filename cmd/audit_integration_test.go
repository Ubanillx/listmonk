package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

const auditHandlerTestTable = `
CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id BIGINT NOT NULL DEFAULT 0,
    actor_type TEXT NOT NULL,
    actor_user_id INTEGER,
    actor_token_id INTEGER,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT 'success',
    reason_code TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    ip INET,
    user_agent TEXT NOT NULL DEFAULT ''
)`

const auditHandlerTestUsersTable = `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT ''
)`

func openAuditHandlerTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("AUDIT_TEST_DSN")
	if dsn == "" {
		t.Skip("AUDIT_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("audit_handler_test_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(auditHandlerTestTable); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(auditHandlerTestUsersTable); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("SET search_path TO public")
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = db.Close()
	})
	return db
}

func TestAuditHandlersKeepPersonalWorkspaceIsolatedPostgresIntegration(t *testing.T) {
	db := openAuditHandlerTestDB(t)
	if _, err := db.Exec(`INSERT INTO users (id, username, name) VALUES (11, 'audit-user', 'Audit User')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO audit_events
		(organization_id, actor_type, actor_user_id, action, object_type, object_id)
		VALUES (0, 'user', 11, 'personal.event.first', 'campaign', '1'),
		       (0, 'user', NULL, 'personal.event.second', 'campaign', '2'),
		       (7, 'user', NULL, 'organization.event', 'campaign', '3')`); err != nil {
		t.Fatal(err)
	}

	a := &App{db: db, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	user := auth.User{Base: auth.Base{ID: 11}, UserRoleID: auth.SuperAdminRoleID}
	req := httptest.NewRequest(http.MethodGet, "/api/audit-events?page=2&per_page=1", nil)
	req.Header.Set(workspaceHeader, "0")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/audit-events")
	c.Set(auth.UserHTTPCtxKey, user)
	if err := a.GetAuditEvents(c); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Data struct {
			Results []auditEventRow `json:"results"`
			Total   int             `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Total != 2 || len(payload.Data.Results) != 1 || payload.Data.Results[0].Action != "personal.event.first" || payload.Data.Results[0].OrganizationID != 0 || payload.Data.Results[0].ActorUsername != "audit-user" {
		t.Fatalf("personal audit response leaked another workspace: %+v", payload.Data)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/audit-events/3", nil)
	req.Header.Set(workspaceHeader, "0")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetPath("/api/audit-events/:id")
	c.SetParamNames("id")
	c.SetParamValues("3")
	c.Set(auth.UserHTTPCtxKey, user)
	err := a.GetAuditEvent(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace detail lookup error = %v, want HTTP 404", err)
	}
}

func TestAuditExportStreamsSelectedAndFilteredFullCSVPostgresIntegration(t *testing.T) {
	db := openAuditHandlerTestDB(t)
	if _, err := db.Exec(`INSERT INTO audit_events
		(organization_id, actor_type, action, object_type, object_id)
		VALUES (0, 'user', 'personal.event', 'campaign', '1'),
		       (0, 'user', 'personal.event', 'campaign', '2'),
		       (7, 'user', 'organization.event', 'campaign', '3')`); err != nil {
		t.Fatal(err)
	}

	a := &App{db: db, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	user := auth.User{Base: auth.Base{ID: 11}, UserRoleID: auth.SuperAdminRoleID}
	newContext := func(path string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(workspaceHeader, "0")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/audit-events/export")
		c.Set(auth.UserHTTPCtxKey, user)
		return c, rec
	}

	c, rec := newContext("/api/audit-events/export?scope=selected&ids=1&ids=3")
	if err := a.ExportAuditEvents(c); err != nil {
		t.Fatal(err)
	}
	selected, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[1][6] != "personal.event" || selected[1][2] != "0" {
		t.Fatalf("selected export rows = %#v", selected)
	}
	if got := rec.Header().Get(echo.HeaderContentDisposition); !strings.Contains(got, "attachment") || !strings.Contains(got, "audit-events-selected") {
		t.Fatalf("selected export disposition = %q", got)
	}

	c, rec = newContext("/api/audit-events/export?scope=all&action=personal.event")
	if err := a.ExportAuditEvents(c); err != nil {
		t.Fatal(err)
	}
	full, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 3 || full[1][6] != "personal.event" || full[2][6] != "personal.event" {
		t.Fatalf("filtered full export rows = %#v", full)
	}
}
