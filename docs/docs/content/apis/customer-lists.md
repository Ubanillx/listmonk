# API / CustomerLists

Customer lists are exposed under `/api/customer-lists`. In addition to private
and public subscription lists, the API supports first-level public pools
(`pool`) and organization allocations (`org_pool_allocation`). See [Public pools](pools.md)
for masking, assignment, exclusions, merge, and internal reply mailbox rules.

Authenticated responses are scoped by the active workspace selected through
`X-Listmonk-Organization-ID`. In an organization workspace, ordinary lists
from the personal workspace or another organization are not returned; in the
personal workspace, only the caller's personal lists are returned. Authorized
public pools remain an explicit cross-workspace delivery/import exception.

List responses include `organization_name` when a row belongs to an
organization and `owner_username`/`owner_name` for the user owner. A
`org_pool_allocation` is displayed as belonging to its target organization; its
creator remains in the owner fields for audit and ownership checks. A
first-level `pool` is platform-wide (`visibility: global`), while its
organization delivery permissions are stored separately.

| Method | Endpoint                                        | Description               |
| :----- | :---------------------------------------------- | :------------------------ |
| GET | [/api/customer-lists](#get-apicustomer-lists) | Retrieve all customer_lists. |
| GET | [/api/public/customer-lists](#get-apipubliccustomer-lists) | Retrieve public customer_lists. |
| GET | [/api/customer-lists/{customer_list_id}](#get-apicustomer-listscustomer_list_id) | Retrieve a specific customer_list. |
| GET | [/api/customer-lists/{customer_list_id}/pool-contacts](#get-apicustomer-listscustomer_list_idpool-contacts) | Retrieve a page of pool contacts. |
| GET | [/api/customer-lists/{customer_list_id}/pool-contacts/export](#get-apicustomer-listscustomer_list_idpool-contactsexport) | Export pool contacts as CSV. |
| GET | [/api/customer-lists/{customer_list_id}/org-pool-allocations](#get-apicustomer-listscustomer_list_idorg-pool-allocations) | Retrieve organization allocations of a pool customer_list. |
| POST | [/api/customer-lists](#post-apicustomer-lists) | Create a new customer_list. |
| POST | [/api/customer-lists/{customer_list_id}/pool-contacts](#post-apicustomer-listscustomer_list_idpool-contacts) | Add a contact to a pool customer_list. |
| PUT | [/api/customer-lists/{customer_list_id}](#put-apicustomer-listscustomer_list_id) | Update a customer_list. |
| DELETE | [/api/customer-lists/{customer_list_id}](#delete-apicustomer-listscustomer_list_id) | Delete a customer_list. |
| DELETE | [/api/customer-lists/{customer_list_id}/pool-contacts/{contact_id}/email](#delete-apicustomer-listscustomer_list_idpool-contactscontact_idemail) | Clear a pool contact's email address. |
| DELETE | [/api/customer-lists](#delete-apicustomer-lists) | Delete multiple customer_lists. |

______________________________________________________________________

#### GET /api/customer-lists

Retrieve customer_lists.

> **Note:** CustomerLists with `status: archived` are hidden from customer_list selectors in campaigns, public subscription forms, and roles by default. They can only be viewed by filtering with `status=archived` or by viewing all customer_lists without a status filter.

##### Parameters

| Name     | Type     | Required | Description                                                                                        |
| :------- | :------- | :------- | :------------------------------------------------------------------------------------------------- |
| query    | string   |          | String for customer_list name search.                                                                       |
| status   | string   |          | Status to filter customer_lists. Options: active, archived. Defaults to showing all customer_lists if not specified. |
| minimal  | boolean  |          | If true, returns customer_lists without customer counts (faster). Defaults to false.                      |
| tag      | []string |          | Tags to filter customer_lists. Repeat in the query for multiple values.                                     |
| order_by | string   |          | Sort field. Options: name, status, created_at, updated_at.                                         |
| order    | string   |          | Sorting order. Options: ASC, DESC.                                                                 |
| page     | number   |          | Page number for pagination.                                                                        |
| per_page | number   |          | Results per page. Set to 'all' to return all results.                                              |

##### Example Request

```shell
# Get all customer_lists
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists?page=1&per_page=100'

# Get only active customer_lists
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists?status=active&per_page=100'

# Get archived customer_lists with minimal data
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists?status=archived&minimal=true&per_page=all'
```

##### Example Response

```json
{
    "data": {
        "results": [
            {
                "id": 1,
                "created_at": "2020-02-10T23:07:16.194843+01:00",
                "updated_at": "2020-03-06T22:32:01.118327+01:00",
                "uuid": "ce13e971-c2ed-4069-bd0c-240e9a9f56f9",
                "name": "Default customer_list",
                "type": "public",
                "optin": "double",
                "status": "active",
                "tags": [
                    "test"
                ],
                "customer_count": 2
            },
            {
                "id": 2,
                "created_at": "2020-03-04T21:12:09.555013+01:00",
                "updated_at": "2020-03-06T22:34:46.405031+01:00",
                "uuid": "f20a2308-dfb5-4420-a56d-ecf0618a102d",
                "name": "get",
                "type": "private",
                "optin": "single",
                "status": "active",
                "tags": [],
                "customer_count": 0
            }
        ],
        "total": 5,
        "per_page": 20,
        "page": 1
    }
}
```

______________________________________________________________________

#### GET /api/public/customer-lists

Retrieve public customer_lists with name and uuid to submit a subscription. This is an unauthenticated call to enable scripting to subscription form.

> **Note:** This endpoint only returns customer_lists with `type: public` and `status: active`. Archived customer_lists are never shown on public subscription forms.

##### Example Request

```shell
curl -X GET 'http://localhost:9000/api/public/customer-lists'
```

##### Example Response

```json
[
  {
    "uuid": "55e243af-80c6-4169-8d7f-bc571e0269e9",
    "name": "Opt-in customer_list"
  }
]
```
______________________________________________________________________

#### GET /api/customer-lists/{customer_list_id}

Retrieve a specific customer_list.

##### Parameters

| Name    | Type   | Required | Description                 |
| :------ | :----- | :------- | :-------------------------- |
| customer_list_id | number | Yes      | ID of the customer_list to retrieve. |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists/5'
```

##### Example Response

```json
{
    "data": {
        "id": 5,
        "created_at": "2020-03-07T06:31:06.072483+01:00",
        "updated_at": "2020-03-07T06:31:06.072483+01:00",
        "uuid": "1bb246ab-7417-4cef-bddc-8fc8fc941d3a",
        "name": "Test customer_list",
        "type": "public",
        "optin": "double",
        "status": "active",
        "tags": [],
        "customer_count": 0
    }
}
```

______________________________________________________________________

#### POST /api/customer-lists

Create a new customer_list.

##### Parameters

| Name        | Type       | Required | Description                                                        |
| :---------- | :--------- | :------- | :----------------------------------------------------------------- |
| name        | string     | Yes      | Name of the new customer_list.                                              |
| type        | string     | Yes      | Type of customer_list. Options: private, public.                            |
| optin       | string     | Yes      | Opt-in type. Options: single, double.                              |
| status      | string     | No       | Status of the customer_list. Options: active, archived. Defaults to active. |
| tags        | string\[\] |          | Associated tags for a customer_list.                                        |
| description | string     | No       | Description of the new customer_list.                                       |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/customer-lists'
```

##### Example Response

```json
{
    "data": {
        "id": 5,
        "created_at": "2020-03-07T06:31:06.072483+01:00",
        "updated_at": "2020-03-07T06:31:06.072483+01:00",
        "uuid": "1bb246ab-7417-4cef-bddc-8fc8fc941d3a",
        "name": "Test customer_list",
        "type": "public",
        "optin": "single",
        "status": "active",
        "tags": [],
        "customer_count": 0,
        "description": "This is a test customer_list"
    }
}
```

______________________________________________________________________

#### PUT /api/customer-lists/{customer_list_id}

Update a customer_list.

##### Parameters

| Name        | Type       | Required | Description                                    |
| :---------- | :--------- | :------- | :--------------------------------------------- |
| customer_list_id     | number     | Yes      | ID of the customer_list to update.                      |
| name        | string     |          | New name for the customer_list.                         |
| type        | string     |          | Type of customer_list. Options: private, public.        |
| optin       | string     |          | Opt-in type. Options: single, double.          |
| status      | string     |          | Status of the customer_list. Options: active, archived. |
| tags        | string\[\] |          | Associated tags for the customer_list.                  |
| description | string     |          | Description of the customer_list.                       |

##### Example Request

```shell
curl -u "api_user:token" -X PUT 'http://localhost:9000/api/customer-lists/5' \
--form 'name=modified test customer_list' \
--form 'type=private'
```

##### Example Response

```json
{
    "data": {
        "id": 5,
        "created_at": "2020-03-07T06:31:06.072483+01:00",
        "updated_at": "2020-03-07T06:52:15.208075+01:00",
        "uuid": "1bb246ab-7417-4cef-bddc-8fc8fc941d3a",
        "name": "modified test customer_list",
        "type": "private",
        "optin": "single",
        "status": "active",
        "tags": [],
        "customer_count": 0,
        "description": "This is a test customer_list"
    }
}
```

______________________________________________________________________

#### DELETE /api/customer-lists/{customer_list_id}

Delete a specific customer_list.

##### Parameters

| Name    | Type   | Required | Description               |
| :------ | :----- | :------- | :------------------------ |
| customer_list_id | Number | Yes      | ID of the customer_list to delete. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/customer-lists/1'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### DELETE /api/customer-lists

Delete multiple customer_lists by IDs or by a search query.

> **Note:** Users can only delete customer_lists they have `manage` permission for. Any customer_lists in the query that the user doesn't have permission to manage is ignored.

##### Parameters

| Name  | Type       | Required                      | Description                                                        |
| :---- | :--------- | :---------------------------- | :----------------------------------------------------------------- |
| id    | number\[\] | Yes (if `query` not provided) | One or more customer_list IDs to delete.                                    |
| query | string     | Yes (if `id` not provided)    | Search query to filter customer_lists for deletion (same as the GET query). |

##### Example Request (by IDs)

```shell
curl -u "api_user:token" -X DELETE 'http://localhost:9000/api/customer-lists?id=10&id=11&id=12'
```

##### Example Request (by search query)

```shell
curl -u "api_user:token" -X DELETE 'http://localhost:9000/api/customer-lists?query=test%20list'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### GET /api/customer-lists/{customer_list_id}/pool-contacts

Retrieve one server-paginated page of the contacts of a `pool` or `org_pool_allocation` customer_list. This is a compatibility alias of `GET /api/pools/:id/contacts` for first-level pools; for a pool allocation, the response is limited to that allocation. See [Public pools](pools.md) for masking and exclusion rules.

Highest administrators receive complete contact records. Other callers receive only the contacts of their own organization's allocation, with masked e-mail addresses.

> **Note:** Requires the `pools:get` permission (platform administrators bypass the role grant) and the `customer_lists:read` API-key scope (`cmd/handlers.go:181`). A non-platform-admin caller must have the pool granted to the active organization.

##### Parameters

| Name | Type | Required | Description |
| :--- | :--- | :------- | :---------- |
| customer_list_id | number | Yes | ID of a `pool` or `org_pool_allocation` customer_list. |
| search | string | | Case-insensitive substring filter on the customer code, name, or e-mail. |
| customer_code | string | | Deprecated alias of `search` used only when `search` is empty. |
| page | number | | Page number, starting at 1. Default 1. |
| per_page | number | | Page size. Default 20, maximum 50. `0` returns every row. |
| order_by | string | | Sort column: `id`, `customer_code`, `name`, `email`, `allocation_department`, `status`, `created_at`, `updated_at`. Default `id`. |
| order | string | | `asc` or `desc`. Default `desc`. |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists/5/pool-contacts?search=A100&page=1&per_page=20&order_by=created_at&order=desc'
```

##### Example Response

```json
{
    "data": {
        "results": [
            {
                "id": 1,
                "uuid": "c2cc0b31-b485-4d72-8ce8-b47081beadec",
                "customer_code": "A100",
                "email": "johx@example.com",
                "name": "John Doe",
                "allocation_department": "Sales",
                "status": "active",
                "created_at": "2026-09-17T10:00:00Z",
                "updated_at": "2026-09-17T10:00:00Z"
            }
        ],
        "search": "A100",
        "query": "",
        "total": 1,
        "per_page": 20,
        "page": 1
    }
}
```

______________________________________________________________________

#### GET /api/customer-lists/{customer_list_id}/pool-contacts/export

Stream the filtered pool contacts as CSV, in the same order as the listing and independent of pagination. Non-platform-administrators may only export pools granted to the active organization and always receive masked e-mail addresses.

> **Note:** Requires the `pools:export` permission and the `customer_lists:read` API-key scope. Accepts the same `search`, `customer_code`, `order_by`, and `order` filters as the listing.

##### Parameters

| Name | Type | Required | Description |
| :--- | :--- | :------- | :---------- |
| customer_list_id | number | Yes | ID of a `pool` or `org_pool_allocation` customer_list. |
| search | string | | Case-insensitive substring filter on the customer code, name, or e-mail. |
| order_by | string | | Sort column, as in the listing. |
| order | string | | `asc` or `desc`. |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists/5/pool-contacts/export?search=A100' -o pool-contacts.csv
```

The response is `text/csv` with the columns `customer_code`, `name`, `email`, `allocation_department`, `status`, `created_at`, `updated_at`.

______________________________________________________________________

#### GET /api/customer-lists/{customer_list_id}/org-pool-allocations

Retrieve the organization allocations bound to a `pool` customer_list. This is a compatibility alias of `GET /api/pools/:id/allocations`; see [Public pools](pools.md).

Highest administrators receive every allocation of the pool. Other callers receive only the allocation of their own organization.

> **Note:** Requires the `customer_lists:read` API-key scope (`cmd/handlers.go:182`).

##### Parameters

| Name | Type | Required | Description |
| :--- | :--- | :------- | :---------- |
| customer_list_id | number | Yes | ID of the pool customer_list. |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/customer-lists/5/org-pool-allocations'
```

##### Example Response

```json
{
    "data": [
        {
            "id": 1,
            "list_id": 6,
            "list_name": "Sales allocation",
            "pool_id": 5,
            "organization_id": 2,
            "organization_name": "Sales",
            "reply_mailbox_id": 3,
            "reply_mailbox_email": "replies@example.com"
        }
    ]
}
```

______________________________________________________________________

#### POST /api/customer-lists/{customer_list_id}/pool-contacts

Add a single contact to a `pool` customer_list. This is a compatibility alias of `POST /api/pools/:id/contacts`; see [Public pools](pools.md). Requires the `pools:manage` permission; a non-platform-admin caller may only add contacts to its own organization and the server pins `allocation_department` to that organization.

##### Parameters

| Name | Type | Required | Description |
| :--- | :--- | :------- | :---------- |
| customer_list_id | number | Yes | ID of the pool customer_list. |
| email | string | Yes | Contact e-mail address. |
| customer_code | string | | Imported customer code. May repeat across contacts. |
| name | string | | Contact person name. |
| allocation_department | string | | Must match the name of an active organization. Platform administrators only; other callers are pinned to their own organization. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/customer-lists/5/pool-contacts' \
-H 'Content-Type: application/json' \
--data-raw '{"customer_code":"A100","email":"john@example.com","name":"John Doe","allocation_department":"Sales"}'
```

##### Example Response

```json
{
    "data": {
        "id": 1,
        "uuid": "c2cc0b31-b485-4d72-8ce8-b47081beadec",
        "customer_code": "A100",
        "email": "john@example.com",
        "name": "John Doe",
        "allocation_department": "Sales",
        "status": "active"
    }
}
```

______________________________________________________________________

#### DELETE /api/customer-lists/{customer_list_id}/pool-contacts/{contact_id}/email

Clear the stored e-mail address of a pool contact. This is a compatibility alias of the pool contact e-mail cleanup route; see [Public pools](pools.md). Requires the `pools:manage` permission; non-platform-administrators may only clear contacts of their own organization's allocation.

##### Parameters

| Name | Type | Required | Description |
| :--- | :--- | :------- | :---------- |
| customer_list_id | number | Yes | ID of a `pool` or `org_pool_allocation` customer_list; the first-level pool is resolved server-side. |
| contact_id | number | Yes | ID of the pool contact. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/customer-lists/5/pool-contacts/1/email'
```

##### Example Response

```json
{
    "data": true
}
```
