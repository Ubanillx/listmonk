package main

import (
	"context"
	"testing"
)

// TestReplyAIMailboxQueryToleratesNonBooleanSettings guards against casting
// arbitrary settings values to boolean. The planner may evaluate the cast before
// the key filter, and array-valued settings abort the whole reply and bounce
// scanner with "cannot cast jsonb array to type boolean".
func TestReplyAIMailboxQueryToleratesNonBooleanSettings(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	if _, err := a.db.Exec(`UPDATE users SET status='enabled' WHERE id=3;
		INSERT INTO settings(key,value) VALUES
		('bounce.enabled','true'::jsonb), ('bounce.mailboxes','[]'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO reply_mailboxes(user_id,organization_id,email,status,scan_enabled,bounce_enabled,verified_at,password)
		VALUES(3,101,'bounce-scan@example.test','active',TRUE,TRUE,NOW(),'secret')`); err != nil {
		t.Fatal(err)
	}

	query, ok := readTestQueries(t)["get-reply-ai-mailboxes"]
	if !ok {
		t.Fatal("get-reply-ai-mailboxes query is missing")
	}

	ctx := context.Background()
	conn, err := a.db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Force the seq-scan plan that evaluates the settings predicate on every row,
	// which is what exposes the unsafe boolean cast in production.
	if _, err := conn.ExecContext(ctx, `SET enable_indexscan = off; SET enable_bitmapscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryxContext(ctx, query.Query)
	if err != nil {
		t.Fatalf("get-reply-ai-mailboxes: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("selected %d mailboxes, want 1", count)
	}

	bounceQuery, ok := readTestQueries(t)["get-bounce-enabled"]
	if !ok {
		t.Fatal("get-bounce-enabled query is missing")
	}
	var enabled bool
	if err := conn.QueryRowxContext(ctx, bounceQuery.Query).Scan(&enabled); err != nil {
		t.Fatalf("get-bounce-enabled: %v", err)
	}
	if !enabled {
		t.Fatal("bounce.enabled was not read as true")
	}
}
