// Package audit persists low- and medium-volume business operation events.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Event is the privacy-safe envelope written to audit_events. Callers should
// put only stable identifiers and small, non-sensitive values in Metadata.
type Event struct {
	OccurredAt     any
	OrganizationID *int64
	ActorType      string
	ActorUserID    *int
	ActorTokenID   *int
	Action         string
	ObjectType     string
	ObjectID       string
	Result         string
	ReasonCode     string
	RequestID      string
	Metadata       any
	IP             string
	UserAgent      string
}

// Writer is the shared audit event sink.
type Writer struct {
	DB  *sqlx.DB
	Log *log.Logger
}

// New constructs a Writer backed by the application database.
func New(db *sqlx.DB, logger *log.Logger) *Writer {
	return &Writer{DB: db, Log: logger}
}

// Record inserts an event. Audit failures are returned to the caller so HTTP
// middleware can log them without changing the business operation result.
func (w *Writer) Record(ctx context.Context, event Event) error {
	if w == nil || w.DB == nil {
		return fmt.Errorf("audit writer is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if event.ActorType == "" {
		event.ActorType = "system"
	}
	if event.Result == "" {
		event.Result = "success"
	}
	if event.Action == "" || event.ObjectType == "" {
		return fmt.Errorf("audit event action and object type are required")
	}
	if event.ObjectID == "" {
		event.ObjectID = ""
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}

	var ip any
	if parsed := net.ParseIP(strings.TrimSpace(event.IP)); parsed != nil {
		ip = parsed.String()
	}
	organizationID := int64(0)
	if event.OrganizationID != nil {
		organizationID = *event.OrganizationID
	}

	_, err = w.DB.ExecContext(ctx, `
		INSERT INTO audit_events (
			occurred_at, organization_id, actor_type, actor_user_id, actor_token_id,
			action, object_type, object_id, result, reason_code, request_id,
			metadata, ip, user_agent
		) VALUES (
			COALESCE($1, NOW()), $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11, $12::JSONB, $13, $14
		)`,
		nullableTime(event.OccurredAt), organizationID, event.ActorType,
		event.ActorUserID, event.ActorTokenID, event.Action, event.ObjectType,
		event.ObjectID, event.Result, trim(event.ReasonCode, 255),
		trim(event.RequestID, 255), metadata, ip, trim(event.UserAgent, 1024),
	)
	return err
}

// RecordTx is the transaction-aware counterpart for core flows that already
// own a database transaction. It is intentionally small so future atomic
// integrations can use the same envelope without duplicating SQL.
func (w *Writer) RecordTx(ctx context.Context, tx *sqlx.Tx, event Event) error {
	if tx == nil {
		return fmt.Errorf("audit transaction is not initialized")
	}
	if w == nil {
		return fmt.Errorf("audit writer is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if event.ActorType == "" {
		event.ActorType = "system"
	}
	if event.Result == "" {
		event.Result = "success"
	}
	if event.Action == "" || event.ObjectType == "" {
		return fmt.Errorf("audit event action and object type are required")
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	var ip any
	if parsed := net.ParseIP(strings.TrimSpace(event.IP)); parsed != nil {
		ip = parsed.String()
	}
	organizationID := int64(0)
	if event.OrganizationID != nil {
		organizationID = *event.OrganizationID
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			occurred_at, organization_id, actor_type, actor_user_id, actor_token_id,
			action, object_type, object_id, result, reason_code, request_id,
			metadata, ip, user_agent
		) VALUES (
			COALESCE($1, NOW()), $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11, $12::JSONB, $13, $14
		)`,
		nullableTime(event.OccurredAt), organizationID, event.ActorType,
		event.ActorUserID, event.ActorTokenID, event.Action, event.ObjectType,
		event.ObjectID, event.Result, trim(event.ReasonCode, 255),
		trim(event.RequestID, 255), metadata, ip, trim(event.UserAgent, 1024),
	)
	return err
}

func trim(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

// nullableTime keeps the SQL statement usable without forcing callers to
// construct a time.Time when they want the database default.
func nullableTime(value any) any {
	if value == nil {
		return nil
	}
	if t, ok := value.(sql.NullTime); ok && !t.Valid {
		return nil
	}
	return value
}
