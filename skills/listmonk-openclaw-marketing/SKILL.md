---
name: listmonk-openclaw-marketing
description: Use when OpenClaw needs to operate listmonk for marketing automation with listmonk's REST APIs and workspace-bound personal API keys. Supports modular steps such as finding or creating lists, importing subscribers, cloning templates, creating campaigns, reusing existing campaign blueprints, updating campaign status, and fetching analytics, as well as an end-to-end workflow runner.
---

# listmonk OpenClaw Marketing

Use this skill when an agent needs to drive listmonk directly for outbound marketing workflows. This skill now supports both:

- an end-to-end workflow runner for the common happy path
- step-by-step scripts for cases where OpenClaw needs finer control

## Required inputs

- `base_url`: listmonk base URL, for example `https://listmonk.example.com`
- `bearer_token`: personal API key created in listmonk at `Profile -> API Keys`
- optional `organization_id`: only for a legacy service token; a personal key is already bound to one workspace
- One of `list_id` or `list_name`
- One subscriber input source:
  - `subscribers_file`: JSON array of subscribers
  - `excel_file`: `.xlsx` file with subscriber rows
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
  - Reuse a list by ID or find/create a list by name.
- `scripts/import_subscribers.py`
  - Import subscribers from JSON or `.xlsx` through listmonk's native batch importer, with pre-validation plus importer-log reporting.
- `scripts/clone_template.py`
  - Clone a base template into a new template.
- `scripts/create_campaign.py`
  - Create a campaign from direct fields or from an existing campaign blueprint.
- `scripts/update_campaign_status.py`
  - Start or schedule a campaign by setting its status.
- `scripts/fetch_campaign_reports.py`
  - Fetch summary, timeseries, link, and recipient analytics.
- `scripts/run_marketing_flow.py`
  - Orchestrate the full list -> subscribers -> template -> campaign -> report flow.

The shared implementation lives under `scripts/listmonk_marketing/`. When updating behavior, prefer changing the shared package instead of duplicating logic in multiple entrypoints.

## Workflow

1. Validate API access with `GET /api/lists?minimal=true&per_page=all`.
2. Reuse an existing list when `list_id` is provided.
3. When only `list_name` is provided, search `GET /api/lists` first and reuse an exact match when found.
4. If no matching list exists, create one with `POST /api/lists`.
5. Import subscribers:
   - If `subscribers_file` is provided, load the JSON array and normalize it into a temporary CSV for batch import.
   - If `excel_file` is provided, parse the `.xlsx` file with `openpyxl`, map one email column and an optional name column, and convert all other non-empty columns into subscriber `attribs`.
   - Upload the generated CSV with `POST /api/import/subscribers`, then poll the import status and logs until the batch finishes.
6. Source selection:
   - If `source_template_id` is provided, clone the base template with `POST /api/templates/{id}/clone`.
   - If `source_campaign_id` or `source_campaign_name` is provided, fetch that campaign and use it as a blueprint for the new campaign.
7. Create the campaign with `POST /api/campaigns`, always setting:
   - `lists`
   - `name`
   - `daily_send_limit`
   - `daily_resume_time`
   - `type=regular`
   - `messenger=email` unless the caller explicitly requests another messenger
   - In source campaign mode, keep the source campaign's content settings by default and only replace the target list plus any explicitly provided CLI overrides.
8. If `auto_start=true` and `send_at` is empty, start with `PUT /api/campaigns/{id}/status` and body `{"status":"running"}`.
9. If `auto_start=true` and `send_at` is set, switch status with `PUT /api/campaigns/{id}/status`; listmonk will keep it as `scheduled`.
10. Fetch analytics with:
   - `GET /api/campaigns/{id}/report/summary`
   - `GET /api/campaigns/{id}/report/timeseries`
   - `GET /api/campaigns/{id}/report/links`
   - `GET /api/campaigns/{id}/report/recipients`

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

The existing API-user tokens remain supported for internal service accounts and
retain their legacy role/list-permission behavior. They may supply
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
- `name_column` is optional and may be a header name or column letter.
- `header_row` defaults to `1`.
- `start_row` defaults to `header_row + 1`.
- Other non-empty columns are converted into subscriber `attribs`.
- Duplicate emails are deduplicated by default before API calls.

## Failure handling

- If list creation fails because the list already exists, re-query lists and reuse the matching list.
- If template clone succeeds but campaign creation fails, return the created `template_id` so the caller can retry or clean up.
- If source campaign lookup is by name, require an exact name match before reusing it.
- If recipient analytics fail with a privacy or tracking error, fall back to summary, timeseries, and link analytics.
- Do not use subscriber SQL query APIs unless the caller explicitly needs them and the service account is trusted for `subscribers:sql_query`.
- If `openpyxl` is missing and Excel mode is requested, fail fast with an installation hint.
- If bulk import fails with `403`, report that the key is missing `subscribers:import`.
- If campaign start or scheduling fails with `403`, report that the key is missing `campaigns:send`.

## Response contract

Each script writes compact JSON to stdout on success and JSON to stderr on failure.

The workflow runner returns a compact object containing:

- `import_source`
- `list_id`
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
