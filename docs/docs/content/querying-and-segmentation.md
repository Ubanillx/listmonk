# Searching and segmenting customers

The customers page supports a customer code, name or e-mail search. Use `search` on
`GET /api/customers` to get the same results through the API. You can narrow
them with `customer_list_id` and, when a list is selected,
`subscription_status`.

To organize a segment, create a customer list and add selected customers to it.
The bulk customer endpoints can apply a search and list filter to all customers
the caller manages in the current workspace. They do not accept SQL expressions.
See the [Customers API](apis/customers.md) for the supported filters and
permissions.

## Sample attributes

Attributes remain available for personalization in campaigns. They are a JSON
map on each customer, for example `{"city":"Bengaluru"}`. The customer search
does not filter on arbitrary attributes. The retired `query` SQL parameter on
customer listing and export returns HTTP 400; the former `/api/customers/query/*`
routes are unavailable.
