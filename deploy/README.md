# Deployment bundle tooling

This directory contains tooling for building a portable deployment bundle that includes:

- an official online listmonk image tar
- a locally built listmonk image tar
- a postgres image tar
- a server-side compose file
- runtime env templates
- helper scripts for `docker load` and `docker compose up`

## Build the bundle

Run this from the repository root:

```bash
sudo ./deploy/package_bundle.sh
```

The output is written to `dist/`:

- `dist/<bundle-name>/`
- `dist/<bundle-name>.tar.gz`

## What the bundle is for

The resulting tarball can be copied to a server and deployed in one of two modes:

- `online`: uses the official bundled image tar
- `local`: uses the locally built bundled image tar

Both modes share the same bundled postgres image tar and compose file.

## Binary deployment server initialization

The current Jenkins systemd deployment expects PostgreSQL to run locally on the
target server. On Debian/Ubuntu, install it with:

```bash
apt-get update
apt-get install -y postgresql postgresql-client
systemctl enable --now postgresql
```

Create a PostgreSQL login and database named `listmonk`, then create
`/opt/listmonk/config.toml` with the database connection settings. The Jenkins
deployment does not copy this file, so database credentials remain on the
server. PostgreSQL should remain bound to `127.0.0.1` (and `::1`) unless a
separate remote database is intentionally used.

## Jenkins pipeline

The repository root contains a `Jenkinsfile` for the binary deployment flow.
Every run fetches the selected branch, runs the Go test suite, executes
`make dist`, and archives:

- the Go binary;
- a `frontend/dist` tarball; and
- a SHA-256 checksum file.

The pipeline uses the `linux-docker` agent label. The selected agent needs
Linux/x86_64, Go 1.26.1 or later, Node.js 22 or later, Yarn 1 or Corepack,
Bash, Git, OpenSSH client tools, `tar`, `gzip`, `sha256sum`, and `curl`.

Set `DEPLOY_TO_SERVER` to true to upload the two release artifacts over SSH.
The remote stage verifies the checksum, installs the files under
`<DEPLOY_DIR>/releases/<release-id>`, atomically updates `<DEPLOY_DIR>/current`,
creates/updates a systemd unit, runs database install/upgrade, and waits for
`/admin/login` to respond. A failed health check restores the previous
`current` release.

Create these Jenkins credentials before enabling deployment:

- `listmonk-root-ssh`: an **SSH Username with private key** credential for
  the `root` account on the deployment server.
- `listmonk-root-known-hosts`: a **Secret file** containing the deployment
  server's verified `known_hosts` entry.

The pipeline deliberately does not use a stored server password. When using
`root`, no sudo setup is required. If you later switch to a dedicated deploy
account, it needs passwordless sudo for `install`, `rm`, `tar`, `chown`,
`chmod`, `ln`, `mv`, `tee`, `systemctl`, and `journalctl` (or an equivalent
narrowly scoped sudoers rule). Prepare the
remote `<DEPLOY_DIR>/config.toml` first; it contains the database settings and
is never copied from Jenkins. The default health-check port is `9173` and can
be changed with the `APP_PORT` parameter. The current Jenkinsfile defaults to
`83.229.120.50` and `root`; using a dedicated non-root deploy account is
recommended for production.
