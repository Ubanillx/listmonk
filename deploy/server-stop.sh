#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-local}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${BUNDLE_ROOT}/docker-compose.bundle.yml"
RUNTIME_ENV="${BUNDLE_ROOT}/env/runtime.env"
MODE_ENV="${BUNDLE_ROOT}/env/${MODE}.env"

if [[ ! -f "${RUNTIME_ENV}" ]]; then
  echo "Missing ${RUNTIME_ENV}. Copy env.runtime.example to env/runtime.env and edit it first." >&2
  exit 1
fi

if [[ ! -f "${MODE_ENV}" ]]; then
  echo "Missing ${MODE_ENV}. Use 'local' or 'online' as the first argument." >&2
  exit 1
fi

docker compose --env-file "${MODE_ENV}" --env-file "${RUNTIME_ENV}" -f "${COMPOSE_FILE}" down
