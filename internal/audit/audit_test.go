package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const auditTestTable = `
CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id BIGINT NOT NULL DEFAULT 0,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user','api_key','system','webhook','customer','anonymous')),
    actor_user_id INTEGER,
    actor_token_id INTEGER,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT 'success' CHECK (result IN ('success','failed','denied')),
    reason_code TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    ip INET,
    user_agent TEXT NOT NULL DEFAULT ''
)`

func openAuditTestDB(t *testing.T) *sqlx.DB {
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
	schema := fmt.Sprintf("audit_test_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(auditTestTable); err != nil {
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

func TestWriterPostgresIntegration(t *testing.T) {
	db := openAuditTestDB(t)
	writer := New(db, log.New(io.Discard, "", 0))

	occurredAt := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	organizationID := int64(7)
	userID := 11
	tokenID := 22
	requestID := strings.Repeat("r", 300)
	userAgent := strings.Repeat("u", 1100)
	err := writer.Record(context.Background(), Event{
		OccurredAt:     occurredAt,
		OrganizationID: &organizationID,
		ActorType:      "api_key",
		ActorUserID:    &userID,
		ActorTokenID:   &tokenID,
		Action:         "campaign.status_changed",
		ObjectType:     "campaign",
		ObjectID:       "42",
		Result:         "failed",
		ReasonCode:     "http_422",
		RequestID:      requestID,
		Metadata:       map[string]any{"attempt": 2, "safe": true},
		IP:             "192.0.2.10",
		UserAgent:      userAgent,
	})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		OccurredAt   time.Time     `db:"occurred_at"`
		Organization int64         `db:"organization_id"`
		ActorType    string        `db:"actor_type"`
		ActorUserID  sql.NullInt64 `db:"actor_user_id"`
		ActorTokenID sql.NullInt64 `db:"actor_token_id"`
		Action       string        `db:"action"`
		ObjectType   string        `db:"object_type"`
		ObjectID     string        `db:"object_id"`
		Result       string        `db:"result"`
		ReasonCode   string        `db:"reason_code"`
		RequestID    string        `db:"request_id"`
		Metadata     string        `db:"metadata"`
		IP           string        `db:"ip"`
		UserAgent    string        `db:"user_agent"`
	}
	if err := db.Get(&got, `SELECT occurred_at, organization_id, actor_type,
		actor_user_id, actor_token_id, action, object_type, object_id, result,
		reason_code, request_id, metadata::TEXT, ip::TEXT, user_agent
		FROM audit_events`); err != nil {
		t.Fatal(err)
	}
	if !got.OccurredAt.Equal(occurredAt) || got.Organization != organizationID || got.ActorType != "api_key" ||
		!got.ActorUserID.Valid || got.ActorUserID.Int64 != int64(userID) ||
		!got.ActorTokenID.Valid || got.ActorTokenID.Int64 != int64(tokenID) {
		t.Fatalf("unexpected actor/time fields: %+v", got)
	}
	if got.Action != "campaign.status_changed" || got.ObjectType != "campaign" || got.ObjectID != "42" || got.Result != "failed" || got.ReasonCode != "http_422" {
		t.Fatalf("unexpected event identity/result: %+v", got)
	}
	if len(got.RequestID) != 255 || len(got.UserAgent) != 1024 || got.IP != "192.0.2.10/32" {
		t.Fatalf("truncation or IP normalization failed: request=%d ua=%d ip=%q", len(got.RequestID), len(got.UserAgent), got.IP)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(got.Metadata), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["attempt"] != float64(2) || metadata["safe"] != true {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestWriterDefaultsAndValidationPostgresIntegration(t *testing.T) {
	db := openAuditTestDB(t)
	writer := New(db, log.New(io.Discard, "", 0))

	if err := writer.Record(context.Background(), Event{
		Action:     "settings.updated",
		ObjectType: "settings",
		IP:         "not-an-ip",
	}); err != nil {
		t.Fatal(err)
	}
	var actor, result, reason, requestID, userAgent, ip string
	var organizationID int64
	if err := db.QueryRow(`SELECT organization_id, actor_type, result, reason_code, request_id,
		COALESCE(ip::TEXT, ''), user_agent FROM audit_events`).Scan(&organizationID, &actor, &result, &reason, &requestID, &ip, &userAgent); err != nil {
		t.Fatal(err)
	}
	if organizationID != 0 || actor != "system" || result != "success" || reason != "" || requestID != "" || ip != "" || userAgent != "" {
		t.Fatalf("unexpected defaults: org=%d actor=%q result=%q reason=%q request=%q ip=%q ua=%q", organizationID, actor, result, reason, requestID, ip, userAgent)
	}

	if err := writer.Record(context.Background(), Event{
		Action:     "settings.updated",
		ObjectType: "settings",
		Metadata:   func() {},
	}); err == nil {
		t.Fatal("unsupported metadata value was accepted")
	}
}

func TestWriterTransactionCommitAndRollbackPostgresIntegration(t *testing.T) {
	db := openAuditTestDB(t)
	writer := New(db, log.New(io.Discard, "", 0))

	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.RecordTx(context.Background(), tx, Event{Action: "campaign.created", ObjectType: "campaign", ObjectID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM audit_events"); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d audit rows", count)
	}

	tx, err = db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.RecordTx(context.Background(), tx, Event{Action: "campaign.created", ObjectType: "campaign", ObjectID: "2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&count, "SELECT COUNT(*) FROM audit_events"); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("commit left %d audit rows, want 1", count)
	}
}
