# listmonk REST workflow for OpenClaw

## Python CLI

The skill ships with a modular Python CLI:

- `run_marketing_flow.py` for the end-to-end flow
- `ensure_list.py`
- `import_customers.py`
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

Create a personal key in `Profile -> API Keys` before running the scripts. Pick
the personal or organization workspace that OpenClaw should use, select the
needed scopes, and copy the value shown once into the skill-local `.env` file
(see `.env.example`):

```dotenv
LISTMONK_BASE_URL=https://listmonk.example.com
LISTMONK_BEARER_TOKEN=lmpk_your_personal_api_key
```

A personal key already selects its bound workspace; do not pass a different
`--organization-id`. The scripts use CLI flags first, then process environment
variables, then `.env` values.

Legacy internal service tokens remain supported. They may select an organization
workspace with `--organization-id 42` or `LISTMONK_ORGANIZATION_ID=42`.

End-to-end JSON input example:

```shell
python3 scripts/run_marketing_flow.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --customer_list-name "OpenClaw Launch" \
  --customers-file ./customers.json \
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
  --customer_list-name "OpenClaw Launch" \
  --customers-file ./customers.json \
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

`customers.json` should be a JSON array like:

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
  --customer_list-name "OpenClaw Launch" \
  --excel-file ./customers.xlsx \
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
- All other non-empty columns are added to customer `attribs`.
- Duplicate emails are deduplicated by default. Use `--no-dedupe-by-email` to disable that behavior.
- The script returns `imported_count`, `skipped_rows`, and `failed_rows` in the final JSON output.

## Modular script examples

Ensure a customer_list exists:

```shell
python3 scripts/ensure_list.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --customer_list-name "OpenClaw Launch" \
  --customer_list-type private \
  --customer_list-optin single \
  --customer_list-status active
```

Import customers:

```shell
python3 scripts/import_customers.py \
  --base-url "https://listmonk.example.com" \
  --bearer-token "$TOKEN" \
  --customer_list-id 12 \
  --customers-file ./customers.json
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
  --customer_list-id 12 \
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
  "$BASE_URL/api/customer-lists?minimal=true&per_page=all"
```

Expected outcome:

- `200` means the key is valid and has `customer_lists:read` access in its bound workspace.
- `403` means the key is invalid, expired, bound to another workspace, or missing a scope.

## 2. Find or create the target customer_list

Find:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/customer-lists?query=OpenClaw%20Launch&page=1&per_page=all"
```

Create:

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/customer-lists" \
  -d '{
    "name": "OpenClaw Launch",
    "type": "private",
    "optin": "single",
    "status": "active",
    "tags": ["openclaw"]
  }'
```

## 3. Add customers

The skill now converts JSON or Excel input into a temporary CSV and uses listmonk's native bulk import API:

```shell
curl -X POST -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/import/customers" \
  -F 'params={"mode":"subscribe","subscription_status":"confirmed","customer_list_ids":[12],"overwrite_userinfo":false,"overwrite_subscription_status":true,"field_map":{"email":"A","name":"B","customer_code":"C"}}' \
  -F "file=@/tmp/customers.csv"
```

Then poll until the import completes:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/import/customers"
```

And fetch logs if you need per-line importer diagnostics:

```shell
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/import/customers/logs"
```

The key used by this step needs the `customers:import` scope.

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
    "customerLists": [12],
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
- In source campaign mode, the workflow recreates a fresh campaign from the source campaign's fields and swaps in the target customer_list. It does not duplicate the original campaign object in place.

## 6. Start or schedule the campaign

Start immediately:

```shell
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "$BASE_URL/api/campaigns/31/status" \
  -d '{"status":"running"}'
```

If `send_at` was set during campaign creation, the same status call keeps the campaign scheduled in listmonk. Starting or scheduling requires the `campaigns:send` scope.

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

### Report `from`/`to` parameters

- Pass `from`/`to` on every report request.
- Use `YYYY-MM-DD` (as above) or RFC 3339 (`2026-09-01T00:00:00%2B08:00`).
- Build the dates with `date +%F`:
  ```shell
  FROM=$(date +%F)   # 2026-09-01
  curl -H "Authorization: Bearer $TOKEN" \
    "$BASE_URL/api/campaigns/31/report/summary?from=$FROM&to=$FROM"
  ```
- Campaign **customer_list** endpoint uses Unix seconds: `/api/campaigns?from=<epoch>&to=<epoch>`.

### "Today" dashboard query (personal API key)

Personal API keys have no dashboard endpoint; use the campaign customer_list plus
per-campaign reports:

```shell
# 1) campaigns created/updated today (epoch seconds)
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns?from=$(date -d 'today 00:00:00' +%s)&to=$(date -d 'today 23:59:59' +%s)&per_page=50"

# 2) per-campaign detail with date strings
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/api/campaigns/31/report/summary?from=$(date +%F)&to=$(date +%F)"
```

The campaign customer_list payload already includes `status`, `sent`, `to_send`, `views`,
`clicks`, `bounces`, and target `customer_lists` — enough for a quick daily overview.

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
   - `import_customers.py`
   - `clone_template.py`
   - `create_campaign.py`
   - `fetch_campaign_reports.py`

6. Run `run_marketing_flow.py` once more to prove the end-to-end runner works too.

The default smoke test does not need to send mail. Keep `--auto-start` off unless you explicitly want to validate campaign status transitions.
