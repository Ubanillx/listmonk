package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	apptz "github.com/knadh/listmonk/internal/tz"
	"github.com/knadh/stuffbin"
)

// V6_62_0 repairs deferred campaigns whose next_resume_at was persisted on a
// clock that did not match the deployment time zone.
//
// Before the deployment adopted a single time zone, a process running with
// time.Local == UTC computed the next resume instant from its own wall clock.
// The stored instant is therefore an absolute time whose deployment-local
// reading is daily_resume_time shifted by the zone difference: a campaign with
// daily_resume_time '09:00' was persisted as 09:00 UTC, which the admin UI
// renders as 17:00 in Asia/Shanghai.
//
// next_resume_at is written once, when the campaign is deferred, so the fixed
// code cannot correct rows that are already stored. This migration re-anchors
// every deferred campaign whose stored wall clock disagrees with its own
// daily_resume_time, moving it to the first future occurrence of that time on
// the deployment's clock. The predicate compares wall clocks, so a row that is
// already correct - including one this migration just repaired - is untouched
// and re-running the migration is a no-op.
func V6_62_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko

	// The DSN pins every listmonk session to this zone, but stating it in the
	// query keeps the repair independent of the session the migration runs in.
	zone := apptz.Name()

	res, err := db.Exec(`
		WITH repaired AS (
			SELECT
				c.id,
				((
					(c.next_resume_at AT TIME ZONE $1)::date
					+ (c.daily_resume_time)::time
				) AT TIME ZONE $1) AS target
			FROM campaigns c
			WHERE c.status = 'deferred'
				AND c.next_resume_at IS NOT NULL
				AND c.daily_resume_time ~ '^([01]?[0-9]|2[0-3]):[0-5][0-9]$'
				AND (c.next_resume_at AT TIME ZONE $1)::time <> (c.daily_resume_time)::time
		)
		UPDATE campaigns c
		SET next_resume_at = CASE
				WHEN r.target > NOW() THEN r.target
				ELSE r.target + (
					CEIL(EXTRACT(EPOCH FROM (NOW() - r.target)) / 86400)::int
					* INTERVAL '1 day'
				)
			END,
			updated_at = NOW()
		FROM repaired r
		WHERE c.id = r.id
			AND c.status = 'deferred';
	`, zone)
	if err != nil {
		return err
	}

	if lo != nil {
		if n, rowsErr := res.RowsAffected(); rowsErr == nil && n > 0 {
			lo.Printf("re-anchored %d deferred campaign resume time(s) to %s", n, zone)
		}
	}
	return nil
}
