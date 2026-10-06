# Public pools

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
contact DTOs; allocation-list reads remain limited to their organization.
Permission names and dependencies are documented in
[业务权限说明](../business-permissions.md).

## Customer reply routing (v6.55.0)

Contact APIs, the aggregate pool view, first-level and allocation contact tables, and CSV exports include `reply_to`. It is an internal routing address and is not masked; recipient `email` remains masked for non-platform administrators. `order_by=reply_to` is supported. Single-contact creation accepts optional `reply_to` with the same email validation as import.

Campaign `pool_reply_priority` defaults to `contact_first`; `organization_first` reverses the two sources. The first available address is used for each customer. An organization mailbox must be verified and active; customer addresses do not require mailbox registration for sending. A recipient without either address blocks preview/send. For reply synchronization, configure the matching organization mailbox before sending; only an active verified mailbox in that target organization is linked to the snapshot.


Public customer pools are first-class `customer_list` types (`pool` and
`org_pool_allocation`). Contact records retain the imported customer code (which may
repeat), name, allocation department and real email server-side; all
non-highest-administrator responses return a masked email. Internal reply
mailbox addresses are not masked.

Masked recipient addresses use `*` characters, for example
`liuxin@gmail.com` becomes `liu***@gmail.com`. The first three local-part
characters remain visible for longer addresses; local parts of three characters
or fewer are fully masked. Each hidden character is replaced with one `*`.
The list APIs, aggregate pool view and CSV exports use the same format.

First-level `pool` rows use the platform-wide `global` scope. A `org_pool_allocation`
row uses the selected organization's scope and exposes that organization's
name as `organization_name`; the creating administrator remains available in
the owner fields for audit purposes.

Key endpoints:

- `GET /api/pools/contacts?search=...&status=active|removed&pool_id=6&allocation_department=...&page=1&per_page=20&order_by=pool_name&order=asc` — page through all first-level public-pool memberships visible in the active workspace. Each row includes `pool_id` and `pool_name`; a contact in two pools appears twice so its source list stays clear. Search, status, exact source-pool ID, exact trimmed allocation department, sorting, count and pagination apply across the combined result. Omit `allocation_department` for all departments; send it as an empty value to select contacts without one. Platform administrators see every pool; organization users see only their own allocation memberships with masked e-mails. Requires `pools:get`.
- `GET /api/pools/contacts/filters` — return the distinct visible `pool_id`, `pool_name`, `allocation_department` combinations for the aggregate view's dropdowns, independent of the current result page. An empty department denotes unassigned. Requires `pools:get` and obeys the same organization boundary.
- `GET /api/pools/contacts/export` — export the same combined result as CSV, including source pool ID and name. Accepts the same search, status, pool ID and department filters. Repeat `contact=<pool_id>:<contact_id>` to export only selected memberships within that authorized, filtered result (maximum 1000 selections). Malformed selections return 400. Requires `pools:export`; organization e-mails remain masked.
- `GET /api/pools/:id/contacts?search=...&status=active|removed&page=1&per_page=20&order_by=created_at&order=desc` — page through first-level or pool-allocation contacts by imported code, name, or e-mail. The equivalent `GET /api/customer-lists/:id/pool-contacts` route also accepts an `org_pool_allocation` list ID. `status=active` shows unremoved contacts; `status=removed` shows unresolved organization removals or exclusions. In a first-level pool, platform administrators see all organizations' exceptions once per contact, with `excluded`, `exclusion_reason`, `exception_organization_name`, and `exception_allocation_id` (when a membership can be restored). Non-platform-administrators see only their current organization's allocation and receive masked e-mail addresses. Unassigned contacts remain active. Allocation-list reads stay scoped to that allocation. Omitting `status` preserves the legacy all-contacts response. The legacy `customer_code` filter remains a fallback alias of `search`. Requires `pools:get`; non-platform-administrators must have the pool granted to the active organization.
- `GET /api/pools/:id/contacts/export` (alias `GET /api/customer-lists/:id/pool-contacts/export`) — stream the same filtered rows as CSV with masked e-mails. Repeat `contact=0:<contact_id>` to narrow the authorized result to selected contacts. Selection validation matches the aggregate export. Requires `pools:export`.
- `POST /api/pools/:id/contacts` — single-contact compatibility route
  (requires `pools:master_manage`); the product import entry is the unified customer
  import endpoint below.
- `DELETE /api/pools/:id/contacts/:contact_id` (alias
  `DELETE /api/customer-lists/:id/pool-contacts/:contact_id`) — permanently
  deletes one contact from a first-level pool. This destructive operation is
  requires `pools:master_manage`; organization allocation list IDs are
  rejected. Pool membership, allocation membership and pool campaign recipient
  rows follow their foreign-key deletion rules.
- `POST /api/pools/allocations` — create and bind an organization allocation; requires `pools:manage`. Ordinary members may act in their current organization only when a delivery grant already exists. With `pools:delivery_manage`, the caller may select another active organization and create its delivery grant in the same transaction. Allocations have no reply mailbox; the organization fallback is configured separately with `mailboxes:manage` and organization-management authorization.
- `GET /api/pools/organizations` — list active organization IDs and names for the delivery authorization picker; requires `pools:delivery_manage`, without granting membership or organization management.
- `POST|DELETE /api/pools/permissions` — grant/revoke a pool's organization delivery authorization; requires `pools:delivery_manage` and a `pool_id`/`organization_id` JSON body. Allocation management alone cannot grant or restore delivery access.
- `DELETE /api/pools/:id/contacts/:contact_id/email` — archive an invalid contact by clearing its email; requires `pools:master_manage`.
- `POST|DELETE|PUT /api/pools/allocations/members` — assign, logically remove, or restore a contact. Requires `pools:manage`; a non-platform-administrator is restricted to its own organization's allocation.
- `POST /api/pools/allocations/:id/import-members` — legacy compatibility route;
  it is not the management UI's import path. Requires `pools:manage` and the
  caller's organization boundary.
- `POST /api/import/customers` — unified customer import endpoint. When
  `customer_list_ids` contains exactly one first-level `pool`, the first CSV
  sheet or XLSX worksheet must map `customer_code`, `name`, `email` and
  `allocation_department`. Chinese template headers `客户编号`/`客户编码`,
  `姓名`, `邮箱`, `分配部门` are recognized; optional `回信邮箱`/`reply_to` is supported and other columns are ignored.
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
  without delivery administration see only their active organization. On narrow screens the columns stack.
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
- `GET /api/pools/:id/management-target?organization_id=...` — allocation state for an explicit organization; requires `pools:manage` or `pools:delivery_manage`. Allocation-only callers must target their active organization.
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
or `its unified reply mailbox "<address>" is not verified and active`. At most
five audiences are listed, the remainder is summarized as `(+N more)`, and the
message closes with the actionable steps, prefixed by `Fix: `: bind the
organization's allocation for that pool in `Customer lists -> Public pool
management` when that is one of the reasons, then have a manager of the
organization open its workspace and save a verified mailbox in `Manage
organizations -> Organization reply mailboxes` as the organization's unified
reply mailbox, and `then retry preview or send.` The steps are single-line and
never localized differently per reason.

An unresolved audience can be fixed by supplying the missing customer reply
addresses or configuring a verified organization fallback. Personal or system
mailboxes are not implicit public-pool routes. Pool exclusions are organization
scoped and do not physically delete
the first-level pool contact.

Pool contacts are not part of the legacy customer export surface, and they never
appear in `/api/customers` results. Their dedicated CSV export requires
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
