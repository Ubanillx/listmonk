# Public pools

## Contact states

Aggregate and single-list contact queries and exports accept `status=all`,
`active`, `blocklisted`, `unsubscribed`, `removed` or `archived`; an omitted
status retains the all-records API behavior. `active` now means an active
contact without a current-scope removal/opt-out. `blocklisted` and `archived`
filter master contact status; `removed` retains the allocation-exclusion view.
Invalid values return 400 and totals are filtered before pagination.

Contact DTOs include read-only boolean `unsubscribed`, including masked DTOs.
It reflects a non-restored organization exclusion with source `unsubscribe`.
Organization callers and allocation-list views read only that organization's
state; platform aggregate/source-pool views report whether any organization
currently has such an opt-out in the pool. Manual removals remain distinct.
Blacklisting and unsubscribing can coexist; restoring an exclusion removes its
current unsubscribe flag while preserving historical campaign click events.
The admin table shows separate colored state badges and defaults to all states.

## Pool list hierarchy in the admin UI

The public pool list page groups organization allocations below their source pool
using `pool_parent_id`. Each source can be expanded or collapsed; name search,
organization and type dropdowns can be combined. A matching allocation retains
its readable source pool as context, outside the matching count and bulk selection.
Pagination keeps each pool and its matching allocations together. Allocations
whose source is outside the current visible scope remain visible with an explicit
notice; the UI never requests otherwise inaccessible source metadata.

Filters operate on the permission-scoped list metadata returned by
`GET /api/customer-lists?type_group=pool&per_page=all` for the current active or
archived view. They do not switch workspace or change customer access permissions.

## Independent business permissions (v6.56.0)

Pool browsing, organization allocation management, master-data maintenance,
delivery authorization, and export are separate role permissions. Delegated
master maintainers can access first-level platform pools while receiving masked
contact DTOs. Ordinary allocation-list reads remain organization-scoped; the dedicated cross-organization management capability exposes scoped allocation metadata and, with separate read permission, masked management rows.
Permission names and dependencies are documented in
[业务权限说明](../business-permissions.md).

## Customer reply routing (v6.55.0)

Contact APIs, the aggregate pool view, first-level and allocation contact tables, and Excel exports include `reply_to`. It is an internal routing address and is not masked; recipient `email` remains masked for non-platform administrators. `order_by=reply_to` is supported. Single-contact creation accepts optional `reply_to` with the same email validation as import.

Campaign `pool_reply_priority` defaults to `contact_first`; `organization_first` reverses the two sources. The first available address is used for each customer. An organization mailbox must be active; AI-enabled mailboxes also require connection verification. Customer addresses do not require mailbox registration for sending. A recipient without either address blocks preview/send. The snapshot links the matching available mailbox in the target organization. Automatic reply synchronization additionally requires AI receiving credentials, successful connection verification, and enabled global AI settings.


Public customer pools are first-class `customer_list` types (`pool` and
`org_pool_allocation`). Contact records retain the imported customer code (which may
repeat), name, allocation department and real email server-side; all
non-highest-administrator responses return a masked email. Internal reply
mailbox addresses are not masked.

Masked recipient addresses use `*` characters, for example
`liuxin@gmail.com` becomes `liu***@gmail.com`. The first three local-part
characters remain visible for longer addresses; local parts of three characters
or fewer are fully masked. Each hidden character is replaced with one `*`.
The list APIs and aggregate pool view use the same email masking; Excel exports preserve it.

First-level `pool` rows use the platform-wide `global` scope. A `org_pool_allocation`
row uses the selected organization's scope and exposes that organization's
name as `organization_name`; the creating administrator remains available in
the owner fields for audit purposes.

Key endpoints:

