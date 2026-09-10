# API / Import

Method   | Endpoint                                        | Description
---------|-------------------------------------------------|------------------------------------------------
GET      | [/api/import/customers](#get-apiimportcustomers) | Retrieve import statistics.
GET      | [/api/import/customers/logs](#get-apiimportcustomerslogs) | Retrieve import logs.
POST     | [/api/import/customers](#post-apiimportcustomers) | Upload a file for bulk customer import.
DELETE   | [/api/import/customers](#delete-apiimportcustomers) | Stop and remove an import.

______________________________________________________________________

#### GET /api/import/customers

Retrieve the status of an ongoing import.

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/import/customers'
```

##### Example Response

```json
{
    "data": {
        "name": "",
        "total": 0,
        "imported": 0,
        "status": "none"
    }
}
```

______________________________________________________________________

#### GET /api/import/customers/logs

Retrieve logs from an ongoing import.

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/import/customers/logs'
```

##### Example Response

```json
{
    "data": "2020/04/08 21:55:20 processing 'import.csv'\n2020/04/08 21:55:21 imported finished\n"
}
```

______________________________________________________________________

#### POST /api/import/customers

Send a CSV / XLSX (optionally ZIP compressed CSV) file to import customers. Use a multipart form POST.

CSV files use commas. Supported fields are email, name, and customer_code.
Subscription imports require customer_code. Only these supported fields are imported.

##### Parameters

| Name   | Type        | Required | Description                              |
|:-------|:------------|:---------|:-----------------------------------------|
| params | JSON string | Yes      | Stringified JSON with import parameters. |
| file   | file        | Yes      | File for upload.                         |


#### `params` (JSON string)
| Name      | Type     | Required | Description                                                                                                                        |
|:----------|:---------|:---------|:-----------------------------------------------------------------------------------------------------------------------------------|
| mode      | string   | Yes      | `subscribe` or `blocklist`                                                                                                         |
| customer_list_ids | []number |          | Array of customer list IDs to subscribe to. |
| overwrite | bool     |          | Whether to overwrite the customer parameters including subscriptions or ignore records that are already present in the database. |
| field_map | object   |          | Optional field mapping. Keys: `email`, `name`, `customer_code`. Values can be header names (`email`) or column references (`A`, `B`, `1`, `2`). |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/import/customers' \
    -F 'params={"mode":"subscribe", "subscription_status":"confirmed", "customer_list_ids":[1, 2], "overwrite": true, "field_map": {"email": "A", "name": "B", "customer_code": "C"}}' \
  -F "file=@/path/to/subs.csv"
```

##### Example Response

```json
    {
        "mode": "subscribe", // subscribe or blocklist
        "customerLists":[1],         // array of customer_list IDs to import into
        "overwrite": true    // overwrite existing entries or skip them?
    }
```

______________________________________________________________________

#### DELETE /api/import/customers

Stop and delete an ongoing import.

##### Example Request

```shell
curl -u "api_user:token" -X DELETE 'http://localhost:9000/api/import/customers'
```

##### Example Response

```json
{
    "data": {
        "name": "",
        "total": 0,
        "imported": 0,
        "status": "none"
    }
}
```
