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

Send a CSV / XLSX (optionally ZIP compressed CSV) file to import customers. Use a multipart form POST. The selected list type determines the import branch.

CSV files use commas. Supported fields are email, name, and customer_code.
Subscription imports require customer_code. Only these supported fields are imported.

When `customer_list_ids` contains exactly one first-level public-pool list
(`type=pool`), the same endpoint uses the public-pool branch instead. The first
CSV sheet or XLSX worksheet must provide customer code, name, email, and
allocation department. The Chinese headers in the supplied workbook—`客户编号`
(`客户编码` is also accepted), `姓名`, `邮箱`, and `分配部门`—are recognized;
extra template columns are ignored. The allocation department must match an
active organization name in the system; an unknown or archived department is
reported as an invalid row and is not written to the pool. A valid department
is saved on the pool contact but is not interpreted as an organization binding.
This branch is synchronous, does not support blocklist, overwrite, or ZIP
uploads, and is restricted to the highest administrator.

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
| field_map | object   |          | Optional field mapping. Normal imports accept `email`, `name`, `customer_code`; public-pool imports additionally accept `allocation_department`. Values can be header names (`email`) or column references (`A`, `B`, `1`, `2`). |

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

##### Public-pool example

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/import/customers' \
    -F 'params={"mode":"subscribe", "customer_list_ids":[42], "field_map":{"customer_code":"客户编号", "name":"姓名", "email":"邮箱", "allocation_department":"分配部门"}}' \
    -F "file=@/path/to/分表 (1) 模板.xlsx"
```

The response is wrapped in `data` and contains only safe aggregate counts:
`target`, `pool_id`, `total`, `valid`, `created`, `existing`, `conflicts`,
`invalid`, `duplicates`, and row-level issues containing row number, customer
code, allocation department and a reason. `allocation_department_not_found`
means the value does not match an active organization name. Uploaded email
values are never returned.
