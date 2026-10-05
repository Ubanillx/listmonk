# Integrating with external systems

In many environments, a mailing list manager's customer database is not run independently but as a part of an existing customer database or a CRM. There are multiple ways of keeping listmonk in sync with external systems.

## Using APIs

The [customer APIs](apis/customers.md) offers several APIs to manipulate the customers database, like addition, updation, and deletion. For bulk synchronisation, a CSV can be generated (and optionally zipped) and posted to the import API.

### OpenClaw workflow

OpenClaw can integrate directly against listmonk's standard `/api/*` endpoints. A common automation flow is:

1. Create or reuse a customer_list with the [customer_list APIs](apis/customer-lists.md).
2. Add or import customers with the [customer APIs](apis/customers.md) or [import APIs](apis/import.md).
3. Clone a base marketing template with the [template APIs](apis/templates.md).
4. Create a draft with `customer_list_ids`, `daily_send_limit`, and `daily_resume_time`. Select `smtp_source=personal|organization`, an organization `smtp_pool_id` when applicable, and the campaign-wide `smtp_rate_limit`.
5. Check the draft, preview, and SMTP overview. Start with `PUT /api/campaigns/{id}/status` and `status=running`, or save a future `send_at` and explicitly set `status=scheduled` when scheduling is requested.
6. Fetch delivery and engagement analytics with single-campaign or cross-campaign [report APIs](apis/campaigns.md), optionally including approximate open locations.

For OpenClaw, create a personal API key in `Profile -> API Keys` for the user who owns the target workspace and SMTP account. Bind the key to the intended personal or organization workspace, select its scopes, and set its expiry month. A personal key cannot switch workspaces. Legacy API-user Bearer tokens remain appropriate for administrator-managed internal service accounts.

The recommended minimum permissions for a marketing automation service account are:

- `customer_lists:read` and `customer_lists:write`
- `customers:write` and `customers:import`
- `templates:read` and `templates:write`
- `campaigns:read`, `campaigns:write`, and `campaigns:analytics`
- `campaigns:send` when OpenClaw may start or schedule a campaign

Add `customers:read` when reading or reusing existing customers, and `campaigns:recipients` when recipient-level reports are needed. Add media scopes only when the automation uploads or reads media. Scopes do not replace user roles: sending, scheduling, testing, control, analytics, sensitive customer data, and mailbox use retain their separate business permissions.

Personal keys cannot configure SMTP or reply mailboxes, call `/api/dashboard/*`, or access `/api/pools/*` and `/api/org-pool-allocations/*`. Use preconfigured senders and authorized first-level public-pool audiences; the ordinary list/import CLI handles private customers only.

For today's delivery and engagement, call `/api/campaigns/report/summary` (and `timeseries`, `links`, `geo`, or `recipients`) with explicit timezone-aware `from`/`to` values. Repeat `id` for selected campaigns or omit IDs for the authorized workspace report set. Do not filter by campaign creation time: older campaigns can produce events today. Date-only endpoints are midnight boundaries, not whole-day expansions.

The repository bundles `skills/listmonk-openclaw-marketing/SKILL.md`, a maintained API reference, workflow examples, and Python scripts. The report CLI supports a single campaign, multiple campaign IDs, or the authorized workspace, optional geographic aggregates, and explicit recipient pagination.

## Interacting directly with the DB

listmonk uses tables with simple schemas to represent customers (`customers`), customer_lists (`customer_lists`), and subscriptions (`customer_list_memberships`). It is easy to add, update, and delete customer information directly with the database tables for advanced usecases. See the [table schemas](https://github.com/knadh/listmonk/blob/master/schema.sql) for more information.
