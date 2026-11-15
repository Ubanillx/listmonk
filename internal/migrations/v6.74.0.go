package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_74_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(SendErrorAlertSQL)
	return err
}

// SendErrorAlertSQL adds the per-campaign alert cooldown and the window alert
// settings. Existing settings are preserved on repeated upgrades.
const SendErrorAlertSQL = `
CREATE TABLE IF NOT EXISTS campaign_send_error_alerts (
    campaign_id   INTEGER PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
    last_alert_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO settings (key, value) VALUES
    ('app.send_error_alert_enabled', 'true'),
    ('app.send_error_alert_window', '"5m"'),
    ('app.send_error_alert_threshold', '50'),
    ('app.send_error_alert_cooldown', '"30m"')
ON CONFLICT (key) DO NOTHING;
`
