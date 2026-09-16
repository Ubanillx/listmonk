package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_41_0 backfills secondary-list membership from the validated allocation
// department stored on existing public-pool contacts. Existing rows in
// pool_segment_members are left untouched so a deliberate removal is not
// silently reversed.
func V6_41_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		INSERT INTO pool_segment_members(segment_id,contact_id,status)
		SELECT ps.id,pm.contact_id,'active'
		FROM pool_segments ps
		JOIN organizations o ON o.id=ps.organization_id AND o.status='active'
		JOIN pool_members pm ON pm.pool_id=ps.pool_id
		JOIN pool_contacts pc ON pc.id=pm.contact_id
		WHERE ps.pool_id IS NOT NULL
			AND LOWER(TRIM(pc.allocation_department))=LOWER(TRIM(o.name))
		ON CONFLICT (segment_id,contact_id) DO NOTHING;
	`)
	return err
}