- `GET /api/pools/contacts?search=...&status=active|removed&pool_id=6&allocation_department=...&page=1&per_page=20&order_by=pool_name&order=asc` — page through all first-level public-pool memberships visible in the active workspace. Each row includes `pool_id` and `pool_name`; a contact in two pools appears twice so its source list stays clear. Search, status, exact source-pool ID, exact trimmed allocation department, sorting, count and pagination apply across the combined result. Omit `allocation_department` for all departments; send it as an empty value to select contacts without one. Platform administrators see every pool; organization users see only their own allocation memberships with masked e-mails. Requires `pools:get`.
- `GET /api/pools/contacts/filters` — return the distinct visible `pool_id`, `pool_name`, `allocation_department` combinations for the aggregate view's dropdowns, independent of the current result page. An empty department denotes unassigned. Requires `pools:get` and obeys the same organization boundary.
- `GET /api/pools/contacts/export` — export the same combined result as Excel, including source pool name and ID. Accepts the same search, status, pool ID and department filters. Repeat `contact=<pool_id>:<contact_id>` to export only selected memberships within that authorized, filtered result (maximum 1000 selections). Malformed selections return 400. Requires `pools:export`; organization e-mails remain masked. `lang` / `X-Listmonk-Language` selects translated headings and labels.
- `GET /api/pools/:id/contacts?search=...&status=active|removed&page=1&per_page=20&order_by=created_at&order=desc` — page through first-level or pool-allocation contacts by imported code, name, or e-mail. The equivalent `GET /api/customer-lists/:id/pool-contacts` route also accepts an `org_pool_allocation` list ID. `status=active` shows unremoved contacts; `status=removed` shows unresolved organization removals or exclusions. In a first-level pool, platform administrators see all organizations' exceptions once per contact, with `excluded`, `exclusion_reason`, `exception_organization_name`, and `exception_allocation_id` (when a membership can be restored). Non-platform-administrators see only their current organization's allocation and receive masked e-mail addresses. Unassigned contacts remain active. Allocation-list reads stay scoped to that allocation. Omitting `status` preserves the legacy all-contacts response. The legacy `customer_code` filter remains a fallback alias of `search`. Requires `pools:get`; non-platform-administrators must have the pool granted to the active organization.
- `GET /api/pools/:id/contacts/export` (alias `GET /api/customer-lists/:id/pool-contacts/export`) — download the same filtered rows as Excel with masked e-mails. Repeat `contact=0:<contact_id>` to narrow the authorized result to selected contacts. Selection validation matches the aggregate export. Requires `pools:export`. Workbook formatting and the separate customer/allocation status fields are described in [Excel exports](../exports.md).
- `POST /api/pools/:id/contacts` — single-contact compatibility route
  (requires `pools:master_manage`); the product import entry is the unified customer
  import endpoint below.
- `DELETE /api/pools/:id/contacts/:contact_id` (alias
  `DELETE /api/customer-lists/:id/pool-contacts/:contact_id`) — permanently
  deletes one contact from a first-level pool. This destructive operation is
  requires `pools:master_manage`; organization allocation list IDs are
  rejected. Pool membership, allocation membership and pool campaign recipient
  rows follow their foreign-key deletion rules.
- `POST /api/pools/allocations` — create and bind an organization allocation; requires `pools:manage` in the active organization or `pools:allocation_platform_manage` across organizations (highest administrators retain their exemption). Ordinary members need an existing delivery grant; cross-organization operators can stage an ungranted allocation. Creation never creates a delivery grant, even for highest administrators. Allocations have no reply mailbox; the organization fallback is configured separately with `mailboxes:manage` and organization-management authorization.
- `GET /api/pools/organizations` — list active organization IDs and names for the delivery authorization picker; requires `pools:delivery_manage`. With `purpose=allocations`, requires `pools:allocation_platform_manage` instead. Neither grants membership or organization management.
- `POST|DELETE /api/pools/permissions` — grant/revoke a pool's organization delivery authorization; requires `pools:delivery_manage` and a `pool_id`/`organization_id` JSON body. Allocation management alone cannot grant or restore delivery access.
- `DELETE /api/pools/:id/contacts/:contact_id/email` — archive an invalid contact by clearing its email; requires `pools:master_manage`.
- `POST|DELETE|PUT /api/pools/allocations/members` — assign, logically remove, or restore a contact. Uses the same allocation action/scope/lifecycle transaction as the new batch management API below. Removing requires a reason. Cross-organization access requires `pools:allocation_platform_manage`; ordinary `pools:manage` stays within the active organization.
- `POST /api/pools/allocations/:id/import-members` — legacy compatibility route;
  it is not the management UI's import path. Requires `pools:manage` and the
  caller's organization boundary.
