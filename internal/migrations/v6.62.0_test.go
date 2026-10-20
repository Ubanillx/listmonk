package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	apptz "github.com/knadh/listmonk/internal/tz"
)

// TestDeferredResumeRepairReanchorsToDeploymentClock reproduces the production
// defect: next_resume_at was persisted by a process whose time.Local was UTC, so
// a campaign configured for 09:00 stored 09:00 UTC and the admin UI rendered
// 17:00 in Asia/Shanghai. The repair must move such a row to the next 09:00 on
// the deployment clock, leave already-correct rows alone, and be re-runnable.
func TestDeferredResumeRepairReanchorsToDeploymentClock(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	schema := fmt.Sprintf("resume_repair_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")

	if _, err := db.Exec(`
		CREATE TYPE campaign_status AS ENUM ('draft','scheduled','running','paused','cancelled','finished','deferred');
		CREATE TABLE campaigns (
			id                INT PRIMARY KEY,
			status            campaign_status NOT NULL,
			daily_resume_time TEXT NOT NULL DEFAULT '09:00',
			next_resume_at    TIMESTAMPTZ,
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`); err != nil {
		t.Fatal(err)
	}

	zone := apptz.Name()
	if _, err := db.Exec(`SET TIME ZONE '` + zone + `'`); err != nil {
		t.Fatal(err)
	}

	// The reported campaign: deferred at 10:17 on 2026-10-20 with '09:00'
	// configured, persisted as 09:00 UTC on the following day (17:00 Shanghai).
	staleTarget := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	// A row written by the fixed code: already 09:00 on the deployment clock.
	freshTarget := time.Date(2026, 10, 21, 9, 0, 0, 0, mustLoad(t, zone))

	if _, err := db.Exec(`
		INSERT INTO campaigns (id, status, daily_resume_time, next_resume_at) VALUES
			(1, 'deferred', '09:00', $1),
			(2, 'deferred', '09:00', $2),
			(3, 'running',  '09:00', $3)
	`, staleTarget, freshTarget, staleTarget); err != nil {
		t.Fatal(err)
	}

	if err := V6_62_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	var got time.Time
	if err := db.Get(&got, `SELECT next_resume_at FROM campaigns WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if local := got.In(mustLoad(t, zone)); local.Hour() != 9 || local.Minute() != 0 {
		t.Fatalf("repaired resume wall clock = %s, want 09:00 %s", local.Format(time.RFC3339), zone)
	}
	if !got.After(time.Now()) {
		t.Fatalf("repaired resume time %s is not in the future", got.Format(time.RFC3339))
	}

	// The already-correct row must be byte-for-byte untouched.
	var untouched time.Time
	if err := db.Get(&untouched, `SELECT next_resume_at FROM campaigns WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if !untouched.Equal(freshTarget) {
		t.Fatalf("correct row changed: %s -> %s", freshTarget, untouched)
	}

	// A campaign that is not deferred owns no resume time to repair.
	var untouched3 time.Time
	if err := db.Get(&untouched3, `SELECT next_resume_at FROM campaigns WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if !untouched3.Equal(staleTarget) {
		t.Fatalf("non-deferred row changed: %s -> %s", staleTarget, untouched3)
	}

	// The repaired day follows the day the stale instant already pointed at, so
	// a row rolled to "tomorrow" is not pushed further out.
	staleDay := staleTarget.In(mustLoad(t, zone)).Format("2006-01-02")
	if day := got.In(mustLoad(t, zone)).Format("2006-01-02"); day < staleDay {
		t.Fatalf("repaired day %s precedes the stored day %s", day, staleDay)
	}

	// Re-running must be a no-op.
	before := got
	if err := V6_62_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&got, `SELECT next_resume_at FROM campaigns WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(before) {
		t.Fatalf("re-running the migration moved the row again: %s -> %s", before, got)
	}
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
