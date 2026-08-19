#!/usr/bin/env bash
set -euo pipefail
trap 'echo "ERROR: Deploy script failed on line $LINENO"; exit 1' ERR

# ============================================================
#  Medha API — CD Deployment Script
#  Runs ON the server, called by GitHub Actions via SSH.
#
#  Usage:
#    GHCR_TOKEN=xxx GHCR_USER=xxx MC_CUSTOMER_ID=xxx \
#    MC_KEY_BASE64=xxx MC_EMAIL=xxx \
#    ./cd-deploy.sh --image <ref> --service <name> --env-file <name>
#
#  Optional flags:
#    --deploy-path   (default: /opt/medha/docker)
#    --compose-file  (default: docker-compose.apps.yml)
#    --skip-migrate  Skip database migration step
# ============================================================

# ── Parse arguments ──────────────────────────────────────────
DEPLOY_PATH="/opt/medha/docker"
COMPOSE_FILE="docker-compose.apps.yml"
SKIP_MIGRATE=false
SKIP_PULL=false
IMAGE=""
SERVICE=""
ENV_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)        IMAGE="$2";        shift 2 ;;
    --service)      SERVICE="$2";      shift 2 ;;
    --env-file)     ENV_FILE="$2";     shift 2 ;;
    --deploy-path)  DEPLOY_PATH="$2";  shift 2 ;;
    --compose-file) COMPOSE_FILE="$2"; shift 2 ;;
    --skip-migrate) SKIP_MIGRATE=true; shift ;;
    --skip-pull)    SKIP_PULL=true;    shift ;;
    *) echo "ERROR: Unknown argument: $1"; exit 1 ;;
  esac
done

# ── Validate required inputs ────────────────────────────────
: "${IMAGE:?--image is required}"
: "${SERVICE:?--service is required}"
: "${ENV_FILE:?--env-file is required}"
if [ "${SKIP_PULL}" = "false" ]; then
  : "${GHCR_TOKEN:?GHCR_TOKEN env var is required}"
  : "${GHCR_USER:?GHCR_USER env var is required}"
fi


echo "==========================================="
echo "  Medha API — CD Deploy"
echo "  Service:  ${SERVICE}"
echo "  Image:    ${IMAGE}"
echo "  Env file: ${ENV_FILE}"
echo "==========================================="

# ── Step 1: Disk check ──────────────────────────────────────
echo "==> [1/7] Disk before deploy:"
df -h /
if [ "$(df --output=avail / | tail -n 1)" -lt 100000 ]; then
  echo "Error: Not enough disk space for deployment!"
  exit 1
fi

# ── Step 2: GHCR login (ephemeral Docker config) ───────────
if [ "${SKIP_PULL}" = "true" ]; then
  echo "==> [2/7] Skipping GHCR login (--skip-pull)"
else
  echo "==> [2/7] Logging in to GHCR..."
  export DOCKER_CONFIG="/dev/shm/.docker-config-$$"
  mkdir -p "${DOCKER_CONFIG}"
  trap 'rm -rf "${DOCKER_CONFIG}"' EXIT
  echo "${GHCR_TOKEN}" | docker login ghcr.io -u "${GHCR_USER}" --password-stdin
fi

# ── Step 3: Pull new image ──────────────────────────────────
if [ "${SKIP_PULL}" = "true" ]; then
  echo "==> [3/7] Skipping image pull (--skip-pull)"
else
  echo "==> [3/7] Pulling image: ${IMAGE}"
  docker pull "${IMAGE}"
fi

# ── Step 4: Verify deploy directory ─────────────────────────
echo "==> [4/7] Verifying deployment directory..."
if [ ! -d "${DEPLOY_PATH}" ]; then
  echo "Deployment path missing. Creating ${DEPLOY_PATH}..."
  mkdir -p "${DEPLOY_PATH}"
fi
cd "${DEPLOY_PATH}"

