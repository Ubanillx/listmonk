# Concepts

## Organization selection

The workspace switcher lists every active organization a platform administrator can enter. Other users see their active memberships. The joined-organizations page and personal-resource migration targets list actual memberships, even for administrators. Page refreshes preserve this distinction.

Workspace information is validated before forms are shown. A temporary loading failure provides a retry button and preserves the selected workspace; a failed later organization refresh keeps the last complete list and provides a retry entry in the workspace switcher. Revoked access is still checked by the server.

## Organization

An organization is a tenant inside listmonk. Users are admitted to it through organization membership, which carries a per-membership role of `member` or `manager` and is separate from system user roles and customer_list roles: membership decides which organization workspaces a user may enter and how far their access reaches inside them, but it grants no feature permission on its own. Organization managers may inspect resources owned by members of their organization, but they cannot modify them or send with them. Organization requests, lifecycle, members and invitations are controlled by platform administrators holding `organizations:platform_manage`.

An organization is either active or archived. Only active organizations are offered as workspaces; an archived organization rejects normal writes and exports, and only platform administrators can run the restricted cleanup and transfer flows on it. [Learn more](roles-and-permissions.md)

## Workspace

A workspace is the boundary that a request runs inside: a user's personal space, or one organization that the user is an active member of. Every resource row carries an `organization_id` and an owner, so the same account sees different data depending on the workspace it has selected. The personal workspace is represented by `organization_id=0` and only ever exposes the caller's own resources; entering it requires the `workspaces:personal` user-role capability, which platform administrators always retain. Requests that select the personal workspace without that capability are rejected, except the four read-only customer_list endpoints used to migrate retained personal resources into an organization.

The active workspace is resolved from the `X-Listmonk-Organization-ID` request header. API token clients must send the header on every request; browser clients keep their selection in localStorage, and the server falls back to the `organization_id` query parameter and then to a non-sensitive workspace cookie. A missing header, `organization_id=0` and the literal `personal` all select the personal workspace, and a personal API key is bound to one workspace and cannot be pointed at another by changing the header. After login, users choose a space on the workspace selection page, which lists their personal space and their active organization memberships, or all active organizations for platform administrators; a user with a single available space enters it automatically. [Learn more](roles-and-permissions.md)

## Resource visibility

