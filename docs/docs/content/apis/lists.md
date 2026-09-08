# API / CustomerLists

| Method | Endpoint                                        | Description               |
| :----- | :---------------------------------------------- | :------------------------ |
| GET    | [/api/customer-lists](#get-apilists)                     | Retrieve all customer_lists.       |
| GET    | [/api/public/customer-lists](#get-public-apilists)       | Retrieve public customer_lists.    |
| GET    | [/api/customer-lists/{customer_list_id}](#get-apilistscustomer_list_id)    | Retrieve a specific customer_list. |
| POST   | [/api/customer-lists](#post-apilists)                    | Create a new customer_list.        |
| PUT    | [/api/customer-lists/{customer_list_id}](#put-apilistscustomer_list_id)    | Update a customer_list.            |
| DELETE | [/api/customer-lists/{customer_list_id}](#delete-apilistscustomer_list_id) | Delete a customer_list.            |
| DELETE | [/api/customer-lists](#delete-apilists)                  | Delete multiple customer_lists.    |

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
