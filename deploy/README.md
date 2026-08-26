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

## Jenkins pipeline

The repository root contains a `Jenkinsfile` for the same build and
distribution flow. Every run:

1. runs the Go test suite;
2. runs `deploy/package_bundle.sh`, which builds the self-contained binary,
   Docker image, and portable deployment tarball;
3. archives the tarball, its SHA-256 checksum, and manifest as Jenkins build
   artifacts.

The pipeline uses the `linux-docker` agent label. The selected agent needs
Linux/x86_64, Go 1.26.1 or later, Node.js 22 or later, Yarn 1 or Corepack,
Docker, Bash, Git, OpenSSH client tools, `tar`, `gzip`, and `sha256sum`.

To enable the optional deployment stage, create these Jenkins credentials:

- `listmonk-ubuntu-ssh`: an **SSH Username with private key** credential for
  the `ubuntu` account on the deployment server.
- `listmonk-ubuntu-known-hosts`: a **Secret file** containing the deployment
  server's verified `known_hosts` entry.

The deployment account must be able to run `sudo -n docker ...` on the server;
the pipeline deliberately does not use a stored server password. On a manual
build, set `DEPLOY_TO_9173` to true to load the bundled local image and update
the app on port 9173. The server's `env/runtime.env`, database volume, and
`uploads` directory are preserved. Before changing `APP_IMAGE`, the pipeline
backs up `env/local.env` and restores it automatically if the health check
fails.
