# Docker suite for development

**NOTE**: This exists only for local development. If you're interested in using
Docker for a production setup, visit the
[docs](https://listmonk.app/docs/installation/#docker) instead.

### Objective

The purpose of this Docker suite for local development is to isolate all the dev
dependencies in a Docker environment. The containers have a host volume mounted
inside for the entire app directory. This helps us to not do a full
`docker build` for every single local change, only restarting the Docker
environment is enough.

## Setting up a dev suite

To spin up a local suite of:

- PostgreSQL
- Mailhog
- Node.js frontend app
- Golang backend app

### Verify your config file

The config file provided at `dev/config.toml` will be used when running the
containerized development stack. Make sure the values set within are suitable
for the feature you're trying to develop.

### Windows prerequisites

Docker Desktop and Git for Windows must be installed. The Makefile uses POSIX
utilities supplied by Git for Windows. Install GNU Make with PowerShell and
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

### Setup DB

Running this will build the appropriate images and initialize the database.

```bash
make init-dev-docker
```

The backend startup script also runs an idempotent install and pending
upgrades. On PowerShell, the complete equivalent is:

```powershell
docker compose -f dev/docker-compose.yml up --build -d
docker compose -f dev/docker-compose.yml ps
```

### Start frontend and backend apps

Running this start your local development stack.

```bash
make dev-docker
```

Visit `http://localhost:8181` on your browser.

The development suite exposes these local endpoints:

| Service | URL |
| --- | --- |
| Admin UI | `http://localhost:8181` |
| Backend | `http://localhost:9173` |
| Adminer | `http://localhost:8171` |
| MailHog UI | `http://localhost:8265` |
| PostgreSQL | `localhost:5437` |

### Public-pool verification

To exercise the first-level pool, organization segment, masked contact DTO, and
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

To exercise actual account-owned SMTP delivery, run the MailHog verifier after
the same fixture. It requires the fixture manager to have no personal SMTP
servers, temporarily creates a `mailhog`-only server, sends the three unique
pool recipients concurrently, verifies the secondary-list `Reply-To` header,
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

This will tear down all the data, including DB.

```bash
make rm-dev-docker
```

To stop containers while preserving the database volume, run
`docker compose -f dev/docker-compose.yml down` instead.

### See local changes in action

- Backend: Anytime you do a change to the Go app, it needs to be compiled. Just
  run `make dev-docker` again and that should automatically handle it for you.
- Frontend: Anytime you change the frontend code, you don't need to do anything.
  Since `yarn` is watching for all the changes and we have mounted the code
  inside the docker container, `yarn` server automatically restarts.
