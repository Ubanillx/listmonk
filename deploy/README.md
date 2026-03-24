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
