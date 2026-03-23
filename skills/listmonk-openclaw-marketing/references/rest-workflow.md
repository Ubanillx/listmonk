# listmonk REST workflow for OpenClaw

## Python CLI

The skill ships with a Python CLI. HTTP handling is zero-dependency, and Excel mode requires `openpyxl`:

```shell
pip install openpyxl
```

Then:

```shell
python3 scripts/run_marketing_flow.py --help
```

JSON input example:

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

Excel input example:

```shell
python3 scripts/run_marketing_flow.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --list-name "OpenClaw Launch" \
  --excel-file ./subscribers.xlsx \
  --excel-sheet "Sheet1" \
  --email-column "邮箱" \
  --name-column "姓名" \
  --header-row 1 \
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

Excel mode rules:

- Only `.xlsx` files are supported.
- `--email-column` is required.
- `--name-column` is optional.
- Column selectors can be header names like `邮箱` or letters like `A`.
- All other non-empty columns are added to subscriber `attribs`.
- Duplicate emails are deduplicated by default. Use `--no-dedupe-by-email` to disable that behavior.
- The script returns `imported_count`, `skipped_rows`, and `failed_rows` in the final JSON output.

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

## 4. Clone the source template

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
    "subject": "Launch Day",
    "lists": [12],
    "type": "regular",
    "content_type": "richtext",
    "body": "",
    "template_id": 25,
    "messenger": "email",
    "daily_send_limit": 500,
    "daily_resume_time": "09:00"
  }'
```

Notes:

- Keep `daily_send_limit` and `daily_resume_time` set for regular email campaigns.
- When the template already contains the wrapper, leaving `body` empty is valid for template-driven campaign content in listmonk's current API shape.
- The workflow clones the template and creates a fresh campaign. It does not clone an existing campaign object.

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
