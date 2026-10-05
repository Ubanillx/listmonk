package migrations

import (
	"database/sql"
	"encoding/json"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/stuffbin"
)

// V6_53_0 adds campaign pacing and moves the effective TLS configuration onto
// each SMTP server. Legacy delays are converted without changing their duration.
func V6_53_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS smtp_rate_limit INT NOT NULL DEFAULT 0
			CHECK (smtp_rate_limit BETWEEN 0 AND 1000000);
		UPDATE campaigns SET smtp_rate_limit = CASE WHEN smtp_source='organization' THEN 100 ELSE 20 END
			WHERE smtp_rate_limit=0 AND (messenger='email' OR messenger LIKE 'email-%');
		ALTER TABLE campaigns ALTER COLUMN smtp_rate_limit SET DEFAULT 20;
	`); err != nil {
		return err
	}
	var raw []byte
	if err := tx.Get(&raw, `SELECT value FROM settings WHERE key='smtp_delivery' FOR UPDATE`); err != nil {
		if err == sql.ErrNoRows {
			return tx.Commit()
		}
		return err
	}
	var legacy struct {
		TLSType string `json:"tls_type"`
		SkipTLS bool   `json:"tls_skip_verify"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return err
	}
	if legacy.TLSType != "" {
		// The old global setting overrode every row. Preserve what was actually
		// used before the upgrade; subsequent runs leave per-server edits alone.
		if _, err := tx.Exec(`UPDATE user_smtp_servers SET tls_type=$1, tls_skip_verify=$2`, legacy.TLSType, legacy.SkipTLS); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE settings SET value=(SELECT JSONB_AGG(elem || JSONB_BUILD_OBJECT('tls_type',$1::TEXT,'tls_skip_verify',$2::BOOLEAN))
			FROM JSONB_ARRAY_ELEMENTS(value) elem) WHERE key='smtp'`, legacy.TLSType, legacy.SkipTLS); err != nil {
			return err
		}
	}
	var delivery models.SMTPDeliverySettings
	if err := json.Unmarshal(raw, &delivery); err != nil {
		return err
	}
	if _, _, err := delivery.SendDelayRange(); err != nil {
		return err
	}
	b, err := json.Marshal(delivery)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE settings SET value=$1, updated_at=NOW() WHERE key='smtp_delivery'`, b); err != nil {
		return err
	}
	return tx.Commit()
}
