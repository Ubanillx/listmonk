#!/bin/sh

set -eu

BUILDSTR_VALUE=${BUILDSTR:-dev}
VERSION_VALUE=${VERSION:-dev}
FRONTEND_DIR_VALUE=${FRONTEND_DIR:-frontend/dist}

ensure_frontend_dist() {
  if [ -f "${FRONTEND_DIR_VALUE}/index.html" ]; then
    return
  fi

  echo "[dev-backend] frontend assets missing at ${FRONTEND_DIR_VALUE}; building frontend dist"
  make build-frontend
}

run_listmonk() {
  CGO_ENABLED=0 go run \
    -ldflags="-s -w -X main.buildString=${BUILDSTR_VALUE} -X main.versionString=${VERSION_VALUE} -X main.frontendDir=${FRONTEND_DIR_VALUE}" \
    cmd/*.go \
    "$@"
}

ensure_frontend_dist

echo "[dev-backend] running idempotent install"
run_listmonk --install --idempotent --yes --config=dev/config.toml

echo "[dev-backend] applying pending upgrades"
run_listmonk --upgrade --yes --config=dev/config.toml

echo "[dev-backend] starting server"
run_listmonk --config=dev/config.toml
