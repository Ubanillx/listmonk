# API / System

Operational endpoints: server configuration and identity, dashboard data, custom field definitions, a live event stream, logs, and maintenance operations. Authentication, response envelopes and error codes are described in the [API introduction](apis.md).

None of these endpoints is public. They all sit in the authenticated `/api` group (`cmd/handlers.go:93`), so `/api/health` is a **protected** endpoint too; the unauthenticated health probe is `GET /health` without the `/api` prefix (`cmd/handlers.go:419`). Personal API keys created under `Profile -> API Keys` cannot call anything on this page and receive HTTP 403 (`cmd/api_keys.go:98`, `cmd/api_keys.go:107`).

| Method | Endpoint                                                          | Permission          | Description                                              |
|--------|-------------------------------------------------------------------|---------------------|----------------------------------------------------------|
| GET    | [/api/health](#get-apihealth)                                     | -                   | Health check                                             |
| GET    | [/api/config](#get-apiconfig)                                     | -                   | Bootstrap configuration for the admin UI                 |
| GET    | [/api/lang/{lang}](#get-apilanglang)                              | -                   | Language pack                                            |
| GET    | [/api/about](#get-apiabout)                                       | -                   | Build, runtime, database and host information            |
| GET    | [/api/dashboard/charts](#get-apidashboardcharts)                  | -                   | Dashboard chart series                                   |
| GET    | [/api/dashboard/counts](#get-apidashboardcounts)                  | -                   | Dashboard totals                                         |
| GET    | [/api/custom-fields](#get-apicustom-fields)                       | -                   | Customer custom field definitions                        |
| POST   | [/api/custom-fields](#post-apicustom-fields)                      | Platform admin      | Create a custom field                                    |
| PUT    | [/api/custom-fields/{key}](#put-apicustom-fieldskey)              | Platform admin      | Update a custom field                                    |
| DELETE | [/api/custom-fields/{key}](#delete-apicustom-fieldskey)           | Platform admin      | Deactivate a custom field                                |
| GET    | [/api/events](#get-apievents)                                     | `settings:get`      | Live server-sent event stream                            |
| GET    | [/api/logs](#get-apilogs)                                         | `settings:get`      | Buffered log lines                                       |
| POST   | [/api/admin/reload](#post-apiadminreload)                         | `settings:manage`   | Reload (restart) the app                                 |
| DELETE | [/api/maintenance/customers/{type}](#delete-apimaintenancecustomerstype) | `settings:maintain` | Delete orphaned or blocklisted customers        |
| DELETE | [/api/maintenance/analytics/{type}](#delete-apimaintenanceanalyticstype) | `settings:maintain` | Delete campaign views and link clicks           |
| DELETE | [/api/maintenance/subscriptions/unconfirmed](#delete-apimaintenancesubscriptionsunconfirmed) | `settings:maintain` | Delete stale unconfirmed memberships |
| POST   | [/api/logout](#post-apilogout)                                    | -                   | Destroy the browser session                              |

______________________________________________________________________

#### GET /api/health

Returns `{"data": true}` with HTTP 200 as soon as the HTTP server can answer (`cmd/handlers.go:448`). Requires authentication (`cmd/handlers.go:109`). Use the unauthenticated `GET /health` for load balancer and container probes.

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/health'
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### GET /api/config

Returns the configuration the admin UI needs to bootstrap. Requires authentication only; no permission is checked (`cmd/handlers.go:110`). The response contains no secrets (`cmd/admin.go:15`, `cmd/admin.go:42`).

| Field                                | Description                                                                                          |
|--------------------------------------|------------------------------------------------------------------------------------------------------|
| site_name                            | Configured site name                                                                                 |
| root_url                             | Root URL of the installation                                                                         |
| from_email                           | Default sender address                                                                               |
| lang                                 | Configured default language                                                                          |
| permissions                          | Raw permission catalog read from the embedded `permissions.json`, not the caller's own permissions (`cmd/init.go:502`) |
| has_legacy_user                      | `true` while `app.admin_username` or `app.admin_password` still exist in the config file or are provided by the matching environment variable (`LISTMONK_app_admin_username` / `LISTMONK_app_admin_password`) (`cmd/init.go:497`, `cmd/main.go:142`) |
| privacy.disable_tracking             | Tracking disabled                                                                                    |
| privacy.individual_tracking          | Individual tracking enabled                                                                          |
| public_subscription.enabled          | Public subscription page enabled                                                                     |
| public_subscription.captcha_enabled  | CAPTCHA enabled                                                                                      |
| public_subscription.captcha_provider | `altcha` or `hcaptcha`, `null` when no CAPTCHA is configured (`cmd/admin.go:61`)                      |
| public_subscription.captcha_key      | hCaptcha site key. The secret stays in the settings                                                   |
| public_subscription.altcha_complexity | Altcha complexity                                                                                   |
| media_provider                       | Upload provider (`filesystem`, `s3`, ...)                                                            |
| messengers                           | Names of the registered messenger backends                                                           |
| langs                                | `[{code, name}]` of the embedded language packs, sorted by code (`cmd/i18n.go:43`)                    |
| update                               | `null`, or the update check result (`cmd/updates.go:15`)                                              |
| needs_restart                        | `true` when settings were saved while a campaign was running                                          |
| version                              | Running version                                                                                      |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/config'
```

##### Example Response

Values marked `"..."` are deployment-specific; `permissions` and `update` are omitted from this excerpt:

```json
{
  "data": {
    "site_name": "...",
    "root_url": "...",
    "from_email": "...",
    "lang": "...",
    "has_legacy_user": false,
    "privacy": {
      "disable_tracking": false,
      "individual_tracking": false
    },
    "public_subscription": {
      "enabled": true,
      "captcha_enabled": false,
      "captcha_provider": null,
      "captcha_key": null,
      "altcha_complexity": 300000
    },
    "media_provider": "...",
    "messengers": [],
    "langs": [
      {
        "code": "...",
        "name": "..."
      }
    ],
    "needs_restart": false,
    "version": "..."
  }
}
```

______________________________________________________________________

#### GET /api/lang/{lang}

Returns the merged language pack in `data`. Requires authentication (`cmd/handlers.go:111`). The English pack is loaded first and the requested language on top of it, so keys missing from the requested pack fall back to English (`cmd/i18n.go:75`); if the requested pack is not embedded, the English pack is returned as is.

##### Parameters

| Name | Type   | Required | Description                                                                                                    |
|------|--------|----------|----------------------------------------------------------------------------------------------------------------|
| lang | String | Yes      | Language code, at most 6 characters of letters, digits, `_` and `-`. Anything else returns HTTP 400 (`cmd/i18n.go:30`) |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/lang/es'
```

##### Example Response

```json
{
  "data": {
    "_.code": "es",
    "_.name": "Español (es)"
  }
}
```

(`i18n/es.json`)

______________________________________________________________________

#### GET /api/about

Returns build, runtime, database and host information. Requires authentication (`cmd/handlers.go:130`).

!!! note
    This endpoint is the exception to the usual envelope: the object is returned at the top level and is **not** wrapped in a `data` key (`cmd/settings.go:481`).

| Field                 | Description                                                                     |
|-----------------------|---------------------------------------------------------------------------------|
| version               | Version of the running binary (`cmd/init.go:949`)                                |
| build                 | Build string (`cmd/init.go:950`)                                                 |
| go_version            | Go runtime version                                                               |
| go_arch               | Build architecture                                                               |
| database.version      | PostgreSQL `VERSION()` (`queries/misc.sql:139`)                                  |
| database.size_mb      | Database size in megabytes                                                       |
| system.num_cpu        | CPU count                                                                        |
| system.memory_alloc_mb | Megabytes allocated by the process, sampled on every request (`cmd/settings.go:478`) |
| system.memory_from_os_mb | Memory obtained from the operating system                                     |
| host.os               | Operating system                                                                 |
| host.arch             | Architecture                                                                     |
| host.hostname         | Hostname of the machine                                                          |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/about'
```

##### Example Response

```json
{
  "version": "...",
  "build": "...",
  "go_version": "...",
  "go_arch": "...",
  "database": {
    "version": "...",
    "size_mb": 0
  },
  "system": {
    "num_cpu": 0,
    "memory_alloc_mb": 0,
    "memory_from_os_mb": 0
  },
  "host": {
    "os": "...",
    "arch": "...",
    "hostname": "..."
  }
}
```

______________________________________________________________________

#### GET /api/dashboard/charts

Returns the two dashboard series for the active workspace. Requires authentication only (`cmd/handlers.go:112`). Platform administrators receive the global materialized view, every other user a query restricted to the resources of the selected workspace (`internal/core/dashboard.go:42`).

| Field          | Description                                                                                   |
|----------------|-----------------------------------------------------------------------------------------------|
| link_clicks    | Array of `{count, date}` entries for the 30 days ending at the newest click (`schema.sql:886`) |
| campaign_views | Array of `{count, date}` entries for the 30 days ending at the newest view                     |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/dashboard/charts'
```

##### Example Response

```json
{
  "data": {
    "link_clicks": [
      {
        "count": 0,
        "date": "..."
      }
    ],
    "campaign_views": []
  }
}
```

______________________________________________________________________

#### GET /api/dashboard/counts

Returns the dashboard totals for the active workspace. Requires authentication only (`cmd/handlers.go:113`).

| Field                                   | Description                                                                 |
|-----------------------------------------|-----------------------------------------------------------------------------|
| customers.total                         | Total customers                                                              |
| customers.blocklisted                   | Blocklisted customers                                                        |
| customers.orphans                       | Customers without any customer list membership                               |
| customerLists / customer_lists          | Customer list totals: `total`, `public`, `private`, `optin_single`, `optin_double` |
| campaigns.total                         | Total campaigns                                                              |
| campaigns.by_status                     | Map of campaign status to count                                              |
| messages                                | Sum of `sent` over all visible campaigns                                      |

The customer list object is keyed `customerLists` for platform administrators, who read the global materialized view (`schema.sql:867`), and `customer_lists` for workspace-scoped responses (`internal/core/dashboard.go:124`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/dashboard/counts'
```

##### Example Response

```json
{
  "data": {
    "customers": {
      "total": 0,
      "blocklisted": 0,
      "orphans": 0
    },
    "customer_lists": {
      "total": 0,
      "public": 0,
      "private": 0,
      "optin_single": 0,
      "optin_double": 0
    },
    "campaigns": {
      "total": 0,
      "by_status": {}
    },
    "messages": 0
  }
}
```

______________________________________________________________________

#### GET /api/custom-fields

Returns the custom field definitions available in forms and campaign templates. Requires authentication only (`cmd/handlers.go:131`).

The response starts with the two built-in identity fields `email` and `name`, marked `system: true`, followed by the administrator-defined fields stored under the `customer.custom_fields` settings key (`cmd/custom_fields.go:48`).

| Field       | Description                                                                                              |
|-------------|----------------------------------------------------------------------------------------------------------|
| key         | Field key                                                                                                 |
| label       | Display label                                                                                             |
| type        | Field type                                                                                                |
| required    | Whether the field is required                                                                             |
| options     | Allowed values for `select` and `multi_select` fields                                                      |
| description | Optional help text                                                                                        |
| active      | `false` for fields that were deactivated                                                                  |
| system      | `true` for the built-in `email` and `name` fields                                                          |
| placeholder | Template expression, for example `{{ .Customer.Attribs.<key> }}` (`cmd/custom_fields.go:49`)              |
| locked      | `true` while a campaign is running; definitions cannot be changed then                                     |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/custom-fields'
```

##### Example Response

```json
{
  "data": [
    {
      "key": "email",
      "label": "...",
      "type": "email",
      "active": true,
      "system": true,
      "placeholder": "{{ .Customer.Email }}",
      "locked": false
    }
  ]
}
```

______________________________________________________________________

#### POST /api/custom-fields

Creates a custom field. Requires a platform administrator; other authenticated users receive HTTP 403 (`cmd/custom_fields.go:65`). While any campaign is running, the request is rejected with HTTP 409 (`cmd/custom_fields.go:72`).

##### Parameters

| Field       | Type     | Required | Description                                                                                                            |
|-------------|----------|----------|------------------------------------------------------------------------------------------------------------------------|
| key         | String   | Yes      | Has to match `^[a-z][a-z0-9_]{0,63}$`; `email`, `name`, `attributes` and `attribs` are reserved (`cmd/custom_fields.go:15`, `cmd/custom_fields.go:108`) |
| label       | String   | Yes      | Display label, at most 2000 bytes (`stdInputMaxLen`, `cmd/handlers.go:18`); multi-byte characters reach the limit sooner (`cmd/custom_fields.go:111`) |
| type        | String   | Yes      | `text`, `textarea`, `number`, `url`, `date`, `select`, `multi_select` or `checkbox` (`cmd/custom_fields.go:19`)          |
| required    | Boolean  | No       | Defaults to `false`                                                                                                     |
| options     | String[] | For `select` and `multi_select` | At least one option, and no empty options (`cmd/custom_fields.go:117`)                                |
| description | String   | No       | Optional help text                                                                                                      |

A duplicate key returns HTTP 400 (`cmd/custom_fields.go:142`). New fields are created active (`cmd/custom_fields.go:139`). The response contains the stored definition list (`models.CustomFieldDefinition`), which does not carry the `system`, `placeholder` and `locked` annotations that `GET /api/custom-fields` adds.

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/custom-fields' \
  -H 'Content-Type: application/json' \
  --data '{"key": "city", "label": "City", "type": "text"}'
```

##### Example Response

```json
{
  "data": [
    {
      "key": "city",
      "label": "City",
      "type": "text",
      "required": false,
      "active": true
    }
  ]
}
```

______________________________________________________________________

#### PUT /api/custom-fields/{key}

Updates a custom field. Requires a platform administrator, and is rejected with HTTP 409 while a campaign is running (`cmd/custom_fields.go:65`).

##### Parameters

| Name | Type   | Required | Description                                                             |
|------|--------|----------|-------------------------------------------------------------------------|
| key  | String | Yes      | Current field key. The key itself cannot be changed; sending a different value returns HTTP 400 (`cmd/custom_fields.go:167`) |

An unknown key returns HTTP 400 (`cmd/custom_fields.go:164`). The body accepts the same fields as `POST /api/custom-fields`.

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/custom-fields/city' \
  -H 'Content-Type: application/json' \
  --data '{"label": "Home city", "type": "text"}'
```

______________________________________________________________________

#### DELETE /api/custom-fields/{key}

Deactivates a custom field: the definition is kept with `active` set to `false` and no customer data is removed (`cmd/custom_fields.go:191`). Requires a platform administrator, and is rejected with HTTP 409 while a campaign is running (`cmd/custom_fields.go:65`).

##### Parameters

| Name | Type   | Required | Description    |
|------|--------|----------|----------------|
| key  | String | Yes      | Field key      |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/custom-fields/city'
```

______________________________________________________________________

#### GET /api/events

Streams live server events. Requires `settings:get` (`cmd/handlers.go:126`). The stream mirrors the process error log, so it follows the same boundary as `GET /api/logs`; the admin UI only opens it for accounts holding `settings:get` (`frontend/src/App.vue`), because an unpermitted `EventSource` would otherwise retry the 403 response forever.

The response is `Content-Type: text/event-stream` with `Cache-Control: no-store` and `Connection: keep-alive`. Every event is sent as a `retry: 3000` hint followed by a `data:` line holding the JSON-encoded event, and the connection stays open until the client disconnects (`cmd/events.go:16`, `cmd/events.go:28`). The handler reads no query parameters (the admin UI nevertheless subscribes to `/api/events?type=error`, `frontend/src/constants.js:38`).

##### Example Request

```shell
curl -u 'api_username:access_token' -N 'http://localhost:9000/api/events'
```

##### Example Response

```text
retry: 3000
data: {"id":"...","type":"error","message":"...","data":null}
```

______________________________________________________________________

#### GET /api/logs

Returns the buffered server log lines. Requires `settings:get` (`cmd/handlers.go:125`).

`data` is an array of strings holding up to 5000 lines (`cmd/main.go:90`, `internal/buflog/buflog.go:44`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/logs'
```

##### Example Response

```json
{
  "data": [
    "..."
  ]
}
```

______________________________________________________________________

#### POST /api/admin/reload

Reloads the app. Requires `settings:manage` (`cmd/handlers.go:124`). The handler schedules a reload signal 500 ms later, which makes the process restart in place, and immediately responds (`cmd/admin.go:126`).

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/admin/reload'
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### DELETE /api/maintenance/customers/{type}

Deletes customers in bulk. Requires `settings:maintain` (`cmd/handlers.go:265`, `internal/auth/models.go:93`).

##### Parameters

| Name | Type   | Required | Description                                                                                                    |
|------|--------|----------|----------------------------------------------------------------------------------------------------------------|
| type | String | Yes      | `orphan` deletes customers without any customer list membership (`queries/customers.sql:312`); `blocklisted` deletes all blocklisted customers (`queries/customers.sql:308`). Any other value returns HTTP 400 (`cmd/maintenance.go:21`) |

`data.count` reports the number of deleted customers.

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/maintenance/customers/orphan'
```

##### Example Response

```json
{
  "data": {
    "count": 0
  }
}
```

______________________________________________________________________

#### DELETE /api/maintenance/analytics/{type}

Deletes campaign analytics. Requires `settings:maintain` (`cmd/handlers.go:266`).

##### Parameters

| Name        | Type   | Required | Description                                                                                          |
|-------------|--------|----------|------------------------------------------------------------------------------------------------------|
| type        | String | Yes      | `views`, `clicks` or `all`. Any other value returns HTTP 400 (`cmd/maintenance.go:66`)                |
| before_date | String | Yes      | RFC 3339 timestamp; only analytics created before it are deleted. An invalid value returns HTTP 400 (`cmd/maintenance.go:61`) |

`all` deletes views first and clicks only when the views deletion succeeded (`cmd/maintenance.go:67`). Passed in the query string or as form data.

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE \
  'http://localhost:9000/api/maintenance/analytics/views?before_date=2025-01-01T00:00:00Z'
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### DELETE /api/maintenance/subscriptions/unconfirmed

Deletes stale `unconfirmed` customer list memberships. Requires `settings:maintain` (`cmd/handlers.go:267`).

Only memberships of double opt-in customer lists whose membership row was created before the given date are deleted; the customers themselves are kept (`queries/customers.sql:385`). `data.count` reports the number of deleted memberships.

##### Parameters

| Name        | Type   | Required | Description                                                    |
|-------------|--------|----------|----------------------------------------------------------------|
| before_date | String | Yes      | RFC 3339 timestamp; an invalid value returns HTTP 400 (`cmd/maintenance.go:42`) |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE \
  'http://localhost:9000/api/maintenance/subscriptions/unconfirmed?before_date=2025-01-01T00:00:00Z'
```

##### Example Response

```json
{
  "data": {
    "count": 0
  }
}
```

______________________________________________________________________

#### POST /api/logout

Logs the caller out. Requires authentication (`cmd/handlers.go:333`). If the request carries a browser session cookie, the session is destroyed; BasicAuth and `Authorization` token requests have no cookie session and are simply acknowledged (`cmd/auth.go:213`). API tokens are not revoked by this endpoint.

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/logout'
```

##### Example Response

```json
{
  "data": true
}
```
