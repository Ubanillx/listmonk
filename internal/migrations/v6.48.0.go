package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_48_0 adds active first-level public-pool list totals to the dashboard
// materialized view. Workspace-scoped requests calculate their own totals.
func V6_48_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		DROP MATERIALIZED VIEW IF EXISTS mat_dashboard_counts;
		CREATE MATERIALIZED VIEW mat_dashboard_counts AS
			WITH customers_by_status AS (
				SELECT COUNT(*) AS num, status FROM customers GROUP BY status
			)
			SELECT NOW() AS updated_at,
				JSON_BUILD_OBJECT(
					'customers', JSON_BUILD_OBJECT(
						'total', (SELECT COALESCE(SUM(num), 0) FROM customers_by_status),
						'blocklisted', (SELECT COALESCE(num, 0) FROM customers_by_status WHERE status='blocklisted'),
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
					'poolLists', JSON_BUILD_OBJECT(
						'total', (SELECT COUNT(*) FROM customer_lists WHERE type='pool' AND status='active')
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
