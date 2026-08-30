#!/bin/sh

set -eu

# The source tree is bind-mounted from the host. Keep native Node modules in
# Docker volumes so Vite and Rollup use Linux binaries inside this container.
install_deps() {
  dir="$1"
  marker="$dir/node_modules/.listmonk-dev-deps-ready"

  if [ ! -f "$marker" ] || [ "$dir/package.json" -nt "$marker" ] || [ "$dir/yarn.lock" -nt "$marker" ]; then
    echo "[dev-frontend] installing Linux dependencies in $dir"
    (
      cd "$dir"
      CYPRESS_INSTALL_BINARY=0 yarn install --frozen-lockfile
    )
    touch "$marker"
  fi
}

install_deps frontend

# The email builder is consumed from frontend/public/static/email-builder. Its
# already-built bundle is sufficient for the Vite application; building it is
# intentionally kept out of the frontend dev-server startup path.
export VUE_APP_VERSION="${VUE_APP_VERSION:-dev}"
cd frontend
exec yarn dev --host 0.0.0.0 --port 8080
