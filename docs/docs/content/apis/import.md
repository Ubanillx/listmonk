# API / Import

## Optional public-pool reply email

Map `params.field_map.reply_to` to a column name or spreadsheet letter. Values must be a single bare email address; blank or absent values are accepted. Invalid values produce `invalid_reply_to` row issues. Reimporting the same customer code/email/name/department updates this routing field, including clearing it with a blank value, without creating another contact. It does not clear blocklisting or organization exclusions.

The default campaign order is customer reply email → verified active organization mailbox. Campaigns can reverse it using `pool_reply_priority=organization_first`. Imported addresses set Reply-To only; configure a corresponding organization mailbox separately to collect replies.


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
Subscription imports require the customer_code column, but its values may be
empty or whitespace-only. New customers store an empty code; reimporting an
existing private customer with an empty code preserves its existing code.
Only these supported fields are imported.

The admin private-customer import form always overwrites existing customer
information and subscription status. It defaults to confirmed subscriptions.
The status selector is shown only when at least one selected private-customer
list uses double opt-in, including a mix of single and double opt-in lists.
Removing all double opt-in lists resets the hidden status to confirmed.
Blocklist imports use unsubscribed status. These form defaults do not change
the defaults or optional overwrite parameters for direct API requests.

When `customer_list_ids` contains exactly one first-level public-pool list
(`type=pool`), the same endpoint uses the public-pool branch instead. The first
CSV sheet or XLSX worksheet must provide customer code, name, email, and
allocation department. The Chinese headers in the supplied workbook—`客户编号`
(`客户编码` is also accepted), `姓名`, `邮箱`, and `分配部门`—are recognized;
the customer code and name columns are required, but their values may be empty
or whitespace-only and are stored as empty strings. Email and allocation
department values remain required in both subscribe and blocklist modes.
Empty codes remain part of the full contact identity used for deduplication;
distinct contacts with empty codes do not generate customer-code conflicts.
An optional `reply_to` column (aliases `回信邮箱`, `回件邮箱`, `回复邮箱`, `reply-to`, `reply_email`) supplies the customer-specific Reply-To address; other extra columns are ignored. The allocation department must match an
active organization name in the system; an unknown or archived department is
reported as an invalid row and is not written to the pool. A valid department
is saved on the pool contact. If the matching organization already has a
pool allocation for the selected pool, the import also creates the contact's
pool-allocation membership. Creating that pool allocation later backfills
existing matching contacts; it does not create an organization.
The `email` cell may contain multiple addresses. The importer extracts bare
addresses separated by semicolons, commas, newlines, slashes, or pipe characters, and
also extracts addresses followed by punctuation or a short note. Each extracted
address becomes one contact row and reuses the source row's customer code, name,
allocation department, and Reply-To value. Duplicate addresses still follow the
normal duplicate handling. Invalid fragments are reported with their original
source row number. Import totals and the 100,000-contact limit apply after
expansion. If a template contains both `部门` and `分配部门`, automatic field
mapping prefers `分配部门` for the allocation target.
This branch is synchronous, does not support overwrite flags or ZIP
uploads, and is restricted to the highest administrator.

Public-pool imports accept `mode=subscribe` or `mode=blocklist`, using the same
required four columns, optional reply email, and department validation. Blocklist mode creates new
contacts as `blocklisted` and marks every matching email in the selected pool
as blocklisted (case-insensitive, even if the identity fields differ). The
response includes `blocklisted`, the number of distinct affected contacts.
Contact status applies across all organization allocations and any pools
sharing that contact record. Separate contacts in other pools are unchanged.
Blocked contacts are excluded by recipient resolution and delivery checks;
normal imports preserve blocking, including new identities with an email
already blocked in the selected pool. Organization removal/restoration does
not clear contact-level blocking.

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