- `POST /api/import/customers` — unified customer import endpoint. When
  `customer_list_ids` contains exactly one first-level `pool`, the first CSV
  sheet or XLSX worksheet must map `customer_code`, `name`, `email` and
  `allocation_department`. Chinese template headers `客户编号`/`客户编码`,
  `姓名`, `邮箱`, `分配部门` are recognized; optional `回信邮箱`/`reply_to` is supported and other columns are ignored.
  The customer code and name columns are required, but their values may be empty
  or whitespace-only; they are stored as empty strings. This applies to both
  normal and blocklist imports. Email and allocation department values remain
  required. Full contact identity still determines duplicates; distinct contacts
  with empty codes do not generate customer-code conflicts.
  The synchronous result contains every issue (not just the first 200), with
  source `row`, `customer_code`, `allocation_department` and `reason`.
  `customer_code_conflict` means a new contact was still created; `duplicate_row`
  means the identical file row was skipped. Other reasons describe validation
  failures. No email values are returned in this issue DTO. The admin result
  supports type/search filtering, pagination, individual details and exporting
  **all** issues to `customer-import-errors.xlsx`, regardless of the active filter
  or page. Exported customer values are text, and headings follow the UI language.
  Requires `pools:master_manage`. `分配部门` must match an
  active `organizations.name`; unknown or archived departments are rejected
  row-by-row and are not written. A valid value is stored on the pool contact
  and, when that organization already has a pool allocation for the pool, also
  creates the corresponding pool-allocation membership. Creating the pool allocation
  list later backfills existing matching contacts; it never creates an
  organization.
  The admin import page displays organizations and their binding states in a
  searchable left column, with the selected organization's allocation or create
  form on the right. Allocation names are prefilled and editable; switching
  organizations retains each draft, and creating refreshes both columns.
  Selecting a target does not switch the active workspace. Allocation managers
  without the explicit cross-organization allocation capability see only their active organization. On narrow screens the columns stack.
- The unified import also accepts `mode=blocklist` with the same four required columns and optional reply email. Matching email addresses within the selected pool are marked
  `blocklisted`; new contacts are created in that state. The contact state
  suppresses delivery across its organization allocations and any other pools
  sharing the same record. Distinct contact records in other pools are not
  changed. Normal imports retain existing blocking and propagate it to new
  identities with that email within the selected pool. Import results include
  a distinct `blocklisted` contact count; pool contact pages display the state.
- `POST /api/pools/import` — legacy ordinary-list-to-pool compatibility route;
  requires `pools:master_manage`, plus the source list's owner/workspace boundary;
  new product flows use the unified customer import endpoint.
- `PUT /api/customer-lists/:id/pool-contacts/:contact_id` (or
  `PUT /api/pools/:id/contacts/:contact_id`) — edit a shared public-pool
  contact. `pools:contacts_edit` permits all contact fields, while
  `pools:contacts_name_edit` permits only `name`; `pools:master_manage`
  retains full edit compatibility but still controls create/delete/import/archive.
  Non-platform callers must have the pool allocated to the active organization.
- `GET /api/pools/:id/management-target?organization_id=...` — allocation state for an explicit organization; requires `pools:manage`, `pools:allocation_platform_manage` or `pools:delivery_manage`. Ordinary allocation-only callers must target their active organization; explicit cross-organization operators may choose an active target without switching workspace.
- `POST /api/campaigns/:id/pools` — attach a first-level public-pool audience to a campaign draft. Organization allocation list IDs are rejected; the selected first-level pool is resolved to the applicable allocation when the campaign is sent.

