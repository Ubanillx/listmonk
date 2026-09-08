# Repository Guidelines

## Project Structure & Module Organization

`cmd/` contains Go entry-point and API logic. Models live in `models/`; shared services are under `internal/`. Schema and queries are in `schema.sql` and `queries/`. Static assets and locales are in `static/` and `i18n/`.

The Vue 2 admin UI is in `frontend/`: views, components, API access, and browser tests are in `src/views/`, `src/components/`, `src/api/`, and `cypress/e2e/`. The React/TypeScript email editor is in `frontend/email-builder/`. Do not edit generated `frontend/dist/` assets.

## Local Docker Sync (Required After Code Changes)

The local dev suite (`dev/docker-compose.yml`, services `backend` / `front` / `db`, launched via `make dev-docker`) binds the repository into the containers. After **every** code change, the running containers must be brought back in sync before the change is considered done:

- **Backend changes (Go, `queries/*.sql`, `schema.sql`, `internal/migrations/`, i18n, config):** restart the backend container so `dev/run-backend.sh` recompiles the working tree with `go run ./cmd` and re-runs `--install` / `--upgrade --yes`:
  `docker restart dev-backend-1` (or `docker compose -f dev/docker-compose.yml restart backend`).
- **Schema / migration changes:** the restart above applies pending migrations automatically. Verify they ran: check `docker logs --tail 30 dev-backend-1` for `running migration <version>` / `upgrade complete` (or `no upgrades to run`), and confirm columns/rows with `docker exec dev-db-1 psql -U <user> -d <db> -c "<check query>"` using the credentials in `dev/config.toml`.
- **Frontend source changes:** by default, rebuild the production admin assets with `make build-frontend` (or `cd frontend && yarn build`) and restart the backend container so `frontend/dist` is served as the latest version at http://localhost:9173. The Vite dev server (`front` container, http://localhost:8181) may hot-reload for preview, but it does not satisfy this sync requirement.
- **Always verify:** the backend is listening (`curl -s -o /dev/null -w "%{http_code}" http://localhost:9173` returns `200`) and `docker logs dev-backend-1` shows no errors after the restart.

Do not leave the local dev suite running stale code after finishing a change.

## Build, Test, and Development Commands

- `make build` builds the Go binary as `./listmonk`.
- `make test` runs the complete Go test suite (`go test ./...`).
- `make build-frontend` installs/builds the admin UI and email editor into `frontend/dist/`.
- `make run` runs the backend against `frontend/dist`; use `make run-frontend` for the Vite UI server on port 8080.
- `make init-dev-docker` initializes the local Docker database; `make dev-docker` starts PostgreSQL, Mailhog, backend, and frontend.
- From `frontend/`, run `yarn lint` before UI changes; `yarn cypress run` executes Cypress end-to-end tests against a running development stack.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use exported `PascalCase` and package-local `camelCase`. Vue components use `PascalCase.vue`; views and Cypress specs follow `Campaigns.vue` and `campaigns.cy.js`. Follow ESLint Airbnb/Vue rules and its 200-character maximum. UI responses are camel-cased; snake-case outgoing fields where required.

## Testing Guidelines

Place Go tests beside implementation in `*_test.go` files and name them `TestBehavior`. Add focused tests for changed permissions, models, or handlers, plus Cypress coverage for observable workflows. No percentage is enforced; run affected tests and `make test` before a PR.

## Commit & Pull Request Guidelines

Use concise imperative Conventional Commit-style subjects, such as `feat: add subscriber import` or `fix: preserve disabled settings`. Propose substantive work in an issue first, keep PRs narrow, and explain behavior, validation, and linked issues. Include screenshots for UI changes.

## Configuration & Security

Start from `config.toml.sample`; never commit credentials, production configuration, or user uploads. Treat schema, migrations, and permission changes as compatibility-sensitive and test upgrade paths.

## Architecture Documentation (Required)

Read [the engineering architecture guide](docs/ARCHITECTURE.md) before changing cross-cutting code. Update it and affected user/deployment docs when ownership, architecture, permissions, APIs, commands, ports, CI, or deployment scripts change. Do not leave stale documentation.

Use [the engineering harness](docs/harness/README.md) for TODOs, plans, status, technical debt, architecture pointers, and business-logic invariants. Keep its status and source references synchronized with code changes.
