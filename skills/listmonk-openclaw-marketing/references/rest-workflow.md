# listmonk REST workflow for OpenClaw

## Python CLI

The skill ships with a modular Python CLI:

- `run_marketing_flow.py` for the end-to-end flow
- `ensure_list.py`
- `import_subscribers.py`
- `clone_template.py`
- `create_campaign.py`
- `update_campaign_status.py`
- `fetch_campaign_reports.py`

HTTP handling is zero-dependency, and Excel mode requires `openpyxl`:

```shell
pip install openpyxl
```

Then inspect the end-to-end runner:

```shell
python3 scripts/run_marketing_flow.py --help
```

End-to-end JSON input example:

```shell
python3 scripts/run_marketing_flow.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-name "OpenClaw Launch" \
  --subscribers-file ./subscribers.json \
  --source-campaign-name "复制用模板" \
  --campaign-name "OpenClaw Launch Campaign" \
  --auto-start \
  --report-from "2026-03-01" \
  --report-to "2026-03-31" \
  --verbose
```

When the reusable blueprint is an existing campaign rather than a template-library entry, the workflow inherits subject, body, content type, template ID, messenger, tags, and send-limit settings from that source campaign. Explicit CLI fields win if both are supplied.

End-to-end template-clone example:

```shell
python3 scripts/run_marketing_flow.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-name "OpenClaw Launch" \
  --subscribers-file ./subscribers.json \
  --source-template-id 4 \
  --new-template-name "OpenClaw Launch Template" \
  --campaign-name "OpenClaw Launch Campaign" \
  --subject "Launch Day" \
  --daily-send-limit 500 \
  --daily-resume-time "09:00" \
  --auto-start \
  --report-from "2026-03-01" \
  --report-to "2026-03-31" \
  --verbose
```

`subscribers.json` should be a JSON array like:

```json
[
  {
    "email": "jane@example.com",
    "name": "Jane",
    "attribs": {
      "city": "Shanghai"
    }
  }
]
```

End-to-end Excel input example:

```shell
python3 scripts/run_marketing_flow.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-name "OpenClaw Launch" \
  --excel-file ./subscribers.xlsx \
  --excel-sheet "Sheet1" \
  --email-column "AG" \
  --name-column "AE" \
  --header-row 2 \
  --source-campaign-name "复制用模板" \
  --campaign-name "OpenClaw Launch Campaign" \
  --auto-start \
  --report-from "2026-03-01" \
  --report-to "2026-03-31" \
  --verbose
```

Excel mode rules:

- Only `.xlsx` files are supported.
- `--email-column` is required.
- `--name-column` is optional.
- Column selectors can be header names like `邮箱` or letters like `A`.
- All other non-empty columns are added to subscriber `attribs`.
- Duplicate emails are deduplicated by default. Use `--no-dedupe-by-email` to disable that behavior.
- The script returns `imported_count`, `skipped_rows`, and `failed_rows` in the final JSON output.

## Modular script examples

Ensure a list exists:

```shell
python3 scripts/ensure_list.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-name "OpenClaw Launch" \
  --list-type private \
  --list-optin single \
  --list-status active
```

Import subscribers:

```shell
python3 scripts/import_subscribers.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-id 12 \
  --subscribers-file ./subscribers.json
```

Clone a template:

```shell
python3 scripts/clone_template.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --source-template-id 4 \
  --new-template-name "OpenClaw Launch Template"
```

Create a campaign:

```shell
python3 scripts/create_campaign.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-id 12 \
  --source-campaign-name "复制用模板" \
  --campaign-name "OpenClaw Launch Campaign" \
  --subject "Launch Day"
```

Fetch reports:

```shell
python3 scripts/fetch_campaign_reports.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --campaign-id 31 \
  --report-from "2026-03-01" \
  --report-to "2026-03-31"
```

## 1. Validate the token

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/lists?minimal=true&per_page=all"
```

Expected outcome:

- `200` means the token is valid and the service account has at least list read access.
- `403` usually means the token is invalid or the service account lacks permission.

## 2. Find or create the target list

Find:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/lists?query=OpenClaw%20Launch&page=1&per_page=all"
```