Preview and send operations reject unresolved pool audiences (missing the
organization's pool allocation, or an eligible customer has neither a reply email nor a usable organization fallback) and answer with
HTTP `400`. The message keeps the leading sentence
`public-pool audience requires an organization allocation and reply mailbox before previewing or sending`,
then names every unresolved audience as the whole chain the operator has to
fix — `pool list "<pool>" -> organization allocation "<allocation>"
(organization "<org>")` — followed by the first failing condition: `has no
target organization, so no reply mailbox can be resolved`, `has no organization
allocation bound to the pool`, `has not configured its unified reply mailbox`,
or `its unified reply mailbox "<address>" is unavailable (must be active; AI receiving connections must be verified)`. At most
five audiences are listed, the remainder is summarized as `(+N more)`, and the
message closes with the actionable steps, prefixed by `Fix: `: bind the
organization's allocation for that pool in `Customer lists -> Public pool
management` when that is one of the reasons, then have a manager of the
organization open its workspace and save an available reply address in `Manage
organizations -> Organization reply mailboxes` as the organization's unified
reply mailbox (AI receiving connections must be verified), and `then retry preview or send.` The steps are single-line and
never localized differently per reason.

An unresolved audience can be fixed by supplying the missing customer reply
addresses or configuring an available organization fallback. Personal or system
mailboxes are not implicit public-pool routes. Pool exclusions are organization
scoped and do not physically delete
the first-level pool contact.

Pool contacts are not part of the legacy customer export surface, and they never
appear in `/api/customers` results. Their dedicated Excel export requires
`pools:export`. Non-highest administrators cannot obtain a pool contact's real
email through list, detail, CSV, or API-key responses.

Public-pool access uses five independent permissions: `pools:get` (browse),
`pools:manage` (organization allocation), `pools:master_manage` (first-level
lists, contacts and import), `pools:delivery_manage` (organization delivery
grants), and `pools:export`. Platform administrators bypass functional grants.
Other callers obey the organization boundary; master-data permission widens
only platform first-level pool scope, and never permits plaintext contact reads.

Campaign responses include `customer_pools[].reply_mailbox_email` so operators
can see the organization fallback. Per-customer addresses and the campaign priority determine the actual Reply-To; delivery uses the immutable recipient snapshot.
When the selected audience is an explicit pool allocation,
`customer_pools[].allocation_list_id` and `customer_pools[].allocation_list_name` identify the
selector-compatible `org_pool_allocation` list; `allocation_id` remains the internal
pool-to-organization binding ID. This lets the campaign editor round-trip a
pool-allocation audience without silently changing it to its parent pool.
This is an internal company address and is not masked; customer contact emails
remain protected by the pool contact DTO policy.

## Admin workflow

Open a first-level pool from **CustomerLists**, then select **Manage pool**.
With delivery administration, first choose the **target organization** in the
management dialog. This is an allocation target, not a workspace switch and
not an organization-membership action: the administrator does not need to join
the organization and remains in the current workspace. The dialog then offers
one **Create and bind** action when allocation management is also granted. It creates
the allocation and binds it; delivery administration may grant access in that
transaction, while allocation-only creation requires an existing grant. The reply route for the
organization's pool recipients follows customer/organization priority. Its **organization fallback** is configured once by the organization's manager in
**Organizations -> Manage organizations -> Organization reply mailboxes**
(`PUT /api/organizations/:id/reply-mailbox`). A pool allocation carries no reply
mailbox of its own, the campaign field **customer reply mailbox** does not feed
this route, and a pool allocation cannot be created from the generic
customer-list form; there is no pool-allocation-to-first-level merge flow.
A caller with allocation and delivery administration performs the split through `POST /api/org-pool-allocations`
(the `/api/pools/allocations` alias is equivalent) with the target organization's
`organization_id`, without joining it. An ordinary active member with allocation
management may perform the same split in their current organization after it
has received delivery authorization. The request must target that organization;
other organizations are rejected with `403` without delivery administration.

Each first-level pool can have one bound pool allocation per organization. The
dialog does not import contact files. Import the pool template (four required columns and optional reply email) from
the unified **Customer import** page; rows whose `分配部门` matches an existing
pool-allocation organization are allocated during import. If the pool allocation
is created afterwards, the create-and-bind transaction backfills those rows. The
dialog is used to select a target organization and create/bind its pool allocation.
The customer-count link for a first-level or pool allocation opens the customer
area's public-pool tab (`Customers.vue`), which renders the contacts with the
same toolbar, pagination, sorting and row actions as ordinary customers instead
of a dedicated page. Allocation views split active members under **Public pool
 customers** and logically removed members under **Removed customers** through the same
server-side status filter, so each tab's count matches its rows. First-level
pools have no organization-specific exception tab. That view keeps the pool data
model separate and applies the same masked DTO policy as the API.
Allocation-only operators work inside their organization; delivery administrators
may select other targets, and master maintainers may maintain first-level pools.
These capabilities remain independent in direct API calls.

## Allocation management API (v6.61.0)

The `:id` below is `org_pool_allocations.id`, **not** the allocation list ID. These session/service endpoints do not extend the personal API key prefix allowlist (`/api/pools/*` remains unavailable to personal keys).

| Method and endpoint | Contract |
| --- | --- |
| `GET /api/pools/allocations/:id/manage` | Minimal allocation detail: source/list/organization IDs and names, `state`, `description`, `version`, `updated_at`, `delivery_allowed`. Requires allocation management scope. |
| `PUT /api/pools/allocations/:id/manage` | JSON `action=update|disable|enable|detach|rebind`, `expected_version`; update accepts `name` (1..200 characters), `description` (max 1000); detach requires nonblank `reason` (max 1000) and exact `confirm_organization`. Unknown fields are rejected. Stale versions return 409. |
| `GET /api/pools/allocations/:id/impact` | `{unfinished_campaigns: number}` in the data envelope; metadata-only impact count, no campaign/customer details. Detach rechecks references under the source lock. |
| `GET /api/pools/allocations/:id/manage-members` | Additional `pools:get`. Query `status=active|removed|candidates`, `search` by name/code, `page` (default 1), `per_page` (default 20, max 100). Returns `results,total,page,per_page`; dedicated safe DTO always masks recipient email. Delegated candidates must match the target allocation department. |
| `POST /api/pools/allocations/:id/manage-members` | JSON `action=assign|remove|restore`, `contact_ids` (1..100), `reason` (required for remove). Entire batch rolls back on invalid source/scope/state; assign cannot silently restore removed contacts. |
| `GET /api/pools/allocations/:id/events` | Additional `audit:get`; same pagination. Target allocation events only, actor name/time/action/safe metadata. Does not expose all-organization audit logs. |

Lifecycle: `active -> disabled -> active`; only cross-organization managers can detach or rebind. Detach is logical and rejects unfinished campaign references; rebind preserves identifiers/history and returns `disabled`. Disabled/detached allocations reject member mutations. No operation here edits shared contacts, deletes customers, clears blocklists, changes delivery grants or configures mailboxes. Mutations validate active organization, source/list, pending transfers and membership again inside the transaction; management mutations and their audit event commit atomically.

The workbench uses `manage_pool` (source pool list ID), `manage_org` and `manage_tab` URL parameters. Its customer, metadata and history tabs are independent of ordinary master-data editing. Queued recipients are checked again at claim and immediately before SMTP; disabling/removing/revoking excludes them from future delivery but cannot recall SMTP already in flight. Explicit all-organization campaign policy remains separate from an ordinary organization's delivery grant.

Upgrade changes: v6.61.0 adds lifecycle/version fields with existing rows active and does not grant the new permission. The old combination of allocation plus delivery permissions no longer implies cross-organization allocation management. Generic allocation-list update/archive/delete is rejected; use the lifecycle API instead.
