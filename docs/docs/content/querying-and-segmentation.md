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
