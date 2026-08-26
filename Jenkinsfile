/*
 * Jenkins prerequisites
 *
 * Agent label: linux-docker
 *   - Linux/x86_64, Go 1.26.1+, Node.js 22+, Yarn 1 (or Corepack), Docker,
 *     Bash, Git, OpenSSH client, tar, gzip, and sha256sum.
 *   - The Jenkins agent user must be allowed to use Docker.
 *
 * Credentials
 *   - listmonk-ubuntu-ssh: "SSH Username with private key" for ubuntu.
 *   - listmonk-ubuntu-known-hosts: Secret file containing the target's
 *     known_hosts entry. Do not disable SSH host-key verification.
 *
 * The deployment user must be able to run `sudo -n docker ...` on the target.
 * This pipeline never stores a server, database, or SMTP password in source.
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
    booleanParam(
      name: 'DEPLOY_TO_9173',
      defaultValue: false,
      description: 'Deploy the packaged local image to the configured Ubuntu server on port 9173.'
    )
  }

  environment {
    APP_IMAGE_NAME = 'listmonk-app'
    DEPLOY_USER = 'ubuntu'
    DEPLOY_HOST = '192.168.1.67'
    DEPLOY_DIR = '/home/ubuntu/listmonk-deploy'
    APP_PORT = '9173'
    DEPLOY_SSH_CREDENTIALS_ID = 'listmonk-ubuntu-ssh'
    DEPLOY_KNOWN_HOSTS_CREDENTIALS_ID = 'listmonk-ubuntu-known-hosts'
  }

  stages {
    stage('Checkout') {
      steps {
        checkout scm
        script {
          env.GIT_SHA = sh(script: 'git rev-parse --short HEAD', returnStdout: true).trim()
          env.RELEASE_ID = "jenkins-${env.BUILD_NUMBER}-${env.GIT_SHA}"
          env.IMAGE_REF = "${env.APP_IMAGE_NAME}:${env.GIT_SHA}"
          env.BUNDLE_NAME = "listmonk-deploy-bundle-${env.GIT_SHA}-${env.BUILD_NUMBER}"
          currentBuild.displayName = "#${env.BUILD_NUMBER} ${env.GIT_SHA}"
        }
      }
    }

    stage('Verify build agent') {
      steps {
        sh '''#!/usr/bin/env bash
set -euo pipefail

for command in go node docker make git bash tar gzip sha256sum ssh scp; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Required command is not available on this Jenkins agent: $command" >&2
    exit 1
  }
done

if ! command -v yarn >/dev/null 2>&1 && ! command -v corepack >/dev/null 2>&1; then
  echo 'Install Yarn 1 or enable Corepack on the Jenkins agent.' >&2
  exit 1
fi

go version
node --version
docker version --format '{{.Server.Version}}'
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

    stage('Package distribution bundle') {
      steps {
        sh '''#!/usr/bin/env bash
set -euo pipefail

# The Makefile accepts YARN as an override. Corepack keeps the build pinned to
# the packageManager version when Yarn is not installed globally on the agent.
if command -v yarn >/dev/null 2>&1; then
  export YARN=yarn
else
  export YARN='corepack yarn'
fi

bash deploy/package_bundle.sh \
  --bundle-name "$BUNDLE_NAME" \
  --output-dir "$WORKSPACE/dist" \
  --local-image-tag "$IMAGE_REF"

bundle_tar="$WORKSPACE/dist/$BUNDLE_NAME.tar.gz"
bundle_manifest="$WORKSPACE/dist/$BUNDLE_NAME/manifest.json"
local_image_tar="$WORKSPACE/dist/$BUNDLE_NAME/images/listmonk-local.tar"

test -s "$bundle_tar"
test -s "$bundle_manifest"
test -s "$local_image_tar"
sha256sum "$bundle_tar" > "$bundle_tar.sha256"
'''
      }
    }

    stage('Archive distribution bundle') {
      steps {
        archiveArtifacts(
          artifacts: 'dist/listmonk-deploy-bundle-*.tar.gz,dist/listmonk-deploy-bundle-*.tar.gz.sha256,dist/listmonk-deploy-bundle-*/manifest.json',
          fingerprint: true
        )
      }
    }

    stage('Deploy to 9173') {
      when {
        expression { params.DEPLOY_TO_9173 }
      }
      steps {
        withCredentials([
          file(credentialsId: env.DEPLOY_KNOWN_HOSTS_CREDENTIALS_ID, variable: 'SSH_KNOWN_HOSTS'),
        ]) {
          sshagent(credentials: [env.DEPLOY_SSH_CREDENTIALS_ID]) {
            sh '''#!/usr/bin/env bash
set -euo pipefail

image_archive="$WORKSPACE/dist/$BUNDLE_NAME/images/listmonk-local.tar"
remote="${DEPLOY_USER}@${DEPLOY_HOST}"
remote_archive="/tmp/${RELEASE_ID}-listmonk-local.tar"

test -s "$image_archive"
test -s "$SSH_KNOWN_HOSTS"

ssh_options=(
  -o BatchMode=yes
  -o ConnectTimeout=15
  -o StrictHostKeyChecking=yes
  -o UserKnownHostsFile="$SSH_KNOWN_HOSTS"
)

scp "${ssh_options[@]}" "$image_archive" "$remote:$remote_archive"

remote_command=$(printf 'DEPLOY_DIR=%q APP_IMAGE=%q APP_PORT=%q IMAGE_ARCHIVE=%q RELEASE_ID=%q bash -s' \
  "$DEPLOY_DIR" "$IMAGE_REF" "$APP_PORT" "$remote_archive" "$RELEASE_ID")

ssh "${ssh_options[@]}" "$remote" "$remote_command" <<'REMOTE_SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

deploy_dir="${DEPLOY_DIR:?}"
app_image="${APP_IMAGE:?}"
app_port="${APP_PORT:?}"
image_archive="${IMAGE_ARCHIVE:?}"
release_id="${RELEASE_ID:?}"
compose_file="$deploy_dir/docker-compose.bundle.yml"
local_env="$deploy_dir/env/local.env"
runtime_env="$deploy_dir/env/runtime.env"
backup=''

rollback() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ] && [ -n "$backup" ] && [ -f "$backup" ]; then
    echo "Deployment failed; restoring $backup" >&2
    cp -p "$backup" "$local_env" || true
    sudo -n docker compose --env-file "$local_env" --env-file "$runtime_env" \
      -f "$compose_file" up -d app || true
  fi
  rm -f "$image_archive" || true
  exit "$status"
}
trap rollback EXIT

command -v docker >/dev/null
command -v curl >/dev/null
sudo -n docker info >/dev/null
test -f "$compose_file"
test -f "$local_env"
test -f "$runtime_env"
test -f "$image_archive"

sudo -n docker load --input "$image_archive"
backup="${local_env}.before-${release_id}"
cp -p "$local_env" "$backup"

temp_env=$(mktemp "$deploy_dir/env/.local.env.XXXXXX")
awk -v image="$app_image" '
  BEGIN { replaced = 0 }
  /^APP_IMAGE=/ { print "APP_IMAGE=" image; replaced = 1; next }
  { print }
  END { if (!replaced) print "APP_IMAGE=" image }
' "$local_env" > "$temp_env"
chmod --reference="$local_env" "$temp_env" 2>/dev/null || true
mv "$temp_env" "$local_env"

sudo -n docker compose --env-file "$local_env" --env-file "$runtime_env" \
  -f "$compose_file" config -q
sudo -n docker compose --env-file "$local_env" --env-file "$runtime_env" \
  -f "$compose_file" up -d app

for attempt in {1..30}; do
  if curl --fail --silent --show-error --location --max-time 5 \
    --output /dev/null "http://127.0.0.1:${app_port}/admin/login"; then
    deployed_image=$(sudo -n docker inspect --format '{{.Config.Image}}' listmonk_app)
    if [ "$deployed_image" = "$app_image" ]; then
      echo "Deployment is healthy: $deployed_image"
      rm -f "$image_archive"
      trap - EXIT
      exit 0
    fi
  fi
  sleep 2
done

echo "Application did not become healthy on port ${app_port}." >&2
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
      echo "Built ${env.IMAGE_REF}; the portable bundle is available in this Jenkins build's artifacts."
    }
    always {
      deleteDir()
    }
  }
}