Create:

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/lists" \
  -d '{
    "name": "OpenClaw Launch",
    "type": "private",
    "optin": "single",
    "status": "active",
    "tags": ["openclaw"]
  }'
```

## 3. Add subscribers

For small batches:

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/subscribers" \
  -d '{
    "email": "jane@example.com",
    "name": "Jane",
    "status": "enabled",
    "lists": [12],
    "preconfirm_subscriptions": true
  }'
```

The Python skill currently creates subscribers row-by-row with `POST /api/subscribers` so it can report precise row-level failures for JSON and Excel inputs.

## 4. Choose the campaign blueprint

If you are reusing an existing campaign as the blueprint, fetch it first:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns?query=%E5%A4%8D%E5%88%B6%E7%94%A8%E6%A8%A1%E6%9D%BF&page=1&per_page=all"
```

Then retrieve the exact campaign by ID:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/2"
```

If you are starting from a template-library entry instead, clone the template:

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/templates/4/clone" \
  -d '{
    "name": "OpenClaw Launch Template"
  }'
```

## 5. Create the campaign

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/campaigns" \
  -d '{
    "name": "OpenClaw Launch Campaign",
    "subject": "测试主题",
    "lists": [12],
    "type": "regular",
    "content_type": "html",
    "body": "# 我是一篇文章",
    "template_id": 1,
    "messenger": "email",
    "daily_send_limit": 500,
    "daily_resume_time": "09:00",
    "tags": ["test"]
  }'
```

Notes:

- Keep `daily_send_limit` and `daily_resume_time` set for regular email campaigns.
- When the template already contains the wrapper, leaving `body` empty is valid for template-driven campaign content in listmonk's current API shape.
- In source campaign mode, the workflow recreates a fresh campaign from the source campaign's fields and swaps in the target list. It does not duplicate the original campaign object in place.

## 6. Start or schedule the campaign

Start immediately:

```shell
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/campaigns/31/status" \
  -d '{"status":"running"}'
```

If `send_at` was set during campaign creation, the same status call keeps the campaign scheduled in listmonk.

## 7. Fetch analytics

Summary:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/31/report/summary?from=2026-03-01&to=2026-03-31"
```

Timeseries:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/31/report/timeseries?from=2026-03-01&to=2026-03-31"
```

Links:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/31/report/links?from=2026-03-01&to=2026-03-31"
```

Recipients:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/31/report/recipients?from=2026-03-01&to=2026-03-31&page=1&per_page=100"
```

If recipient analytics return a privacy-related `403`, degrade gracefully to summary, timeseries, and link reports.

## Local smoke test

The repository's `dev` stack is sufficient for a real smoke test without any external service:

1. Start an isolated Postgres + Mailhog stack:

```shell
docker compose -p listmonk-skilltest -f dev/docker-compose.yml up -d db mailhog
```

2. Start the backend with bootstrap credentials and an admin API user:

```shell
docker compose -p listmonk-skilltest -f dev/docker-compose.yml run -d --service-ports \
  -e LISTMONK_ADMIN_USER=skilladmin \
  -e LISTMONK_ADMIN_PASSWORD=skilladmin123 \
  -e LISTMONK_ADMIN_API_USER=skillapi \
  backend
```

3. Read the backend logs and capture the printed `LISTMONK_ADMIN_API_TOKEN`.

4. Use BasicAuth once against `/api/users` and `/api/users/{id}/integration-tokens` to create a Bearer token for smoke testing.

5. Run the modular chain:
   - `ensure_list.py`
   - `import_subscribers.py`
   - `clone_template.py`
   - `create_campaign.py`
   - `fetch_campaign_reports.py`

6. Run `run_marketing_flow.py` once more to prove the end-to-end runner works too.

The default smoke test does not need to send mail. Keep `--auto-start` off unless you explicitly want to validate campaign status transitions.
