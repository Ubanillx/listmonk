---
name: listmonk-openclaw-marketing
description: Use when OpenClaw needs to operate listmonk for marketing automation: create or reuse subscriber lists, add subscribers, clone templates, create campaigns, start or schedule sends, and fetch campaign analytics using listmonk's REST APIs and Bearer integration tokens.
---

# listmonk OpenClaw Marketing

Use this skill when an agent needs to drive listmonk directly for outbound marketing workflows.

## Required inputs

- `base_url`: listmonk base URL, for example `https://listmonk.example.com`
- `bearer_token`: integration token created for a dedicated listmonk API user
- One of `list_id` or `list_name`
- One subscriber input source:
  - `subscribers_file`: JSON array of subscribers
  - `excel_file`: `.xlsx` file with subscriber rows
- `source_template_id`
- `new_template_name`
- `campaign_name`
- `subject`
- `daily_send_limit`
- `daily_resume_time`
- `auto_start`

## Workflow

1. Validate API access with `GET /api/lists?minimal=true&per_page=all`.
2. Reuse an existing list when `list_id` is provided.
3. When only `list_name` is provided, search `GET /api/lists` first and reuse an exact match when found.
4. If no matching list exists, create one with `POST /api/lists`.
5. Import subscribers:
   - If `subscribers_file` is provided, load the JSON array and create subscribers with `POST /api/subscribers`.
   - If `excel_file` is provided, parse the `.xlsx` file with `openpyxl`, map one email column and an optional name column, and convert all other non-empty columns into subscriber `attribs`.
   - Create subscribers with `POST /api/subscribers`, capturing imported, skipped, and failed rows.
6. Clone the base template with `POST /api/templates/{id}/clone`.
7. Create the campaign with `POST /api/campaigns`, always setting:
   - `lists`
   - `template_id`
   - `daily_send_limit`
   - `daily_resume_time`
   - `type=regular`
   - `messenger=email` unless the caller explicitly requests another messenger
   - Override campaign-level configuration only. Do not modify the cloned template body by default.
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
Authorization: Bearer <integration_token>
```

The Bearer token inherits the permissions of the API user it is bound to. If a request fails with `403`, report which permission is missing rather than retrying blindly.

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
- If recipient analytics fail with a privacy or tracking error, fall back to summary, timeseries, and link analytics.
- Do not use subscriber SQL query APIs unless the caller explicitly needs them and the service account is trusted for `subscribers:sql_query`.
- If `openpyxl` is missing and Excel mode is requested, fail fast with an installation hint.

## Response contract

Return a compact object containing:

- `import_source`
- `list_id`
- `template_id`
- `campaign_id`
- `status`
- `imported_count`
- `skipped_rows`
- `failed_rows`
- optional `report_summary`

## References

- Run `scripts/run_marketing_flow.py --help` for the CLI version of this workflow.
- Read `references/rest-workflow.md` for concrete request/response shapes and a recommended OpenClaw sequence.
