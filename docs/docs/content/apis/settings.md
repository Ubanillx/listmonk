# API / Settings

Read and update the global application settings. Authentication, response envelopes and error codes are described in the [API introduction](apis.md).

Every endpoint on this page lives in the authenticated `/api` group (`cmd/handlers.go:93`) and requires a session or a legacy API-user token whose role carries the `settings` permissions. Personal API keys created under `Profile -> API Keys` cannot call any of them and receive HTTP 403 (`cmd/api_keys.go:98`, `cmd/api_keys.go:107`).

| Method | Endpoint                                                       | Permission        | Description                                                |
|--------|----------------------------------------------------------------|-------------------|------------------------------------------------------------|
| GET    | [/api/settings](#get-apisettings)                              | `settings:get`    | Get all settings with secrets masked                       |
| PUT    | [/api/settings](#put-apisettings)                              | `settings:manage` | Replace the complete settings object                       |
| PUT    | [/api/settings/{key}](#put-apisettingskey)                     | `settings:manage` | Update a single settings key                               |
| POST   | [/api/settings/smtp/test](#post-apisettingssmtptest)           | `settings:manage` | Send a test e-mail using unsaved SMTP settings             |
| POST   | [/api/settings/bounce/mailbox/test](#post-apisettingsbouncemailboxtest) | `settings:manage` | Test a POP bounce mailbox (see the Bounces page)   |
| POST   | [/api/settings/reply-ai/models](#post-apisettingsreply-aimodels) | `settings:manage` | List the models advertised by the Reply-AI gateway         |
| POST   | [/api/settings/reply-ai/test](#post-apisettingsreply-aitest)   | `settings:manage` | Test a Reply-AI model end to end                           |

______________________________________________________________________

#### GET /api/settings

Returns the complete settings object. Requires `settings:get` (`cmd/handlers.go:115`, `internal/auth/models.go:91`).

Settings are stored as flat, dotted keys (`app.site_name`, `bounce.mailboxes`, `reply_ai`, ...). The response is a map of those keys, which are the same keys accepted by `PUT /api/settings/{key}` (`queries/misc.sql:7`, `models/settings.go:47`).

Stored secrets are never returned in clear text. Each of them is replaced by a run of `•` characters, one per rune of the stored value, so the mask reveals the password length (`cmd/settings.go:30`, `cmd/settings.go:65`):

| Key                                  | Masked at          |
|--------------------------------------|--------------------|
| `smtp[].password`                    | `cmd/settings.go:66` |
| `bounce.mailboxes[].password`        | `cmd/settings.go:69` |
| `messengers[].password`              | `cmd/settings.go:72` |
| `upload.s3.aws_secret_access_key`    | `cmd/settings.go:76` |
| `bounce.sendgrid_key`                | `cmd/settings.go:77` |
| `bounce.postmark.password`           | `cmd/settings.go:78` |
| `bounce.forwardemail.key`            | `cmd/settings.go:79` |
| `reply_ai.api_key`                   | `cmd/settings.go:80` |
| `security.captcha.hcaptcha.secret`   | `cmd/settings.go:81` |
| `security.oidc.client_secret`        | `cmd/settings.go:82` |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/settings'
```

##### Example Response

The response contains every key modelled by the server. A few orphan keys exist in the `settings` table that are not modelled, for example `upload.max_file_size` (`schema.sql:362`): they are neither returned here nor writable through this API. The excerpt below uses the values seeded by a default installation (`schema.sql:324`); the masked passwords there are the 8-character default `password`:

```json
{
  "data": {
    "app.site_name": "Mailing customer_list",
    "app.root_url": "http://localhost:9000",
    "app.from_email": "listmonk <noreply@listmonk.yoursite.com>",
    "smtp": [
      {
        "enabled": true,
        "is_primary": true,
        "from_email": "listmonk <noreply@listmonk.yoursite.com>",
        "host": "smtp.yoursite.com",
        "port": 465,
        "auth_protocol": "plain",
        "username": "username",
        "password": "••••••••",
        "tls_type": "TLS"
      }
    ],
    "bounce.mailboxes": [
      {
        "enabled": false,
        "type": "pop",
        "host": "pop.yoursite.com",
        "port": 995,
        "username": "username",
        "password": "••••••••",
        "tls_enabled": true
      }
    ],
    "reply_ai": {
      "enabled": false,
      "base_url": "",
      "api_key": "",
      "model": "",
      "timeout": "15s",
      "min_confidence": 0.98
    }
  }
}
```

______________________________________________________________________

#### PUT /api/settings

Replaces the settings object. Requires `settings:manage` (`cmd/handlers.go:116`, `internal/auth/models.go:92`).

!!! warning
    The request body is a **complete** settings object. Omitted fields are bound as zero values, and every key present in the marshaled object is written back to the `settings` table, so always start from `GET /api/settings`, edit the result, and PUT that object back. Only `customer.custom_fields` is preserved from the stored settings and cannot be changed here (`cmd/settings.go:100`); manage it through `/api/custom-fields` instead.

The scalar secret fields keep their stored value when they are empty (`""`) in the request: `upload.s3.aws_secret_access_key`, `bounce.sendgrid_key`, `bounce.postmark.password`, `bounce.forwardemail.key`, `security.captcha.hcaptcha.secret` and `security.oidc.client_secret` (`cmd/settings.go:280`). The passwords inside arrays, `smtp[].password`, `bounce.mailboxes[].password` and `messengers[].password`, are kept only when the item carries the original `uuid` seen in the `GET` response; an item without a `uuid` is treated as a new entry, is assigned a fresh `uuid` and is stored with an empty password instead (`cmd/settings.go:146`, `cmd/settings.go:172`, `cmd/settings.go:216`, `cmd/settings.go:258`). `reply_ai.api_key` may be empty or fully masked to reuse the stored key (`cmd/settings.go:228`).

The object is validated before it is saved; invalid values return HTTP 400 with a localized message. Examples: at least one SMTP server has to be enabled, exactly one of them has to be primary, messenger and SMTP names have to be unique, a bounce mailbox cannot enable SSL/TLS and STARTTLS at the same time, CORS origins have to be `http(s)` URLs or `*`, and `app.cache_slow_queries_interval` is only validated as a cron expression when `app.cache_slow_queries` is `true` (`cmd/settings.go:109`, `cmd/settings.go:193`, `cmd/settings.go:328`, `cmd/settings.go:349`).

A successful save triggers an automatic restart of the app. If a campaign is running, the restart is deferred instead:

```json
{"data": true}
```

```json
{"data": {"needs_restart": true}}
```

(`cmd/settings.go:388`)

##### Example Request

```shell
# settings.json is the object returned by GET /api/settings with the intended changes.
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/settings' \
  -H 'Content-Type: application/json' \
  --data @settings.json
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### PUT /api/settings/{key}

Updates one settings key. Requires `settings:manage` (`cmd/handlers.go:117`).

##### Parameters

| Name | Type   | Required | Description                                              |
|------|--------|----------|----------------------------------------------------------|
| key  | String | Yes      | Settings key exactly as returned by `GET /api/settings`  |

The request body is the JSON value of that key itself, not an object that wraps it (`cmd/settings.go:374`). The key `customer.custom_fields` is rejected with HTTP 403 and has to be changed through `/api/custom-fields` (`cmd/settings.go:370`).

The response is the same restart notification as `PUT /api/settings` (`cmd/settings.go:385`).

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/settings/maintenance.db' \
  -H 'Content-Type: application/json' \
  --data '{"vacuum": true, "vacuum_cron_interval": "0 2 * * *"}'
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### POST /api/settings/smtp/test

Sends a test e-mail through the submitted SMTP settings without saving them. Requires `settings:manage` (`cmd/handlers.go:118`).

The body is one SMTP server object as it appears in `smtp[]` of `GET /api/settings`, plus the recipient address in the top-level `email` field (`cmd/settings.go:433`, `cmd/settings.go:439`). The test uses a temporary pool with a single connection and a 2-second idle/wait timeout (`cmd/settings.go:444`). A missing `email`, an unparsable body, or a server that cannot be initialized returns HTTP 400; a delivery failure returns HTTP 500 with the SMTP error (`cmd/settings.go:441`, `cmd/settings.go:450`, `cmd/settings.go:466`).

##### Parameters

| Field      | Type   | Required | Description                                      |
|------------|--------|----------|--------------------------------------------------|
| email      | String | Yes      | Recipient of the test e-mail                     |
| (rest)     | -      | Yes      | Any field of an `smtp[]` entry, e.g. `host`, `port`, `auth_protocol`, `username`, `password`, `from_email`, `tls_type` |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/settings/smtp/test' \
  -H 'Content-Type: application/json' \
  --data '{
    "host": "smtp.yoursite.com",
    "port": 465,
    "auth_protocol": "plain",
    "username": "username",
    "password": "password",
    "from_email": "listmonk <noreply@listmonk.yoursite.com>",
    "tls_type": "TLS",
    "email": "admin@yoursite.com"
  }'
```

##### Example Response

`data` contains the buffered server log lines (`cmd/settings.go:470`):

```json
{
  "data": [
    "..."
  ]
}
```

______________________________________________________________________

#### POST /api/settings/bounce/mailbox/test

Tests the POP mailbox from the unsaved bounce settings form. Requires `settings:manage` (`cmd/handlers.go:119`). The request and response format, the four reported steps, and the empty/masked password behavior are documented in [Test a POP bounce mailbox](../bounces.md#test-a-pop-bounce-mailbox) and its [Diagnostic API](../bounces.md#diagnostic-api) section.

______________________________________________________________________

#### POST /api/settings/reply-ai/models

Lists the models an OpenAI-compatible gateway advertises, using an unsaved settings form. Requires `settings:manage` (`cmd/handlers.go:122`). The probe never saves settings and the API key is only sent in the outbound `Authorization` header; it is never echoed back, stored or logged (`cmd/reply_ai_settings.go:56`, `cmd/reply_ai_settings.go:86`). See [AI classification of customer replies](../bounces.md#ai-classification-of-customer-replies) for the feature itself.

##### Parameters

| Field           | Type   | Required | Description                                                                                                                                   |
|-----------------|--------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| base_url        | String | Yes      | Gateway root such as `https://api.openai.com/v1`. When the root does not end in `/v1`, `<base_url>/v1/models` is tried as a fallback and the root that answered is reported in `suggested_base_url` (`internal/replyai/gateway.go:167`) |
| api_key         | String | No       | Blank or fully masked reuses the stored `reply_ai.api_key`; if no key is stored the request fails with HTTP 400 (`cmd/reply_ai_settings.go:71`) |
| timeout         | String | No       | Go duration string, `15s` by default (`cmd/reply_ai_settings.go:50`)                                                                            |
| model           | String | No       | Accepted but not used by model discovery                                                                                                       |
| sample_text     | String | No       | Accepted but not used by model discovery                                                                                                       |
| expected_intent | String | No       | If set, has to be `unsubscribe`, `complaint`, `product_complaint` or `other`, otherwise HTTP 400 (`cmd/reply_ai_settings.go:42`)                |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/settings/reply-ai/models' \
  -H 'Content-Type: application/json' \
  --data '{"base_url": "https://api.openai.com/v1", "api_key": ""}'
```

##### Example Response

`models` entries carry the gateway's model id, its optional `owned_by`, and a `chat_hint` that is `false` for model families that can never classify a reply (`internal/replyai/gateway.go:93`, `internal/replyai/gateway.go:501`). `chat_candidates` counts the entries with `chat_hint: true`. `suggested_base_url` is only present when the gateway had to be reached through its `/v1` mount.

```json
{
  "data": {
    "api_root": "https://api.openai.com/v1",
    "models": [
      {
        "id": "...",
        "owned_by": "...",
        "chat_hint": true
      }
    ],
    "chat_candidates": 1
  }
}
```

Gateway authentication failures are mapped to HTTP 400; a gateway without a model list path, or any other gateway error, is mapped to HTTP 502 (`cmd/reply_ai_settings.go:144`).

______________________________________________________________________

#### POST /api/settings/reply-ai/test

Tests configuration, gateway reachability, the advertised model list and a real classification round trip, and always answers HTTP 200 with a per-step report so every part of the configuration can be checked independently (`cmd/reply_ai_settings.go:112`). Requires `settings:manage` (`cmd/handlers.go:123`). Only unusable input (missing `base_url`, key or `model`) is rejected with HTTP 400 (`cmd/reply_ai_settings.go:124`).

##### Parameters

| Field           | Type   | Required | Description                                                                                                                          |
|-----------------|--------|----------|--------------------------------------------------------------------------------------------------------------------------------------|
| base_url        | String | Yes      | Same as for model discovery                                                                                                          |
| model           | String | Yes      | Model to test. The test continues even when the gateway does not advertise it, but reports a `warning` (`internal/replyai/gateway.go:335`) |
| api_key         | String | No       | Blank or fully masked reuses the stored `reply_ai.api_key`                                                                            |
| timeout         | String | No       | Go duration string, `15s` by default                                                                                                  |
| sample_text     | String | No       | Reply body sent to the model. When empty, a built-in unsubscribe sample is used (`internal/replyai/gateway.go:80`)                     |
| expected_intent | String | No       | Intent the decision is compared against. It defaults to `unsubscribe` when both `sample_text` and the intent are empty; an empty value with a supplied `sample_text` matches any intent (`internal/replyai/gateway.go:343`) |

##### Response fields

| Field             | Description                                                                                                        |
|-------------------|--------------------------------------------------------------------------------------------------------------------|
| status            | `success`, `warning` or `failed`. `failed` if any step failed, `warning` if any step warned (`internal/replyai/gateway.go:402`) |
| steps[]           | Four checkpoints named `config`, `gateway`, `model` and `completion`, each with `name`, `status`, a machine-readable `reason` code (for example `invalid_base_url`, `missing_api_key`, `unreachable`, `timeout`, `auth_rejected`, `no_model_list`, `model_not_advertised`, `invalid_classification`, `unexpected_intent`), a redacted `detail` and `latency_ms` (`internal/replyai/gateway.go:26`, `internal/replyai/gateway.go:44`) |
| api_root          | Root the probe actually used                                                                                        |
| suggested_base_url | Present when the gateway only answered on its `/v1` mount                                                          |
| model             | Model under test                                                                                                    |
| sample            | Reply text that was sent to the model                                                                               |
| expected_intent   | Intent the decision was compared against                                                                            |
| matched           | Whether the returned intent equals `expected_intent`                                                                |
| decision          | The parsed classification: `intent`, `confidence` and `reason_code` (`internal/replyai/client.go:44`)               |
| response          | Bounded excerpt of the model's raw message                                                                          |
| model_count       | Number of models the gateway advertised (0 when the list was unavailable)                                            |
| latency_ms        | Total probe duration                                                                                                |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/settings/reply-ai/test' \
  -H 'Content-Type: application/json' \
  --data '{"base_url": "https://api.openai.com/v1", "api_key": "", "model": "gpt-4o-mini"}'
```
