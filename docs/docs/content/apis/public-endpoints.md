# API / Public endpoints

Public endpoints are the unauthenticated HTTP surface of the server. They are
registered on a router group that carries **no authentication middleware**, no
API-key scope check and no workspace middleware (`cmd/handlers.go`, the
"Public API endpoints" block), so they accept requests without a session,
`Authorization` header or API key.

That is the main difference from the authenticated `/api/*` group described in
[APIs](apis.md): the authenticated group resolves the caller before the handler
runs and answers anonymous requests with a JSON error, while a public handler
has to implement its own protection. The protections that exist are listed per
endpoint below: the public-subscription honeypot/CAPTCHA gate, the
`app.enable_public_subscription_page` and `app.enable_public_archive` settings,
and, for media files, an explicit "this blob belongs to a live public archive
campaign" check. No request-rate limiter is configured for these routes; the
in-process throttle only guards admin login and 2FA verification
(`cmd/auth.go`).

Because every response is produced by the handler itself, the uniform
`{"data": ...}` envelope of the authenticated API is **not** guaranteed on this
surface; the shapes documented below are what each handler returns. Public
requests are still written to the audit log (`cmd/audit.go`).

Method | Endpoint | Description
-------|----------|---------------------------------
GET    | [/api/public/customer-lists](#get-apipubliccustomer-lists) | List the customer lists available for public subscription
POST   | [/api/public/subscription](#post-apipublicsubscription) | Create a subscription from the public form or JSON API
GET    | [/api/public/captcha/altcha](#get-apipubliccaptchaaltcha) | Get an Altcha CAPTCHA challenge
GET    | [/api/public/archive](#get-apipublicarchive) | List campaigns published to the public archive
GET    | [/api/media/file/{media_id}/{filename}](#get-apimediafilemedia_idfilename) | Serve a media file by ID (canonical URL)
GET    | [/api/media/file/{filename}](#get-apimediafilefilename) | Serve a media file by stored filename (legacy URL)
POST, PUT, DELETE | [/api/org-pool-allocations/*](#apiorg-pool-allocations) | Alias family for `/api/pools/allocations/*`

______________________________________________________________________

#### GET /api/public/customer-lists

Return the customer lists a visitor may subscribe to: rows with
`type = 'public'` and `status = 'active'` that have an owner and are not
pending transfer (`internal/core/workspace_queries.go`, used by
`cmd/public.go`). This is what the built-in subscription form renders.

Unlike the rest of the API, the response is a **bare JSON array** of minimal
objects, not a `data`-wrapped object:

```json
[
  {
    "uuid": "55e243af-80c6-4169-8d7f-bc571e0269e9",
    "name": "Opt-in customer_list"
  }
]
```

The handler does not test `app.enable_public_subscription_page`, so the list
stays readable even when the HTML subscription form is switched off. Use
[POST /api/public/subscription](#post-apipublicsubscription) to write.

```shell
curl 'http://localhost:9000/api/public/customer-lists'
```

See [Public customer lists](customer-lists.md#get-apipubliccustomer-lists) for
the matching entry in the authenticated customer-list API.

______________________________________________________________________

#### POST /api/public/subscription

Create a subscription from the built-in public form or from a script. Accepts
form-encoded and JSON bodies; the fields are `name`, `email` and the selected
list UUIDs as the repeated form field `l` or the JSON array `list_uuids`.
Multiple lists are allowed, but they must all belong to the same owner and
workspace.

The endpoint runs the same abuse guard as the HTML form:

- a filled-in hidden `nonce` honeypot field is rejected with HTTP 502;
- when a CAPTCHA is enabled, the solution must be posted as `altcha` (Altcha)
  or `h-captcha-response` (hCaptcha). A missing or invalid solution is rejected
  with HTTP 400, and an Altcha solution can only be used once.

When `app.enable_public_subscription_page` is off, this endpoint answers HTTP
400, while the built-in form page `/subscription/form` answers HTTP 404. The
success response carries the double opt-in flag of the created subscription:

```json
{
  "data": {
    "has_optin": true
  }
}
```

Request and response examples are in
[Customers](customers.md#post-apipublicsubscription).

______________________________________________________________________

#### GET /api/public/captcha/altcha

Return a fresh Altcha CAPTCHA challenge for the built-in subscription form
(`<altcha-widget challengeurl=".../api/public/captcha/altcha">` in
`static/public/templates/subscription-form.html`).

The route is only served when Altcha is the active CAPTCHA provider. With no
CAPTCHA configured, or with hCaptcha selected, it responds HTTP 404
`captcha not enabled`. The body is the raw challenge JSON (again not wrapped in
`data`) with the fields `algorithm`, `challenge`, `maxNumber`, `salt` and
`signature`. The challenge is generated with SHA-256, `maxNumber` is the
configured `security.captcha.altcha.complexity`, and it expires after 5 minutes.

```shell
curl 'http://localhost:9000/api/public/captcha/altcha'
```

Submitting the solved payload happens through
[POST /api/public/subscription](#post-apipublicsubscription) in the field
`altcha`.

______________________________________________________________________

#### GET /api/public/archive

List campaigns published to the public archive (Campaigns -> Create new ->
Archive -> Publish to public archive). This route is only registered when
`app.enable_public_archive` is on; otherwise requests fall through to the
`/api/*` not-found handler and return HTTP 404.

The listing is paginated with the global pagination query parameters `page` and
`per_page` (default 20, maximum 50) and returns the standard paginated
envelope. Each result object contains:

| Field        | Description                                                              |
|:-------------|:-------------------------------------------------------------------------|
| `uuid`       | Campaign UUID.                                                            |
| `subject`    | Campaign subject, with template tags rendered.                            |
| `content`    | Always empty in this listing; the RSS feed renders the body only when `app.enable_public_archive_rss_content` is on. |
| `created_at` | Campaign creation timestamp.                                              |
| `send_at`    | Campaign send timestamp.                                                  |
| `url`        | Archive page URL, built from the configured archive URL and the campaign's archive slug, or its UUID when no slug is set. |

The envelope fields are `results`, `total`, `page` and `per_page`; the public
archive does not parse the `search` and `query` parameters. An archive with no
campaigns returns HTTP 200 with `results: []`.

The corresponding HTML pages and the RSS feed are listed under
[Adjacent public routes](#adjacent-public-routes-not-under-apipublic) and
described in [Archives](../archives.md).

```shell
curl 'http://localhost:9000/api/public/archive?page=1&per_page=20'
```

______________________________________________________________________

#### GET /api/media/file/{media_id}/{filename}

Canonical media URL returned by the media library for newly stored files (in
the route, the ID parameter is named `:id`). `{media_id}` is the media row ID
and `{filename}` must match that row's stored filename or thumbnail name; a
mismatch returns HTTP 404, so a media ID never grants access on its own.

The route is deliberately registered outside the authenticated `/api` group so
that an active public archive can render its linked images, but it is still
wrapped in the session/API-key middleware with the `media:read` scope. An
unauthenticated request is granted only when the blob is referenced by a
campaign that is currently in the public archive **and**
`app.enable_public_archive` is on; every other anonymous request is answered
with HTTP 404. On the ID route every archive failure — public archive disabled,
row missing, or filename mismatch — is collapsed into HTTP 404
(`cmd/media.go:457`), while the filename route maps a missing mapping to HTTP
404 only and returns other errors unchanged (`cmd/media.go:422`), so a lookup
failure there can surface as a 5xx. Authenticated browser requests are resolved
against the active workspace
(using the `X-Listmonk-Organization-ID` header, the workspace cookie, or the
`organization_id` query parameter), which is required for private and
organization media.

```shell
curl 'http://localhost:9000/api/media/file/12/logo.png'
```

______________________________________________________________________

#### GET /api/media/file/{filename}

The historical, filename-only spelling of the same handler. It is kept for
template and campaign bodies that already contain such URLs; new content should
use the ID-qualified form above. Visibility, workspace resolution and response
headers are identical, with one difference: because a filename may be shared by
cloned media rows, the lookup resolves the caller's workspace row (or, for an
anonymous public-archive request, the public archive row) rather than a
particular ID. Filenames are accepted URL-escaped.

```shell
curl 'http://localhost:9000/api/media/file/logo.png'
```

______________________________________________________________________

#### Response headers for media files

| Sniffed content | `Content-Type` | Additional headers |
|:----------------|:---------------|:-------------------|
| HTML / XHTML (`text/html`, `application/xhtml+xml`) | `application/octet-stream` | `Content-Disposition: attachment` and `Content-Security-Policy: default-src 'none'; sandbox` |
| XML (`text/xml`, `application/xml`, which covers SVG that sniffs as XML) | the sniffed type | `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox` |
| Anything else (for example `image/png`, `application/pdf`) | the sniffed type | none |

Every media response additionally sets `Cache-Control: private, max-age=300`
and `X-Content-Type-Options: nosniff`, and the content type is always derived by
sniffing the stored bytes, never from the type declared at upload. HTML and
XHTML uploads are refused by the media library in the first place
(`cmd/media.go`).

Legacy storage URLs (`upload.filesystem.upload_uri`, or an S3 public URL below
the root URL) are routed through the same compatibility handler and the same
header policy (`cmd/init.go`).

______________________________________________________________________

#### /api/org-pool-allocations/*

`/api/org-pool-allocations/*` is a second, fully equivalent spelling of
`/api/pools/allocations/*`. Every pair below is registered on the same handler
function with the same API-key scope (`customer_lists:read` /
`customer_lists:write`), so requests and responses are identical — this is one
resource with two route spellings, not two APIs:

| `/api/org-pool-allocations/*`                        | `/api/pools/allocations/*`                        |
|:----------------------------------------------|:-----------------------------------------------|
| `POST /api/org-pool-allocations`                     | `POST /api/pools/allocations`                     |
| `PUT /api/org-pool-allocations/:id/reply-mailbox`    | `PUT /api/pools/allocations/:id/reply-mailbox`    |
| `POST /api/org-pool-allocations/members`             | `POST /api/pools/allocations/members`             |
| `PUT /api/org-pool-allocations/members`              | `PUT /api/pools/allocations/members`              |
| `DELETE /api/org-pool-allocations/members`           | `DELETE /api/pools/allocations/members`           |
| `POST /api/org-pool-allocations/:id/import-members`  | `POST /api/pools/allocations/:id/import-members`  |

Unlike the write routes, the read routes are not symmetrical: allocations are
listed by `GET /api/pools/:id/allocations`, which is the same handler as
`GET /api/customer-lists/:id/org-pool-allocations`. There is no
`GET /api/org-pool-allocations/...` listing route. Both prefixes allow a 32 MB request
body for allocation member imports (`cmd/body_limit.go`).

Pool-contact member routes additionally require the configurable `pools:manage`
permission; browsing uses `pools:get` and `GET /api/pools/:id/contacts[/export]`
(alias `GET /api/customer-lists/:id/pool-contacts[/export]`) uses
`pools:get` / `pools:export`. Platform administrators bypass the grants, every
other caller is scoped to the active workspace organization.

Payloads and pool semantics, including the highest-administrator and
organization boundaries, are documented once in [Public pools](pools.md); this
page does not repeat them.

______________________________________________________________________

## Adjacent public routes (not under /api/public/)

The public archive pages, subscription confirmation and click/view tracking are
browser-facing routes registered on the same unauthenticated group but **not**
below `/api/public/`. They return HTML, redirects or images rather than the API
envelope.

- `GET /subscription/form` and `POST /subscription/form` — the built-in
  subscription form and its form-post target. Both return HTTP 404 when
  `app.enable_public_subscription_page` is off. The JSON form-post counterpart
  is [POST /api/public/subscription](#post-apipublicsubscription).
- `GET|POST /subscription/optin/:subUUID` — the double opt-in confirmation page
  reached from the "Confirm subscription" link; `confirm=true` confirms the
  pending subscriptions.
- `GET /subscription/:campUUID/:subUUID` and
  `POST /subscription/:campUUID/:subUUID` — the subscription preferences /
  unsubscribe page linked as `{{ UnsubscribeURL }}` in campaigns.
- `POST /subscription/export/:subUUID` and `POST /subscription/wipe/:subUUID` —
  customer self-service data export and erasure, gated by the privacy settings.
- `GET /link/:linkUUID/:campUUID/:subUUID` — link click tracking; records the
  click (unless tracking is disabled globally) and answers with a redirect to
  the underlying URL.
- `GET /campaign/:campUUID/:subUUID/px.png` — the open-tracking pixel; it always
  returns the PNG, whether or not the view is recorded.
- `GET /archive`, `GET /archive/:id`, `GET /archive/latest` and
  `GET /archive.xml` — the public archive pages and RSS feed, all registered
  only when `app.enable_public_archive` is on. The two single-campaign pages
  render user-authored campaign HTML and therefore send
  `Content-Security-Policy: sandbox` to isolate them from the application
  origin; the archive listing page does not render campaign bodies and does not
  set that header. See [Archives](../archives.md).
- `GET /public/custom.css` and `GET /public/custom.js` — the appearance files
  configured under Settings.
- `POST /webhooks/service/:service` — inbound provider bounce webhooks, present
  only when bounce webhooks are enabled. See [Bounces](../bounces.md).
