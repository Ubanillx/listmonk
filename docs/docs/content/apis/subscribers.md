# API / Customers

| Method | Endpoint                                                                                | Description                                    |
| ------ | --------------------------------------------------------------------------------------- | ---------------------------------------------- |
| GET    | [/api/customers](#get-apicustomers)                                                 | Query and retrieve customers.                |
| GET    | [/api/customers/{customer_id}](#get-apicustomerscustomer_id)                    | Retrieve a specific customer.                |
| GET    | [/api/customers/{customer_id}/export](#get-apicustomerscustomer_idexport)       | Export a specific customer.                  |
| GET    | [/api/customers/{customer_id}/bounces](#get-apicustomerscustomer_idbounces)     | Retrieve a  customer bounce records.         |
| POST   | [/api/customers](#post-apicustomers)                                                | Create a new customer.                       |
| POST   | [/api/customers/{customer_id}/optin](#post-apicustomerscustomer_idoptin)        | Sends optin confirmation email to customers. |
| POST   | [/api/public/subscription](#post-apipublicsubscription)                                 | Create a public subscription.                  |
| PUT    | [/api/customers/customer-lists](#put-apicustomerslists)                                      | Modify customer customer_list memberships.            |
| PUT    | [/api/customers/{customer_id}](#put-apicustomerscustomer_id)                    | Update a specific customer.                  |
| PUT    | [/api/customers/{customer_id}/blocklist](#put-apicustomerscustomer_idblocklist) | Blocklist a specific customer.               |
| PUT    | [/api/customers/blocklist](#put-apicustomersblocklist)                              | Blocklist one or many customers.             |
| PUT    | [/api/customers/query/blocklist](#put-apicustomersqueryblocklist)                   | Blocklist customers based on SQL expression. |
| DELETE | [/api/customers/{customer_id}](#delete-apicustomerscustomer_id)                 | Delete a specific customer.                  |
| DELETE | [/api/customers/{customer_id}/bounces](#delete-apicustomerscustomer_idbounces)  | Delete a specific customer's bounce records. |
| DELETE | [/api/customers](#delete-apicustomers)                                              | Delete one or more customers.                |
| POST   | [/api/customers/query/delete](#post-apicustomersquerydelete)                        | Delete customers based on SQL expression.    |

______________________________________________________________________

#### GET /api/customers

Retrieve all customers.

##### Query parameters

| Name                | Type   | Required | Description                                                           |
| :------------------ | :----- | :------- | :-------------------------------------------------------------------- |
| query               | string |          | Customer search by SQL expression.                                  |
| customer_list_id             | int[]  |          | ID of customer_lists to filter by. Repeat in the query for multiple values.    |
| subscription_status | string |          | Subscription status to filter by if there are one or more `customer_list_id`s. |
| order_by            | string |          | Result sorting field. Options: name, status, created_at, updated_at.  |
| order               | string |          | Sorting order: ASC for ascending, DESC for descending.                |
| page                | number |          | Page number for paginated results.                                    |
| per_page            | number |          | Results per page. Set as 'all' for all results.                       |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers?page=1&per_page=100'
```

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers?customer_list_id=1&customer_list_id=2&page=1&per_page=100'
```

```shell
curl -u 'api_username:access_token' -X GET 'http://localhost:9000/api/customers' \
    --url-query 'page=1' \
    --url-query 'per_page=100' \
    --url-query "query=customers.name LIKE 'Test%' AND customers.attribs->>'city' = 'Bengaluru'"
```

##### Example Response

```json
{
    "data": {
        "results": [
            {
                "id": 1,
                "created_at": "2020-02-10T23:07:16.199433+01:00",
                "updated_at": "2020-02-10T23:07:16.199433+01:00",
                "uuid": "ea06b2e7-4b08-4697-bcfc-2a5c6dde8f1c",
                "email": "john@example.com",
                "name": "John Doe",
                "attribs": {
                    "city": "Bengaluru",
                    "good": true,
                    "type": "known"
                },
                "status": "enabled",
                "customerLists": [
                    {
                        "subscription_status": "unconfirmed",
                        "id": 1,
                        "uuid": "ce13e971-c2ed-4069-bd0c-240e9a9f56f9",
                        "name": "Default customer_list",
                        "type": "public",
                        "tags": [
                            "test"
                        ],
                        "created_at": "2020-02-10T23:07:16.194843+01:00",
                        "updated_at": "2020-02-10T23:07:16.194843+01:00"
                    }
                ]
            },
            {
                "id": 2,
                "created_at": "2020-02-18T21:10:17.218979+01:00",
                "updated_at": "2020-02-18T21:10:17.218979+01:00",
                "uuid": "ccf66172-f87f-4509-b7af-e8716f739860",
                "email": "quadri@example.com",
                "name": "quadri",
                "attribs": {},
                "status": "enabled",
                "customerLists": [
                    {
                        "subscription_status": "unconfirmed",
                        "id": 1,
                        "uuid": "ce13e971-c2ed-4069-bd0c-240e9a9f56f9",
                        "name": "Default customer_list",
                        "type": "public",
                        "tags": [
                            "test"
                        ],
                        "created_at": "2020-02-10T23:07:16.194843+01:00",
                        "updated_at": "2020-02-10T23:07:16.194843+01:00"
                    }
                ]
            },
            {
                "id": 3,
                "created_at": "2020-02-19T19:10:49.36636+01:00",
                "updated_at": "2020-02-19T19:10:49.36636+01:00",
                "uuid": "5d940585-3cc8-4add-b9c5-76efba3c6edd",
                "email": "sugar@example.com",
                "name": "sugar",
                "attribs": {},
                "status": "enabled",
                "customerLists": []
            }
        ],
        "query": "",
        "total": 3,
        "per_page": 20,
        "page": 1
    }
}
```

______________________________________________________________________

#### GET /api/customers/{customer_id}

Retrieve a specific customer.

##### Parameters

| Name          | Type   | Required | Description      |
| :------------ | :----- | :------- | :--------------- |
| customer_id | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers/1'
```

##### Example Response

```json
{
    "data": {
        "id": 1,
        "created_at": "2020-02-10T23:07:16.199433+01:00",
        "updated_at": "2020-02-10T23:07:16.199433+01:00",
        "uuid": "ea06b2e7-4b08-4697-bcfc-2a5c6dde8f1c",
        "email": "john@example.com",
        "name": "John Doe",
        "attribs": {
            "city": "Bengaluru",
            "good": true,
            "type": "known"
        },
        "status": "enabled",
        "customerLists": [
            {
                "subscription_status": "unconfirmed",
                "id": 1,
                "uuid": "ce13e971-c2ed-4069-bd0c-240e9a9f56f9",
                "name": "Default customer_list",
                "type": "public",
                "tags": [
                    "test"
                ],
                "created_at": "2020-02-10T23:07:16.194843+01:00",
                "updated_at": "2020-02-10T23:07:16.194843+01:00"
            }
        ]
    }
}
```
______________________________________________________________________

#### GET /api/customers/{customer_id}/export

Export a specific customer data that gives profile, customer_list subscriptions, campaign views and link clicks information. Names of private customer_lists are replaced with "Private customer_list".

##### Parameters

| Name          | Type   | Required | Description      |
| :------------ | :----- | :------- | :--------------- |
| customer_id | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers/1/export'
```

##### Example Response

```json
{
  "profile": [
    {
      "id": 1,
      "uuid": "c2cc0b31-b485-4d72-8ce8-b47081beadec",
      "email": "john@example.com",
      "name": "John Doe",
      "attribs": {
        "city": "Bengaluru",
        "good": true,
        "type": "known"
      },
      "status": "enabled",
      "created_at": "2024-07-29T11:01:31.478677+05:30",
      "updated_at": "2024-07-29T11:01:31.478677+05:30"
    }
  ],
  "subscriptions": [
    {
      "subscription_status": "unconfirmed",
      "name": "Private customer_list",
      "type": "private",
      "created_at": "2024-07-29T11:01:31.478677+05:30"
    }
  ],
  "campaign_views": [],
  "link_clicks": []
}
```
______________________________________________________________________

#### GET /api/customers/{customer_id}/bounces

Get a specific customer bounce records.
##### Parameters

| Name          | Type   | Required | Description      |
| :------------ | :----- | :------- | :--------------- |
| customer_id | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers/1/bounces'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 841706,
      "type": "hard",
      "source": "demo",
      "meta": {
        "some": "parameter"
      },
      "created_at": "2024-08-22T09:05:12.862877Z",
      "email": "thomas.hobbes@example.com",
      "customer_uuid": "137c0d83-8de6-44e2-a55f-d4238ab21969",
      "customer_id": 99,
      "campaign": {
        "id": 2,
        "name": "Welcome to listmonk"
      }
    },
    {
      "id": 841680,
      "type": "hard",
      "source": "demo",
      "meta": {
        "some": "parameter"
      },
      "created_at": "2024-08-19T14:07:53.141917Z",
      "email": "thomas.hobbes@example.com",
      "customer_uuid": "137c0d83-8de6-44e2-a55f-d4238ab21969",
      "customer_id": 99,
      "campaign": {
        "id": 1,
        "name": "Test campaign"
      }
    }
  ]
}
```

______________________________________________________________________

#### POST /api/customers

Create a new customer.

##### Parameters

| Name                     | Type       | Required | Description                                                                                                                   |
|:-------------------------|:-----------|:---------|:------------------------------------------------------------------------------------------------------------------------------|
| email                    | string     | Yes      | Customer's email address.                                                                                                   |
| name                     | string     | Yes      | Customer's name.                                                                                                            |
| status                   | string     | Yes      | Customer's status: `enabled`, `blocklisted`.                                                                                |
| customer_lists                    | number\[\] |          | CustomerList of customer_list IDs to subscribe to.                                                                                             |
| attribs                  | JSON       |          | Optional JSON object attributes for the customer that can be used in message templates. Example `{"location": "Somewhere"}` |
| preconfirm_subscriptions | bool       |          | If true, subscriptions are marked as confirmed and no opt-in emails are sent for double opt-in customer_lists.                         |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers' -H 'Content-Type: application/json' \
    --data '{"email":"customer@domain.com","name":"The Customer","status":"enabled","customerLists":[1],"attribs":{"city":"Bengaluru","projects":3,"stack":{"languages":["go","python"]}}}'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "created_at": "2019-07-03T12:17:29.735507+05:30",
    "updated_at": "2019-07-03T12:17:29.735507+05:30",
    "uuid": "eb420c55-4cfb-4972-92ba-c93c34ba475d",
    "email": "customer@domain.com",
    "name": "The Customer",
    "attribs": {
      "city": "Bengaluru",
      "projects": 3,
      "stack": { "languages": ["go", "python"] }
    },
    "status": "enabled",
    "customerLists": [1]
  }
}
```

______________________________________________________________________

#### POST /api/customers/{customers_id}/optin

Sends opt-in confirmation email to customers.

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/customers/11/optin' -H 'Content-Type: application/json' \
--data {}
```

##### Example Response

```json
{
    "data": true
}
```
______________________________________________________________________

#### POST /api/public/subscription

Create a public subscription, accepts both form encoded or JSON encoded body.

##### Parameters

| Name       | Type       | Required | Description                 |
| :--------- | :--------- | :------- | :-------------------------- |
| email      | string     | Yes      | Customer's email address. |
| name       | string     |          | Customer's name.          |
| list_uuids | string\[\] | Yes      | CustomerList of customer_list UUIDs.         |

##### Example JSON Request

```shell
curl 'http://localhost:9000/api/public/subscription' -H 'Content-Type: application/json' \
    --data '{"email":"customer@domain.com","name":"The Customer","list_uuids": ["eb420c55-4cfb-4972-92ba-c93c34ba475d", "0c554cfb-eb42-4972-92ba-c93c34ba475d"]}'
```

##### Example Form Request

```shell
curl -u 'http://localhost:9000/api/public/subscription' \
    -d 'email=customer@domain.com' -d 'name=The Customer' -d 'l=eb420c55-4cfb-4972-92ba-c93c34ba475d' -d 'l=0c554cfb-eb42-4972-92ba-c93c34ba475d'
```

Note: For form request, use `l` for multiple customer_lists instead of `customer_lists`.

##### Example Response

```json
{
  "data": true
}
```

______________________________________________________________________

#### PUT /api/customers/customer-lists

Modify customer customer_list memberships.

##### Parameters

| Name            | Type       | Required           | Description                                                       |
| :-------------- | :--------- | :----------------- | :---------------------------------------------------------------- |
| ids             | number\[\] | Yes                | Array of user IDs to be modified.                                 |
| action          | string     | Yes                | Action to be applied: `add`, `remove`, or `unsubscribe`.          |
| target_customer_list_ids | number\[\] | Yes                | Array of customer_list IDs to be modified.                                 |
| status          | string     | Required for `add` | Customer status: `confirmed`, `unconfirmed`, or `unsubscribed`. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/customers/customer-lists' \
-H 'Content-Type: application/json' \
--data-raw '{"ids": [1, 2, 3], "action": "add", "target_customer_list_ids": [4, 5, 6], "status": "confirmed"}'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### PUT /api/customers/{customer_id}

Update a specific customer.

> Refer to parameters from [POST /api/customers](#post-apicustomers). Note: All parameters must be set, if not, the customer will be removed from all previously assigned customer_lists.

______________________________________________________________________

#### PUT /api/customers/{customer_id}/blocklist

Blocklist a specific customer.

##### Parameters

| Name          | Type   | Required | Description      |
| :------------ | :----- | :------- | :--------------- |
| customer_id | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/customers/9/blocklist'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### PUT /api/customers/blocklist

Blocklist multiple customer.

##### Parameters

| Name | Type   | Required | Description      |
| :--- | :----- | :------- | :--------------- |
| ids  | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X PUT 'http://localhost:9000/api/customers/blocklist' -H 'Content-Type: application/json' --data-raw '{"ids":[2,1]}'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### PUT /api/customers/query/blocklist

Blocklist customers based on SQL expression.

> Refer to the [querying and segmentation](../querying-and-segmentation.md#querying-and-segmenting-customers) section for more information on how to query customers with SQL expressions.

##### Parameters

| Name     | Type     | Required | Description                                  |
| :------- | :------- | :------- | :------------------------------------------- |
| query    | string   | Yes      | SQL expression to filter customers with.   |
| customer_list_ids | []number | No       | Optional customer_list IDs to limit the filtering to. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/customers/query/blocklist' \
-H 'Content-Type: application/json' \
--data-raw '{"query":"customers.name LIKE \'John Doe\' AND customers.attribs->>'\''city'\'' = '\''Bengaluru'\''"}'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### DELETE /api/customers/{customer_id}

Delete a specific customer.

##### Parameters

| Name          | Type   | Required | Description      |
| :------------ | :----- | :------- | :--------------- |
| customer_id | Number | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/customers/9'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### DELETE /api/customers/{customer_id}/bounces

Delete a customer's bounce records

##### Parameters

| Name | Type          | Required | Description      |
| :--- | :------------ | :------- | :--------------- |
| id   | customer_id | Yes      | Customer's ID. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/customers/9/bounces'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### DELETE /api/customers

Delete one or more customers.

##### Parameters

| Name | Type       | Required | Description                |
| :--- | :--------- | :------- | :------------------------- |
| id   | number\[\] | Yes      | Array of customer's IDs. |

##### Example Request

```shell
curl -u 'api_username:access_token' -X DELETE 'http://localhost:9000/api/customers?id=10&id=11'
```

##### Example Response

```json
{
    "data": true
}
```

______________________________________________________________________

#### POST /api/customers/query/delete

Delete customers based on SQL expression.

##### Parameters

| Name     | Type     | Required | Description                                                        |
| :------- | :------- | :------- | :----------------------------------------------------------------- |
| query    | string   | No       | SQL expression to filter customers with.                         |
| customer_list_ids | []number | No       | Optional customer_list IDs to limit the filtering to.                       |
| all      | bool     | No       | When set to `true`, ignores any query and deletes all customers. |


##### Example Request

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/api/customers/query/delete' \
-H 'Content-Type: application/json' \
--data-raw '{"query":"customers.name LIKE \'John Doe\' AND customers.attribs->>'\''city'\'' = '\''Bengaluru'\''"}'
```

##### Example Response

```json
{
    "data": true
}
```
