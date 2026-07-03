#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
WORK_DIR="$TMP_ROOT/work"
INSTALL_PREFIX="$TMP_ROOT/google-cloud-sdk"
INSTALL_LOG="$TMP_ROOT/gcloud-install.log"
GCLOUD_LOG="$TMP_ROOT/gcloud.log"

cleanup() {
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

copy_repo() {
  mkdir -p "$WORK_DIR"
  shopt -s dotglob nullglob
  local item base
  for item in "$ROOT_DIR"/*; do
    base="$(basename "$item")"
    case "$base" in
      .git|.cache|bin|logs|secrets|.env|.env.backup.*)
        continue
        ;;
    esac
    cp -R "$item" "$WORK_DIR/"
  done
}

assert_contains() {
  local file="$1"
  local text="$2"
  if ! grep -Fq "$text" "$file"; then
    echo "expected $file to contain: $text" >&2
    echo "--- $file ---" >&2
    sed -n '1,260p' "$file" >&2 || true
    exit 1
  fi
}

copy_repo

cd "$WORK_DIR"
export BYTO_GCLOUD_FORCE_MISSING=1
export BYTO_GCLOUD_INSTALL_DRY_RUN=1
export BYTO_GCLOUD_INSTALL_PREFIX="$INSTALL_PREFIX"
export BYTO_GCLOUD_INSTALL_LOG="$INSTALL_LOG"
export FAKE_GCLOUD_LOG="$GCLOUD_LOG"

if ! command -v make >/dev/null 2>&1; then
  echo "make is required for this e2e test" >&2
  exit 1
fi

make SHELL="$(command -v bash)" setup production \
  PROJECT=ci-project \
  LOCATION=global \
  MODEL=gemini-2.5-flash \
  SERVICE_ACCOUNT_NAME=ci-gateway \
  KEY_PATH=secrets/ci-gateway.json \
  API_KEY=ci-gateway-key \
  INSTALL_GCLOUD=1 \
  NON_INTERACTIVE=1 \
  SKIP_VERIFY=1

test -x "$INSTALL_PREFIX/bin/gcloud"
test -s .env
test -s secrets/ci-gateway.json

assert_contains "$INSTALL_LOG" "dry-run os="
case "$(uname -s)" in
  Linux)
    assert_contains "$INSTALL_LOG" "dry-run os=linux"
    assert_contains "$INSTALL_LOG" "installer=linux"
    ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    assert_contains "$INSTALL_LOG" "dry-run os=windows"
    assert_contains "$INSTALL_LOG" "installer=windows"
    ;;
esac

assert_contains .env "GOOGLE_CLOUD_PROJECT=ci-project"
assert_contains .env "GOOGLE_CLOUD_LOCATION=global"
assert_contains .env "GATEWAY_API_KEYS=ci-gateway-key"
assert_contains .env "GATEWAY_ALLOW_UNAUTHENTICATED=false"
assert_contains .env "GOOGLE_APPLICATION_CREDENTIALS=secrets/ci-gateway.json"
assert_contains .env "VERTEX_ACCESS_TOKEN="

assert_contains secrets/ci-gateway.json '"type": "service_account"'
assert_contains secrets/ci-gateway.json '"client_email": "ci-gateway@ci-project.iam.gserviceaccount.com"'
assert_contains "$GCLOUD_LOG" "config set project ci-project"
assert_contains "$GCLOUD_LOG" "services enable aiplatform.googleapis.com iam.googleapis.com serviceusage.googleapis.com cloudresourcemanager.googleapis.com"
assert_contains "$GCLOUD_LOG" "iam service-accounts create ci-gateway"
assert_contains "$GCLOUD_LOG" "projects add-iam-policy-binding ci-project --member=serviceAccount:ci-gateway@ci-project.iam.gserviceaccount.com --role=roles/aiplatform.user"
assert_contains "$GCLOUD_LOG" "projects add-iam-policy-binding ci-project --member=serviceAccount:ci-gateway@ci-project.iam.gserviceaccount.com --role=roles/serviceusage.serviceUsageConsumer"
assert_contains "$GCLOUD_LOG" "iam service-accounts keys create secrets/ci-gateway.json --iam-account=ci-gateway@ci-project.iam.gserviceaccount.com"

echo "production setup install e2e passed"
