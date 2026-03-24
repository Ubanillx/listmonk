#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${BUNDLE_ROOT}/docker-compose.bundle.yml"
LOCAL_ENV="${BUNDLE_ROOT}/env/local.env"
RUNTIME_ENV="${BUNDLE_ROOT}/env/runtime.env"

if [[ ! -f "${LOCAL_ENV}" ]]; then
  echo "Missing ${LOCAL_ENV}" >&2
  exit 1
fi

if [[ ! -f "${RUNTIME_ENV}" ]]; then
  echo "Missing ${RUNTIME_ENV}. Copy env.runtime.example to env/runtime.env and edit it first." >&2
  exit 1
fi

docker compose --env-file "${LOCAL_ENV}" --env-file "${RUNTIME_ENV}" -f "${COMPOSE_FILE}" up -d
