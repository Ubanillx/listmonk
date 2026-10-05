# Development environment

**NOTE**: This exists only for local development. If you're interested in using
Docker for a production setup, visit the
[docs](https://listmonk.app/docs/installation/#docker) instead.

### Objective

The default workflow keeps middleware in Docker and runs the Go backend and Vue
frontend directly on the host. Air rebuilds and restarts Go when backend files
change; Vite provides frontend hot reload. The old all-in-Docker workflow is
still available for isolated CI or regression runs.

## Setting up a dev suite

The default workflow starts:

- PostgreSQL
- Mailhog
- Adminer

Go and Node.js are installed and run on the host.

### Verify your config file

`dev/config.local.toml` is for the host backend and points to the published
PostgreSQL port (`127.0.0.1:5437`). `dev/config.toml` remains the container
config and uses the Compose service name `db`.

### Windows prerequisites

Docker Desktop, Go 1.26+, Node.js 20+, Yarn 1.x, and Git for Windows must be
installed. The Makefile uses POSIX utilities supplied by Git for Windows.
Install GNU Make with PowerShell and
`winget`:

```powershell
winget install --id ezwinports.make --exact --source winget
```

Restart the shell after installation so the `make` command is available. Keep
`C:\Program Files\Git\usr\bin` before the GNU Make directory in `PATH`, so
Make selects Git's `sh` and Unix utilities. Verify the setup before first use:

```powershell
make --version
make -n dev-docker
```

If Make is unavailable, use the PowerShell Compose equivalents below.

### Start middleware, backend, and frontend separately

Use three PowerShell terminals from the repository root, in this order:

```powershell
# Terminal 1: Docker middleware; this command returns when services are ready.
pwsh -File .\dev\start-middleware.ps1

# Terminal 2: database install/upgrade, then Go + Air hot reload.
pwsh -File .\dev\start-backend.ps1

# Terminal 3: Node.js + Vite hot reload.
pwsh -File .\dev\start-frontend.ps1
```

The backend and frontend commands stay in the foreground and show their logs;
press Ctrl+C in either terminal to stop that process. The middleware script
starts only PostgreSQL, MailHog, and Adminer. The backend script initializes or
upgrades the database and runs Air without starting containers. The frontend
script uses `make run-frontend-local` to install/build missing frontend assets
before Vite starts. All three scripts work when invoked from another directory.

The equivalent manual commands are:

```powershell
docker compose -f dev/docker-compose.yml up -d --wait db mailhog adminer
go run ./cmd --install --idempotent --yes --config dev/config.local.toml
go run ./cmd --upgrade --yes --config dev/config.local.toml
go run github.com/air-verse/air@v1.67.4 -c dev/.air.toml
# Run this in a separate terminal:
make run-frontend-local
```

Visit `http://localhost:8181/admin/` for the admin UI. Vite proxies API and the
server-rendered auth flow (login, workspace selection, password recovery, and
OIDC) to the host backend at `http://localhost:9173`.

The local workflow exposes these endpoints:

| Service | URL |
| --- | --- |
| Admin UI (Vite dev server, hot reload) | `http://localhost:8181/admin/` |
| Go backend (Air hot reload, also serves built admin UI) | `http://localhost:9173` |
| Adminer | `http://localhost:8171` |
| MailHog UI | `http://localhost:8265` |
| PostgreSQL | `localhost:5437` |

The SMTP port published by MailHog is `localhost:6125`. Configure a test SMTP
server in Settings with host `127.0.0.1` and port `6125` when a mail delivery
test is needed.

For the legacy all-in-Docker workflow, use `make init-dev-docker` followed by
`make dev-docker`. Those targets start the frontend and backend services in
addition to the middleware and expose the same ports.

### Public-pool verification

The ordinary Cypress regression suite uses a disposable database and backend:

```powershell
node dev/run-cypress.js --spec cypress/e2e/customer-lists.cy.js
```

Run this from the repository root. Omit `--spec` for the full suite. The runner
builds the admin UI, uses the dedicated `listmonk-cypress` Compose project on
`127.0.0.1:9273`, and removes its temporary database afterward. Direct
`yarn cypress run` cannot reset the shared development database.

To exercise the first-level pool, organization allocation, masked contact DTO, and
internal reply mailbox flow against the running stack, load the deterministic
fixture and run the PowerShell checks:

```powershell
Get-Content dev/pools_e2e_seed.sql |
  docker exec -i dev-db-1 psql -v ON_ERROR_STOP=1 -U <db-user> -d <db-name>
powershell -NoProfile -ExecutionPolicy Bypass -File dev/pools_e2e_verify.ps1
```

The fixture uses the `wsqa-pool-*` prefix. It is idempotent and the verifiers
resolve the fixture IDs by name, so a reused development database does not
need fixed sequence values. The verifier restores its test member and removes
its temporary campaign automatically; contact rows are never physically
deleted. It also checks the `campaign_pool_recipients`
snapshot directly through Docker PostgreSQL; set `POOL_QA_DB_CONTAINER`,
`POOL_QA_DB_USER`, and `POOL_QA_DB_NAME` when the development database uses
non-default names.

The verifier signs in both as the fixture manager and as the highest
administrator, so both credentials must work: `POOL_QA_PASSWORD` (default
`possible1.`) for the `wsqa_*` fixture users and `POOL_QA_SUPER_PASSWORD`
(default `possible1.`) for the `root` account. Both sessions are probed before
the assertions, and the run aborts with guidance when either login fails.
`LISTMONK_QA_BASE_URL` overrides the default base URL, and `POOL_QA_POOL_ID` /
`POOL_QA_SEGMENT_ID` override the name-based fixture lookup.

To exercise actual account-owned SMTP delivery, run the MailHog verifier after
the same fixture. It requires the fixture manager to have no personal SMTP
servers, temporarily creates a `mailhog`-only server, sends the three unique
pool recipients concurrently, verifies the pool-allocation `Reply-To` header,
then deletes both the campaign and temporary SMTP server.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File dev/pools_smtp_e2e_verify.ps1
```

For the browser smoke check, run the opt-in spec from `frontend/` after the
fixture is loaded:

```powershell
$env:CYPRESS_BASE_URL = 'http://localhost:8181'
yarn cypress run --spec cypress/e2e/pools.cy.js --env POOL_E2E=true --browser electron
```

The browser matrix verifies that an authorized organization manager sees only
masked customer email while seeing its internal reply mailbox in full, a
highest administrator sees the source email, and an organization without a
pool delivery grant has no pool management entry point and receives `403` for
the first-level list detail endpoint.

### Tear down

This removes the development containers and database volume:

```bash
make rm-dev-docker
```

To stop containers while preserving the database volume, run
`docker compose -f dev/docker-compose.yml down` instead.

### See local changes in action

- **Backend (`cmd/`, `internal/`, `models/`, `queries/`, `schema.sql`,
  `internal/migrations/`)**: `make run-backend-local` runs Air with
  `dev/.air.toml`; saving a watched file rebuilds and restarts Go. SQL and TOML
  changes are watched as well. The install/upgrade step runs when the backend
  script starts; stop and rerun it after adding a migration.
- **Frontend (`frontend/src/`)**: `make run-frontend-local` runs Vite on
  `http://localhost:8181/admin/`; saving a Vue or JavaScript file updates the browser.
- **Email editor (`frontend/email-builder/`)**: run `make build-email-builder`
  when changing the editor package, then reload the Vite page.
- **Built admin UI on `http://localhost:9173`**: run `make build-frontend` if
  you need to inspect the packaged `frontend/dist` assets. The normal local
  workflow uses Vite and does not require this rebuild.

The Go watcher is invoked with `go run github.com/air-verse/air@v1.67.4`, so
`make run-backend-local` is reproducible even when `air` is not on `PATH`.
To install the binary for direct use, run `make install-dev-tools` and ensure
`$(go env GOPATH)/bin` is on `PATH`.
