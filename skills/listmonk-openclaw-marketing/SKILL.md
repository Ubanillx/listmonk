---
name: listmonk-openclaw-marketing
description: Use when OpenClaw needs to operate listmonk for marketing automation with listmonk's REST APIs and workspace-bound personal API keys. Supports modular steps such as finding or creating customer lists, importing subscribers, cloning templates, creating campaigns, reusing existing campaign blueprints, updating campaign status, and fetching analytics, as well as an end-to-end workflow runner. Consult this skill whenever the task mentions listmonk REST endpoints, customer_list_ids, customer_code, campaign scheduling, public pools, or listmonk campaign reports.
---

# listmonk OpenClaw Marketing

Use this skill when an agent needs to drive listmonk directly for outbound marketing workflows. This skill now supports both:

- an end-to-end workflow runner for the common happy path
- step-by-step scripts for cases where OpenClaw needs finer control

## Required inputs

- `base_url`: listmonk base URL, for example `https://listmonk.example.com`
- `bearer_token`: personal API key created in listmonk at `Profile -> API Keys`
- optional `organization_id`: only for a legacy service token; a personal key is already bound to one workspace
- One of `customer_list_id` or `customer_list_name`
- One customer input source:
  - `customers_file`: JSON array; every row must contain `email` and `customer_code`, with optional `name` and `attribs`
  - `excel_file`: `.xlsx` file with customer rows; `email_column` and `customer_code_column` are required, while `name_column` is optional
- One source mode:
  - `source_template_id` plus `new_template_name`
  - or `source_campaign_id`
  - or `source_campaign_name`
- `campaign_name`
- `auto_start`

## Configuration file

The scripts automatically load `.env` from this skill directory. Start with
`.env.example`, copy it to `.env`, and set the personal key value returned by
`Profile -> API Keys`:

```dotenv
LISTMONK_BASE_URL=https://listmonk.example.com
LISTMONK_BEARER_TOKEN=lmpk_your_personal_api_key
```

The key is shown only once after creation or rotation, so keep it in this
`.env` file or an equivalent secret store and do not paste it into prompts,
committed files, or logs. The repository ignores the skill-local `.env` file.
`LISTMONK_ORGANIZATION_ID` is only for legacy service tokens; never set it for
a personal key unless it matches the key's server-bound workspace. Command-line
flags override process environment variables, which override `.env` values.
Set `LISTMONK_ENV_FILE` when the file must live at a different path.

## Script layout

Use the smallest script that fits the task:

- `scripts/ensure_list.py`
  - Reuse a customer_list by ID or find/create a customer_list by name.
- `scripts/import_subscribers.py`
  - Import subscribers from JSON or `.xlsx`; use the native batch importer for native fields and direct customer creation when `attribs` must be preserved, with pre-validation plus importer-log reporting.
- `scripts/clone_template.py`
  - Clone a base template into a new template.
- `scripts/create_campaign.py`
  - Create a campaign from direct fields or from an existing campaign blueprint.
- `scripts/update_campaign_status.py`
  - Start or schedule a campaign by setting its status.
- `scripts/fetch_campaign_reports.py`
  - Fetch summary, timeseries, link, and recipient analytics.
- `scripts/run_marketing_flow.py`
  - Orchestrate the full customer_list -> customers -> template -> campaign -> report flow.

The shared implementation lives under `scripts/listmonk_marketing/`. When updating behavior, prefer changing the shared package instead of duplicating logic in multiple entrypoints.

## Workflow

1. Validate API access with `GET /api/customer-lists?minimal=true&per_page=all`.
2. Reuse an existing customer_list when `customer_list_id` is provided.
3. When only `customer_list_name` is provided, search `GET /api/customer-lists` first and reuse an exact match when found.
4. If no matching customer_list exists, create one with `POST /api/customer-lists`.
5. Import customers:
   - Require `email` and `customer_code` for every JSON row. Native batch import supports only `email`, `name`, and `customer_code`; it writes a temporary CSV with `customer_list_ids` and polls the importer status/logs.
   - For JSON rows containing non-empty `attribs`, use direct `POST /api/customers` calls so attributes are not lost. This is slower and per-row, but preserves the submitted data.
   - For `.xlsx`, map `email_column`, `customer_code_column`, and optional `name_column`. Other non-empty columns become `attribs` and therefore use the direct customer API path.
   - Normal workflow lists may be `private` or `public`. Do not pass `pool` or `org_pool_allocation` lists through this workflow; public-pool allocation uses the dedicated pool APIs and import rules.
6. Source selection:
   - If `source_template_id` is provided, clone the base template with `POST /api/templates/{id}/clone`.
   - If `source_campaign_id` or `source_campaign_name` is provided, fetch that campaign and use it as a blueprint for the new campaign.
7. Create the campaign with `POST /api/campaigns`, always setting:
   - `customer_list_ids`
   - `name`
   - `daily_send_limit`
   - `daily_resume_time`
   - `type=regular`
   - `messenger=email` unless the caller explicitly requests another messenger
   - Optional `reply_mailbox_id` only when it belongs to the API-key owner and is already configured in the workspace
   - In source campaign mode, keep the source campaign's content settings by default and only replace the target customer_list plus any explicitly provided CLI overrides.
