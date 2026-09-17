# Public pools

Public customer pools are first-class `customer_list` types (`pool` and
`org_pool_allocation`). Contact records retain the imported customer code (which may
repeat), name, allocation department and real email server-side; all
non-highest-administrator responses return a masked email. Internal reply
mailbox addresses are not masked.

First-level `pool` rows use the platform-wide `global` scope. A `org_pool_allocation`
row uses the selected organization's scope and exposes that organization's
name as `organization_name`; the creating administrator remains available in
the owner fields for audit purposes.

Key endpoints:

- `GET /api/pools/:id/contacts?search=...&page=1&per_page=20&order_by=created_at&order=desc` — page through first-level or pool-allocation contacts by imported code, name, or e-mail. The equivalent `GET /api/customer-lists/:id/pool-contacts` route also accepts a `org_pool_allocation` list ID; pool-allocation reads are limited to that allocation. The legacy `customer_code` filter remains as a fallback alias of `search`. Requires `pools:get`; non-platform-administrators must have the pool granted to the active organization.
- `GET /api/pools/:id/contacts/export` (alias `GET /api/customer-lists/:id/pool-contacts/export`) — stream the same filtered rows as CSV with masked e-mails. Requires `pools:export`.
- `POST /api/pools/:id/contacts` — single-contact compatibility route
  (requires `pools:manage`); the product import entry is the unified customer
  import endpoint below.
- `POST /api/pools/allocations` — split a first-level pool into a new organization allocation. The list, pool grant and binding are created atomically. The allocation carries no reply mailbox of its own: pool recipients reply to the organization's unified reply mailbox, configured once from **Organizations -> Manage organizations -> Organization reply mailboxes**.
- `POST|DELETE|PUT /api/pools/allocations/members` — assign, logically remove, or restore a contact. Requires `pools:manage`; a non-platform-administrator is restricted to its own organization's allocation.
- `POST /api/pools/allocations/:id/import-members` — legacy compatibility route;
  it is not the management UI's import path. Requires `pools:manage` and the
  caller's organization boundary.
- `POST /api/import/customers` — unified customer import endpoint. When
  `customer_list_ids` contains exactly one first-level `pool`, the first CSV
  sheet or XLSX worksheet must map `customer_code`, `name`, `email` and
  `allocation_department`. Chinese template headers `客户编号`/`客户编码`,
  `姓名`, `邮箱`, `分配部门` are recognized; other columns are ignored.
  Only the highest administrator may use this branch. `分配部门` must match an
  active `organizations.name`; unknown or archived departments are rejected
  row-by-row and are not written. A valid value is stored on the pool contact
  and, when that organization already has a pool allocation for the pool, also
  creates the corresponding pool-allocation membership. Creating the pool allocation
  list later backfills existing matching contacts; it never creates an
  organization.
- `POST /api/pools/import` — legacy ordinary-list-to-pool compatibility route;
  new product flows use the unified customer import endpoint.
- `GET /api/pools/:id/management-target?organization_id=...` — highest-admin-only target context: the target organization's existing allocation state.
- `POST /api/campaigns/:id/pools` — attach a pool or allocation audience to a campaign draft.

Preview and send operations reject unresolved pool audiences (missing the
organization's pool allocation or its unified reply mailbox) and answer with
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

The rejection never falls back to a personal or system mailbox: an unresolved
pool audience is a configuration gap in the target organization, and the
organization's manager fixes it by setting the organization's unified reply
mailbox. Pool exclusions are organization scoped and do not physically delete
the first-level pool contact.

Pool contacts are not part of the legacy customer export surface, and they never
appear in `/api/customers` results. Their dedicated CSV export requires
`pools:export`. Non-highest administrators cannot obtain a pool contact's real
email through list, detail, CSV, or API-key responses.

Public-pool contact access is governed by three independent, role-configurable
permissions: `pools:get` (browse/search), `pools:manage` (create, assign,
remove, restore, clear e-mail) and `pools:export`. Platform administrators
bypass the grants; every other caller is additionally restricted to the pool
lists and allocations granted to the active workspace organization.

Campaign responses include `customer_pools[].reply_mailbox_email` so operators
can see the effective internal reply route (一级公海 -> 公海分配 -> 组织统一回件邮箱).
When the selected audience is an explicit pool allocation,
`customer_pools[].allocation_list_id` and `customer_pools[].allocation_list_name` identify the
selector-compatible `org_pool_allocation` list; `allocation_id` remains the internal
pool-to-organization binding ID. This lets the campaign editor round-trip a
pool-allocation audience without silently changing it to its parent pool.
This is an internal company address and is not masked; customer contact emails
remain protected by the pool contact DTO policy.

## Admin workflow

Open a first-level pool from **CustomerLists**, then select **Manage pool**.
For a highest administrator, first choose the **target organization** in the
management dialog. This is an allocation target, not a workspace switch and
not an organization-membership action: the administrator does not need to join
the organization and remains in the current workspace. The dialog then offers
one **Create and bind** action. It creates the pool allocation, grants delivery
access and binds it to the open pool in one transaction. The reply route for the
organization's pool recipients is the organization's single **unified reply
mailbox**, configured once by the organization's manager in
**Organizations -> Manage organizations -> Organization reply mailboxes**
(`PUT /api/organizations/:id/reply-mailbox`). A pool allocation carries no reply
mailbox of its own, the campaign field **customer reply mailbox** does not feed
this route, and a pool allocation cannot be created from the generic
customer-list form; there is no pool-allocation-to-first-level merge flow.
A highest administrator performs the split through `POST /api/org-pool-allocations`
(the `/api/pools/allocations` alias is equivalent) with the target organization's
`organization_id`, without joining it. An organization manager may perform the
same split for their own organization: the request must carry that organization's
`organization_id`, and any other organization is rejected with `403`. Ordinary
organization members cannot create lists.

Each first-level pool can have one bound pool allocation per organization. The
dialog does not import contact files. Import the four-column pool template from
the unified **Customer import** page; rows whose `分配部门` matches an existing
pool-allocation organization are allocated during import. If the pool allocation
is created afterwards, the create-and-bind transaction backfills those rows. The
dialog is used to select a target organization and create/bind its pool allocation.
The customer-count link for a first-level or pool allocation opens the customer
area's public-pool tab (`Customers.vue`), which renders the contacts with the
same toolbar, pagination, sorting and row actions as ordinary customers instead
of a dedicated page. That view keeps the pool data model separate and applies
the same masked DTO policy as the API.
Organization operators continue to work only inside their own organization
workspace; they cannot select another target organization or manage a
first-level pool. The server enforces the same boundary for direct API calls.
