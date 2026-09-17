listmonk supports (>= v4.0.0) creating systems users with granular permissions to various features, including customer_list-specific permissions. Users can login with a username and password, or via an OIDC (OpenID Connect) handshake if an auth provider is connected. Various permissions can be grouped into "user roles", which can be assigned to users. CustomerList-specific permissions can be grouped into "customer_list roles".

## Workspaces and resource boundaries

Roles are necessary but not sufficient for access. Every authenticated request is
also limited to the selected personal or organization workspace and the
resource's owner, visibility, and transfer state. A global role or a
customer_list-specific grant never exposes a resource in another workspace.

- CustomerLists and customers remain private to their owner. Organization managers
  may inspect member-owned records in their active organization, but cannot
  modify them or send with them. Customer and single-customer exports are direct
  downloads; `customers:export` and the same workspace, ownership, and masking
  rules apply.
- Templates and campaigns can be private, organization-visible, or global.
  Members can read organization-visible resources in their active organization;
  global resources remain readable across workspaces. Sending, direct exports, and
  mutations apply stricter owner and workspace checks.
- Organization membership has separate `member` and `manager` roles. It does
  not grant system user-role or customer_list-role permissions. Archived organizations
  reject normal writes.

The server is authoritative. The admin UI may hide unavailable actions, but it
does not replace API authorization.

## User roles

A user role is a collection of user related permissions. User roles are attached to user accounts. User roles can be managed in `Admin -> Users -> User roles` The permissions are described below.

