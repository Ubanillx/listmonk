# Public pools

Public customer pools are first-class `customer_list` types (`pool` and
`pool_segment`). Contact records retain the imported customer code (which may
repeat) and real email server-side; all non-highest-administrator responses
return a masked email. Internal reply mailbox addresses are not masked.

Key endpoints:

- `GET /api/pools/:id/contacts?customer_code=...` — locate pool contacts by imported code.
- `POST /api/pools/:id/contacts` — import a contact (highest administrator).
- `POST /api/pools/segments` — split a first-level pool into a new organization segment. The list, pool grant and binding are created atomically; the organization manager configures its reply mailbox separately in the organization workspace.
- `PUT /api/pools/segments/:id/reply-mailbox` — update the segment's internal reply mailbox (organization manager in the segment's organization only).
- `POST|DELETE|PUT /api/pools/segments/members` — assign, logically remove, or restore a contact.
- `POST /api/pools/segments/:id/import-members` — upload a CSV/XLSX allocation file
  with `customer_code` and `email` columns. The server matches both normalized
  fields within the parent pool and returns created, reactivated, already
  assigned, unmatched, ambiguous and invalid row counts. The response never
  includes uploaded email values.
- `POST /api/pools/import` — import an ordinary customer list into a pool; this is separate from secondary-list splitting.
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
An organization manager may perform the same split while working in that
organization; ordinary organization members cannot create lists.

Each first-level pool can have one bound secondary list per organization. If
the active organization already has one, the dialog selects it and operators
assign contacts directly instead of creating another secondary list.

After either action, select the secondary list in the dialog and upload the
allocation template (`customer_code,email`). The server performs the batch
match and reports row-level failures (missing record or ambiguous duplicate)
without returning real email values. A collapsed **Single-record maintenance**
panel remains available for searching by customer code, logical removal,
restore, and clearing an invalid email; it is not the bulk allocation path.
Organization operators continue to work only inside their own organization
workspace; they cannot select another target organization.
