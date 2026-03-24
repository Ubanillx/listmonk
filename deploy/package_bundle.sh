#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./deploy/package_bundle.sh [options]

Creates a complete deployment bundle that includes:
  - one local-build listmonk image tar
  - one official online listmonk image tar
  - one postgres image tar
  - server-side compose/env/start scripts

Options:
  --bundle-name NAME         Bundle directory/tarball base name
  --output-dir DIR           Output directory, defaults to dist
  --online-image IMAGE       Official image to pull, defaults to listmonk/listmonk:latest
  --postgres-image IMAGE     Postgres image to bundle, defaults to postgres:17-alpine
  --local-image-tag TAG      Tag for the locally built image, defaults to listmonk-app:<gitsha>
  --skip-make-dist           Do not run make dist before docker build
  --skip-online-pull         Do not docker pull the official app image
  --skip-postgres-pull       Do not docker pull the postgres image
  -h, --help                 Show this help

Examples:
  sudo ./deploy/package_bundle.sh
  sudo ./deploy/package_bundle.sh --bundle-name listmonk-prod-20260324
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/dist"
GIT_SHA="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo dev)"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
BUNDLE_NAME="listmonk-deploy-bundle-${TIMESTAMP}"
ONLINE_IMAGE="listmonk/listmonk:latest"
POSTGRES_IMAGE="postgres:17-alpine"
LOCAL_IMAGE_TAG="listmonk-app:${GIT_SHA}"
RUN_MAKE_DIST=1
PULL_ONLINE=1
PULL_POSTGRES=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle-name)
      BUNDLE_NAME="$2"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --online-image)
      ONLINE_IMAGE="$2"
      shift 2
      ;;
    --postgres-image)
      POSTGRES_IMAGE="$2"
      shift 2
      ;;
    --local-image-tag)
      LOCAL_IMAGE_TAG="$2"
      shift 2
      ;;
    --skip-make-dist)
      RUN_MAKE_DIST=0
      shift
      ;;
    --skip-online-pull)
      PULL_ONLINE=0
      shift
      ;;
    --skip-postgres-pull)
      PULL_POSTGRES=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_cmd docker
require_cmd tar

mkdir -p "${OUTPUT_DIR}"
BUNDLE_DIR="${OUTPUT_DIR}/${BUNDLE_NAME}"
IMAGES_DIR="${BUNDLE_DIR}/images"
ENV_DIR="${BUNDLE_DIR}/env"
SCRIPTS_DIR="${BUNDLE_DIR}/scripts"

rm -rf "${BUNDLE_DIR}"
mkdir -p "${IMAGES_DIR}" "${ENV_DIR}" "${SCRIPTS_DIR}" "${BUNDLE_DIR}/uploads"

if [[ ${RUN_MAKE_DIST} -eq 1 ]]; then
  require_cmd make
  echo "Running make dist"
  make -C "${ROOT_DIR}" dist
fi

if [[ ! -f "${ROOT_DIR}/listmonk" ]]; then
  echo "Missing ${ROOT_DIR}/listmonk. Build it first or omit --skip-make-dist." >&2
  exit 1
fi

if [[ ${PULL_ONLINE} -eq 1 ]]; then
  echo "Pulling ${ONLINE_IMAGE}"
  docker pull "${ONLINE_IMAGE}"
fi

if [[ ${PULL_POSTGRES} -eq 1 ]]; then
  echo "Pulling ${POSTGRES_IMAGE}"
  docker pull "${POSTGRES_IMAGE}"
fi

echo "Building local image ${LOCAL_IMAGE_TAG}"
docker build -t "${LOCAL_IMAGE_TAG}" "${ROOT_DIR}"

echo "Saving images"
docker save -o "${IMAGES_DIR}/listmonk-online.tar" "${ONLINE_IMAGE}"
docker save -o "${IMAGES_DIR}/listmonk-local.tar" "${LOCAL_IMAGE_TAG}"
docker save -o "${IMAGES_DIR}/postgres.tar" "${POSTGRES_IMAGE}"

