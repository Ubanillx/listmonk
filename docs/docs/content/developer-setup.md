# Developer setup
The app has two distinct components, the Go backend and the VueJS frontend. In the dev environment, both are run independently.


### Pre-requisites
- Go 1.26+
- Node.js 20+ and Yarn 1.x
- GNU Make
- Docker Desktop (for PostgreSQL, MailHog, and Adminer)


### First time setup
`git clone https://github.com/knadh/listmonk.git`. The project uses go.mod, so it's best to clone it outside the Go src path.

1. Copy `config.toml.sample` as `config.toml` (or run `./listmonk --new-config`) for a normal installation. For local development use the checked-in `dev/config.local.toml`, which connects to the published Docker PostgreSQL port at `127.0.0.1:5437`.
2. On Windows, start Docker middleware with `pwsh -File .\dev\start-middleware.ps1`. The backend script below runs the idempotent database install and upgrade with the host Go toolchain.
3. The Go live-reload runner is invoked as `go run github.com/air-verse/air@v1.67.4`. To install the binary instead, run `make install-dev-tools` and add `$(go env GOPATH)/bin` to `PATH`.

> [mailhog](https://github.com/mailhog/MailHog) is an excellent standalone mock SMTP server (with a UI) for testing and dev.


### Running the dev environment

The recommended workflow runs middleware in Docker and both application
processes on the host. Open three PowerShell terminals in this order:

```powershell
# Terminal 1: start PostgreSQL, MailHog, and Adminer; command returns when ready.
pwsh -File .\dev\start-middleware.ps1

# Terminal 2: install/upgrade the database, then run Go with Air hot reload.
pwsh -File .\dev\start-backend.ps1

# Terminal 3: run the Vue admin UI with Vite hot reload.
pwsh -File .\dev\start-frontend.ps1
```

The backend and frontend terminals remain active; press Ctrl+C to stop either
process. From Bash, the corresponding commands are `make dev-middleware`,
`make run-backend-local`, and `make run-frontend-local`. The Make backend target
also starts middleware and therefore is convenient when separation is not needed.

The admin UI is available at `http://localhost:8181/admin/`. Vite proxies API,
authentication, and workspace-selection requests to the Go server at
`http://localhost:9173`. Adminer is
at `http://localhost:8171`, MailHog is at `http://localhost:8265`, and
PostgreSQL is at `localhost:5437` (MailHog SMTP is `localhost:6125`).

The admin **Customers** menu orders public pool customers, public pool lists,
private customers, private customer lists, import, and bounces as second-level
entries; Forms follows them. `/admin/pool-lists` contains pool creation and
allocation management, while `/admin/customer-lists` contains ordinary lists.
The public pool customer entry opens `/admin/pool`, which shows customers
across all accessible pools.

On Windows, install Git for Windows and GNU Make with
`winget install --id ezwinports.make --exact --source winget`; keep
`C:\Program Files\Git\usr\bin` before the GNU Make directory in `PATH`.

`dev/.air.toml` watches Go, SQL, and TOML files. A backend restart runs the
idempotent install and pending upgrades. Changes under
`frontend/email-builder/` require `make build-email-builder` because that
editor is consumed as a built bundle. `make build-frontend` is only needed when
inspecting the packaged admin UI served directly by Go at `:9173`.

To stop middleware while preserving its volume, run
`docker compose -f dev/docker-compose.yml down`. `make rm-dev-docker` also
removes the database volume.

The previous all-in-Docker workflow remains available for isolated regression
runs:

```shell
make init-dev-docker
make dev-docker
```

### Devcontainer

Open the repository in VS Code, then choose **Dev Containers: Rebuild and
Reopen in Container**. The devcontainer uses the Compose file directly and
starts the all-in-Docker services for that isolated workflow.

It forwards `9173` for Go and `8181` for Vite.


### Keeping the running dev suite in sync

The host-based workflow watches source files: Air restarts Go after changes to
Go, SQL, or TOML files, and Vite reloads Vue/JavaScript modules. A backend
script startup runs the idempotent install and pending upgrades; stop and rerun
the backend script after adding a migration. Changes under
`frontend/email-builder/` still require `make build-email-builder` because the
editor is consumed as a built bundle. `make build-frontend` is only needed when
inspecting the packaged admin UI served directly by Go at `:9173`.


### Tests, lint, and the email editor

- `make test` runs the Go test suite (`go test ./...`).
- `cd frontend && yarn lint` runs ESLint for the Vue admin UI.
- From the repository root, `node dev/run-cypress.js --spec cypress/e2e/customer-lists.cy.js`
  runs the customer-list browser regression in a disposable Docker Compose
  stack. Omit `--spec` for the whole suite. The runner builds the frontend,
  starts the test backend at `127.0.0.1:9273`, and removes its temporary database
  afterward. Direct `yarn cypress run` cannot reset the shared development DB.
- `make build-email-builder` builds `frontend/email-builder/`, the React +
  TypeScript visual email editor, and copies its bundle into the admin UI.


### Where the engineering documentation lives

- [Engineering architecture](architecture.md) covers the repository layout, the
  permission algorithm, and the build, test, and deployment commands.
- [Engineering harness](harness.md) is the ledger of TODOs, plans, status,
  technical debt, and business-logic invariants.
- `docs/README.md` maps every documentation source in the repository, including
  how to preview this documentation site locally.


# Production build
Run `make dist` to build the Go binary, build the Javascript frontend, and embed the static assets producing a single self-contained binary, `listmonk`
