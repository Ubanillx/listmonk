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
