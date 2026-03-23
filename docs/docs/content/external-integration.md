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

For service-to-service access, prefer Bearer integration tokens bound to a dedicated API user. The token inherits that user's role and list permissions, making it easy to constrain OpenClaw to specific lists or campaign capabilities.

The recommended minimum permissions for a marketing automation service account are:

- `templates:get`
- `templates:manage`
- `campaigns:get`
- `campaigns:manage`
- `campaigns:get_analytics`
- `subscribers:get`
- `subscribers:manage`
- list-level `get` and `manage` permissions for the lists OpenClaw should control

## Interacting directly with the DB

listmonk uses tables with simple schemas to represent subscribers (`subscribers`), lists (`lists`), and subscriptions (`subscriber_lists`). It is easy to add, update, and delete subscriber information directly with the database tables for advanced usecases. See the [table schemas](https://github.com/knadh/listmonk/blob/master/schema.sql) for more information.