| Group       | Permission              | Description                                                                                                                                                                                                                          |
| ----------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| customer_lists       | customer_lists:get_all           | Get details of all accessible customer_lists in the active workspace                                                                                                                                                                         |
|             | customer_lists:manage_all        | Create, update, and delete all owner-managed customer_lists in the active workspace                                                                                                                                                          |
| customers | customers:get         | Get individual customer details                                                                                                                                                                                                    |
|             | customers:get_all     | Get all customers and their details in the active workspace                                                                                                                                                                       |
|             | customers:manage      | Add and update customers |
|             | customers:delete      | Delete customers, including bulk and query-based deletion |
|             | customers:blocklist   | Blocklist customers, including bulk and query-based blocklisting |
|             | customers:membership_manage | Add, remove, or unsubscribe customers from customer lists |
|             | customers:import      | Import customers from external files                                                                                                                                                                                               |
|             | customers:export      | Export customer and blocklist data; ownership, workspace, and masking rules still apply |
|             | customers:sensitive_read | View unmasked customer e-mail and attributes where the current customer list would otherwise mask them; this does not bypass workspace or ownership boundaries. Pool contacts are always masked except for platform administrators, so this permission does not unmask them |
|             | customers:sql_query   | Run raw SQL queries on customer data.<br /><span style="color: #de4a45;">**WARNING:**</span><span style="font-size: 0.875em; line-height: 1.3; color:#888;">This permission allows execution of arbitrary SQL expressions and SQL functions. While it is readonly on the table data, it allows querying of all customer_lists and customers directly from the database superceding individual customer_list and customer permissions. Raw SQL expressions make it possible to obtain Postgres database configuration and potentially interact with other Postgres system features. Give this permission ONLY to trusted users. [Learn more](#customerssql_query). |
| transactional | tx:send             | Send transactional messages to customers |
| campaigns   | campaigns:get           | Get and view campaigns belonging to permitted customer_lists                                                                                                                                                                                  |
|             | campaigns:get_all       | Get and view campaigns across accessible customer_lists in the active workspace                                                                                                                                                               |
|             | campaigns:get_analytics | Access campaign performance metrics                                                                                                                                                                                                  |
|             | campaigns:manage        | Create, update, and delete campaigns                                                                                                                                                                                                 |
|             | campaigns:manage_all    | Manage campaigns across all permitted customer_lists in the active workspace |
|             | campaigns:send         | Start or resume immediate delivery; ownership, SMTP, and API scope checks still apply |
|             | campaigns:test         | Send campaign test messages; campaign management, ownership, SMTP, and API scope checks still apply |
|             | campaigns:schedule    | Schedule campaign delivery; campaign management, ownership, SMTP, and API scope checks still apply |
|             | campaigns:control     | Pause, cancel, unschedule, or archive campaigns |
|             | campaigns:recipients   | View individual recipient details; requires customer read access, tracking, and the campaign privacy boundary |
| bounces     | bounces:get             | Get email bounce records                                                                                                                                                                                                             |
|             | bounces:manage          | View and process bounced email workflows; destructive actions use their dedicated permissions |
|             | bounces:delete         | Delete bounce records, including cleanup from a customer detail page |
|             | bounces:blocklist      | Blocklist customers represented by bounce records |
|             | webhooks:post_bounce    | Receive bounce notifications via webhook                                                                                                                                                                                             |
| media       | media:get               | Get uploaded media files                                                                                                                                                                                                             |
|             | media:manage            | Upload, update, and delete media                                                                                                                                                                                                     |
| templates   | templates:get           | Get email templates                                                                                                                                                                                                                  |
|             | templates:manage        | Create, update, and delete templates                                                                                                                                                                                                 |
| users       | users:get               | Get system user accounts                                                                                                                                                                                                             |
|             | users:manage            | Create, update, and delete user accounts <span style="color: #de4a45;">**WARNING:**</span><span style="font-size: 0.875em; line-height: 1.3; color:#888;">This permission allows creation of users with any role, including Super Admin. This permission should only be given to Super Admin level accounts</span>                              |
|             | users:tokens            | Create, list, and revoke API user integration tokens |
|             | roles:get               | Get user roles and permissions                                                                                                                                                                                                       |
|             | roles:manage            | Create and modify user roles                                                                                                                                                                                                         |
| settings    | settings:get            | Get system settings and logs, including the live server error stream (`GET /api/events`). Without this permission the admin UI does not subscribe to the stream; dashboard charts and counts are not affected. |
|             | settings:manage         | Modify system configuration                                                                                                                                                                                                          |
|             | settings:maintain       | Perform system maintenance tasks                                                                                                                                                                                                     |
| audit       | audit:get               | View business audit events in the active workspace                                                                                                                                                                                   |
| workspaces  | workspaces:personal     | Enter the personal workspace. Without this permission the account can only enter organization workspaces; platform administrators always retain the personal workspace. |
| organizations | organizations:platform_manage | Manage organization requests, lifecycle, members, invites, forwarding, and restricted transfers. This does not grant ordinary resource access or membership. |

Platform administration permissions (`users:*`, `roles:*`, `settings:*`, and
`organizations:platform_manage`) remain broad platform controls and are not
split into business-role actions. The Super Admin role retains full access.
Business roles should use the customer, campaign, and bounce actions above;
`audit:get` also controls audit-log export. They still cannot cross a workspace,
ownership, organization, or API Key scope boundary.

The business audit page uses server-side filtering and pagination. Users with
`audit:get` can export selected events from the currently loaded page or export
all events matching the current filters; both exports remain limited to the
active workspace. The table shows captured object summaries (such as a template
name, media filename, campaign subject, or current status), resolves retained
actor IDs to the current username/name, and keeps technical metadata behind the
expanded event details. Historical events that predate an object or actor
summary may still show only their stable ID; anonymous login events cannot be
retroactively associated with a username that was never stored.

## Personal workspace capability

Every user with a username and password (or OIDC) login starts without a
personal workspace unless their user role grants `workspaces:personal`.
Platform administrators (the Super Admin role) always keep the personal
workspace regardless of role permissions.

Behavior of an account without the capability:

- After login the user is sent to the workspace selection page, which customer_lists
  only the organization workspaces they are an active member of. A user with a
  single available space is entered automatically.
- Requests that select the personal workspace (missing workspace header,
  `organization_id=0`, or a personal-bound API key) are rejected with 403,
  except the four resource customer_list endpoints used by the migration UI
  (`/api/customer-lists`, `/api/templates`, `/api/campaigns`, `/api/media` on GET),
  which remain readable because the personal workspace only ever exposes the
  caller's own resources. Detail reads, exports, and every mutation still
  require the capability.
- Personal resources that existed before the capability was revoked are
  retained but hidden. They can still be copied or moved into an organization
  from `My organizations`, so the capability can be revoked without data loss
  and re-granted later without data loss either way.
- Accounts that are neither allowed a personal workspace nor members of any
  organization see a blocking message on the selection page and cannot enter
  the admin UI until an administrator grants access.

## CustomerList roles

A customer_list role is a collection of permissions assigned per customer_list. Each customer_list can be assigned a view (read) or manage (update) permission. CustomerList roles are attached to user accounts. Only the customer_lists defined in a customer_list role are accessible by the user, in the admin UI and via API calls. The `customer_lists:get_all` and `customer_lists:manage_all` user-role permissions override per-customer_list permissions, but neither bypasses the active workspace, resource owner, or transfer boundary.

## E-mail masking and customer codes

Two per-resource protections control what a viewer sees of a customer record:

- **Customer code.** Every customer carries a required (non-unique) `customer_code`
  business identifier on the admin and import paths. Public subscription forms
  and public APIs do not require it. When importing a CSV/XLSX/ZIP file in
  "subscribe" mode, the customer code column must be mapped (or present under a
  `customer_code` header); rows without a value are skipped. The customer code
  is included in customer listings and in CSV exports.
- **Masked e-mails.** Each customer_list has a "mask e-mails" (`mask_emails`) setting. When
  enabled, viewers who lack sensitive-data access to a customer (customer_list owners,
  customer-list managers, and platform administrators are always exempt) see masked
  e-mail addresses such as `liuxxx@gmail.com` instead of the full address in
  customer listings, detail views, API responses, and CSV exports scoped to
  that customer_list. The local part keeps its first 3 characters; the remainder is
  replaced with `x`s, preserving the length (local parts of 3 characters or
  fewer are fully replaced). Viewers with no sensitive-data access and no
  masking-enabled customer_list context continue to receive the pre-existing redaction
  (empty e-mail). Masking affects display only — searching and segmentation
  still match against the full address.

## API users

Regular users can create personal API keys from `Profile -> API Keys`. Each key is restricted to one personal or organization workspace, must expire within 24 months, and can be narrowed with business API scopes. The key never changes the user's role, customer_list role, organization membership, or resource ownership.

A user account can also be of type API. API users are administrator-managed internal service accounts. Unlike regular user accounts that have custom passwords or OIDC for authentication, API users get an automatically generated secret token and can retain the legacy API-token behavior.

## `customers:sql_query`

This permission allowers users to write and execute arbitrary SQL queries on the database. Although it is executed as a read-only transaction disallowing changing of data in the database tables, it allows querying of all customer_lists, customers and other data directly from the database superceding individual customer_list and customer permissions.

Raw SQL expressions also make it possible to obtain Postgres database configuration and potentially interact with other Postgres system features. Give this permission ONLY to trusted users.

If this permission is being assigned to many users, it is highly recommended that you create a custom Postgres role disallowing any privileged operations. For example:

```sql
CREATE ROLE listmonk_app WITH
    LOGIN
    PASSWORD '...'
    NOSUPERUSER
    NOCREATEDB
    NOCREATEROLE
    NOREPLICATION;
```

- “我参与的组织”页面的“迁移个人资源”默认折叠，点击标题展开或收起。仅最高管理员或具有 `workspaces:personal` 权限的人员显示；其他人员不显示迁移板块和待迁移资源统计，也不请求个人资源列表。
