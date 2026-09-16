# API / Organizations

Organizations are tenant workspaces that own customer lists, customers, campaigns,
templates and media on behalf of their members. The authenticated admin API exposes
them under `/api/organizations`, and the workspace a request actually runs in under
`/api/workspace`.

Authenticated requests are scoped by the active workspace, selected with the
`X-Listmonk-Organization-ID` request header (the `organization_id` query parameter
and the browser workspace cookie are fallbacks; an API token bound to a workspace
supplies its own organization when the header is missing). See
[Introduction](apis.md) for authentication, response structure, timestamps and
common HTTP error codes. The endpoints on this page are **not** part of the
personal API key surface: personal keys are restricted to the business prefixes in
`isPersonalAPIKeyBusinessPath` (`cmd/api_keys.go:107`), which does not include
`/api/organizations` or `/api/workspace`, and are rejected with HTTP 403
(`cmd/api_keys.go:98`).

Two authorization levels appear throughout this page:

- **Workspace endpoints** resolve the active organization and require an active
  membership (or a platform administrator) — `cmd/organizations.go:156`.
- **Platform endpoints** additionally require the built-in Super Admin or a role
  holding `organizations:platform_manage` (`cmd/organizations.go:311`,
  `internal/auth/models.go:96`), and answer HTTP 403
  `platform organization management permission required` otherwise. An operator
  holding that permission may also use the organization-management paths
  (`members`, `resources/transfer`, `templates/*`, `reply-forwarding`, `invites`)
  inside a selected organization without joining it — `cmd/organizations.go:194`.

