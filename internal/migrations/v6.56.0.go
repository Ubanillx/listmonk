package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_56_0 preserves existing list deletion, asset sharing and self-service
// mailbox configuration. Global pool administration is never backfilled into
// an ordinary role; allocation maintenance cannot become master-data access.
func V6_56_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		UPDATE roles r SET permissions = ARRAY(
			SELECT DISTINCT p FROM unnest(r.permissions || ARRAY['customer_lists:delete']) p
		) WHERE r.type='user' AND r.id<>1 AND (
			'customer_lists:manage_all'=ANY(r.permissions) OR EXISTS (
				SELECT 1 FROM users u JOIN roles child ON child.parent_id=u.list_role_id
				WHERE u.user_role_id=r.id AND 'customer_list:manage'=ANY(child.permissions)
			)
		);
		UPDATE roles SET permissions = ARRAY(
			SELECT DISTINCT p FROM unnest(permissions || ARRAY['assets:share']) p
		) WHERE type='user' AND id<>1 AND permissions && ARRAY['templates:manage','media:manage'];
		UPDATE roles SET permissions = ARRAY(
			SELECT DISTINCT p FROM unnest(permissions || ARRAY['mailboxes:use','mailboxes:manage']) p
		) WHERE type='user' AND id<>1;
		UPDATE roles r SET permissions = ARRAY(
			SELECT DISTINCT p FROM unnest(r.permissions || ARRAY['pools:manage']) p
		) WHERE r.type='user' AND r.id<>1 AND EXISTS (
			SELECT 1 FROM users u JOIN organization_members m ON m.user_id=u.id
			WHERE u.user_role_id=r.id AND m.role='manager' AND m.removed_at IS NULL
		);
	`)
	return err
}
