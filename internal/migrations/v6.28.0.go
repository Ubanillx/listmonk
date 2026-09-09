package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// ExportSchema is also applied by the fresh-install schema.
const ExportSchema = `
CREATE TABLE IF NOT EXISTS data_export_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 organization_id BIGINT NOT NULL DEFAULT 0,
 request JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','complete','failed','expired')),
 filename TEXT NOT NULL,
 row_count BIGINT NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '',
 access_stamp TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 started_at TIMESTAMPTZ,
 completed_at TIMESTAMPTZ,
 expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days',
 download_count INTEGER NOT NULL DEFAULT 0,
 last_downloaded_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_data_export_jobs_owner ON data_export_jobs(user_id, organization_id, created_at DESC);
CREATE TABLE IF NOT EXISTS data_export_chunks (
 job_id UUID NOT NULL REFERENCES data_export_jobs(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL,
 content BYTEA NOT NULL,
 PRIMARY KEY(job_id, sequence)
);`

func V6_28_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(ExportSchema)
	return err
}