| Method | Endpoint                                                                 | Description                                        |
| :----- | :----------------------------------------------------------------------- | :------------------------------------------------- |
| GET    | [/api/workspace](#get-apiworkspace)                                       | Resolve the active workspace of the request        |
| GET    | [/api/organizations](#get-apiorganizations)                               | List all organizations (platform)                  |
| POST   | [/api/organizations](#post-apiorganizations)                              | Create an organization with initial members (platform) |
| GET    | [/api/organizations/me](#get-apiorganizationsme)                          | List the caller's active organizations             |
| GET    | [/api/organizations/members](#get-apiorganizationsmembers)                | List members of the active organization            |
| POST   | [/api/organizations/members](#post-apiorganizationsmembers)               | Add a member to the active organization            |
| PUT    | [/api/organizations/members/{user_id}](#put-apiorganizationsmembersuser_id) | Change a member's organization role               |
| DELETE | [/api/organizations/members/{user_id}](#delete-apiorganizationsmembersuser_id) | Remove a member from the active organization   |
| GET    | [/api/organizations/{id}/members](#get-apiorganizationsidmembers)         | List members of any organization (platform)        |
| POST   | [/api/organizations/{id}/members/bulk](#post-apiorganizationsidmembersbulk) | Bulk-add registered accounts to an organization (platform) |
| POST   | [/api/organizations/requests](#post-apiorganizationsrequests)             | Request a new organization                         |
| GET    | [/api/organizations/requests/mine](#get-apiorganizationsrequestsmine)     | List the caller's own organization requests        |
| DELETE | [/api/organizations/requests/{id}](#delete-apiorganizationsrequestsid)    | Withdraw a pending organization request            |
| GET    | [/api/organizations/requests](#get-apiorganizationsrequests)              | List organization requests (platform)              |
| PUT    | [/api/organizations/requests/{id}](#put-apiorganizationsrequestsid)       | Approve or reject an organization request (platform) |
| POST   | [/api/organizations/join](#post-apiorganizationsjoin)                     | Join an organization with an invitation code       |
| POST   | [/api/organizations/leave](#post-apiorganizationsleave)                   | Leave the active organization                      |
| GET    | [/api/organizations/invites](#get-apiorganizationsinvites)                | List invitations of the active organization        |
| POST   | [/api/organizations/invites](#post-apiorganizationsinvites)               | Create an invitation for the active organization   |
| DELETE | [/api/organizations/invites/{id}](#delete-apiorganizationsinvitesid)      | Revoke an invitation                               |
| POST   | [/api/organizations/resources/migrate](#post-apiorganizationsresourcesmigrate) | Copy or move personal resources into an organization |
| POST   | [/api/organizations/resources/customer-lists/migrate](#post-apiorganizationsresourcescustomer-listsmigrate) | Copy or move personal customer_lists into an organization |
| POST   | [/api/organizations/resources/transfer](#post-apiorganizationsresourcestransfer) | Transfer a former member's pending resources  |
| POST   | [/api/organizations/{id}/resources/transfer](#post-apiorganizationsidresourcestransfer) | Transfer an archived organization's resources (platform) |
| POST   | [/api/organizations/templates/{id}/transfer](#post-apiorganizationstemplatesidtransfer) | Hand over an organization-shared template  |
| POST   | [/api/organizations/templates/{id}/unpublish](#post-apiorganizationstemplatesidunpublish) | Unpublish an organization-shared template |
| GET    | [/api/organizations/reply-forwarding](#get-apiorganizationsreply-forwarding) | List reply-forwarding rules of the active organization |
| PUT    | [/api/organizations/reply-forwarding/{id}](#put-apiorganizationsreply-forwardingid) | Enable or disable a reply-forwarding rule |
| DELETE | [/api/organizations/reply-forwarding/{id}](#delete-apiorganizationsreply-forwardingid) | Disable a reply-forwarding rule    |
| POST   | [/api/organizations/{id}/archive](#post-apiorganizationsidarchive)       | Archive an organization (platform)                 |
| DELETE | [/api/organizations/{id}](#delete-apiorganizationsid)                     | Permanently delete an archived organization (platform) |

Every route above is registered on the authenticated API group
(`cmd/handlers.go:93`) and passes through the audit middleware
(`cmd/handlers.go:106`); audit action names exist for the mutating organization
endpoints (`cmd/audit.go:269`–`cmd/audit.go:311`).

______________________________________________________________________

### Workspace

#### GET /api/workspace

Return the workspace the request resolves to. The resolver reads
`X-Listmonk-Organization-ID` first, then the `organization_id` query parameter,
then the browser workspace cookie (`listmonk_workspace_organization_id`). The
workspace-bound fallback that resource endpoints apply to personal API keys does
not reach this page: a personal API key is rejected on `/api/workspace` and on
every `/api/organizations` route before the resolver runs. A missing header, `0`
or `personal` selects the caller's personal workspace, which requires the
`workspaces:personal` capability (Super Admin is exempt) and otherwise answers
HTTP 403 `personal workspace is disabled for this account`. An organization
workspace is re-validated on every request: non-admin members must still be
active members, a non-admin request for an archived organization answers HTTP 409
`organization is archived`, and only a platform administrator may select an
archived organization (for resource transfer or cleanup). Source:
`cmd/organizations.go:78`, `cmd/organizations.go:156`,
`cmd/workspace_permissions.go:16`.
workspace, which requires the `workspaces:personal` capability (Super Admin is
exempt) and otherwise answers HTTP 403
`personal workspace is disabled for this account`. An organization workspace is
re-validated on every request: non-admin members must still be active members, a
non-admin request for an archived organization answers HTTP 409
`organization is archived`, and only a platform administrator may select an
archived organization (for resource transfer or cleanup). Source:
`cmd/organizations.go:78`, `cmd/organizations.go:156`,
`cmd/workspace_permissions.go:16`.

##### Parameters

| Name                       | Type   | Required | Description                                                             |
| :------------------------- | :----- | :------- | :---------------------------------------------------------------------- |
| X-Listmonk-Organization-ID | Header | No       | Organization workspace ID; omit for the caller's personal workspace.     |
| organization_id            | Number | No       | Query-parameter fallback used when the header is not sent.              |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/workspace' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": {
    "organization_id": 7,
    "organization_name": "Acme",
    "role": "manager",
    "personal": false,
    "platform_admin": false
  }
}
```

`organization_name` and `role` are omitted when empty, and `archived` is only
present for a platform administrator that selected an archived organization
(`models/organizations.go:114`). The personal workspace answers with
`organization_id: 0`, `personal: true` and no organization name or role.

The same resolver backs every other workspace-scoped endpoint in this
documentation set. One read-only exception applies to the personal workspace: the
four list endpoints that back the personal-resource migration UI
(`/api/customer-lists`, `/api/templates`, `/api/campaigns`, `/api/media`) are
listed without the `workspaces:personal` capability, while every detail read,
mutation and export still requires it (`cmd/organizations.go:111`,
`cmd/workspace_permissions.go:43`).

______________________________________________________________________

### Directory and membership

#### GET /api/organizations

List organizations for platform administration. Available organizations are
returned by default; pass `include_archived=true` to include archived ones.
Requires platform administrator or `organizations:platform_manage`
(`cmd/handlers.go:319`, `cmd/organizations.go:328`). The rows carry an empty
`my_role` because the endpoint is not membership-scoped
(`internal/core/organizations.go:57`).

##### Parameters

| Name             | Type    | Required | Description                                                              |
| :--------------- | :------ | :------- | :----------------------------------------------------------------------- |
| include_archived | Boolean | No       | `true` also returns archived organizations; default is active only.       |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations?include_archived=true'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 7,
      "created_at": "2025-01-01T09:00:00.000000+05:30",
      "updated_at": "2025-01-01T09:00:00.000000+05:30",
      "name": "Acme",
      "description": "Acme marketing team",
      "status": "active",
      "created_by_user_id": 1,
      "archived_at": null,
      "member_count": 4,
      "my_role": ""
    }
  ]
}
```

#### GET /api/organizations/me

List the caller's own active organizations, ordered by name, with `my_role` and
`member_count`. Archived organizations are excluded, so the response is a
workspace-switcher list (`cmd/organizations.go:319`,
`internal/core/organizations.go:37`). No extra permission is required beyond
authentication.

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/me'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 7,
      "name": "Acme",
      "description": "Acme marketing team",
      "status": "active",
      "created_by_user_id": 1,
      "archived_at": null,
      "member_count": 4,
      "my_role": "manager"
    }
  ]
}
```

#### GET /api/organizations/members

List the members of the active organization workspace, including former members
(`removed_at` is set for them), ordered by removal state, role and name. Requires
an organization manager, a platform administrator, or
`organizations:platform_manage`; a personal workspace answers HTTP 400
`select an organization workspace` and an archived organization answers HTTP 409
(`cmd/organizations.go:262`, `cmd/organizations.go:486`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/members' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": [
    {
      "organization_id": 7,
      "user_id": 4,
      "role": "manager",
      "joined_at": "2025-01-01T09:00:00.000000+05:30",
      "removed_at": null,
      "removed_by_user_id": null,
      "username": "jane",
      "name": "Jane Doe",
      "email": "jane@example.com"
    }
  ]
}
```

#### POST /api/organizations/members

Add an existing account to the active organization directly, without consuming an
invitation. Requires organization manager access. Re-adding a former member
restores the relationship and its previous resources stay pending for transfer, so
use the transfer endpoint below afterwards
(`cmd/organizations.go:498`, `internal/core/organizations.go:425`).

##### Parameters

| Name    | Type   | Required | Description                                                                                                        |
| :------ | :----- | :------- | :----------------------------------------------------------------------------------------------------------------- |
| user_id | Number | No       | ID of the user to add. Required unless `account` is supplied.                                                       |
| account | String | No       | Registered username or e-mail address resolved to a user ID; HTTP 404 `registered account not found` otherwise.     |
| role    | String | No       | `member` (default) or `manager`; anything else answers HTTP 400 `invalid organization member role`.                 |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/members' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"account": "jane", "role": "manager"}'
```

##### Example Response

```json
{
  "data": {
    "organization_id": 7,
    "user_id": 4,
    "role": "manager",
    "joined_at": "2025-01-01T09:00:00.000000+05:30",
    "removed_at": null,
    "removed_by_user_id": null,
    "username": "jane",
    "name": "Jane Doe",
    "email": "jane@example.com"
  }
}
```

HTTP 201 is returned on success. Demoting the last remaining manager answers HTTP
400 `an organization must retain at least one manager`.

#### PUT /api/organizations/members/{user_id}

Change the organization role of one member of the active organization
(`cmd/organizations.go:526`, `internal/core/organizations.go:497`). Requires
organization manager access.

##### Parameters

| Name    | Type   | Required | Description                                                              |
| :------ | :----- | :------- | :----------------------------------------------------------------------- |
| user_id | Number | Yes      | ID of the organization member.                                            |
| role    | String | Yes      | `member` or `manager`; any other value answers HTTP 400.                  |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/organizations/members/4' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"role": "member"}'
```

##### Example Response

```json
{
  "data": true
}
```

A `user_id` that is not an active member answers HTTP 403
`not an active organization member`, and demoting the last manager answers HTTP
400 `an organization must retain at least one manager`.

#### DELETE /api/organizations/members/{user_id}

Revoke a member's access to the active organization. The member's resources in
that organization become pending-transfer rows, scheduled and deferred campaigns
become drafts, running campaigns are paused, a pending import owned by that member
is stopped, and their dedicated reply mailboxes are retained with a forwarding
rule to the organization creator (or the first active manager)
(`cmd/organizations.go:546`, `internal/core/organizations.go:538`,
`cmd/reply_forward_rules.go:71`). Requires organization manager access.

##### Parameters

| Name    | Type   | Required | Description                    |
| :------ | :----- | :------- | :----------------------------- |
| user_id | Number | Yes      | ID of the organization member. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/organizations/members/4' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": true
}
```

Removing the last manager answers HTTP 400 `an organization must retain at least
one manager`. If no active manager with a work e-mail is left to receive the
former member's replies, the membership is already removed when the request
answers HTTP 409 `organization has no active manager with a work email for reply
forwarding` (`cmd/organizations.go:560`, `cmd/reply_forward_rules.go:77`).

#### GET /api/organizations/{id}/members

Platform variant of the member listing that addresses any organization by ID
instead of the workspace header, and is used to pick a transfer target for an
archived organization that can no longer be selected as a workspace. Requires
platform administrator or `organizations:platform_manage`; an unknown organization
answers HTTP 404 (`cmd/handlers.go:308`, `cmd/organizations.go:650`). The response
body has the same shape as `GET /api/organizations/members`.

##### Parameters

| Name | Type   | Required | Description         |
| :--- | :----- | :------- | :------------------ |
| id   | Number | Yes      | Organization ID.     |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/7/members'
```

#### POST /api/organizations

Create an active organization directly from the platform management surface.
The request must include one `manager_user_id`; optional `members` and
`member_user_ids` are added in the same transaction. The creator is recorded in
`created_by_user_id` but is not implicitly added as a member. Requires Super
Admin or `organizations:platform_manage`.

```json
{
  "name": "Acme",
  "description": "Acme marketing team",
  "manager_user_id": 4,
  "members": [
    {"user_id": 5, "role": "member"}
  ]
}
```

The manager is always inserted with the `manager` organization role. User IDs
must refer to existing accounts, roles may be `member` or `manager`, and the
organization name is unique case-insensitively. Organization creation and all
initial memberships roll back together on validation or database failure.

#### POST /api/organizations/{id}/members/bulk

Add existing registered accounts to the selected active organization in one
transaction. Requires Super Admin or `organizations:platform_manage`; the
request is path-scoped and does not require switching into the organization.
Rows accept `account` (username or email), optional `user_id`, and `role`
(`member` or `manager`). `users` is accepted as an alias for `members`.

```json
{
  "members": [
    {"account": "jane", "role": "manager"},
    {"account": "alex@example.com", "role": "member"}
  ]
}
```

Validation errors return HTTP 200 with `data.errors`, including the source row,
field, and stable code (`missing_account`, `invalid_role`,
`account_not_found`, or `duplicate_account`). No rows are written when any
validation error is returned. Re-importing a former member restores the
membership; the final organization state must retain at least one manager.

______________________________________________________________________

### Organization requests

#### POST /api/organizations/requests

Submit a request to create a new organization. Any authenticated user may ask; a
platform administrator reviews the request later
(`cmd/organizations.go:348`, `internal/core/organizations.go:118`). The response is
the stored request with `status: pending`, returned with HTTP 201.

##### Parameters

| Name        | Type   | Required | Description                                                                         |
| :---------- | :----- | :------- | :---------------------------------------------------------------------------------- |
| name        | String | Yes      | Requested organization name, 2–2000 characters; HTTP 409 if the name is already used. |
| description | String | No       | Free-form description, up to 2000 characters.                                         |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/requests' \
--header 'Content-Type: application/json' \
--data '{"name": "Acme", "description": "Acme marketing team"}'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "created_at": "2025-01-01T09:00:00.000000+05:30",
    "updated_at": "2025-01-01T09:00:00.000000+05:30",
    "requested_name": "Acme",
    "description": "Acme marketing team",
    "status": "pending",
    "requested_by_user_id": 4,
    "reviewed_by_user_id": null,
    "reviewed_at": null,
    "review_note": "",
    "organization_id": null,
    "requested_by_name": ""
  }
}
```

#### GET /api/organizations/requests/mine

Return every organization-creation request submitted by the caller, including
reviewed and withdrawn history, newest first
(`cmd/organizations.go:381`, `internal/core/organizations.go:162`). No permission
beyond authentication is required, and no other account's requests are visible.
The response body has the same shape as `POST /api/organizations/requests`.

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/requests/mine'
```

#### DELETE /api/organizations/requests/{id}

Withdraw the caller's own pending organization request. The row is kept as history
with `status: withdrawn`, so it can no longer be reviewed
(`cmd/organizations.go:389`, `internal/core/organizations.go:179`).

##### Parameters

| Name | Type   | Required | Description               |
| :--- | :----- | :------- | :------------------------ |
| id   | Number | Yes      | Organization request ID.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/organizations/requests/3'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "requested_name": "Acme",
    "description": "Acme marketing team",
    "status": "withdrawn",
    "requested_by_user_id": 4,
    "reviewed_by_user_id": null,
    "reviewed_at": null,
    "review_note": "",
    "organization_id": null,
    "requested_by_name": ""
  }
}
```

A request belonging to another account answers HTTP 404
`organization request not found`, and a request that is no longer pending answers
HTTP 400 `only pending organization requests can be withdrawn`. The
`requested_by_name` field is only populated by the two listing endpoints, which
join the requesting user; the create, withdraw and review responses return the
stored row directly and leave it empty (`internal/core/organizations.go:274`,
`internal/core/organizations.go:289`, `internal/core/organizations.go:396`).

#### GET /api/organizations/requests

List organization requests for platform review. By default only pending requests
are returned; pass `include_resolved=true` for the full history, newest first
(`cmd/handlers.go:321`, `cmd/organizations.go:368`). Requires platform
administrator or `organizations:platform_manage`.

##### Parameters

| Name             | Type    | Required | Description                                              |
| :--------------- | :------ | :------- | :------------------------------------------------------- |
| include_resolved | Boolean | No       | `true` also returns approved, rejected and withdrawn requests. |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/requests?include_resolved=true'
```

The response is an array of the request objects shown under
`POST /api/organizations/requests`, each with `requested_by_name` populated.

#### PUT /api/organizations/requests/{id}

Approve or reject an organization request. Approving creates the organization and
inserts the requester as its first `manager` in the same transaction; rejecting
records only the review. Requires platform administrator or
`organizations:platform_manage`
(`cmd/handlers.go:322`, `cmd/organizations.go:397`,
`internal/core/organizations.go:215`).

##### Parameters

| Name    | Type    | Required | Description                                                                          |
| :------ | :------ | :------- | :----------------------------------------------------------------------------------- |
| id      | Number  | Yes      | Organization request ID.                                                             |
| approve | Boolean | Yes      | `true` creates the organization, `false` rejects the request.                        |
| note    | String  | No       | Review note stored on the request as `review_note`.                                  |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/organizations/requests/3' \
--header 'Content-Type: application/json' \
--data '{"approve": true, "note": "approved by ops"}'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "requested_name": "Acme",
    "description": "Acme marketing team",
    "status": "approved",
    "requested_by_user_id": 4,
    "reviewed_by_user_id": 1,
    "reviewed_at": "2025-01-02T09:00:00.000000+05:30",
    "review_note": "approved by ops",
    "organization_id": 7,
    "requested_by_name": ""
  }
}
```

A request that was already reviewed answers HTTP 400 `organization request has
already been reviewed`, and approving with a name that is already taken answers
HTTP 409 `organization name is already in use`. As with the withdrawal above, the
`requested_by_name` field is empty in this response because the review returns the
stored row.

______________________________________________________________________

### Joining and invitations

#### POST /api/organizations/join

Join an organization with an invitation code. The code is consumed atomically, and
an account that previously left the organization has its membership restored as a
`member`. The response is the joined organization with `my_role: "member"`
(`cmd/organizations.go:469`, `internal/core/organizations.go:358`). The
workspace-selection page posts to this endpoint with the invite-code form.

##### Parameters

| Name | Type   | Required | Description                            |
| :--- | :----- | :------- | :------------------------------------- |
| code | String | Yes      | Plaintext invitation code; HTTP 400 when empty. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/join' \
--header 'Content-Type: application/json' \
--data '{"code": "Gh3k...invite-code"}'
```

##### Example Response

```json
{
  "data": {
    "id": 7,
    "name": "Acme",
    "description": "Acme marketing team",
    "status": "active",
    "created_by_user_id": 1,
    "archived_at": null,
    "member_count": 5,
    "my_role": "member"
  }
}
```

Errors: HTTP 404 `invitation is invalid or revoked` (also when the organization is
not active), HTTP 400 `invitation has expired`,
`invitation has reached its usage limit`, or
`user is already an organization member` (`internal/core/organizations.go:372`).

#### POST /api/organizations/leave

Leave the organization selected by the workspace header. The caller's resources in
that organization become pending-transfer rows and their campaigns are handled
exactly like the manager-driven removal above
(`cmd/organizations.go:574`). No permission beyond active membership is required.

There is no request body.

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/leave' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": true
}
```

A personal workspace answers HTTP 400 `select an organization workspace`, an
archived organization answers HTTP 409 `organization is archived`, and the last
remaining manager cannot leave (HTTP 400
`an organization must retain at least one manager`).

#### GET /api/organizations/invites

List the invitations of the active organization, newest first. Requires
organization manager access (`cmd/organizations.go:882`,
`internal/core/organizations.go:320`).

The invitation hash is never serialized (`code_hash` has JSON tag `-`), and the
plaintext `code` is only present in the creation response. Expiry and usage are
reported through `expires_at`, `max_uses` and `use_count`
(`models/organizations.go:77`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/invites' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 12,
      "created_at": "2025-01-01T09:00:00.000000+05:30",
      "updated_at": "2025-01-01T09:00:00.000000+05:30",
      "organization_id": 7,
      "name": "Field team",
      "created_by_user_id": 4,
      "expires_at": "2025-02-01T09:00:00.000000+05:30",
      "revoked_at": null,
      "max_uses": 10,
      "use_count": 2,
      "organization_name": "Acme"
    }
  ]
}
```

#### POST /api/organizations/invites

Create a reusable invitation for the active organization. The plaintext code is
generated server-side (24 random bytes, base64url encoded), stored only as a
SHA-256 hash, and returned once in the response as `code`; it cannot be retrieved
again. Requires organization manager access
(`cmd/organizations.go:894`, `cmd/organizations.go:967`,
`internal/core/organizations.go:267`).

##### Parameters

| Name       | Type    | Required | Description                                                                                       |
| :--------- | :------ | :------- | :------------------------------------------------------------------------------------------------ |
| name       | String  | No       | Label for the invitation.                                                                          |
| expires_at | String  | No       | Future RFC3339 timestamp; anything else answers HTTP 400 `expiration must be a future RFC3339 timestamp`. |
| max_uses   | Number  | No       | Maximum number of uses; a value below 1 answers HTTP 400 `maximum uses must be greater than zero`. Omit for unlimited. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/invites' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"name": "Field team", "expires_at": "2025-02-01T09:00:00+05:30", "max_uses": 10}'
```

##### Example Response

```json
{
  "data": {
    "id": 12,
    "organization_id": 7,
    "name": "Field team",
    "created_by_user_id": 4,
    "expires_at": "2025-02-01T09:00:00.000000+05:30",
    "revoked_at": null,
    "max_uses": 10,
    "use_count": 0,
    "organization_name": "Acme",
    "code": "Gh3k...invite-code"
  }
}
```

HTTP 201 is returned on success.

#### DELETE /api/organizations/invites/{id}

Revoke an invitation of the active organization by stamping `revoked_at`. Requires
organization manager access; an invitation that is not active (or belongs to
another organization) answers HTTP 404
`active organization invitation not found` (`cmd/organizations.go:933`,
`internal/core/organizations.go:332`).

##### Parameters

| Name | Type   | Required | Description       |
| :--- | :----- | :------- | :---------------- |
| id   | Number | Yes      | Invitation ID.     |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/organizations/invites/12' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

### Personal resource migration

Both migration endpoints copy or move the caller's own personal (`organization_id`
NULL) resources into an organization. The destination defaults to the active
workspace, which lets a client offer a direct "move to current organization"
action; `target_organization_id` explicitly selects another organization and is
validated through the same workspace resolver. The destination must be an
organization workspace (HTTP 400 `select an organization destination`) and must not
be archived (HTTP 409). Sources must be owned by the caller, and the legacy
creation grant for the resource type is still required, so an organization
boundary can never be widened by a global role
(`cmd/organizations.go:696`, `cmd/organizations.go:832`).

For templates, campaigns, and media only `visibility = 'private'` rows move, so a
global asset cannot change its publication contract
(`internal/core/workspace_migration.go:210`). Customer lists are owner-private by
design: the migration takes the caller's own personal rows, normalizes them to
`private`, and excludes first-level pools and secondary lists, which are platform
assets rather than personal resources (`internal/core/workspace_migration.go:95`,
`internal/core/workspace.go:384`; see [Public pools](pools.md)).

#### POST /api/organizations/resources/migrate

Copy or move personal templates, draft campaigns, media files or customer lists.
Requires the matching legacy grant: `templates:manage`, `campaigns:manage`
(any), `media:manage`, or `customer_lists:manage_all` respectively.

##### Parameters

| Name                   | Type    | Required | Description                                                                          |
| :--------------------- | :------ | :------- | :----------------------------------------------------------------------------------- |
| resource               | String  | Yes      | `customer_lists`, `templates`, `campaigns` or `media`; anything else answers HTTP 400 `unsupported personal resource type`. |
| ids                    | Array   | Yes      | Non-empty list of the caller's source resource IDs.                                   |
| mode                   | String  | Yes      | `copy` or `move`; anything else answers HTTP 400 `migration mode must be copy or move`. |
| target_organization_id | Number  | No       | Destination organization; defaults to the active workspace.                            |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/resources/migrate' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"resource": "templates", "ids": [12, 13], "mode": "move"}'
```

##### Example Response

```json
{
  "data": {
    "resource": "templates",
    "ids": [12, 13],
    "mode": "move"
  }
}
```

The returned `ids` are the identifiers the resources have after the migration;
migrated transactional templates are recompiled and cached
(`cmd/organizations.go:854`).

#### POST /api/organizations/resources/customer-lists/migrate

Customer-list-specific variant of the previous endpoint, kept for API
compatibility. It uses the same core migration and subscription-merge guarantees
but checks `customer_lists:manage_all` explicitly
(`cmd/organizations.go:700`, `internal/core/workspace_migration.go:73`).

##### Parameters

| Name                   | Type   | Required | Description                                                                               |
| :--------------------- | :----- | :------- | :------------------------------------------------------------------------------------------ |
| customer_list_ids      | Array  | Yes      | Non-empty list of the caller's personal customer_list IDs; HTTP 400 `at least one customer_list is required`. |
| mode                   | String | Yes      | `copy` or `move`.                                                                           |
| target_organization_id | Number | No       | Destination organization; defaults to the active workspace.                                  |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/resources/customer-lists/migrate' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"customer_list_ids": [3, 4], "mode": "copy"}'
```

##### Example Response

```json
{
  "data": {
    "customer_list_ids": [3, 4],
    "mode": "copy"
  }
}
```

______________________________________________________________________

### Resource transfer between members

#### POST /api/organizations/resources/transfer

Give every pending resource left behind by a former member of the active
organization to an active member of that organization. Customer conflicts are
merged by scoped e-mail, preserving subscriptions and historical analytics, and
customer lists are moved first so merged subscriptions point at the transferred
lists. Requires organization manager (or platform) access
(`cmd/organizations.go:602`, `internal/core/organizations.go:633`).

##### Parameters

| Name           | Type   | Required | Description                                                                             |
| :------------- | :----- | :------- | :--------------------------------------------------------------------------------------- |
| target_user_id | Number | Yes      | Active organization member that receives the pending resources; HTTP 400 `target user is required` when missing or below 1. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/resources/transfer' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"target_user_id": 4}'
```

##### Example Response

```json
{
  "data": true
}
```

A target that is not an active member answers HTTP 403
`not an active organization member`, and an archived organization answers HTTP 409
`archived organization resources must be transferred through the archive cleanup
flow` (`internal/core/organizations.go:646`).

#### POST /api/organizations/{id}/resources/transfer

Platform cleanup counterpart that moves the complete pending resource set of an
**archived** organization into the personal workspace of one of its active
members. It is path-based because an archived organization cannot be selected as a
workspace. Requires platform administrator or `organizations:platform_manage`
(`cmd/organizations.go:627`, `internal/core/organizations.go:722`).

##### Parameters

| Name           | Type   | Required | Description                                                     |
| :------------- | :----- | :------- | :---------------------------------------------------------------- |
| id             | Number | Yes      | Organization ID.                                                  |
| target_user_id | Number | Yes      | Active member that receives the resources; HTTP 400 when missing.  |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/7/resources/transfer' \
--header 'Content-Type: application/json' \
--data '{"target_user_id": 4}'
```

##### Example Response

```json
{
  "data": true
}
```

An organization that is not archived answers HTTP 409 `archive the organization
before transferring its resources`, and a non-member target answers HTTP 403
`not an active organization member`.

#### POST /api/organizations/templates/{id}/transfer

Hand an organization-shared template to another active member of the active
organization, as a complete ownership hand-off. Private media referenced by the
template is cloned into the target's workspace so the new owner can edit and send
it. Private and global templates deliberately do not use this path. Requires
organization manager access (`cmd/organizations.go:664`,
`internal/core/organizations.go:843`).

##### Parameters

| Name           | Type   | Required | Description                                                              |
| :------------- | :----- | :------- | :------------------------------------------------------------------------- |
| id             | Number | Yes      | Template ID.                                                               |
| target_user_id | Number | Yes      | Active organization member that becomes the new owner; HTTP 400 when missing. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/templates/9/transfer' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"target_user_id": 4}'
```

##### Example Response

```json
{
  "data": true
}
```

HTTP 404 `organization-shared template not found` is returned when the template is
not organization-shared in the active organization or is already pending transfer,
and an archived organization answers HTTP 409
(`internal/core/organizations.go:856`).

#### POST /api/organizations/templates/{id}/unpublish

Remove the organization-wide visibility of a shared template; the template stays
with its current owner as a private template. Global templates cannot be
unpublished here. Requires organization manager access
(`cmd/organizations.go:684`, `internal/core/organizations.go:992`).

##### Parameters

| Name | Type   | Required | Description   |
| :--- | :----- | :------- | :------------ |
| id   | Number | Yes      | Template ID.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/templates/9/unpublish' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": true
}
```

HTTP 404 `organization-shared template not found` is returned when the template is
not organization-shared or is pending transfer.

______________________________________________________________________

### Reply forwarding

When a member leaves or is removed, the dedicated reply mailboxes used by their
organization campaigns are retained (`status: retained`) and a server-side
forwarding rule keeps customer replies flowing to the organization creator, or to
the first active manager when the creator is gone. These three endpoints let an
organization manager inspect, pause and resume that relay without exposing mailbox
credentials (`cmd/reply_forward_rules.go:65`). All of them require organization
manager access (`cmd/reply_forward_rules.go:131`).

#### GET /api/organizations/reply-forwarding

List the forwarding rules of the active organization, active rules first, then the
most recently updated ones. The rows combine the rule with the source mailbox and
the former member that owned it
(`cmd/reply_forward_rules.go:12`, `cmd/reply_forward_rules.go:131`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/organizations/reply-forwarding' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 5,
      "created_at": "2025-01-01T09:00:00.000000+05:30",
      "updated_at": "2025-01-01T09:00:00.000000+05:30",
      "reply_mailbox_id": 3,
      "organization_id": 7,
      "target_user_id": 1,
      "target_email": "owner@example.com",
      "status": "active",
      "disabled_at": null,
      "disabled_by": null,
      "last_error": "",
      "last_forward_at": "2025-01-02T09:00:00.000000+05:30",
      "mailbox_email": "replies@example.com",
      "mailbox_name": "Replies",
      "source_user_id": 4,
      "source_email": "jane@example.com",
      "source_name": "Jane Doe"
    }
  ]
}
```

#### PUT /api/organizations/reply-forwarding/{id}

Enable or disable a forwarding rule of the active organization. Enabling always
re-resolves the target address, so a creator who has left is replaced by an active
organization manager; disabling stamps `disabled_at`/`disabled_by` on the rule.
Requires organization manager access (`cmd/reply_forward_rules.go:158`).

##### Parameters

| Name   | Type   | Required | Description                                                                        |
| :----- | :----- | :------- | :----------------------------------------------------------------------------------- |
| id     | Number | Yes      | Forwarding rule ID.                                                                  |
| status | String | Yes      | `active` or `disabled`; anything else answers HTTP 400 `status must be active or disabled`. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/organizations/reply-forwarding/5' \
--header 'X-Listmonk-Organization-ID: 7' \
--header 'Content-Type: application/json' \
--data '{"status": "disabled"}'
```

##### Example Response

```json
{
  "data": true
}
```

A rule that does not exist in the active organization answers HTTP 404
`reply forwarding rule not found`. Enabling answers HTTP 409
`organization has no active manager with a work email for reply forwarding` when no
target can be resolved.

#### DELETE /api/organizations/reply-forwarding/{id}

Disable a forwarding rule of the active organization. The source mailbox and its
original messages are intentionally retained; this is an explicit alias for
`status: disabled` on the previous endpoint
(`cmd/reply_forward_rules.go:202`, `cmd/reply_forward_rules.go:204`). Requires
organization manager access.

##### Parameters

| Name | Type   | Required | Description          |
| :--- | :----- | :------- | :------------------- |
| id   | Number | Yes      | Forwarding rule ID.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/organizations/reply-forwarding/5' \
--header 'X-Listmonk-Organization-ID: 7'
```

##### Example Response

```json
{
  "data": true
}
```

HTTP 404 `reply forwarding rule not found` is returned when the rule does not exist
in the active organization.

______________________________________________________________________

### Organization lifecycle

#### POST /api/organizations/{id}/archive

Archive an organization. Its resources become explicit cleanup items, scheduled and
deferred campaigns become drafts, running campaigns are paused, a running
organization import is stopped, retained reply mailboxes keep receiving customer
replies through forwarding rules, and non-admin members can no longer select the
organization as a workspace. Requires platform administrator or
`organizations:platform_manage`
(`cmd/handlers.go:323`, `cmd/organizations.go:416`,
`internal/core/organizations.go:1028`).

##### Parameters

| Name | Type   | Required | Description       |
| :--- | :----- | :------- | :---------------- |
| id   | Number | Yes      | Organization ID.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/organizations/7/archive'
```

##### Example Response

```json
{
  "data": true
}
```

An organization that is already archived answers HTTP 409
`organization is already archived`.

#### DELETE /api/organizations/{id}

Permanently delete an archived organization after its member data and resources
have been transferred or cleaned up. The endpoint refuses an organization that is
not archived (HTTP 409 `archive the organization before permanently deleting it`)
and refuses an archived organization that still owns rows in `customer_lists`,
`customers`, `templates`, `campaigns` or `media` (HTTP 409
`transfer or clean all organization resources before permanently deleting it`).
Invitations and memberships are deleted with it, and the creation request remains
as an audit record. Requires platform administrator or
`organizations:platform_manage` (`cmd/handlers.go:324`,
`cmd/organizations.go:457`, `internal/core/organizations.go:1116`).

##### Parameters

| Name | Type   | Required | Description       |
| :--- | :----- | :------- | :---------------- |
| id   | Number | Yes      | Organization ID.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/organizations/7'
```

##### Example Response

```json
{
  "data": true
}
```