8. If `auto_start=true` and `send_at` is empty, start with `PUT /api/campaigns/{id}/status` and body `{"status":"running"}`.
9. If `auto_start=true` and `send_at` is set, use the same endpoint with body `{"status":"scheduled"}`. The explicit scheduled status is required by the current API contract.
10. Fetch analytics with:
   - `GET /api/campaigns/{id}/report/summary`
   - `GET /api/campaigns/{id}/report/timeseries`
   - `GET /api/campaigns/{id}/report/links`
   - `GET /api/campaigns/{id}/report/recipients`

### Querying "today" data (dashboard view)

Personal API keys have no dashboard endpoint, and the current `GET /api/campaigns`
collection does not accept `from`/`to` time filters. Fetch the campaign collection with
`per_page=all` (optionally narrowing it with `status`, `query`, or `tags`), filter the
returned `created_at`/`updated_at` values client-side when present, then pull per-campaign
reports for the remaining IDs.

### Report `from`/`to` parameters

- Always pass `from` and `to` on all four report endpoints:
  - `GET /api/campaigns/{id}/report/summary?from=...&to=...`
  - `GET /api/campaigns/{id}/report/timeseries?from=...&to=...`
  - `GET /api/campaigns/{id}/report/links?from=...&to=...`
  - `GET /api/campaigns/{id}/report/recipients?from=...&to=...&page=1&per_page=100`
- Format `from`/`to` as `YYYY-MM-DD` (e.g. `2026-09-01`) or RFC 3339
  (e.g. `2026-09-01T00:00:00%2B08:00`).
- Do not send `from`/`to` to the campaign collection endpoint; those parameters belong to
  the four report endpoints.

## Authentication

Use this header on all authenticated requests:

```http
Authorization: Bearer <personal_api_key>
```

Personal API keys are bound to one personal or organization workspace, expire at
the end of a configured month, and can be revoked or rotated by their owner.
They can only call the selected business APIs and must include the scope needed
by each request. If a request fails with `403`, report the missing API key scope
or workspace mismatch rather than retrying blindly.

The main workflow needs `customer_lists:read` (and `customer_lists:write` when creating a
list), `customers:read`/`customers:write` for direct customer calls, `customers:import`
for native batch import, `templates:read`/`templates:write` for template cloning,
`campaigns:write` for campaign create/update/status requests, `campaigns:send` for
starting or scheduling, and `campaigns:analytics` for aggregate reports. Recipient
details additionally require the recipient-report scope and the server-side recipient,
customer, and tracking permissions. API-key scopes do not replace role action
permissions: the owning user must also be allowed to send/schedule and use the selected
SMTP/reply mailbox. Use `--reply-mailbox-id` only for an owner-configured mailbox;
personal keys cannot configure another user's SMTP or reply mailbox, so the workspace
owner must preconfigure those settings.

The existing API-user tokens remain supported for internal service accounts and
retain their legacy role/customer_list-permission behavior. They may supply
`--organization-id` (or `LISTMONK_ORGANIZATION_ID`) when they need to select an
organization workspace. Do not send a different organization ID with a personal
key: the server rejects it.

For local smoke tests only, BasicAuth may be used once to bootstrap a legacy
service token from an admin API user. Production OpenClaw integrations should
use a personal API key from the owner whose workspace and SMTP account will send
the campaign.

## Excel mode

- Only `.xlsx` files are supported.
- `openpyxl` is required for Excel mode.
- `email_column` is required and may be a header name or column letter.
- `customer_code_column` is required and may be a header name or column letter.
- `name_column` is optional and may be a header name or column letter.
- `header_row` defaults to `1`.
- `start_row` defaults to `header_row + 1`.
- Other non-empty columns are converted into customer `attribs`; rows with attributes use
  direct `POST /api/customers` calls because the native batch importer cannot preserve them.
- Duplicate emails are deduplicated by default before API calls.

## Failure handling

- If customer_list creation fails because the customer_list already exists, re-query customer_lists and reuse the matching customer_list.
- If template clone succeeds but campaign creation fails, return the created `template_id` so the caller can retry or clean up.
- If source campaign lookup is by name, require an exact name match before reusing it.
- If recipient analytics fail with a privacy or tracking error, fall back to summary, timeseries, and link analytics.
- Query today's overview with `/api/campaigns?per_page=all` and filter campaign timestamps client-side; pass `from`/`to` as date strings (`YYYY-MM-DD` or RFC 3339) only to report endpoints.
- Do not use customer SQL query APIs unless the caller explicitly needs them and the service account is trusted for `customers:sql_query`.
- If `openpyxl` is missing and Excel mode is requested, fail fast with an installation hint.
- If bulk import fails with `403`, report that the key is missing `customers:import` or the user's import permission.
- If campaign create/update fails with `403`, check `campaigns:write`; if start or scheduling fails, check `campaigns:send`, the user's send/schedule action permission, ownership, and the preconfigured SMTP/reply mailbox.

## Response contract

Each script writes compact JSON to stdout on success and JSON to stderr on failure.

The workflow runner returns a compact object containing:

- `import_source`
- `customer_list_id`
- `template_id`
- optional `source_campaign_id`
- `campaign_id`
- `status`
- `imported_count`
- `skipped_rows`
- `failed_rows`
- optional `report_summary`

## References

- Run `python3 scripts/run_marketing_flow.py --help` for the end-to-end CLI.
- Run `python3 scripts/<script>.py --help` for any modular step.
- Read `references/rest-workflow.md` for concrete request/response shapes, script examples, and the local smoke test procedure.
