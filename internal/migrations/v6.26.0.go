package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_26_0 rebuilds the global dashboard cache with the customer terminology
// used by the API and admin UI. Existing databases may still have a view
// created by the legacy schema, which returns subscribers/lists keys.
func V6_26_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		DROP MATERIALIZED VIEW IF EXISTS mat_dashboard_counts;
		CREATE MATERIALIZED VIEW mat_dashboard_counts AS
			WITH customers_by_status AS (
				SELECT COUNT(*) AS num, status FROM customers GROUP BY status
			)
			SELECT NOW() AS updated_at,
				JSON_BUILD_OBJECT(
					'customers', JSON_BUILD_OBJECT(
						'total', (SELECT SUM(num) FROM customers_by_status),
						'blocklisted', (SELECT num FROM customers_by_status WHERE status='blocklisted'),
						'orphans', (
							SELECT COUNT(id) FROM customers
							LEFT JOIN customer_list_memberships ON (customers.id = customer_list_memberships.customer_id)
							WHERE customer_list_memberships.customer_id IS NULL
						)
					),
					'customerLists', JSON_BUILD_OBJECT(
						'total', (SELECT COUNT(*) FROM customer_lists),
						'private', (SELECT COUNT(*) FROM customer_lists WHERE type='private'),
						'public', (SELECT COUNT(*) FROM customer_lists WHERE type='public'),
						'optin_single', (SELECT COUNT(*) FROM customer_lists WHERE optin='single'),
						'optin_double', (SELECT COUNT(*) FROM customer_lists WHERE optin='double')
					),
					'campaigns', JSON_BUILD_OBJECT(
						'total', (SELECT COUNT(*) FROM campaigns),
						'by_status', (
							SELECT JSON_OBJECT_AGG(status, num) FROM
							(SELECT status, COUNT(*) AS num FROM campaigns GROUP BY status) r
						)
					),
					'messages', (SELECT COALESCE(SUM(sent), 0) FROM campaigns)
				) AS data;
		CREATE UNIQUE INDEX mat_dashboard_stats_idx ON mat_dashboard_counts (updated_at);
	`)
	return err
}
