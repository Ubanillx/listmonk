# Delivery provider reporting API

All `/api/delivery-provider/*` routes require an authenticated **platform administrator**. Organization ownership, workspace selection and ordinary settings permissions do not grant access. Personal API keys are outside the independent route allowlist. Responses use the existing `{ "data": ... }` envelope and `Cache-Control: no-store`.

| Method | Route | Contract |
| --- | --- | --- |
| GET | `/api/delivery-provider/config` | Safe status only: `username`, `configured`, `has_password`, `updated_at`, `tested_at`, `test_error`, `verified`, `provider`. Never returns a password. |
| PUT | `/api/delivery-provider/config` | JSON `username`, `password`, `expected_updated_at`. Empty password keeps the previous one only for the same username. New configuration uses empty `expected_updated_at`. Stale versions return 409. No external request during save. |
| POST | `/api/delivery-provider/test` | Uses saved credentials to query customer and service. Persists safe success/failure metadata. Returns status including `verified`; a provider connection failure is represented by `verified: false`, `test_error`, with HTTP 200 after successfully persisting the result. Infrastructure errors remain non-200. |
| GET | `/api/delivery-provider/account` | `results: { customer, service }`, `source`, `fetched_at`, `warning`. Service failure can return account details with a warning and missing service. |
| GET | `/api/delivery-provider/statistics` | `date`, `date_end` in YYYY-MM-DD, and/or `ident` or positive sending task `id`; omit dates for full-task results. At least a date or task is required. |
| GET | `/api/delivery-provider/failures` | At least one of `date`, `date_end`, `task_ident`, `task_id`; required numeric-string `type` in -1,1..10. Types 1..3 require task identifier. Optional `search` performs local case-insensitive email substring filtering. Optional `group_by=email` merges repeated addresses and counts occurrences. Neither local option is forwarded to BestEDM. |
| GET | `/api/delivery-provider/failures/export` | Same applied filters including `search` and `group_by=email`; optional `lang`; returns `.xlsx`, including source column and, when grouped, a numeric error count and all classifications/descriptions with their counts. |
| GET | `/api/delivery-provider/logs` | Required `date`, full `mail_to`; optional `task_ident`. No automatic polling. |

Report routes return `data: { results: [...], source: "live", fetched_at, warning }`. Field keys inside raw report records retain the provider's snake_case/compound keys. Missing numerical fields are not synthesized. Without `group_by=email`, repeated failed addresses remain separate records for compatibility. `task_id` in a statistics record is not the sending task ID.

With `group_by=email`, each failure result has `email`, numeric `error_count`, and `errors: [{error_type, error_type_dec, error_count}]`. Email grouping trims whitespace and ignores case, retaining the first spelling and first-seen order. Every distinct classification/description is preserved. Counts refer to occurrences in the filtered upstream response, not inferred SMTP attempts or unique sends. Filtering happens before aggregation; the console paginates these email groups. Empty results remain `[]`. The export uses the same aggregation function. Other nonempty `group_by` values are rejected.

There is no sample mode: `mode` is not an accepted query field, so `mode=demo` and similar requests fail validation with HTTP 400. Every report route reads the saved credentials and reports upstream failures as errors rather than empty or zeroed results. Configuration and test endpoints always concern the real account.

An empty result is successful: the provider answers "no rows" with `{"err_msg": "未找到相应结果…"}` on the log API and `<result><status>no_data</status></result>` on the report APIs. Both become `results: []` with HTTP 200, so the console shows an empty table. Other `err_msg` texts and non-`no_data` statuses remain errors.

Queries reject unknown/repeated fields (including `mode`), invalid dates, reversed ranges, non-positive IDs, invalid emails, ambiguous ID+batch selectors and date ranges over 366 days. Reports limit each provider response to 4 MB. Errors use the normal Listmonk HTTP error envelope, with safe descriptions and provider error codes; raw response bodies and credentials are not logged or returned.

Account settings are kept in the reserved `settings` row `integration.bestedm`; ordinary settings models omit that key and the generic keyed write endpoint rejects it. Save uses a transaction/advisory lock and version check. Test-result writes compare the original JSON record so stale in-flight tests cannot overwrite a newly saved credential pair. The connection does not reload the app or modify SMTP transports.