cp "${ROOT_DIR}/deploy/docker-compose.bundle.yml" "${BUNDLE_DIR}/docker-compose.bundle.yml"
cp "${ROOT_DIR}/deploy/env.runtime.example" "${ENV_DIR}/runtime.env.example"
cp "${ROOT_DIR}/deploy/server-load-images.sh" "${SCRIPTS_DIR}/load-images.sh"
cp "${ROOT_DIR}/deploy/server-start-online.sh" "${SCRIPTS_DIR}/start-online.sh"
cp "${ROOT_DIR}/deploy/server-start-local.sh" "${SCRIPTS_DIR}/start-local.sh"
cp "${ROOT_DIR}/deploy/server-stop.sh" "${SCRIPTS_DIR}/stop.sh"

chmod +x "${SCRIPTS_DIR}/load-images.sh" "${SCRIPTS_DIR}/start-online.sh" "${SCRIPTS_DIR}/start-local.sh" "${SCRIPTS_DIR}/stop.sh"

cat > "${ENV_DIR}/online.env" <<EOF
APP_IMAGE=${ONLINE_IMAGE}
POSTGRES_IMAGE=${POSTGRES_IMAGE}
EOF

cat > "${ENV_DIR}/local.env" <<EOF
APP_IMAGE=${LOCAL_IMAGE_TAG}
POSTGRES_IMAGE=${POSTGRES_IMAGE}
EOF

cat > "${BUNDLE_DIR}/manifest.json" <<EOF
{
  "bundle_name": "${BUNDLE_NAME}",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "git_sha": "${GIT_SHA}",
  "online_image": "${ONLINE_IMAGE}",
  "local_image": "${LOCAL_IMAGE_TAG}",
  "postgres_image": "${POSTGRES_IMAGE}"
}
EOF

cat > "${BUNDLE_DIR}/README.md" <<'EOF'
# listmonk deployment bundle

This bundle includes:

- `images/listmonk-online.tar`: official online image tar
- `images/listmonk-local.tar`: locally built image tar
- `images/postgres.tar`: postgres image tar
- `docker-compose.bundle.yml`: server deployment compose file
- `env/runtime.env.example`: runtime variables template
- `env/online.env`: uses the official image
- `env/local.env`: uses the locally built image
- `scripts/load-images.sh`: loads all tar files into Docker
- `scripts/start-online.sh`: starts listmonk with the official image
- `scripts/start-local.sh`: starts listmonk with the locally built image
- `scripts/stop.sh`: stops the stack

## Server deployment

1. Extract the bundle on the server.
2. Run `docker load` for all bundled images:

   ```bash
   sudo ./scripts/load-images.sh
   ```

3. Prepare runtime config:

   ```bash
   cp env/runtime.env.example env/runtime.env
   ```

4. Edit `env/runtime.env` and set at least:
   - `LISTMONK_ADMIN_USER`
   - `LISTMONK_ADMIN_PASSWORD`
   - `POSTGRES_PASSWORD`
   - `APP_HOSTNAME`

5. Start one of the two app image variants:

   Official image:

   ```bash
   sudo ./scripts/start-online.sh
   ```

   Local image:

   ```bash
   sudo ./scripts/start-local.sh
   ```

6. Check logs:

   ```bash
   sudo docker compose --env-file env/runtime.env -f docker-compose.bundle.yml logs -f app
   ```
EOF

TARBALL_PATH="${OUTPUT_DIR}/${BUNDLE_NAME}.tar.gz"
tar -C "${OUTPUT_DIR}" -czf "${TARBALL_PATH}" "${BUNDLE_NAME}"

echo "Bundle directory: ${BUNDLE_DIR}"
echo "Bundle tarball: ${TARBALL_PATH}"
