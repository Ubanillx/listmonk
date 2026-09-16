<a href="https://zerodha.tech"><img src="https://zerodha.tech/static/images/github-badge.svg" align="right" /></a>

[![listmonk-logo](https://user-images.githubusercontent.com/547147/231084896-835dba66-2dfe-497c-ba0f-787564c0819e.png)](https://listmonk.app)

listmonk is a standalone, self-hosted, newsletter and mailing list manager. It is fast, feature-rich, and packed into a single binary. It uses a PostgreSQL database as its data store.

[![listmonk-dashboard](https://github.com/user-attachments/assets/689b5fbb-dd25-4956-a36f-e3226a65f9c4)](https://listmonk.app)

Visit [listmonk.app](https://listmonk.app) for more info. Check out the [**live demo**](https://demo.listmonk.app).

## Installation

### Docker

The latest image is available on DockerHub at [`listmonk/listmonk:latest`](https://hub.docker.com/r/listmonk/listmonk/tags?page=1&ordering=last_updated&name=latest).
Download and use the sample [docker-compose.yml](https://github.com/knadh/listmonk/blob/master/docker-compose.yml).


```shell
# Download the compose file to the current directory.
curl -LO https://github.com/knadh/listmonk/raw/master/docker-compose.yml

# Run the services in the background.
docker compose up -d
```
Visit `http://localhost:39100`

See [installation docs](https://listmonk.app/docs/installation)

__________________

### Binary
- Download the [latest release](https://github.com/knadh/listmonk/releases) and extract the listmonk binary.
- `./listmonk --new-config` to generate config.toml. Edit it.
- `./listmonk --install` to setup the Postgres DB (or `--upgrade` to upgrade an existing DB. Upgrades are idempotent and running them multiple times have no side effects).
- Run `./listmonk` and visit `http://localhost:9000`

See [installation docs](https://listmonk.app/docs/installation)
__________________


## Documentation
- [docs/README.md](docs/README.md): map of all documentation in this repository.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): engineering architecture and operations guide.
- [docs/harness/README.md](docs/harness/README.md): engineering tracking records (TODOs, plans, status, technical debt).
- [listmonk.app/docs](https://listmonk.app/docs): user documentation.

## Local development
Use the Docker Compose suite in `dev/`: `make init-dev-docker` initializes the database and `make dev-docker` starts the stack. The admin UI is at http://localhost:9173 and the Vite frontend dev server is at http://localhost:8181; see `dev/README.md` for details.

Common commands: `make build`, `make test`, `make build-frontend`, `make build-email-builder`, `cd frontend && yarn lint`. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and [frontend/email-builder/README.md](frontend/email-builder/README.md) for the email editor.

## Developers
listmonk is free and open source software licensed under AGPLv3. If you are interested in contributing, refer to the [developer setup](https://listmonk.app/docs/developer-setup). The backend is written in Go and the frontend is Vue with Buefy for UI. 


## License
listmonk is licensed under the AGPL v3 license.
