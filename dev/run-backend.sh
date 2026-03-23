#!/bin/sh

set -eu

BUILDSTR_VALUE=${BUILDSTR:-dev}
VERSION_VALUE=${VERSION:-dev}
FRONTEND_DIR_VALUE=${FRONTEND_DIR:-frontend/dist}

run_listmonk() {
  CGO_ENABLED=0 go run \
    -ldflags="-s -w -X main.buildString=${BUILDSTR_VALUE} -X main.versionString=${VERSION_VALUE} -X main.frontendDir=${FRONTEND_DIR_VALUE}" \
    cmd/*.go \
    "$@"
}

echo "[dev-backend] running idempotent install"
run_listmonk --install --idempotent --yes --config=dev/config.toml

echo "[dev-backend] applying pending upgrades"
run_listmonk --upgrade --yes --config=dev/config.toml

echo "[dev-backend] starting server"
run_listmonk --config=dev/config.toml
