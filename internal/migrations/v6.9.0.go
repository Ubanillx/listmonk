package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_9_0 repairs installations that ran an earlier v6.7.0 migration. That
// version created the workspace e-mail index before assigning owners to old
// subscriber rows, so case-variant historical e-mails could block an upgrade.
func V6_9_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	if err := resetScopedSubscriberEmailIndex(db); err != nil {
		return err
	}
	if err := mergeDuplicateScopedSubscribers(db); err != nil {
		return err
	}
	return createScopedSubscriberEmailIndex(db)
}
