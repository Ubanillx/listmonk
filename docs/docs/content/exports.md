# Excel exports

All downloadable business reports and import templates use **Excel `.xlsx`**.
Ordinary JSON API responses, CSV/XLSX uploads, media downloads, and email-editor
source files keep their own formats.

The admin UI sends its selected language as `lang`. API clients can pass
`?lang=zh-CN`, `?lang=zh-TW`, or another installed language code, or set
`X-Listmonk-Language`. The query parameter takes precedence over the header;
without either, the configured application language is used. Missing
translations fall back to English. Custom-field labels use the administrator's
configured labels; customer names, list names, and other recorded content are
preserved. Language selection affects one request and never changes global settings.

| Download | Endpoint / entry | Fields and organization |
| --- | --- | --- |
| Private customers and blocklist | `GET /api/customers/export` | Customer code, name, permitted email, status, creation/update time; permitted custom fields in separate columns, remaining attributes, UUID. |
| Single-customer data | `GET /api/customers/:id/export` | Separate profile, subscriptions, campaign opens and link-click sheets, restricted by `privacy.exportable`. |
| Public-pool customers | `GET /api/pools/:id/contacts/export`, alias `/api/customer-lists/:id/pool-contacts/export` | Customer code, name, masked/permitted email, reply-to email, department, customer status, current-scope allocation status, removal reason and timestamps. |
| Combined public pools | `GET /api/pools/contacts/export` | The same contact fields, plus source pool name and ID. Memberships in different pools remain separate rows. |
| Audit events | `GET /api/audit-events/export` | Time, readable action/actor/object/result/reason first; stable IDs, original action/object/reason codes, request ID, metadata, IP and user agent follow. |
| Campaign send failures | `GET /api/campaigns/:id/send-errors/export` | Customer identity, reason, failure stage, SMTP code, diagnostics, attempt count, first/last failure time, recipient source and tracing IDs; a separate reason-summary sheet includes historical errors without details. |
| Subscriber self-service | `POST /subscription/export/:subUUID` | The same category-based customer workbook, emailed as `customer-data.xlsx` to the customer's own address. |
| User/member import templates | `GET /api/import-templates/users` or `/api/import-templates/members` | An empty first sheet with translated headers and a separate instructions sheet with required/optional fields and accepted values. These contain no stored user or organization data. |

Every workbook uses readable widths, wrapped text, a styled frozen header and
alternating row shading. Non-empty data sheets have Excel table filters.
Times are real Excel dates displayed as `yyyy-mm-dd hh:mm:ss`; UTC is explicit
in each time-column heading. Customer codes and identifiers remain text to
preserve leading zeros and long numbers. Recorded text never becomes a formula.
Unknown status/reason codes remain visible as their original values.

Selected records and search filters keep the same authorization and workspace
boundaries as their source pages. Private email, UUID and attributes require
`customers:sensitive_read`; without it, masked-list exports can retain masked
email, while other exports omit the email column. Public-pool email remains
masked for anyone other than a platform administrator. Campaign diagnostics
that can contain sensitive recipient data remain hidden where required.

Large exports use batched/streamed source queries. A sheet reaching Excel's
1,048,576-row limit continues in another sheet with the same headings. A value
exceeding the 32,767-character cell limit produces an error instead of silently
losing content. The server completes the workbook before starting the download;
temporary export files are removed at the end of the request. There are no
persisted export jobs or export workers.

Fill import templates in the **first sheet**. Notes and required-field markers
are kept on the instructions sheet, never inserted into importable rows.
Translated headers in the active language and English, Simplified Chinese and
Traditional Chinese aliases are recognized; legacy machine headings such as
`username` and `user_role` continue to work. Importing still requires the normal
user/organization permissions. Upload formats remain CSV and XLSX.

Downloads return `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`,
`Content-Disposition: attachment`, `Cache-Control: no-store` and
`X-Content-Type-Options: nosniff`. Integrations previously reading export CSV/JSON
must now read XLSX. Ordinary API list/detail endpoints still return JSON.
