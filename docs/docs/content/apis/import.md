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

##### Parameters

| Name   | Type        | Required | Description                              |
|:-------|:------------|:---------|:-----------------------------------------|
| params | JSON string | Yes      | Stringified JSON with import parameters. |
| file   | file        | Yes      | File for upload.                         |


#### `params` (JSON string)
| Name      | Type     | Required | Description                                                                                                                        |
|:----------|:---------|:---------|:-----------------------------------------------------------------------------------------------------------------------------------|
| mode      | string   | Yes      | `subscribe` or `blocklist`                                                                                                         |
| delim     | string   | Yes (CSV/ZIP) | Single character indicating delimiter used in the CSV file, eg: `,`                                                           |
| customer_lists     | []number |          | Array of customer_list IDs to subscribe to.                                                                                                 |
| overwrite | bool     |          | Whether to overwrite the customer parameters including subscriptions or ignore records that are already present in the database. |
| field_map | object   |          | Optional field mapping. Keys: `email`, `name`, `attributes`. Values can be header names (`email`) or column references (`A`, `B`, `1`, `2`). |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/import/customers' \
    -F 'params={"mode":"subscribe", "subscription_status":"confirmed", "delim":",", "customerLists":[1, 2], "overwrite": true, "field_map": {"email": "A", "name": "B", "attributes": "C"}}' \
  -F "file=@/path/to/subs.csv"
```

##### Example Response

```json
    {
        "mode": "subscribe", // subscribe or blocklist
        "delim": ",",        // delimiter in the uploaded file
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