Shareable resources, namely ordinary customer lists, templates, campaigns and media, carry a visibility that controls who may read them beyond their owner: `private` (the owner and platform administrators, plus the read-only oversight of organization managers in their active organization), `organization` (additionally readable by every member of the resource's organization) and `global` (readable from every workspace). An organization-shared ordinary list exposes list metadata and counts, while customer records and list mutations retain their owner and permission boundaries. Only templates and campaigns can be `global`: media and ordinary customer lists can never be global, and customers always stay private to their owner. A first-level [public pool](#public-pool) is the single `global` customer list type.

Visibility is a publication scope only. It never moves a resource between workspaces or changes its owner, and being able to read a resource never implies the right to use it in a message, to copy it, to send with it, to export it or to modify it; those are checked separately. [Learn more](roles-and-permissions.md)

## Customer

A customer is a recipient identified by an e-mail address and name. Customers receive e-mails that are sent from listmonk. A customer can be added to any number of customer_lists. Customers who are not a part of any customer_lists are considered *orphan* records.

### Attributes

Attributes are arbitrary properties attached to a customer in addition to their e-mail and name. They are represented as a JSON map. It is not necessary for all customers to have the same attributes. Customers can be [searched and organized](querying-and-segmentation.md) into customer_lists by customer code, name, e-mail and list membership, and attributes can be inserted into the e-mails sent to them. For example:

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

### Subscription statuses

A customer can be added to one or more customer_lists, and each such relationship can have one of these statuses.

| Status         | Description                                                                                                                                                                                 |
|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `unconfirmed`  | The customer was added to the customer_list directly without their explicit confirmation. Nonetheless, the customer will receive campaign messages sent to single opt-in campaigns.              |
| `confirmed`    | The customer confirmed their subscription by clicking on 'accept' in the confirmation e-mail. Only confirmed customers in opt-in customer_lists will receive campaign messages send to the customer_list. |
| `unsubscribed` | The customer is unsubscribed from the customer_list and will not receive any campaign messages sent to the customer_list.                                                                                   |

### Segmentation

Segmentation is the process of organizing customers into smaller groups, such as customer lists. The customers page supports customer code, name and e-mail search plus list and membership filters. [Learn more](querying-and-segmentation.md).

## CustomerList

A customer_list (or a _mailing list_) is a collection of customers grouped under a name, for instance, _clients_. CustomerLists are used to organise customers and send e-mails to specific groups. A customer_list can be single opt-in or double opt-in. Customers added to double opt-in customer_lists have to explicitly accept the subscription by clicking on the confirmation e-mail they receive. Until then, they do not receive campaign messages.

## CustomerList role

A customer_list role is a per-customer_list grant of `get` (view) or `manage` (update) permission attached to a user account, and it names the customer_lists that the user may access in the admin UI and in API calls. The `customer_lists:get_all` and `customer_lists:manage_all` user-role permissions override per-list grants, but neither bypasses the active workspace, the resource owner or a pending transfer. [Learn more](roles-and-permissions.md)

## Public pool

A public pool is a first-class customer_list type (`pool` or `org_pool_allocation`), and is not a `public` list that accepts anonymous subscriptions. A first-level `pool` is a platform-wide `global` resource whose contacts are stored as pool contacts instead of ordinary customers, while each organization's delivery permission for it is stored separately; an authorized pool is the explicit cross-workspace exception to the delivery and import boundaries. Contact records keep the imported customer code (which may repeat), name, allocation department and real e-mail address server-side, and every response to a user other than a platform administrator returns a masked e-mail; pool contacts are not part of the ordinary customer export surface. An internal reply mailbox is a company address and is never masked.

Importing pool contacts and managing a first-level pool are platform-administrator actions. The administrator can open **Customers → Import → Public-pool customer import**, select or create a first-level pool, then select each target organization and view or create its bound allocation on the same page. Creating a list returns to the import form with that list selected. An organization's own manager may also create and bind that organization's allocation from the organization workspace and configure its unified reply mailbox there. An `org_pool_allocation` belongs to that organization's scope, exposes its `organization_name`, keeps the creating user in the owner fields for audit, and each pool can have at most one bound allocation per organization. For an organization, unsubscribes, bounces and manual removals are recorded as organization-scoped exclusions that never alter the first-level master data or another organization's assignment. [Learn more](apis/pools.md)

## Campaign

A campaign is an e-mail (or any other kind of messages) that is sent to one or more customer_lists.


## Transactional message

A transactional message is an arbitrary message sent to a customer using the transactional message API. For example a welcome e-mail on signing up to a service; an order confirmation e-mail on purchasing an item; a password reset e-mail when a user initiates an online account recovery process.


## Template

A template is a re-usable HTML design that can be used across campaigns and when sending arbitrary transactional messages. Most commonly, templates have standard header and footer areas with logos and branding elements, where campaign content is inserted in the middle. listmonk supports [Go template](https://gowebexamples.com/templates/) expressions that lets you create powerful, dynamic HTML templates. [Learn more](templating.md).

## Messenger

listmonk supports multiple custom messaging backends in additional to the default SMTP e-mail backend, enabling not just e-mail campaigns, but arbitrary message campaigns such as SMS, FCM notifications etc. A *Messenger* is a web service that accepts a campaign message pushed to it as a JSON request, which the service can in turn broadcast as SMS, FCM etc. [Learn more](messengers.md).

## Tracking pixel

The tracking pixel is a tiny, invisible image that is inserted into an e-mail body to track e-mail views. This allows measuring the read rate of e-mails. While this is exceedingly common in e-mail campaigns, it carries privacy implications and should be used in compliance with rules and regulations such as GDPR. It is possible to track reads anonymously without associating an e-mail read to a customer.

## Click tracking

It is possible to track the clicks on every link that is sent in an e-mail. This allows measuring the clickthrough rates of links in e-mails. While this is exceedingly common in e-mail campaigns, it carries privacy implications and should be used in compliance with rules and regulations such as GDPR. It is possible to track link clicks anonymously without associating an e-mail read to a customer.

## Bounce

A bounce occurs when an e-mail that is sent to a recipient "bounces" back for one of many reasons including the recipient address being invalid, their mailbox being full, or the recipient's e-mail service provider marking the e-mail as spam. listmonk can automatically process such bounce e-mails that land in a configured POP mailbox, or via APIs of SMTP e-mail providers such as AWS SES and Sengrid. Based on settings, customers returning bounced e-mails can either be blocklisted or deleted automatically. [Learn more](bounces.md).
