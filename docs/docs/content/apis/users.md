# API / Users

| Method | Endpoint                                                     | Description                                      |
| :----- | :----------------------------------------------------------- | :----------------------------------------------- |
| GET    | [/api/users/{user_id}/integration-tokens](#get-apiusersuser_idintegration-tokens) | CustomerList integration bearer tokens for an API user.  |
| POST   | [/api/users/{user_id}/integration-tokens](#post-apiusersuser_idintegration-tokens) | Create a new integration bearer token.           |
| DELETE | [/api/users/{user_id}/integration-tokens/{token_id}](#delete-apiusersuser_idintegration-tokenstoken_id) | Revoke an integration bearer token.              |

______________________________________________________________________

#### GET /api/users/{user_id}/integration-tokens

CustomerList integration bearer tokens for an API user. The plaintext token value is never returned from this endpoint.

##### Example Request

```shell
curl -H "Authorization: Bearer lmit_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  'http://localhost:9000/api/users/2/integration-tokens'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 3,
      "user_id": 2,
      "name": "openclaw-prod",
      "last_used_at": "2026-03-23T14:30:00.000000+08:00",
      "revoked_at": null,
      "created_at": "2026-03-23T14:00:00.000000+08:00",
      "updated_at": "2026-03-23T14:30:00.000000+08:00"
    }
  ]
}
```

______________________________________________________________________

#### POST /api/users/{user_id}/integration-tokens

Create a new integration bearer token for an API user. The plaintext token is returned only once in the creation response.

##### Parameters

| Name | Type   | Required | Description                   |
| :--- | :----- | :------- | :---------------------------- |
| name | string | Yes      | Friendly name for the token.  |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/users/2/integration-tokens' \
  -H 'Content-Type: application/json' \
  -d '{"name":"openclaw-prod"}'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "user_id": 2,
    "name": "openclaw-prod",
    "last_used_at": null,
    "revoked_at": null,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T14:00:00.000000+08:00",
    "token": "lmit_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
```

______________________________________________________________________

#### DELETE /api/users/{user_id}/integration-tokens/{token_id}

Revoke an integration bearer token. Revoked tokens stop working immediately.

##### Example Request

```shell
curl -u "api_user:token" -X DELETE \
  'http://localhost:9000/api/users/2/integration-tokens/3'
```

##### Example Response

```json
{
  "data": true
}
```

### SMTP connection test feedback

The personal profile and organization SMTP forms send a test message using the
current configuration without saving it. A saved server ID allows the backend
to reuse its masked/empty password. The test requires `mailboxes:manage` and
the existing ownership/workspace permissions.

While a test runs, the button shows progress and prevents duplicate tests.
Success or failure remains visible below the SMTP card's test controls. Server
errors (including authentication, TLS and connection failures) display the API
message; network failures display the request error. A failed test can be
retried after correcting the configuration.

### Reply mailbox receiving configuration

`POST /api/profile/reply-mailboxes` and
`PUT /api/profile/reply-mailboxes/{id}` require `mailboxes:manage` and the
mailbox owner's active workspace. With `ai_enabled: false` (the default), only
`email` is required; `name` and `is_default` are optional. Saving makes the
address available for campaign Reply-To selection without a connection test.
No receiving credentials are required or used for this mode.

With `ai_enabled: true`, creation requires `password` (or a client authorization
code). The receiving fields are `username`, `imap_host`, `imap_port`,
`imap_tls` and `folder`. On update an omitted/empty password retains the saved
password; enabling AI without any saved or replacement password returns 400.
Connection changes clear verification. Disabling AI preserves the saved
receiving configuration and makes the ordinary address usable again; disabled
and retained lifecycle states are not silently re-enabled by saving.

Responses include `has_password` but never the password. Connection testing is
`POST /api/profile/reply-mailboxes/test` with only `{"id": 12}`. Save first:
missing IDs or address-only mailboxes return 400. Tests load saved credentials
on the server, ignore draft connection fields, enforce the existing owner and
workspace boundary (404 for another owner's/workspace's mailbox), and return
`{"data": true}` on success. A concurrent configuration change returns 409
instead of verifying the old configuration. AI scanning requires both global
classification and per-mailbox opt-in plus a verified connection. A successful
connection test preserves disabled and retained lifecycle states; use the
explicit enable endpoint to resume a disabled mailbox.


### Reply mailbox delivery and receiving boundaries (v6.57.0)

Receiving configuration and connection testing use IMAP with the saved host, port, TLS setting and folder. AI scans preserve a UID/UIDVALIDITY cursor and process successive batches of at most 200 unseen messages; they do not mark messages read or delete them. Disabling a mailbox retains `ai_enabled` and credentials for re-enabling.

Private campaigns with mixed public-pool/private audiences retain `reply_mailbox_id` for their private recipients. Pool recipients keep independent Reply-To snapshots. Private campaign start/schedule and delivery reject a disabled or changed mailbox while retaining sent route history. AI source resolution requires a sent delivery in the actual mailbox, resolves shared mailboxes through the delivered customer, and ignores ambiguous sources. Platform-admin-created organization mailboxes may scan without the administrator joining the organization, provided the organization is active.
