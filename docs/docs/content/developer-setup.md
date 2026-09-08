# Developer setup
The app has two distinct components, the Go backend and the VueJS frontend. In the dev environment, both are run independently.


### Pre-requisites
- `go`
- `nodejs` (if you are working on the frontend) and `yarn`
- PostgreSQL database. If it is not installed locally, use the repository development suite: run `make init-dev-docker` and then `make dev-docker`.
- Docker Desktop for the containerized development suite.


### First time setup
`git clone https://github.com/knadh/listmonk.git`. The project uses go.mod, so it's best to clone it outside the Go src path.

1. Copy `config.toml.sample` as `config.toml` and add your config.
2. `make dist` to build the listmonk binary. Once the binary is built, run `./listmonk --install` to run the DB setup. For subsequent dev runs, use `make run`.

> [mailhog](https://github.com/mailhog/MailHog) is an excellent standalone mock SMTP server (with a UI) for testing and dev.


### Running the dev environment
You can run your dev environment locally or inside containers.

The local Vite server is available at `http://localhost:8080`; the containerized frontend is available at `http://localhost:8181`.


1. Locally

    - Run `make run` to start the listmonk dev server on `:9000`.
    - Run `make run-frontend` to start the Vue frontend in dev mode using yarn on `:8080`. All `/api/*` calls are proxied to the app running on `:9000`. Refer to the [frontend README](https://github.com/knadh/listmonk/blob/master/frontend/README.md) for an overview on how the frontend is structured.

2. Inside containers (Using Makefile)

    - Run `make init-dev-docker` to setup container for db.
    - Run `make dev-docker` to setup docker container suite.
    - Run `make rm-dev-docker` to clean up docker container suite.

    The Makefile uses POSIX utilities. On Windows, install Git for Windows and
    GNU Make with `winget install --id ezwinports.make --exact --source winget`.
    Restart the shell and ensure `C:\Program Files\Git\usr\bin` comes before
    the GNU Make directory in `PATH`; then verify with `make --version` and
    `make -n dev-docker`. The backend performs an idempotent database install
    and applies pending upgrades on startup.

    PowerShell users can run the equivalent detached startup directly:

    ```powershell
    docker compose -f dev/docker-compose.yml up --build -d
    docker compose -f dev/docker-compose.yml ps
    ```

    The containerized endpoints are `http://localhost:8181` (admin UI),
    `http://localhost:9173` (backend), `http://localhost:8171` (Adminer),
    `http://localhost:8265` (MailHog), and PostgreSQL on `localhost:5437`.

    To stop the suite without deleting its database volume, use
    `docker compose -f dev/docker-compose.yml down`. The `make rm-dev-docker`
    target removes the containers and database volume.

3. Inside containers (Using devcontainer)

    - Open repo in vscode, open command palette, and select "Dev Containers: Rebuild and Reopen in Container".

It will set up db, and start frontend/backend for you.


# Production build
Run `make dist` to build the Go binary, build the Javascript frontend, and embed the static assets producing a single self-contained binary, `listmonk`
