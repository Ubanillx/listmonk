# Querying and segmenting customers

listmonk allows the writing of partial Postgres SQL expressions to query, filter, and segment customers.

## Database fields

These are the fields in the customer database that can be queried.

| Field                    | Description                                                                                         |
| ------------------------ | --------------------------------------------------------------------------------------------------- |
| `customers.uuid`       | The randomly generated unique ID of the customer                                                  |
| `customers.email`      | E-mail ID of the customer                                                                         |
| `customers.name`       | Name of the customer                                                                              |
| `customers.status`     | Status of the customer (`enabled`, `disabled`, `blocklisted`)                                     |
| `customers.attribs`    | Map of arbitrary attributes represented as JSON. Accessed via the `->` and `->>` Postgres operator. |
| `customers.created_at` | Timestamp when the customer was first added                                                       |
| `customers.updated_at` | Timestamp when the customer was modified                                                          |

## Sample attributes

Here's a sample JSON map of attributes assigned to an imaginary customer.

```json
{
  "city": "Bengaluru",
  "likes_tea": true,
  "spoken_languages": ["English", "Malayalam"],
  "projects": 3,
  "stack": {
    "frameworks": ["echo", "go"],
    "languages": ["go", "python"],
    "preferred_language": "go"
  }
}
```

## Sample SQL query expressions

#### Find a customer by e-mail

```sql
-- Exact match
customers.email = 'some@domain.com'

-- Partial match to find e-mails that end in @domain.com.
customers.email LIKE '%@domain.com'

```

#### Find a customer by name

```sql
-- Find all customers whose name start with John.
customers.email LIKE 'John%'

```

#### Multiple conditions

```sql
-- Find all Johns who have been blocklisted.
customers.email LIKE 'John%' AND customers.status = 'blocklisted'
```

#### Querying customers who viewed the campaign email

```sql
-- Find all customers who viewed the campaign email.
EXISTS(SELECT 1 FROM campaign_views WHERE campaign_views.customer_id=customers.id AND campaign_views.campaign_id=<put_id_of_campaign>)
```

#### Querying attributes

```sql
-- The ->> operator returns the value as text. Find all customers
-- who live in Bengaluru and have done more than 3 projects.
-- Here 'projects' is cast into an integer so that we can apply the
-- numerical operator >
customers.attribs->>'city' = 'Bengaluru' AND
    (customers.attribs->>'projects')::INT > 3
```

#### Querying nested attributes

```sql
-- Find all blocklisted customers who like to drink tea, can code Python
-- and prefer coding Go.
--
-- The -> operator returns the value as a structure. Here, the "languages" field
-- The ? operator checks for the existence of a value in a customer_list.
customers.status = 'blocklisted' AND
    (customers.attribs->>'likes_tea')::BOOLEAN = true AND
    customers.attribs->'stack'->'languages' ? 'python' AND
    customers.attribs->'stack'->>'preferred_language' = 'go'

```

To learn how to write SQL expressions to do advancd querying on JSON attributes, refer to the Postgres [JSONB documentation](https://www.postgresql.org/docs/11/functions-json.html).

## Query boundaries

Advanced expressions are evaluated inside the caller's workspace, and every part of an expression has to stay inside it. The following rules are enforced before an expression runs (see `internal/core/customer_sql_guard.go`):

- **Only the caller's own customer activity tables may be referenced in subqueries**: `campaign_views`, `link_clicks`, `bounces` and `customer_list_memberships`. Each reference must be correlated with the outer customer row, for example `campaign_views.customer_id = customers.id`. An uncorrelated subquery evaluates identically for every customer and would turn the result count into a yes/no answer about other workspaces, so it is rejected.
- **Tables that are global or shared are not available to expressions**: `users`, `campaigns`, `links`, `customer_lists`, `campaign_customer_lists`, a second `customers` join, and any table outside the allowlist. Resolve the campaign, link or customer_list you are interested in to its ID first (for example through the API or the admin UI), then filter on `campaign_id`, `link_id` or `customer_list_id` in the activity table. The `customer_list_id` and `subscription_status` request parameters cover the common membership filters.
- **Credential and secret columns are rejected** wherever they appear (`password`, `twofa_key`, `token_hash`, ...), as are whole row references such as `to_jsonb(u)`, which would read every column of a row.
- **Blocking, stateful, filesystem and statistics constructs are rejected**: `pg_sleep`, advisory locks, `FOR UPDATE`/`FOR SHARE`, set returning functions such as `generate_series`, `pg_read_file`, `dblink`, `set_config`, and size or row count functions such as `pg_total_relation_size` or `pg_stat_get_live_tuples`.
- A query over an unlisted table, or an expression that does not parse, returns `400 Bad Request` with `invalid customer SQL expression: ...`.

The owner of each customer is available through the `u` alias (`u.username`, `u.name`), and the customer row through its own columns (`customers.email`, `customers.status`, `customers.attribs`, ...).
