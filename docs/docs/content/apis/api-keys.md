# API / Personal API keys

Regular users manage their own Bearer credentials in `Profile -> API Keys`.
Every personal key is bound to one workspace, has one or more business scopes,
and expires at the end of the selected month. Each user may have at most ten
active keys for the same workspace. The secret is returned only by create and
rotate responses.

| Method | Endpoint | Description |
| :-- | :-- | :-- |
| GET | `/api/profile/api-key-scopes` | List selectable business scopes. |
| GET | `/api/profile/api-keys` | List the current user's keys and metadata. |
| POST | `/api/profile/api-keys` | Create a workspace-bound key. |
| PUT | `/api/profile/api-keys/{id}` | Update a key's name, scopes, or expiry. |
| POST | `/api/profile/api-keys/{id}/rotate` | Immediately revoke and replace a key. |
| DELETE | `/api/profile/api-keys/{id}` | Revoke a key. |

The profile endpoints require a browser session. A personal API key cannot
manage keys, profiles, SMTP settings, users, roles, settings, or organization
membership.

## Create a key

`workspace_organization_id` is `0` for the personal workspace and an active
organization ID for an organization workspace. `expires_at` is a required
`YYYY-MM` value between one and 24 months from the current month.

```json
{
  "name": "openclaw-production",
  "workspace_organization_id": 42,
  "scopes": [
    "lists:read",
    "lists:write",
    "subscribers:write",
    "subscribers:import",
    "templates:read",
    "templates:write",
    "campaigns:read",
    "campaigns:write",
    "campaigns:send",
    "campaigns:analytics"
  ],
  "expires_at": "2027-03"
}
```

The response includes metadata and one `token` value prefixed with `lmpk_`.
Store it in the integration secret store immediately; it cannot be read again.

## Scope behavior

Scopes only narrow API access. The key still uses its creator's user role,
list-role grants, organization membership, ownership checks, and campaign SMTP
ownership rules. `campaigns:send` is required when setting a campaign to
`running` or `scheduled`.

The API key always selects its bound workspace. Omitting the workspace header
uses that workspace; supplying a different `X-Listmonk-Organization-ID` is
rejected.

## Rotate and revoke

Rotation returns a replacement secret and revokes the old key immediately.
Use the new credential before making the next request. Revocation also takes
effect immediately.
