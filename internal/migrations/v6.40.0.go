package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_40_0 repairs legacy public-pool secondary-list metadata. Valid secondary
// lists derive their organization and parent pool from pool_segments and are
// organization-visible. Rows that have no bound segment are historical
// standalone secondary lists and are archived rather than deleted.
func V6_40_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		UPDATE customer_lists l
		SET organization_id = NULL,
			visibility = 'global',
			updated_at = NOW()
		WHERE l.type = 'pool'
			AND l.status = 'active'
			AND (l.organization_id IS NOT NULL OR l.visibility IS DISTINCT FROM 'global');

		UPDATE customer_lists l
		SET organization_id = ps.organization_id,
			pool_parent_id = ps.pool_id,
			visibility = 'organization',
			updated_at = NOW()
		FROM pool_segments ps
		WHERE ps.list_id = l.id
			AND ps.pool_id IS NOT NULL
			AND l.type = 'pool_segment'
			AND (
				l.organization_id IS DISTINCT FROM ps.organization_id
				OR l.pool_parent_id IS DISTINCT FROM ps.pool_id
				OR l.visibility IS DISTINCT FROM 'organization'
			);

		UPDATE customer_lists l
		SET status = 'archived', updated_at = NOW()
		WHERE l.type = 'pool_segment'
			AND l.status = 'active'
			AND NOT EXISTS (
				SELECT 1
				FROM pool_segments ps
				WHERE ps.list_id = l.id AND ps.pool_id IS NOT NULL
			);
	`)
	return err
}
