/*
 * listmonk CI/CD pipeline
 *
 * The Jenkins agent builds the Go binary and frontend assets locally, then
 * deploys the release over SSH unless SKIP_DEPLOY is selected. The remote
 * systemd service is activated after a successful upload.
 *
 * Agent label: linux-docker
 * Required tools: Go 1.26.1+, Node.js 22+, Yarn 1.x (or Corepack), Make,
 * Git, Bash, OpenSSH client, tar, gzip, sha256sum and curl.
 *
 * Credentials:
 *   listmonk-root-ssh: SSH Username with private key for root
 *   listmonk-root-known-hosts: Secret file containing the verified
 *                              known_hosts entry for the deployment host
 */
pipeline {
  agent {
    label 'linux-docker'
  }

  options {
    disableConcurrentBuilds()
    buildDiscarder(logRotator(numToKeepStr: '20', artifactNumToKeepStr: '10'))
    timeout(time: 60, unit: 'MINUTES')
    skipDefaultCheckout(true)
  }

  parameters {
    string(
      name: 'SOURCE_BRANCH',
      defaultValue: 'master',
      description: 'Branch to fetch and build from origin.'
    )
    booleanParam(
      name: 'SKIP_DEPLOY',
      defaultValue: false,
      description: 'Skip deployment for an artifact-only build. Normal CI/CD builds deploy automatically.'
    )
    string(
      name: 'DEPLOY_HOST',
      defaultValue: '83.229.120.50',
      description: 'Deployment server hostname or IP address.'
    )
    string(
      name: 'DEPLOY_USER',
      defaultValue: 'root',
      description: 'SSH and systemd service user on the deployment server.'
    )
    string(
      name: 'DEPLOY_DIR',
      defaultValue: '/opt/listmonk',
      description: 'Directory containing releases, current symlink and config.toml.'
    )
    string(
      name: 'SERVICE_NAME',
      defaultValue: 'listmonk',
      description: 'systemd unit name without the .service suffix.'
    )
    string(
      name: 'APP_PORT',
      defaultValue: '9173',
      description: 'HTTP port used for the post-deploy health check.'
    )
  }

  environment {
    DEPLOY_SSH_CREDENTIALS_ID = 'listmonk-root-ssh'
    DEPLOY_KNOWN_HOSTS_CREDENTIALS_ID = 'listmonk-root-known-hosts'
  }

  stages {
    stage('Fetch latest source') {
      steps {
        // The job SCM supplies the repository URL and Jenkinsfile. Fetch the
        // selected branch explicitly so the build always uses its current tip.
        checkout scm
        script {
          // Explicitly export the parameter before the shell step. This also
          // handles the first run after adding the parameter to an existing job.
          env.SOURCE_BRANCH = (params.SOURCE_BRANCH ?: 'master').trim()
        }
        sh '''#!/usr/bin/env bash
set -euo pipefail

source_branch="${SOURCE_BRANCH:-master}"
git check-ref-format --branch "$source_branch" >/dev/null
git fetch --prune --tags origin \
  "+refs/heads/${source_branch}:refs/remotes/origin/${source_branch}"
git checkout -B "$source_branch" "origin/$source_branch"
git reset --hard "origin/$source_branch"
git clean -fdx
'''
        script {
          env.GIT_SHA = sh(script: 'git rev-parse --short HEAD', returnStdout: true).trim()
          env.RELEASE_ID = "jenkins-${env.BUILD_NUMBER}-${env.GIT_SHA}"
          env.BINARY_NAME = "listmonk-${env.RELEASE_ID}"
          env.FRONTEND_ARCHIVE_NAME = "frontend-dist-${env.RELEASE_ID}.tar.gz"
          currentBuild.displayName = "#${env.BUILD_NUMBER} ${env.GIT_SHA}"
        }
      }
    }

    stage('Verify build agent') {
      steps {
        sh '''#!/usr/bin/env bash
set -euo pipefail

for command in go node make git bash tar gzip sha256sum ssh scp curl sort head awk sed; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Required command is not available on this Jenkins agent: $command" >&2
    exit 1
  }
done

require_version() {
  local tool="$1"
  local actual="$2"
  local minimum="$3"
  if [ "$(printf '%s\\n' "$minimum" "$actual" | sort -V | head -n1)" != "$minimum" ]; then
    echo "$tool $actual is too old; require at least $minimum" >&2
    exit 1
  fi
}

if ! command -v yarn >/dev/null 2>&1 && ! command -v corepack >/dev/null 2>&1; then
  echo 'Install Yarn 1 or enable Corepack on the Jenkins agent.' >&2
  exit 1
fi

go_version=$(go version | awk '{sub(/^go/, "", $3); print $3}')
node_version=$(node --version | sed 's/^v//')
require_version go "$go_version" '1.26.1'
require_version node "$node_version" '22.0.0'

go version
node --version
if command -v yarn >/dev/null 2>&1; then
  yarn --version
else
  corepack yarn --version
fi
'''
      }
    }

    stage('Test') {
      steps {
        sh '''#!/usr/bin/env bash
set -euo pipefail
make test
'''
      }
    }

    stage('Build release') {
      steps {
        sh '''#!/usr/bin/env bash
set -euo pipefail

rm -rf dist
mkdir -p dist

# Makefile's YARN variable is overridable. Use the pinned Yarn 1 installation
# when present, otherwise let Corepack select the packageManager version.
if command -v yarn >/dev/null 2>&1; then
  export YARN=yarn
else
  export YARN='corepack yarn'
fi

make dist
test -s listmonk
test -f frontend/dist/index.html

cp -p listmonk "dist/$BINARY_NAME"
tar -C frontend/dist -czf "dist/$FRONTEND_ARCHIVE_NAME" .
sha256sum \
  "dist/$BINARY_NAME" \
  "dist/$FRONTEND_ARCHIVE_NAME" \
  | sed 's#  dist/#  #' > "dist/$RELEASE_ID.sha256"

test -s "dist/$BINARY_NAME"
test -s "dist/$FRONTEND_ARCHIVE_NAME"
test -s "dist/$RELEASE_ID.sha256"
'''
      }
    }

    stage('Archive release') {
      steps {
        archiveArtifacts(
          artifacts: 'dist/listmonk-jenkins-*,dist/frontend-dist-jenkins-*.tar.gz,dist/jenkins-*.sha256',
          fingerprint: true
        )
      }
    }

    stage('Deploy release') {
      when {
        expression { !params.SKIP_DEPLOY }
      }
      steps {
        withCredentials([
          file(credentialsId: env.DEPLOY_KNOWN_HOSTS_CREDENTIALS_ID, variable: 'SSH_KNOWN_HOSTS')
        ]) {
          sshagent(credentials: [env.DEPLOY_SSH_CREDENTIALS_ID]) {
            sh '''#!/usr/bin/env bash
set -euo pipefail

binary_archive="$WORKSPACE/dist/$BINARY_NAME"
frontend_archive="$WORKSPACE/dist/$FRONTEND_ARCHIVE_NAME"
checksum_file="$WORKSPACE/dist/$RELEASE_ID.sha256"
remote="${DEPLOY_USER}@${DEPLOY_HOST}"
remote_stage="/tmp/${RELEASE_ID}"

test -s "$binary_archive"
test -s "$frontend_archive"
test -s "$checksum_file"
test -s "$SSH_KNOWN_HOSTS"

ssh_options=(
  -o BatchMode=yes
  -o ConnectTimeout=15
  -o StrictHostKeyChecking=yes
  -o UserKnownHostsFile="$SSH_KNOWN_HOSTS"
)

ssh "${ssh_options[@]}" "$remote" \
  "rm -rf '$remote_stage' && mkdir -m 700 -p '$remote_stage'"
scp "${ssh_options[@]}" \
  "$binary_archive" "$frontend_archive" "$checksum_file" \
  "$remote:$remote_stage/"

remote_command=$(printf 'DEPLOY_DIR=%q SERVICE_NAME=%q APP_PORT=%q DEPLOY_USER=%q RELEASE_ID=%q REMOTE_STAGE=%q BINARY_NAME=%q FRONTEND_ARCHIVE_NAME=%q bash -s' \
  "$DEPLOY_DIR" "$SERVICE_NAME" "$APP_PORT" "$DEPLOY_USER" "$RELEASE_ID" "$remote_stage" "$BINARY_NAME" "$FRONTEND_ARCHIVE_NAME")

ssh "${ssh_options[@]}" "$remote" "$remote_command" <<'REMOTE_SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

deploy_dir="${DEPLOY_DIR:?}"
service_name="${SERVICE_NAME:?}"
app_port="${APP_PORT:?}"
deploy_user="${DEPLOY_USER:?}"
release_id="${RELEASE_ID:?}"
stage_dir="${REMOTE_STAGE:?}"
binary_name="${BINARY_NAME:?}"
frontend_archive_name="${FRONTEND_ARCHIVE_NAME:?}"
release_dir="$deploy_dir/releases/$release_id"
current_link="$deploy_dir/current"
service_file="/etc/systemd/system/${service_name}.service"
config_file="$deploy_dir/config.toml"
old_target=''
new_link="${current_link}.new-${release_id}"

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo -n "$@"
  fi
}

case "$service_name" in
  (*[!A-Za-z0-9_.@-]*) echo "Invalid SERVICE_NAME" >&2; exit 1 ;;
esac
if [[ ! "$app_port" =~ ^[0-9]+$ ]] || (( app_port < 1 || app_port > 65535 )); then
  echo "Invalid APP_PORT: $app_port" >&2
  exit 1
fi

rollback() {
  local status=$?
  trap - EXIT
  if [ "$status" -ne 0 ]; then
    echo "Deployment failed; attempting rollback." >&2
    if [ -n "$old_target" ]; then
      restore_link="${current_link}.rollback-${release_id}"
      as_root ln -s "$old_target" "$restore_link" || true
      as_root mv -Tf "$restore_link" "$current_link" || true
      as_root systemctl restart "$service_name" || true
    else
      as_root systemctl stop "$service_name" || true
      as_root rm -f "$current_link" || true
    fi
  fi
  as_root rm -f "$new_link" || true
  as_root rm -rf "$release_dir" "$stage_dir" || true
  exit "$status"
}
trap rollback EXIT

command -v sha256sum >/dev/null
command -v tar >/dev/null
command -v curl >/dev/null
command -v systemctl >/dev/null
test -d "$stage_dir"
test -s "$stage_dir/$binary_name"
test -s "$stage_dir/$frontend_archive_name"
(
  cd "$stage_dir"
  sha256sum -c "$release_id.sha256"
)

if [ ! -r "$config_file" ]; then
  echo "Missing $config_file; create and configure it before deploying." >&2
  exit 1
fi

if [ -L "$current_link" ]; then
  old_target=$(readlink "$current_link")
fi

deploy_group=$(id -gn "$deploy_user")
as_root install -d -o "$deploy_user" -g "$deploy_group" -m 0755 \
  "$deploy_dir" "$deploy_dir/releases"
as_root rm -rf "$release_dir"
as_root install -d -o "$deploy_user" -g "$deploy_group" -m 0755 \
  "$release_dir" "$release_dir/frontend"
as_root install -o "$deploy_user" -g "$deploy_group" -m 0755 \
  "$stage_dir/$binary_name" "$release_dir/listmonk"
as_root tar -xzf "$stage_dir/$frontend_archive_name" -C "$release_dir/frontend"
as_root chown -R "$deploy_user:$deploy_group" "$release_dir"

as_root rm -f "$new_link"
as_root ln -s "$release_dir" "$new_link"
as_root mv -Tf "$new_link" "$current_link"

as_root tee "$release_dir/run-listmonk.sh" >/dev/null <<RUN_SCRIPT
#!/usr/bin/env bash
set -euo pipefail

"$deploy_dir/current/listmonk" --install --idempotent --yes --config "$config_file"
"$deploy_dir/current/listmonk" --upgrade --yes --config "$config_file"
exec "$deploy_dir/current/listmonk" --config "$config_file" --static-dir "$deploy_dir/current/frontend"
RUN_SCRIPT
as_root chmod 0755 "$release_dir/run-listmonk.sh"
as_root chown "$deploy_user:$deploy_group" "$release_dir/run-listmonk.sh"

as_root tee "$service_file" >/dev/null <<UNIT
[Unit]
Description=listmonk newsletter manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$deploy_user
WorkingDirectory=$deploy_dir/current
ExecStart=$deploy_dir/current/run-listmonk.sh
Restart=on-failure
RestartSec=5
Environment=LISTMONK_app__address=0.0.0.0:$app_port

[Install]
WantedBy=multi-user.target
UNIT

as_root systemctl daemon-reload
as_root systemctl enable "$service_name"
as_root systemctl restart "$service_name"

for attempt in {1..30}; do
  if curl --fail --silent --show-error --location --max-time 5 \
    --output /dev/null "http://127.0.0.1:${app_port}/admin/login"; then
    if [ "$(as_root systemctl is-active "$service_name")" = 'active' ]; then
      echo "Deployment is healthy: $release_id"
      trap - EXIT
      as_root rm -rf "$stage_dir"
      exit 0
    fi
  fi
  sleep 2
done

echo "${service_name} did not become healthy on port ${app_port}." >&2
as_root journalctl -u "$service_name" --no-pager -n 80 || true
exit 1
REMOTE_SCRIPT
'''
          }
        }
      }
    }
  }

  post {
    success {
      echo "Built ${env.RELEASE_ID}; release artifacts are archived in this Jenkins build."
    }
    always {
      deleteDir()
    }
  }
}
