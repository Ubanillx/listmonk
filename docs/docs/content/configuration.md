# Configuration

### TOML Configuration file
One or more TOML files can be read by passing `--config config.toml` multiple times. Apart from a few low level configuration variables and the database configuration, all other settings can be managed from the `Settings` dashboard on the admin UI.

To generate a new sample configuration file, run `listmonk --new-config`

### Environment variables
Variables in config.toml can also be provided as environment variables prefixed by `LISTMONK_` with periods replaced by `__` (double underscore). To start listmonk purely with environment variables without a configuration file, set the environment variables and pass the config flag as `--config=""`.

Example:

| **Environment variable**       | Example value  |
| ------------------------------ | -------------- |
| `LISTMONK_app__address`        | "0.0.0.0:9000" |
| `LISTMONK_db__host`            | db             |
| `LISTMONK_db__port`            | 9432           |
| `LISTMONK_db__user`            | listmonk       |
| `LISTMONK_db__password`        | listmonk       |
| `LISTMONK_db__database`        | listmonk       |
| `LISTMONK_db__ssl_mode`        | disable        |


### Customizing system templates
See [system templates](templating.md#system-templates).


### HTTP routes
When configuring auth proxies and web application firewalls, use this table.

#### Private admin endpoints.

| Methods | Route              | Description             |
| ------- | ------------------ | ----------------------- |
| `*`     | `/api/*`           | Admin APIs              |
| `GET`   | `/admin/*`         | Admin UI and HTML pages |
| `POST`  | `/webhooks/bounce` | Admin bounce webhook    |


#### Public endpoints to expose to the internet.

| Methods     | Route                 | Description                                   |
| ----------- | --------------------- | --------------------------------------------- |
| `GET, POST` | `/subscription/*`     | HTML subscription pages                       |
| `GET, `     | `/link/*`             | Tracked link redirection                      |
| `GET`       | `/campaign/*`         | Pixel tracking image                          |
| `GET`       | `/public/*`           | Static files for HTML subscription pages      |
| `POST`      | `/webhooks/service/*` | Bounce webhook endpoints for AWS and Sendgrid |
| `GET`       | `/uploads/*`          | The file upload path configured in media settings |



## Configuration file reference

Every key that the server reads at startup is listed below, grouped by its TOML
section and using the names and types that the code reads. They can also be
provided as environment variables using the mapping described in
[Environment variables](#environment-variables): `app.batch_size` becomes
`LISTMONK_app__batch_size`.

Only the keys below live in the configuration file alone:

| Key | Why it cannot come from the database |
| --- | --- |
| `app.address` | The listen address is read before the HTTP server starts, and it is not a stored setting. |
| `db.*` | The connection parameters are read before the database — and therefore any setting in it — is reachable. |
| `app.admin_username`, `app.admin_password` | Deprecated legacy API user; omit them unless you are migrating from an old installation. |

Every other key in this reference is a database setting: a fresh install seeds it
in the `settings` table from `schema.sql` (upgrades seed it from
`internal/migrations/`), and the server loads that table *after* the config files
and the environment variables. For those keys the value in the admin `Settings`
dashboard wins over the same key in `config.toml`, so change them there; a value
in the config file only takes effect while the key is absent from the database.

The default values listed below are the ones written on a fresh install. Some
keys were seeded with different values by older migrations, for example
`privacy.allow_preferences` and the `bounce.actions` counts, so an upgraded
installation may hold values that differ from the defaults below.

!!! note

    `--new-config` writes the `config.toml.sample` packaged inside the binary,
    which is why that file carries `[app]` and `[db]` only: those are the
    configuration-file keys. The database-backed sections below do not belong in
    `config.toml` — set them in the admin `Settings` dashboard, or override them
    per instance with environment variables such as `LISTMONK_app__batch_size`.

### `[app]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `address` | string | none, the packaged sample uses `localhost:9000` | yes | Listen address of the web server, eg: `0.0.0.0:9000`. The database has no such setting, so it must come from the config file or an environment variable. |
| `root_url` | string | `http://localhost:9000` | no | Public URL of the instance, without a trailing slash. Used to build tracking, unsubscribe and opt-in URLs, the OIDC redirect URL and the `Secure` flag of the session cookie. |
| `site_name` | string | `Mailing customer_list` | no | Instance name shown in the admin UI and on the public pages. |
| `logo_url` | string | `""` | no | Logo image shown on the public pages. |
| `favicon_url` | string | `""` | no | Favicon shown on the public pages. |
| `from_email` | string | `listmonk <noreply@listmonk.yoursite.com>` | no | Default `From` address for campaigns and system e-mails. |
| `notify_emails` | string[] | `[]` | no | Addresses that receive system notifications, such as import and campaign notifications. |
| `lang` | string | `zh-CN` | no | UI language. It must match one of the language files bundled in `i18n/`, eg: `en` or `de`. |
| `enable_public_subscription_page` | bool | `true` | no | Serve the public subscription pages at `/subscription/*`. |
| `enable_public_archive` | bool | `true` | no | Serve the public campaign archive at `/archive`. |
| `enable_public_archive_rss_content` | bool | `true` | no | Include the full campaign body in the archive RSS feed. When `false`, the feed only carries titles and links. |
| `send_optin_confirmation` | bool | `true` | no | Send opt-in confirmation e-mails for double opt-in subscriptions. |
| `check_updates` | bool | `true` | no | Check for new releases once a day and show a notification in the admin UI. Set to `false` on air-gapped installations. |
| `concurrency` | int | `10` | no | Number of campaign workers, that is, messages processed simultaneously. |
| `message_rate` | int | `10` | no | Maximum messages per second, per worker. |
| `batch_size` | int | `1000` | no | Number of customers fetched from the database in a single cycle (~5s) when a campaign is running. Higher values reduce database round trips and use more memory. |
| `max_send_errors` | int | `1000` | no | Number of send errors after which a campaign is paused. |
| `message_sliding_window` | bool | `false` | no | Enable the per-account sliding window send rate. |
| `message_sliding_window_duration` | duration string | `1h` | no | Sliding window length, eg: `30m`, `1h`. Only used when `message_sliding_window` is enabled. |
| `message_sliding_window_rate` | int | `10000` | no | Maximum number of messages allowed per sliding window. Only used when `message_sliding_window` is enabled. |
| `cache_slow_queries` | bool | `false` | no | Cache aggregate statistics in materialized views instead of computing them on every request. See [Slow query caching](maintenance/performance.md). |
| `cache_slow_queries_interval` | cron string | `0 3 * * *` | no | Cron schedule for refreshing the cached statistics. Only used when `cache_slow_queries` is enabled. |
| `admin_username` | string | none | no | Deprecated legacy API user. When set (longer than 2 characters) together with `admin_password` (longer than 6 characters), it is used as an API user and the app logs a warning asking you to remove it. |
| `admin_password` | string | none | no | Deprecated legacy API password for `admin_username`. |

### `[db]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `host` | string | none, the packaged sample uses `localhost` | yes | PostgreSQL host. |
| `port` | int | none, the packaged sample uses `5432` | yes | PostgreSQL port. |
| `user` | string | none, the packaged sample uses `listmonk` | yes | PostgreSQL user. |
| `password` | string | none, the packaged sample uses `listmonk` | yes | PostgreSQL password. |
| `database` | string | none, the packaged sample uses `listmonk` | yes | PostgreSQL database name. It is also shown in the `--install` prompt. |
| `ssl_mode` | string | none, the packaged sample uses `disable` | yes | PostgreSQL SSL mode, eg: `disable`, `require`, `verify-full`. |
| `params` | string | `""` | no | Extra Postgres DSN parameters, eg: `application_name=listmonk gssencmode=disable`. |
| `max_open` | int | `25` | no | Maximum number of open connections in the pool. |
| `max_idle` | int | `25` | no | Maximum number of idle connections in the pool. |
| `max_lifetime` | duration string | `300s` | no | Maximum lifetime of a pooled connection. |

### `[privacy]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `individual_tracking` | bool | `false` | no | Count unique (per-customer) campaign views and link clicks instead of raw totals. |
| `disable_tracking` | bool | `false` | no | Disable campaign view and link tracking entirely. |
| `unsubscribe_header` | bool | `true` | no | Add the `List-Unsubscribe` header to campaign e-mails, including RFC 8058 one-click unsubscribe headers on opt-in confirmation e-mails. |
| `allow_blocklist` | bool | `true` | no | Allow customers to blocklist themselves from the public subscription pages. |
| `allow_export` | bool | `true` | no | Allow customers to export their own data from the public subscription pages. |
| `allow_wipe` | bool | `true` | no | Allow customers to erase their own data from the public subscription pages. |
| `allow_preferences` | bool | `true` | no | Allow customers to manage their subscription preferences (lists, attributes) from the public pages. |
| `exportable` | string[] | `["profile", "subscriptions", "campaign_views", "link_clicks"]` | no | Categories of data included in customer data exports. |
| `record_optin_ip` | bool | `false` | no | Record the IP address of the customer when they confirm an opt-in. |
| `domain_blocklist` | string[] | `[]` | no | Domains rejected during customer import. |
| `domain_allowlist` | string[] | `[]` | no | When not empty, only these domains are accepted during customer import. |

### `[security]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `cors_origins` | string[] | `[]` | no | Origins allowed by the API CORS middleware. When empty, CORS is disabled. Entries are normalized to `scheme://host` by the settings API; `*` is also accepted. |

#### `[security.oidc]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | no | Enable OIDC single sign-on. |
| `provider_url` | string | `""` | yes, when enabled | OIDC issuer or discovery URL of the provider. |
| `provider_name` | string | `""` | no | Label of the OIDC login button on the login form. Falls back to the provider's own name. |
| `client_id` | string | `""` | yes, when enabled | OIDC client ID. |
| `client_secret` | string | `""` | yes, when enabled | OIDC client secret. |
| `auto_create_users` | bool | `false` | no | Create local users on first SSO login instead of rejecting unknown users. |
| `default_user_role_id` | int | `null` | yes, when `auto_create_users` is enabled | Role ID (`Settings` -> `Users` -> `Roles`) assigned to auto-created users. Must be `1` or higher. |
| `default_list_role_id` | int | `null` | no | Role ID assigned to auto-created users in customer lists. |

See [OIDC SSO](oidc.md) for provider specific setup.

#### `[security.captcha]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `altcha.enabled` | bool | `false` | no | Enable the Altcha proof-of-work CAPTCHA on public forms. Takes precedence over hCaptcha when both are enabled. |
| `altcha.complexity` | int | `300000` | no | Maximum number used in the Altcha proof-of-work challenge, that is, its difficulty. |
| `hcaptcha.enabled` | bool | `false` | no | Enable the hCaptcha CAPTCHA on public forms. |
| `hcaptcha.key` | string | `""` | yes, when enabled | hCaptcha site key used to render the widget. |
| `hcaptcha.secret` | string | `""` | yes, when enabled | hCaptcha secret used to verify responses against `https://hcaptcha.com/siteverify`. |

### `[appearance]`

Custom CSS and JS blobs are injected into the admin UI and the public pages. In
the admin UI they are edited as `Settings` -> `Appearance`. Any valid TOML
string works, including multi-line `"""` strings.

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `admin.custom_css` | string | `""` | no | Served as `/admin/custom.css` and included in the admin UI. |
| `admin.custom_js` | string | `""` | no | Served as `/admin/custom.js` and included in the admin UI. |
| `public.custom_css` | string | `""` | no | Served as `/public/custom.css` and included in the public pages. |
| `public.custom_js` | string | `""` | no | Served as `/public/custom.js` and included in the public pages. |

### `[upload]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `provider` | string | `filesystem` | no | Media upload provider: `filesystem` or `s3`. Any other value aborts startup. |
| `extensions` | string[] | `["jpg", "jpeg", "png", "gif", "svg", "*"]` | no | File extensions accepted for uploads. `*` allows all extensions. |
| `filesystem.upload_path` | string | `uploads` | no | Directory on disk where uploaded files are stored. When empty, the process working directory is used. |
| `filesystem.upload_uri` | string | `/uploads` | no | URL path under which uploaded files are served. |
| `s3.url` | string | `https://ap-south-1.s3.amazonaws.com` | no | S3 endpoint. When empty, `https://s3.<region>.amazonaws.com` is used. Set it for S3-compatible services such as MinIO. |
| `s3.public_url` | string | `""` | no | Public URL of the bucket, used instead of signed URLs when set. |
| `s3.aws_access_key_id` | string | `""` | no | AWS access key. When the key and the secret are both empty, the IAM role of the host is used. |
| `s3.aws_secret_access_key` | string | `""` | no | AWS secret key. |
| `s3.aws_default_region` | string | `ap-south-1` | no | AWS region. |
| `s3.bucket` | string | `""` | yes, for the s3 provider | Bucket name. |
| `s3.bucket_path` | string | `/` | no | Path prefix inside the bucket for uploaded files. |
| `s3.bucket_type` | string | `public` | no | `public` uploads objects with a `public-read` ACL, any other value keeps objects private and serves them through expiring signed URLs. |
| `s3.expiry` | duration string | `167h` | no | Validity of signed URLs for private objects. Values below 1 second fall back to `167h`. |

`upload.max_file_size` is seeded by `schema.sql` but is not modelled by the
settings API, so it can be neither read nor written through it.
`upload.s3.bucket_domain` exists in the settings table and the settings API, but
is not read by the server at startup.

### `[bounce]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | no | Start the bounce processor. When `false`, bounce webhooks and mailbox scanning are not started. |
| `webhooks_enabled` | bool | `false` | no | Accept bounces on the `/webhooks/bounce` webhook. |
| `ses_enabled` | bool | `false` | no | Accept AWS SES bounce notifications. |
| `sendgrid_enabled` | bool | `false` | no | Accept Sendgrid bounce notifications. |
| `sendgrid_key` | string | `""` | yes, when `sendgrid_enabled` is enabled | Sendgrid webhook verification key. |
| `postmark.enabled` | bool | `false` | no | Accept Postmark bounce notifications. |
| `postmark.username` | string | `""` | yes, when Postmark is enabled | Postmark webhook username. |
| `postmark.password` | string | `""` | yes, when Postmark is enabled | Postmark webhook password. |
| `forwardemail.enabled` | bool | `false` | no | Accept Forward Email bounce notifications. |
| `forwardemail.key` | string | `""` | yes, when Forward Email is enabled | Forward Email webhook key. |
| `actions` | table | `{"soft": {"count": 2, "action": "none"}, "hard": {"count": 1, "action": "blocklist"}, "complaint": {"count": 1, "action": "blocklist"}}` | no | Number of bounces of each type (`soft`, `hard`, `complaint`) after which an action (`none` or `blocklist`) is applied to the customer. |
| `mailboxes` | array of tables | one disabled POP entry | no | Mailboxes scanned for bounces. See the keys below. |

Keys of each `[[bounce.mailboxes]]` entry:

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | no | Enable scanning of this mailbox. Only the first enabled mailbox is used. |
| `type` | string | `pop` | no | Mailbox protocol. Only `pop` is implemented. |
| `host` | string | none | yes, when enabled | Mailbox server host. |
| `port` | int | none | yes, when enabled | Mailbox server port. |
| `auth_protocol` | string | none | no | Authentication protocol, eg: `userpass`. When set to `none`, no authentication is attempted. |
| `username` | string | none | yes, when enabled | Mailbox login username. |
| `password` | string | none | yes, when enabled | Mailbox login password. |
| `scan_interval` | duration string | `15m` | no | How often the mailbox is scanned. |
| `tls_enabled` | bool | `true` | no | Use TLS for the connection. |
| `tls_skip_verify` | bool | `false` | no | Skip TLS certificate verification. |
| `starttls` | bool | `false` | no | Use STARTTLS instead of implicit TLS. |
| `folder` | string | `""` | no | Mailbox folder to scan. Reserved for IMAP mailboxes. |

### `[smtp]`

An array of platform SMTP servers used for system e-mails (password resets,
notifications, opt-in confirmations). Campaign and transactional mail is sent
through the personal SMTP servers configured per account in the admin UI.

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `true` for the first entry in the sample | no | Enable this SMTP server. Disabled servers are ignored. |
| `is_primary` | bool | `true` for the first entry in the sample | no | Use this server as the sender of system e-mails. At least one enabled server must be marked primary, otherwise startup fails. |
| `name` | string | `""` | no | Name of the server, used in logs and as messenger name. |
| `uuid` | string | `""` | no | UUID of the server. Metadata used by the admin UI. |
| `from_email` | string | `listmonk <noreply@listmonk.yoursite.com>` | no | `From` address used by this server. |
| `daily_limit` | int | `0` | no | Maximum messages sent per day through this server. `0` means unlimited. |
| `host` | string | none | yes, when enabled | SMTP server host. |
| `port` | int | none | yes, when enabled | SMTP server port. |
| `auth_protocol` | string | none | yes, when enabled | Authentication protocol: `plain`, `login`, `cram`, or `none` for servers without authentication. Any other value aborts startup. |
| `username` | string | `""` | no | SMTP user. |
| `password` | string | `""` | no | SMTP password. |
| `hello_hostname` | string | `""` | no | Hostname sent in the SMTP `HELO`/`EHLO` command. |
| `max_conns` | int | `10` | no | Maximum number of concurrent connections in the pool. |
| `idle_timeout` | duration string | `15s` | no | How long an idle connection is kept before it is closed. |
| `wait_timeout` | duration string | `5s` | no | How long to wait for a free connection before failing. |
| `max_msg_retries` | int | `2` | no | Number of silent retries of a message that fails to send. |
| `tls_type` | string | `TLS` | no | TLS mode: `TLS` for implicit TLS, `STARTTLS` for STARTTLS. Any other value, including `none`, disables TLS. |
| `tls_skip_verify` | bool | `false` | no | Skip TLS certificate verification. |
| `email_headers` | table | empty | no | Additional SMTP headers added to e-mails sent through this server. |

The same settings are edited as `Settings` -> `SMTP`.

### `[[messengers]]`

HTTP postback messengers that receive campaign e-mails as JSON POST requests. In
the admin UI they are edited as `Settings` -> `Messengers`.

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | no | Enable this messenger. Disabled messengers are ignored. |
| `name` | string | none | yes | Name of the messenger, used in logs and in the admin UI. |
| `root_url` | string | none | yes | URL the postback payloads are POSTed to. |
| `username` | string | `""` | no | Basic auth username for the postback endpoint. |
| `password` | string | `""` | no | Basic auth password for the postback endpoint. |
| `max_conns` | int | `0` | no | Maximum number of connections to the endpoint. |
| `retries` | int | `0` | no | Number of retries for a failed postback. |
| `timeout` | duration string | `0s` | no | HTTP request timeout. |

The admin UI also stores a `uuid` for each messenger, which the server does not
read.

### `[reply_ai]`

Opt-in, OpenAI-compatible classifier for inbound customer replies. It
classifies replies into an intent and can blocklist explicit unsubscribes and
complaints. When enabled, `base_url`, `api_key` and `model` must be set or
startup fails.

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | `false` | no | Enable reply classification. |
| `base_url` | string | `""` | yes, when enabled | Base URL of the OpenAI-compatible chat completions API. |
| `api_key` | string | `""` | yes, when enabled | API key sent as a bearer token. |
| `model` | string | `""` | yes, when enabled | Model name, eg: `gpt-4o-mini`. |
| `timeout` | duration string | `15s` | no | HTTP request timeout for classification requests. |
| `min_confidence` | float | `0.98` | no | Minimum confidence between `0` and `1` required to act on a classification. Values outside that range fail startup. |

### `[maintenance.db]`

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `vacuum` | bool | `false` | no | Run `VACUUM` on the database on a schedule. See [VACUUM-ing](maintenance/performance.md). |
| `vacuum_cron_interval` | cron string | `0 2 * * *` | no | Cron schedule for the `VACUUM` job. Only used when `vacuum` is enabled. |

### Command line and top level keys

These keys are usually passed as command line flags, but can also be written as
top level keys in the config file or as environment variables (a single
underscore becomes a hyphen for top level keys, eg:
`LISTMONK_static_dir` -> `static-dir`).

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `config` | string[] | `["config.toml"]` | no | Paths of the config files to read. `--config` can be given multiple times and the files are merged in order. Pass `--config=""` to run purely from environment variables. |
| `passive` | bool | `false` | no | Run in passive mode without processing campaigns. |
| `static-dir` | string | `""` | no | Directory to load static files from, instead of the assets embedded in the binary. |
| `i18n-dir` | string | `""` | no | Directory to load translation files from, instead of the translations embedded in the binary. |

The remaining flags (`--install`, `--upgrade`, `--yes`, `--idempotent`,
`--new-config`, `--version`) are one-time commands rather than configuration.
Run `listmonk --help` for their descriptions.

## Media uploads

#### Using filesystem

When configuring `docker` volume mounts for using filesystem media uploads, you can follow either of two approaches. [The second option may be necessary if](https://github.com/knadh/listmonk/issues/1169#issuecomment-1674475945) your setup requires you to use `sudo` for docker commands. 

After making any changes you will need to run `sudo docker compose stop ; sudo docker compose up`. 

And under `https://listmonk.mysite.com/admin/settings` you put `/listmonk/uploads`. 

#### Jenkins/systemd release deployments

For the repository's Jenkins binary deployment, keep the filesystem upload
path as the relative path `uploads`. The deployment keeps
`<DEPLOY_DIR>/uploads` outside the versioned release directories and links
each release's `uploads` path to it, so switching the `current` release does
not hide existing media. Back up this directory together with the database.
If you use an absolute upload path, keep that directory outside the release
tree and manage its persistence and backup separately.

#### Using volumes

Using `docker volumes`, you can specify the name of volume and destination for the files to be uploaded inside the container.


```yml
app:
    volumes:
      - type: volume
        source: listmonk-uploads
        target: /listmonk/uploads

volumes:
  listmonk-uploads:
```

!!! note

    This volume is managed by `docker` itself, and you can see find the host path with `docker volume inspect listmonk_listmonk-uploads`.

#### Using bind mounts

```yml
  app:
    volumes:
      - ./path/on/your/host/:/path/inside/container
```
Eg:
```yml
  app:
    volumes:
      - ./data/uploads:/listmonk/uploads
```
The files will be available inside `/data/uploads` directory on the host machine.

To use the default `uploads` folder:
```yml
  app:
    volumes:
      - ./uploads:/listmonk/uploads
```

## Logs

### Docker

https://docs.docker.com/engine/reference/commandline/logs/
```
sudo docker logs -f
sudo docker logs listmonk_app -t
sudo docker logs listmonk_db -t
sudo docker logs --help
```
Container info: `sudo docker inspect listmonk_listmonk`

Docker logs to `/dev/stdout` and `/dev/stderr`. The logs are collected by the docker daemon and stored in your node's host path (by default). The same can be configured (/etc/docker/daemon.json) in your docker daemon settings to setup other logging drivers, logrotate policy and more, which you can read about [here](https://docs.docker.com/config/containers/logging/configure/).

### Binary

listmonk logs to `stdout`, which is usually not saved to any file. To save listmonk logs to a file use `./listmonk > listmonk.log`.

Settings -> Logs in admin shows the last 1000 lines of the standard log output but gets erased when listmonk is restarted.

For the [service file](https://github.com/knadh/listmonk/blob/master/listmonk%40.service), you can use `ExecStart=/bin/bash -ce "exec /usr/bin/listmonk --config /etc/listmonk/config.toml --static-dir /etc/listmonk/static >>/etc/listmonk/listmonk.log 2>&1"` to create a log file that persists after restarts. [More info](https://github.com/knadh/listmonk/issues/1462#issuecomment-1868501606).


## Time zone

To change listmonk's time zone (logs, etc.) edit `docker-compose.yml`:
```
environment:
    - TZ=Etc/UTC
```
with any Timezone listed [here](https://en.wikipedia.org/wiki/CustomerList_of_tz_database_time_zones). Then run `sudo docker-compose stop ; sudo docker-compose up` after making changes.

## SMTP

### Retries
The `Settings -> SMTP -> Retries` denotes the number of times a message that fails at the moment of sending is retried silently using different connections from the SMTP pool. The messages that fail even after retries are the ones that are logged as errors and ignored.

## SMTP ports
Some server hosts block outgoing SMTP ports (25, 465). You may have to contact your host to unblock them before being able to send e-mails. Eg: [Hetzner](https://docs.hetzner.com/cloud/servers/faq/#why-can-i-not-send-any-mails-from-my-server).


## Performance

### Batch size

The batch size parameter is useful when working with very large customer_lists with millions of customers for maximising throughput. It is the number of customers that are fetched from the database sequentially in a single cycle (~5 seconds) when a campaign is running. Increasing the batch size uses more memory, but reduces the round trip to the database.
