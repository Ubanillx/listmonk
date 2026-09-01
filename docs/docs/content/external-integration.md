# Integrating with external systems

In many environments, a mailing list manager's subscriber database is not run independently but as a part of an existing customer database or a CRM. There are multiple ways of keeping listmonk in sync with external systems.

## Using APIs

The [subscriber APIs](apis/subscribers.md) offers several APIs to manipulate the subscribers database, like addition, updation, and deletion. For bulk synchronisation, a CSV can be generated (and optionally zipped) and posted to the import API.

### OpenClaw workflow

OpenClaw can integrate directly against listmonk's standard `/api/*` endpoints. A common automation flow is:

1. Create or reuse a list with the [list APIs](apis/lists.md).
2. Add or import subscribers with the [subscriber APIs](apis/subscribers.md) or [import APIs](apis/import.md).
3. Clone a base marketing template with the [template APIs](apis/templates.md).
4. Create a campaign with `daily_send_limit` and `daily_resume_time` set.
5. Start the campaign with `PUT /api/campaigns/{id}/status`.
6. Fetch delivery and engagement analytics with the campaign report endpoints.

For OpenClaw, create a personal API key in `Profile -> API Keys` for the user who owns the target workspace and SMTP account. Bind the key to the intended personal or organization workspace, select its scopes, and set its expiry month. A personal key cannot switch workspaces. Legacy API-user Bearer tokens remain appropriate for administrator-managed internal service accounts.

The recommended minimum permissions for a marketing automation service account are:

- `lists:read` and `lists:write`
- `subscribers:write` and `subscribers:import`
- `templates:read` and `templates:write`
- `campaigns:read`, `campaigns:write`, and `campaigns:analytics`
- `campaigns:send` when OpenClaw may start or schedule a campaign

Add `subscribers:read` and `campaigns:recipients` only when recipient-level data is required. Add media scopes only when the automation uploads or reads media.

## Interacting directly with the DB

listmonk uses tables with simple schemas to represent subscribers (`subscribers`), lists (`lists`), and subscriptions (`subscriber_lists`). It is easy to add, update, and delete subscriber information directly with the database tables for advanced usecases. See the [table schemas](https://github.com/knadh/listmonk/blob/master/schema.sql) for more information.
