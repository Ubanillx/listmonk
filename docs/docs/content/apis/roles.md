# API / Roles

`/api/roles/*` manages the platform-level role definitions behind
`Admin -> Users -> User roles`: **user roles** (named sets of user permissions)
and **customer_list roles** (named sets of per-customer_list
`customer_list:get` / `customer_list:manage` grants). The permission model
itself, including permission groups and how roles combine with workspaces and
resource ownership, is documented in
[User roles and permissions](../roles-and-permissions.md); this page documents
only the HTTP surface. Authentication, response envelopes, and error shapes are
described in the [API introduction](apis.md).

| Method | Endpoint                                                        | Description                                                       |
| :----- | :-------------------------------------------------------------- | :---------------------------------------------------------------- |
| GET    | [/api/roles/users](#get-apirolesusers)                          | List all user roles.                                              |
| GET    | [/api/roles/customer-lists](#get-apirolescustomer-lists)        | List all customer_list roles and their per-customer_list grants.  |
| POST   | [/api/roles/users](#post-apirolesusers)                         | Create a user role.                                               |
| POST   | [/api/roles/customer-lists](#post-apirolescustomer-lists)       | Create a customer_list role.                                      |
| PUT    | [/api/roles/users/{id}](#put-apirolesusersid)                   | Update a user role.                                               |
| PUT    | [/api/roles/customer-lists/{id}](#put-apirolescustomer-listsid) | Update a customer_list role.                                      |
| DELETE | [/api/roles/{id}](#delete-apirolesid)                           | Delete a user or customer_list role.                              |

Reading requires the `roles:get` permission; creating, updating, and deleting
require `roles:manage`. Platform administrators bypass the permission check.
The endpoints accept a browser session or a legacy API-user token; personal API
keys created in `Profile -> API Keys` are rejected because `/api/roles` is not
part of the personal API key business surface. Role definitions are
platform-level: the handlers do not resolve a workspace, so
`X-Listmonk-Organization-ID` does not change their results.

Role names must be unique within a role type. A user role and a customer_list
role may share a name, but two roles of the same type may not
(`CREATE UNIQUE INDEX idx_roles_name ON roles (type, name)`, `schema.sql:431`).
Every role mutation is written to the business audit log with a stable action
(`role.user_created`, `role.customer_list_created`, `role.updated`,
`role.deleted`); created and updated roles additionally store a `name`/`type`
object summary in the event metadata. Role reads are not audited.

______________________________________________________________________

#### GET /api/roles/users

Returns all user roles ordered by creation time. Each entry contains `id`,
`created_at`, `updated_at`, `type` (`user`), `name`, `permissions` (the granted
user permissions), and `customer_lists`. User roles do not carry
per-customer_list grants, so `customer_lists` is not populated by this
endpoint; those grants belong to customer_list roles.

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/roles/users'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 2,
      "created_at": "2026-03-23T14:00:00.000000+08:00",
      "updated_at": "2026-03-23T14:00:00.000000+08:00",
      "type": "user",
      "name": "Campaign operator",
      "permissions": [
        "campaigns:get",
        "campaigns:manage",
        "customer_lists:get_all"
      ],
      "customer_lists": null
    }
  ]
}
```

______________________________________________________________________

#### GET /api/roles/customer-lists

Returns all customer_list roles ordered by creation time. Each entry contains
`id`, `created_at`, `updated_at`, `name`, and `customer_lists`: the granted
customer_lists, each with its `id`, `name`, and the granted `permissions`
(`customer_list:get` and/or `customer_list:manage`).

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/roles/customer-lists'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 5,
      "created_at": "2026-03-23T14:00:00.000000+08:00",
      "updated_at": "2026-03-23T14:30:00.000000+08:00",
      "name": "Organization A lists",
      "customer_lists": [
        {
          "id": 3,
          "name": "Product updates",
          "permissions": [
            "customer_list:get",
            "customer_list:manage"
          ]
        }
      ]
    }
  ]
}
```

______________________________________________________________________

#### POST /api/roles/users

Creates a user role. The name is trimmed and must be between 1 and 2000
characters. Every permission must exist in the platform permission list; an
unknown value is rejected with `Invalid fields: permission: <value>`. The
available permissions are listed in
[User roles and permissions](../roles-and-permissions.md).

##### Parameters

| Name        | Type             | Required | Description                                                                                                              |
| :---------- | :--------------- | :------- | :----------------------------------------------------------------------------------------------------------------------- |
| name        | string           | Yes      | Name of the role. Trimmed. Unique among user roles.                                                                      |
| permissions | array of strings | Yes      | User permissions to attach to the role, for example `campaigns:get`. Send an empty array to create a role with no permissions. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/roles/users' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Campaign operator",
    "permissions": ["campaigns:get", "campaigns:manage", "customer_lists:get_all"]
  }'
```

##### Example Response

```json
{
  "data": {
    "id": 2,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T14:00:00.000000+08:00",
    "type": "user",
    "name": "Campaign operator",
    "permissions": [
      "campaigns:get",
      "campaigns:manage",
      "customer_lists:get_all"
    ],
    "customer_lists": null
  }
}
```

______________________________________________________________________

#### POST /api/roles/customer-lists

Creates a customer_list role. The name is trimmed, must be between 1 and 2000
characters, and must be unique among customer_list roles.

##### Parameters

| Name           | Type              | Required | Description                                                                                                                                                                     |
| :------------- | :---------------- | :------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| name           | string            | Yes      | Name of the role. Trimmed.                                                                                                                                                       |
| customer_lists | array of objects  | No       | Per-customer_list grants. Each object carries `id` (the customer_list ID) and `permissions` (`customer_list:get` and/or `customer_list:manage`). Entries with an empty `permissions` array are ignored, and any other permission value is rejected with `Invalid fields: customer_list permission: <value>`. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/roles/customer-lists' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Organization A lists",
    "customer_lists": [
      {"id": 3, "permissions": ["customer_list:get", "customer_list:manage"]}
    ]
  }'
```

##### Example Response

The response returns the stored role record. The resolved grants are returned
by `GET /api/roles/customer-lists`.

```json
{
  "data": {
    "id": 5,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T14:00:00.000000+08:00",
    "name": "Organization A lists",
    "customer_lists": null
  }
}
```

______________________________________________________________________

#### PUT /api/roles/users/{id}

Updates a user role. The request body has the same shape as
`POST /api/roles/users` and replaces the role's name and permission list. Role
ID `1` (the Super Admin role) cannot be updated and returns `400 Invalid ID(s)`.

##### Parameters

| Name        | Type             | Required | Description                                                     |
| :---------- | :--------------- | :------- | :-------------------------------------------------------------- |
| id          | number           | Yes      | ID of the user role to update.                                  |
| name        | string           | Yes      | New name of the role. Trimmed.                                  |
| permissions | array of strings | Yes      | Full list of user permissions for the role after the update.    |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/roles/users/2' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Campaign operator",
    "permissions": ["campaigns:get", "campaigns:manage"]
  }'
```

##### Example Response

```json
{
  "data": {
    "id": 2,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T15:10:00.000000+08:00",
    "type": "user",
    "name": "Campaign operator",
    "permissions": [
      "campaigns:get",
      "campaigns:manage"
    ],
    "customer_lists": null
  }
}
```

______________________________________________________________________

#### PUT /api/roles/customer-lists/{id}

Updates a customer_list role. The request body has the same shape as
`POST /api/roles/customer-lists`. The `customer_lists` array is the complete
desired set of grants: grants for customer_lists that are omitted from the
array are deleted, and entries with an empty `permissions` array are skipped.
Role ID `1` cannot be updated and returns `400 Invalid ID(s)`.

##### Parameters

| Name           | Type             | Required | Description                                                    |
| :------------- | :--------------- | :------- | :------------------------------------------------------------- |
| id             | number           | Yes      | ID of the customer_list role to update.                        |
| name           | string           | Yes      | New name of the role. Trimmed.                                 |
| customer_lists | array of objects | No       | Complete set of per-customer_list grants after the update.     |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/roles/customer-lists/5' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Organization A lists",
    "customer_lists": [
      {"id": 3, "permissions": ["customer_list:get"]}
    ]
  }'
```

##### Example Response

```json
{
  "data": {
    "id": 5,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T15:10:00.000000+08:00",
    "name": "Organization A lists",
    "customer_lists": null
  }
}
```

______________________________________________________________________

#### DELETE /api/roles/{id}

Deletes a user or customer_list role. Role ID `1` (the Super Admin role) cannot
be deleted and returns `400 Invalid ID(s)`.

##### Parameters

| Name | Type   | Required | Description                             |
| :--- | :----- | :------- | :-------------------------------------- |
| id   | number | Yes      | ID of the user or customer_list role.   |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/roles/5'
```

##### Example Response

```json
{
  "data": true
}
```

!!! warning
    `users.user_role_id` references `roles(id)` with `ON DELETE RESTRICT`,
    while `users.list_role_id` references `roles(id)` with `ON DELETE CASCADE`
    (`schema.sql:445-446`). Deleting a user or customer_list role that is
    assigned to a user therefore does not silently detach it: a still-assigned
    user role fails to delete, and deleting a customer_list role that is
    assigned removes the user accounts that reference it. Check
    `Admin -> Users` before deleting a customer_list role.
