# Performance

listmonk is built to be highly performant and can handle millions of customers with minimal system resources.

However, as the Postgres database grows—with a large number of customers, campaign views, and click records—it can significantly slow down certain aspects of the program, particularly in counting records and aggregating various statistics. For instance, loading admin pages that do these aggregations can take tens of seconds if the database has millions of customers.

- Aggregate counts, statistics, and charts on the landing dashboard.
- Customer count beside every customer_list on the CustomerLists page.
- Total customer count on the Customers page.

However, at that scale, viewing the exact number of customers or statistics every time the admin panel is accessed becomes mostly unnecessary. On installations with millions of customers, where the above pages do not load instantly, it is highly recommended to turn on the `Settings -> Performance -> Cache slow database queries` option.

## Slow query caching

When this option is enabled, the customer counts on the CustomerLists page, the Customers page, and the statistics on the dashboard, etc., are no longer counted in real-time in the database. Instead, they are updated periodically and cached, resulting in a massive performance boost. The periodicity can be configured on the Settings -> Performance page using a standard crontab expression (default: `0 3 * * *`, which means 3 AM daily). Use a tool like [crontab.guru](https://crontab.guru) for easily generating a desired crontab expression.

## VACUUM-ing

Running [`VACUUM ANALYZE`](https://www.postgresql.org/docs/current/sql-vacuum.html) on large Postgres databases at regular intervals (for instance, once a week), is recommended. It reclaims disk space and improves Postgres' query performance. Do note that this is a blocking operation and all database queries can come to a stand-still on a large database while the operation is running (generally only a few seconds).

## Cleanup previews

The Maintenance page counts matching records when opened or when conditions
change, and refreshes that count before showing the deletion confirmation.
Cleanup applies across **all personal and organization workspaces**, using the
existing `settings:maintain` permission. A selected workspace does not narrow it.
Failed previews show a retry action; zero or unavailable counts disable cleanup.
Cancel keeps the selected conditions and sends no delete request.
The date picker uses midnight (00:00) in the deployment timezone, excludes the
selected day, and shows that exact boundary beside the count. Browser timezone
differences do not change the selected deployment calendar date.

The read-only API is `GET /api/maintenance/{dataset}/{type}/preview`, with these
accepted combinations:

| Dataset | Type | Counted records |
|---|---|---|
| customers | orphan | Customers with no customer list memberships |
| customers | blocklisted | Customers whose status is blocklisted |
| subscriptions | unconfirmed | Unconfirmed memberships in double-opt-in lists, created strictly before `before_date` |
| analytics | all / views / clicks | Campaign view and/or link-click records created strictly before `before_date` |

`before_date` is required for subscriptions and analytics, in RFC3339 format.
Invalid datasets, types, or dates return HTTP 400. The response envelope is
`{"data":{"count":2,"views":1,"clicks":1}}`; views/clicks are zero for other
datasets. Counts come directly from database records, even when slow-query caching
is enabled. Deleting customers also deletes subscriptions and other dependent
records according to existing foreign-key behavior; the displayed customer count
does not include those related rows. Historical analytics with nullable customer
references remain, as before.

A preview is a query-time snapshot. The delete action matches the original
conditions again when executed, so newly added/changed records may affect the
final count. The confirmation includes this distinction and the exact cutoff in
the deployment timezone. Previewing never performs cleanup or changes records.
