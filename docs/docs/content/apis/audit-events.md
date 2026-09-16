# API / Audit events

`audit_events` is the durable business-operation audit log: it records
low- and medium-volume business mutations and automation lifecycle events of
the active workspace. High-frequency facts (opens, clicks, recipient state, and
bounces) stay in their dedicated tables and are not copied here. The feature
overview, its permission, and the admin UI behavior are described in
[User roles and permissions](../roles-and-permissions.md); this page documents
the HTTP surface. Authentication, response envelopes, and error shapes are
described in the [API introduction](apis.md).

| Method | Endpoint                                                  | Description                                              |
| :----- | :-------------------------------------------------------- | :------------------------------------------------------- |
| GET    | [/api/audit-events](#get-apiaudit-events)                 | List audit events in the active workspace.               |
| GET    | [/api/audit-events/export](#get-apiaudit-eventsexport)    | Export matching audit events as a CSV stream.            |
| GET    | [/api/audit-events/{id}](#get-apiaudit-eventsid)          | Retrieve a single audit event.                           |

All three endpoints require the `audit:get` permission; platform administrators
bypass the permission check. They accept a browser session or a legacy API-user
token. Personal API keys created in `Profile -> API Keys` are rejected because
`/api/audit-events` is not part of the personal API key business surface.

Every request is scoped to the workspace selected by the
`X-Listmonk-Organization-ID` header (`0`, or no header, is the personal
workspace). The workspace is applied as an exact `COALESCE(organization_id, 0)`
filter, and an organization's ID is retained on its events even when the
organization is later archived or purged, so organization history never falls
into personal-workspace queries.

An audit event contains:

| Field            | Description                                                                                                     |
| :--------------- | :-------------------------------------------------------------------------------------------------------------- |
| id               | Event ID.                                                                                                       |
| occurred_at      | Event timestamp.                                                                                                |
| organization_id  | Workspace of the event; `0` is the personal workspace.                                                          |
| actor_type       | `user`, `api_key`, `system`, `webhook`, `customer`, or `anonymous`.                                             |
| actor_user_id    | User ID of the actor, `0` when there is none.                                                                   |
| actor_token_id   | API key (integration token) ID, `0` when there is none.                                                         |
| actor_username   | Username resolved from the actor's current user row; empty when the account no longer exists.                   |
| actor_name       | Name resolved from the actor's current user row; empty when the account no longer exists.                       |
| action           | Stable dot-named business action, for example `customer.updated`, `campaign.status_changed`, `role.updated`.    |
| object_type      | Type of the affected object, for example `customer`, `campaign`, `role`, `template`, `audit_event`.             |
| object_id        | Stable ID of the affected object; may be empty.                                                                 |
| result           | `success`, `failed`, or `denied`.                                                                               |
| reason_code      | Failure or denial reason; HTTP-recorded failures use `http_<status>`, for example `http_403`.                   |
| request_id       | Request ID taken from the client's `X-Request-ID` header or generated for audited requests.                      |
| metadata         | Stored JSON object with the HTTP method, status, and route of the request plus small, non-sensitive snapshots.   |
| ip               | Recorded client IP, empty when it was not a valid address.                                                      |
| user_agent       | Recorded `User-Agent`, empty when absent.                                                                       |

Metadata is limited to stable identifiers and small, non-sensitive values
(`object_details` and `actor_details` snapshots, `http_method`, `http_status`,
`route`, and similar). Credentials, tokens, message bodies, attachments, and
full recipient sets are never written to it. The `GET` endpoints of this API
are not themselves audited; the export is (see below).

______________________________________________________________________

#### GET /api/audit-events

Returns the workspace's audit events, newest first (`occurred_at DESC,
id DESC`). The response is the standard paginated envelope: `data.results`,
`data.total`, `data.page`, and `data.per_page` (the envelope also carries empty
`search` and `query` fields).

##### Query parameters

| Name        | Type   | Required | Description                                                                                     |
| :---------- | :----- | :------- | :---------------------------------------------------------------------------------------------- |
| page        | number | No       | 1-based page number. Defaults to `1`; values above `100000` are rejected with `400`.            |
| per_page    | number | No       | Page size. Defaults to `50`; values above `100` are capped at `100`.                             |
| action      | string | No       | Exact match on the action name, for example `role.updated`.                                     |
| result      | string | No       | Exact match on the result: `success`, `failed`, or `denied`.                                    |
| object_type | string | No       | Exact match on the object type, for example `customer` or `campaign`.                           |
| object_id   | string | No       | Exact match on the object ID.                                                                   |

Filters are combined with AND; there is no free-text search.

##### Example Request

```shell
curl -u 'api_username:access_token' \
  -H 'X-Listmonk-Organization-ID: 0' \
  'http://localhost:9000/api/audit-events?page=1&per_page=50&result=denied'
```

##### Example Response

```json
{
  "data": {
    "results": [
      {
        "id": 128,
        "occurred_at": "2026-03-23T14:30:00.000000+08:00",
        "organization_id": 0,
        "actor_type": "user",
        "actor_user_id": 2,
        "actor_token_id": 0,
        "actor_username": "ops",
        "actor_name": "Ops User",
        "action": "role.updated",
        "object_type": "role",
        "object_id": "3",
        "result": "denied",
        "reason_code": "http_403",
        "request_id": "0f6f6f0e-1f2d-4a9c-8a5f-3f2c2a7a1b9d",
        "metadata": {
          "http_method": "PUT",
          "http_status": 403,
          "route": "/api/roles/users/:id",
          "actor_details": {
            "name": "Ops User",
            "username": "ops"
          }
        },
        "ip": "203.0.113.10",
        "user_agent": "curl/8.4.0"
      }
    ],
    "total": 1,
    "per_page": 50,
    "page": 1
  }
}
```

______________________________________________________________________

#### GET /api/audit-events/export

Streams a CSV export of the workspace's audit events. Unlike the list
endpoint, the export ignores `page` and `per_page` and writes every matching
row in one response, newest first. The `action`, `result`, `object_type`, and
`object_id` filters of the list endpoint apply to both export scopes.

The response has `Content-Type: text/csv; charset=utf-8`, a
`Content-Disposition` attachment filename of
`audit-events-<scope>-<YYYYMMDD-HHMMSS>.csv`, `Cache-Control: no-store`, and
`X-Content-Type-Options: nosniff`.

##### Query parameters

| Name        | Type             | Required | Description                                                                                                                                                                    |
| :---------- | :--------------- | :------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| scope       | string           | No       | `selected` exports only the events listed in `ids`; `all` exports every event matching the filters. Defaults to `selected` when one or more `ids` is given, otherwise `all`. Any other value is rejected with `400 invalid audit export scope`. |
| ids         | number or array  | No       | Event IDs to export, either repeated (`ids=1&ids=3`) or comma-separated (`ids=1,3`). Only valid with `scope=selected`, which requires at least one ID, and accepts at most 1000. |
| action      | string           | No       | Exact match on the action name.                                                                                                                                                 |
| result      | string           | No       | Exact match on the result.                                                                                                                                                      |
| object_type | string           | No       | Exact match on the object type.                                                                                                                                                 |
| object_id   | string           | No       | Exact match on the object ID.                                                                                                                                                   |

Selected IDs are still filtered by the active workspace, so an event ID from
another workspace is not exported. The export itself is recorded in the audit
log as the action `audit.exported` with the object type `audit_event`.

The CSV header row is:

```
id,occurred_at,organization_id,actor_type,actor_user_id,actor_token_id,action,object_type,object_id,result,reason_code,request_id,metadata,ip,user_agent
```

`occurred_at` is written in UTC (`RFC3339Nano`), `metadata` is the stored JSON
object (`{}` when empty), and the numeric actor fields are empty strings when
they are `0`.

##### Example Request

```shell
curl -u 'api_username:access_token' \
  -H 'X-Listmonk-Organization-ID: 1' \
  -o audit-events.csv \
  'http://localhost:9000/api/audit-events/export?scope=all&action=campaign.status_changed'
```

##### Example Response

A `text/csv` stream, for example:

```csv
id,occurred_at,organization_id,actor_type,actor_user_id,actor_token_id,action,object_type,object_id,result,reason_code,request_id,metadata,ip,user_agent
104,2026-03-23T06:30:00.000000Z,1,user,2,,campaign.status_changed,campaign,7,success,,0f6f6f0e-1f2d-4a9c-8a5f-3f2c2a7a1b9d,"{""http_method"":""PUT"",""http_status"":200,""route"":""/api/campaigns/:id/status""}",203.0.113.10,"curl/8.4.0"
```

______________________________________________________________________

#### GET /api/audit-events/{id}

Returns a single audit event. The lookup is restricted to the active
workspace: an event that exists in another workspace is reported as not found.

##### Parameters

| Name | Type   | Required | Description                          |
| :--- | :----- | :------- | :----------------------------------- |
| id   | number | Yes      | ID of the audit event to retrieve.   |

##### Example Request

```shell
curl -u 'api_username:access_token' \
  -H 'X-Listmonk-Organization-ID: 0' \
  'http://localhost:9000/api/audit-events/128'
```

##### Example Response

```json
{
  "data": {
    "id": 128,
    "occurred_at": "2026-03-23T14:30:00.000000+08:00",
    "organization_id": 0,
    "actor_type": "user",
    "actor_user_id": 2,
    "actor_token_id": 0,
    "actor_username": "ops",
    "actor_name": "Ops User",
    "action": "role.updated",
    "object_type": "role",
    "object_id": "3",
    "result": "denied",
    "reason_code": "http_403",
    "request_id": "0f6f6f0e-1f2d-4a9c-8a5f-3f2c2a7a1b9d",
    "metadata": {
      "http_method": "PUT",
      "http_status": 403,
      "route": "/api/roles/users/:id",
      "actor_details": {
        "name": "Ops User",
        "username": "ops"
      }
    },
    "ip": "203.0.113.10",
    "user_agent": "curl/8.4.0"
  }
}
```

A missing event, or one belonging to another workspace, returns
`404 audit event not found`. An ID below `1` returns `400`.

## Retention and privacy

The API exposes no retention setting and no delete endpoint, and the
application never prunes `audit_events`: the table and its indexes are created
by `internal/migrations/v6.32.0.go` and no code removes rows from it. Events
therefore accumulate until they are removed outside the API by direct database
maintenance. Workspace history is preserved independently of the
organization's lifecycle because the original `organization_id` is retained.
