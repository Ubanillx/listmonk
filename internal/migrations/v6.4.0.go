package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_4_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS integration_tokens (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			last_used_at TIMESTAMP WITH TIME ZONE NULL,
			revoked_at TIMESTAMP WITH TIME ZONE NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_integration_tokens_user_id ON integration_tokens(user_id);
		CREATE INDEX IF NOT EXISTS idx_integration_tokens_active ON integration_tokens(user_id, revoked_at);
	`)
	if err != nil {
		return err
	}

	return nil
}