# ── Step 5: Update docker-compose image tag ─────────────────
echo "==> [5/7] Updating image for service '${SERVICE}' in ${COMPOSE_FILE}..."
sed -i.bak "/^[[:space:]]*${SERVICE}:/,/^[[:space:]]*image:/ s|image:.*|image: ${IMAGE}|" "${COMPOSE_FILE}" || {
  echo "ERROR: sed failed to update compose file. Ensure permissions and disk space!"
  exit 1
}


# ── Step 5b: Update environment secrets ─────────────────────
echo "==> [5b/7] Updating environment secrets..."
ENV_DIR="../envs"
ENV_PATH="${ENV_DIR}/${ENV_FILE}"
[ ! -d "${ENV_DIR}" ] && mkdir -p "${ENV_DIR}"
[ ! -f "${ENV_PATH}" ] && touch "${ENV_PATH}"

# Helper: idempotent upsert of key=value into env file
upsert_env() {
  local key="$1" val="$2" file="$3"
  if grep -q "^${key}=" "${file}" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${val}|g" "${file}"
  else
    echo "${key}=${val}" >> "${file}"
  fi
}

upsert_env "MESSAGECENTRAL_CUSTOMER_ID"  "${MC_CUSTOMER_ID:-}"  "${ENV_PATH}"
upsert_env "MESSAGECENTRAL_KEY_BASE64"   "${MC_KEY_BASE64:-}"   "${ENV_PATH}"
upsert_env "MESSAGECENTRAL_EMAIL"        "${MC_EMAIL:-}"        "${ENV_PATH}"
upsert_env "MESSAGECENTRAL_BASE_URL"     "https://cpaas.messagecentral.com" "${ENV_PATH}"

# ── Step 6: Run migrations ─────────────────────────────────
if [ "${SKIP_MIGRATE}" = "true" ]; then
  echo "==> [6/7] Skipping migrations (--skip-migrate)"
else
  echo "==> [6/7] Running migrations..."
  docker compose -f "${COMPOSE_FILE}" run --rm -T "${SERVICE}" ./medha-api -migrate </dev/null || {
    echo "ERROR: Migrations failed! Aborting deployment..."
    exit 1
  }
fi

# ── Step 7: Targeted deploy (only this service) ────────────
echo "==> [7/7] Deploying ${SERVICE} (targeted restart)..."

docker compose -f "${COMPOSE_FILE}" stop "${SERVICE}" </dev/null || true
docker compose -f "${COMPOSE_FILE}" rm -f "${SERVICE}" </dev/null || true
docker compose -f "${COMPOSE_FILE}" up -d --no-deps "${SERVICE}" </dev/null

# Restart policy adjustment
sed -i 's/restart: unless-stopped/restart: always/g' "${COMPOSE_FILE}"

# ── Verify ──────────────────────────────────────────────────
echo "==> Verifying deployed runtime image..."
CONTAINER_ID=$(docker compose -f "${COMPOSE_FILE}" ps -q "${SERVICE}" </dev/null 2>/dev/null || echo "")
if [ -z "${CONTAINER_ID}" ]; then
  echo "ERROR: No container found for ${SERVICE}"
  docker compose -f "${COMPOSE_FILE}" logs --tail 50 "${SERVICE}" </dev/null
  exit 1
fi

RUNNING_IMAGE=$(docker inspect "${CONTAINER_ID}" --format='{{.Config.Image}}' 2>/dev/null || echo "not_found")
echo "Expected: ${IMAGE}"
echo "Running : ${RUNNING_IMAGE}"

if [ "${RUNNING_IMAGE}" != "${IMAGE}" ]; then
  echo "ERROR: ${SERVICE} is not running the expected image!"
  docker compose -f "${COMPOSE_FILE}" logs --tail 50 "${SERVICE}" </dev/null
  exit 1
fi

# ── Cleanup ─────────────────────────────────────────────────
echo "==> Pruning dangling images..."
docker image prune -f || true

echo "==> Final disk usage:"
df -h /

echo "==========================================="
echo "  ✅ Deploy complete: ${SERVICE}"
echo "==========================================="
