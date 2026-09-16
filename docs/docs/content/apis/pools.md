# Public pools

Public customer pools are first-class `customer_list` types (`pool` and
`pool_segment`). Contact records retain the imported customer code (which may
repeat), name, allocation department and real email server-side; all
non-highest-administrator responses return a masked email. Internal reply
mailbox addresses are not masked.

First-level `pool` rows use the platform-wide `global` scope. A `pool_segment`
row uses the selected organization's scope and exposes that organization's
name as `organization_name`; the creating administrator remains available in
the owner fields for audit purposes.

Key endpoints:

- `GET /api/pools/:id/contacts?customer_code=...` — locate first-level or secondary-list contacts by imported code. The equivalent `GET /api/customer-lists/:id/pool-contacts` route also accepts a `pool_segment` list ID; secondary-list reads are limited to that segment.
- `POST /api/pools/:id/contacts` — legacy single-contact compatibility route
  (highest administrator); the product import entry is the unified customer
  import endpoint below.
- `POST /api/pools/segments` — split a first-level pool into a new organization segment. The list, pool grant and binding are created atomically; the organization manager configures its reply mailbox separately from **Organizations -> Manage organizations -> Organization reply mailboxes**.
- `PUT /api/pools/segments/:id/reply-mailbox` — update the segment's internal reply mailbox (organization manager in the segment's organization only).
- `POST|DELETE|PUT /api/pools/segments/members` — assign, logically remove, or restore a contact.
- `POST /api/pools/segments/:id/import-members` — legacy compatibility route;
  it is not the management UI's import path and is highest-administrator-only.
- `POST /api/import/customers` — unified customer import endpoint. When
  `customer_list_ids` contains exactly one first-level `pool`, the first CSV
  sheet or XLSX worksheet must map `customer_code`, `name`, `email` and
  `allocation_department`. Chinese template headers `客户编号`/`客户编码`,
  `姓名`, `邮箱`, `分配部门` are recognized; other columns are ignored.
  Only the highest administrator may use this branch. `分配部门` must match an
  active `organizations.name`; unknown or archived departments are rejected
  row-by-row and are not written. A valid value is stored on the pool contact
  and, when that organization already has a secondary list for the pool, also
  creates the corresponding secondary-list membership. Creating the secondary
  list later backfills existing matching contacts; it never creates an
  organization.
- `POST /api/pools/import` — legacy ordinary-list-to-pool compatibility route;
  new product flows use the unified customer import endpoint.
- `GET /api/pools/:id/management-target?organization_id=...` — highest-admin-only target context: the target organization's existing segment state.
- `POST /api/campaigns/:id/pools` — attach a pool or segment audience to a campaign draft.

Preview and send operations reject unresolved pool audiences (missing an
organization segment or its reply mailbox). Pool exclusions are organization
scoped and do not physically delete the first-level pool contact.

Pool contacts are not part of the legacy customer export surface. Non-highest
administrators cannot obtain a pool contact's real email through list, detail,
CSV, or API-key responses.

Campaign responses include `customer_pools[].reply_mailbox_email` so operators
can see the effective internal reply route (`一级公海 -> 二级列表 -> 回件邮箱`).
When the selected audience is an explicit secondary list,
`customer_pools[].segment_list_id` and `customer_pools[].segment_list_name` identify the
selector-compatible `pool_segment` list; `segment_id` remains the internal
pool-to-organization binding ID. This lets the campaign editor round-trip a
secondary-list audience without silently changing it to its parent pool.
This is an internal company address and is not masked; customer contact emails
remain protected by the pool contact DTO policy.

## Admin workflow

Open a first-level pool from **CustomerLists**, then select **Manage pool**.
For a highest administrator, first choose the **target organization** in the
management dialog. This is an allocation target, not a workspace switch and
not an organization-membership action: the administrator does not need to join
the organization and remains in the current workspace. The dialog then offers
one **Create and bind** action. It creates the secondary list, grants delivery
access and binds it to the open pool in one transaction. The target organization
configures its internal reply mailbox from its own organization workspace. A secondary list cannot be created from the
generic customer-list form, and there is no secondary-to-primary merge flow.
A highest administrator performs the split through `POST /api/pool-segments`
(the `/api/pools/segments` alias is equivalent) with the target organization's
`organization_id`, without joining it. An organization manager may perform the
same split for their own organization: the request must carry that organization's
`organization_id`, and any other organization is rejected with `403`. Ordinary
organization members cannot create lists.

Each first-level pool can have one bound secondary list per organization. The
dialog does not import contact files. Import the four-column pool template from
the unified **Customer import** page; rows whose `分配部门` matches an existing
secondary-list organization are allocated during import. If the secondary list
is created afterwards, the create-and-bind transaction backfills those rows. The
dialog is used to select a target organization and create/bind its secondary list.
The customer-count link for a first-level or secondary list opens the dedicated
pool contact view; it does not use the ordinary customer table. That view keeps
the pool data model separate and applies the same masked DTO policy as the API.
Organization operators continue to work only inside their own organization
workspace; they cannot select another target organization or manage a
first-level pool. The server enforces the same boundary for direct API calls.
