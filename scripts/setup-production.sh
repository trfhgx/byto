#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PROJECT_ID="${PROJECT:-${GOOGLE_CLOUD_PROJECT:-}}"
LOCATION="${LOCATION:-${GOOGLE_CLOUD_LOCATION:-global}}"
SERVICE_ACCOUNT_NAME="${SERVICE_ACCOUNT_NAME:-llm-gateway-sa}"
SERVICE_ACCOUNT_DISPLAY="${SERVICE_ACCOUNT_DISPLAY:-Byto Gateway Production}"
KEY_PATH="${KEY_PATH:-secrets/llm-gateway-sa.json}"
VERIFY_MODEL="${MODEL:-${VERIFY_MODEL:-}}"
API_KEYS="${GATEWAY_API_KEYS:-}"
MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-config/models.json}"
MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-true}"
ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-false}"
MODEL_ALIASES="${MODEL_ALIASES:-}"
VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"
PORT="${PORT:-8080}"
LOG_PATH="${LOG_PATH:-logs/requests.jsonl}"
REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
VERTEX_RETRY_MAX_ATTEMPTS="${VERTEX_RETRY_MAX_ATTEMPTS:-3}"
VERTEX_RETRY_INITIAL_MS="${VERTEX_RETRY_INITIAL_MS:-250}"
VERTEX_RETRY_MAX_MS="${VERTEX_RETRY_MAX_MS:-2000}"
NON_INTERACTIVE=0
SKIP_VERIFY=0
SETUP_LOG_DIR="$ROOT_DIR/.cache/setup"
HAS_TTY=0

usage() {
  cat <<'EOF'
Usage:
  make setup production PROJECT=my-gcp-project MODEL=gemini-2.5-flash
  make setup-production PROJECT=my-gcp-project MODEL=gemini-2.5-flash

Options:
  PROJECT=PROJECT_ID             Google Cloud project ID.
  LOCATION=LOCATION              Vertex AI location, default: global.
  MODEL=MODEL                    Optional live model verification.
  SERVICE_ACCOUNT_NAME=NAME      Service account name, default: llm-gateway-sa.
  KEY_PATH=PATH                  Service account key path, default: secrets/llm-gateway-sa.json.
  NON_INTERACTIVE=1              Do not prompt.
  SKIP_VERIFY=1                  Skip live gateway verification.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) PROJECT_ID="${2:-}"; shift 2 ;;
    --location) LOCATION="${2:-}"; shift 2 ;;
    --model) VERIFY_MODEL="${2:-}"; shift 2 ;;
    --service-account) SERVICE_ACCOUNT_NAME="${2:-}"; shift 2 ;;
    --key-path) KEY_PATH="${2:-}"; shift 2 ;;
    --api-key) API_KEYS="${2:-}"; shift 2 ;;
    --non-interactive) NON_INTERACTIVE=1; shift ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if [ -t 1 ]; then
  BOLD="$(printf '\033[1m')"
  DIM="$(printf '\033[2m')"
  GREEN="$(printf '\033[32m')"
  YELLOW="$(printf '\033[33m')"
  RED="$(printf '\033[31m')"
  CYAN="$(printf '\033[36m')"
  RESET="$(printf '\033[0m')"
  CLEAR_LINE="$(printf '\033[2K')"
else
  BOLD=""
  DIM=""
  GREEN=""
  YELLOW=""
  RED=""
  CYAN=""
  RESET=""
  CLEAR_LINE=""
fi

if [ -r /dev/tty ] && [ -w /dev/tty ]; then
  HAS_TTY=1
fi

SPINNER_FRAMES=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")

step() {
  echo
  echo "${BOLD}${CYAN}$*${RESET}"
}

ok() {
  echo "${GREEN}OK${RESET} $*"
}

warn() {
  echo "${YELLOW}WARN${RESET} $*"
}

fail() {
  echo "${RED}ERROR${RESET} $*" >&2
  exit 1
}

note() {
  echo "${DIM}$*${RESET}"
}

prompt() {
  local label="$1"
  local default="$2"
  local value=""
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    printf '%s' "$default"
    return
  fi
  if [ "$HAS_TTY" -ne 1 ]; then
    fail "Interactive production setup needs a terminal. Rerun from a terminal or use NON_INTERACTIVE=1."
  fi
  read -r -p "$label [$default]: " value </dev/tty
  if [ -z "$value" ]; then
    value="$default"
  fi
  printf '%s' "$value"
}

run_quiet() {
  local label="$1"
  shift
  mkdir -p "$SETUP_LOG_DIR"
  local safe_label
  safe_label="$(printf '%s' "$label" | tr '[:upper:] ' '[:lower:]-' | tr -cd 'a-z0-9_-')"
  local log="$SETUP_LOG_DIR/$(date +%Y%m%d%H%M%S)-$safe_label.log"
  local frame=0
  local status=0

  if [ -t 1 ]; then
    printf '  %s %s' "${SPINNER_FRAMES[$frame]}" "$label"
  else
    echo "RUN $label"
  fi

  "$@" >"$log" 2>&1 &
  local pid=$!
  while kill -0 "$pid" >/dev/null 2>&1; do
    if [ -t 1 ]; then
      frame=$(((frame + 1) % ${#SPINNER_FRAMES[@]}))
      printf '\r%s  %s %s' "$CLEAR_LINE" "${SPINNER_FRAMES[$frame]}" "$label"
    fi
    sleep 0.12
  done

  set +e
  wait "$pid"
  status=$?
  set -e

  if [ "$status" -eq 0 ]; then
    if [ -t 1 ]; then
      printf '\r%s  %s %s\n' "$CLEAR_LINE" "${GREEN}OK${RESET}" "$label"
    else
      echo "OK $label"
    fi
    return 0
  fi

  if [ -t 1 ]; then
    printf '\r%s  %s %s\n' "$CLEAR_LINE" "${RED}FAIL${RESET}" "$label"
  else
    echo "FAIL $label"
  fi
  echo "Log: $log" >&2
  tail -n 80 "$log" >&2 || true
  exit "$status"
}

copy_to_clipboard() {
  local value="$1"
  if command -v pbcopy >/dev/null 2>&1; then
    printf '%s' "$value" | pbcopy
    ok "Copied gateway API key to clipboard"
  elif command -v wl-copy >/dev/null 2>&1; then
    printf '%s' "$value" | wl-copy
    ok "Copied gateway API key to clipboard"
  elif command -v xclip >/dev/null 2>&1; then
    printf '%s' "$value" | xclip -selection clipboard
    ok "Copied gateway API key to clipboard"
  else
    warn "Could not copy API key to clipboard on this OS"
  fi
}

generate_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c 64
  fi
}

service_key_valid() {
  [ -s "$KEY_PATH" ] && grep -q '"client_email"' "$KEY_PATH" && grep -q '"private_key"' "$KEY_PATH"
}

create_service_account_key() {
  mkdir -p "$SETUP_LOG_DIR"
  local log="$SETUP_LOG_DIR/$(date +%Y%m%d%H%M%S)-create-service-account-key.log"
  if gcloud iam service-accounts keys create "$KEY_PATH" --iam-account="$SA_EMAIL" >"$log" 2>&1; then
    ok "Create service account key"
    chmod 600 "$KEY_PATH" 2>/dev/null || true
    return
  fi
  rm -f "$KEY_PATH"
  if grep -q 'constraints/iam.disableServiceAccountKeyCreation' "$log"; then
    fail "Google org policy blocks service-account key creation: constraints/iam.disableServiceAccountKeyCreation. Use an existing valid key at KEY_PATH, change the org policy, or deploy on Google infrastructure with metadata auth."
  fi
  echo "Log: $log" >&2
  tail -n 80 "$log" >&2 || true
  fail "Could not create service account key"
}

load_existing_env() {
  if [ ! -f .env ]; then
    return
  fi
  set -a
  # shellcheck disable=SC1091
  source ./.env
  set +a
  PROJECT_ID="${PROJECT_ID:-${GOOGLE_CLOUD_PROJECT:-}}"
  LOCATION="${LOCATION:-${GOOGLE_CLOUD_LOCATION:-global}}"
  API_KEYS="${API_KEYS:-${GATEWAY_API_KEYS:-}}"
  MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-config/models.json}"
  MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-true}"
  ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-false}"
  MODEL_ALIASES="${MODEL_ALIASES:-}"
  VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"
  PORT="${PORT:-8080}"
  LOG_PATH="${LOG_PATH:-logs/requests.jsonl}"
  REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
  VERTEX_RETRY_MAX_ATTEMPTS="${VERTEX_RETRY_MAX_ATTEMPTS:-3}"
  VERTEX_RETRY_INITIAL_MS="${VERTEX_RETRY_INITIAL_MS:-250}"
  VERTEX_RETRY_MAX_MS="${VERTEX_RETRY_MAX_MS:-2000}"
  KEY_PATH="${GOOGLE_APPLICATION_CREDENTIALS:-$KEY_PATH}"
}

write_env() {
  if [ -f .env ]; then
    local backup=".env.backup.$(date +%Y%m%d%H%M%S)"
    cp .env "$backup"
    ok "Backed up existing .env to $backup"
  fi

  cat > .env <<EOF_ENV
# Required
GOOGLE_CLOUD_PROJECT=$PROJECT_ID
GOOGLE_CLOUD_LOCATION=$LOCATION

# Gateway auth.
GATEWAY_API_KEYS=$API_KEYS
GATEWAY_ALLOW_UNAUTHENTICATED=false

# Model behavior: services must send real Gemini model IDs or configured aliases.
MODEL_CATALOG_PATH=$MODEL_CATALOG_PATH
MODEL_CATALOG_REFRESH_ON_START=$MODEL_CATALOG_REFRESH_ON_START
ALLOW_ANY_GEMINI_MODEL=$ALLOW_ANY_GEMINI_MODEL

# Optional aliases if you want them. Keep empty if you want strict real model names only.
MODEL_ALIASES=$MODEL_ALIASES

# Vertex endpoint base. Keep default unless you know you need a regional endpoint.
VERTEX_BASE_URL=$VERTEX_BASE_URL

# Production Vertex auth.
GOOGLE_APPLICATION_CREDENTIALS=$KEY_PATH
VERTEX_ACCESS_TOKEN=

# Server
PORT=$PORT
LOG_PATH=$LOG_PATH
REQUEST_TIMEOUT_SECONDS=$REQUEST_TIMEOUT_SECONDS

# Lightweight Vertex retry policy for transient transport/upstream failures.
VERTEX_RETRY_MAX_ATTEMPTS=$VERTEX_RETRY_MAX_ATTEMPTS
VERTEX_RETRY_INITIAL_MS=$VERTEX_RETRY_INITIAL_MS
VERTEX_RETRY_MAX_MS=$VERTEX_RETRY_MAX_MS
EOF_ENV
  ok "Wrote .env"
}

load_existing_env

step "Byto Production Setup"
note "Creates/reuses a Google service account, downloads an ignored key file, writes .env, and optionally verifies Vertex through the gateway."

if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "your-project-id" ] || [ "$PROJECT_ID" = "test-project" ]; then
  PROJECT_ID="$(prompt "Google Cloud project" "$PROJECT_ID")"
fi
LOCATION="$(prompt "Vertex location" "$LOCATION")"
SERVICE_ACCOUNT_NAME="$(prompt "Service account name" "$SERVICE_ACCOUNT_NAME")"
KEY_PATH="$(prompt "Service account key path" "$KEY_PATH")"
if [ -z "$API_KEYS" ] || [ "$API_KEYS" = "dev-local-key" ] || [ "$API_KEYS" = "dev-local-key-change-me" ]; then
  API_KEYS="$(generate_key)"
  ok "Generated gateway API key"
fi

if [ -z "$PROJECT_ID" ]; then
  fail "PROJECT is required"
fi
if ! command -v gcloud >/dev/null 2>&1; then
  fail "gcloud is required for production setup"
fi
if ! command -v go >/dev/null 2>&1; then
  fail "Go is required for live verification"
fi

SA_EMAIL="$SERVICE_ACCOUNT_NAME@$PROJECT_ID.iam.gserviceaccount.com"
KEY_DIR="$(dirname "$KEY_PATH")"
mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR" 2>/dev/null || true

step "Google Cloud"
run_quiet "Set gcloud project" gcloud config set project "$PROJECT_ID"
if ! gcloud auth list --filter=status:ACTIVE --format='value(account)' | grep -q .; then
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    fail "No active gcloud account. Run gcloud auth login first."
  fi
  run_quiet "Authenticate gcloud" gcloud auth login
fi
run_quiet "Enable Google APIs" gcloud services enable aiplatform.googleapis.com iam.googleapis.com serviceusage.googleapis.com cloudresourcemanager.googleapis.com

if gcloud iam service-accounts describe "$SA_EMAIL" >/dev/null 2>&1; then
  ok "Service account exists: $SA_EMAIL"
else
  run_quiet "Create service account" gcloud iam service-accounts create "$SERVICE_ACCOUNT_NAME" --display-name="$SERVICE_ACCOUNT_DISPLAY"
fi

step "IAM"
for role in roles/aiplatform.user roles/serviceusage.serviceUsageConsumer; do
  run_quiet "Grant $role" gcloud projects add-iam-policy-binding "$PROJECT_ID" --member="serviceAccount:$SA_EMAIL" --role="$role" --quiet
done

step "Service Account Key"
if service_key_valid; then
  ok "Using existing key: $KEY_PATH"
else
  if [ -f "$KEY_PATH" ]; then
    warn "Ignoring invalid or empty key file at $KEY_PATH"
    rm -f "$KEY_PATH"
  fi
  create_service_account_key
fi

write_env
copy_to_clipboard "$API_KEYS"

if [ "$SKIP_VERIFY" -eq 0 ] && [ -n "$VERIFY_MODEL" ]; then
  step "Live Verification"
  run_quiet "Verify gateway with service account" env \
    GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
    GOOGLE_CLOUD_LOCATION="$LOCATION" \
    GATEWAY_API_KEYS="$API_KEYS" \
    GATEWAY_ALLOW_UNAUTHENTICATED=false \
    GOOGLE_APPLICATION_CREDENTIALS="$KEY_PATH" \
    VERTEX_ACCESS_TOKEN= \
    MODEL_CATALOG_PATH= \
    ALLOW_ANY_GEMINI_MODEL=true \
    VERTEX_BASE_URL="$VERTEX_BASE_URL" \
    REQUEST_TIMEOUT_SECONDS="$REQUEST_TIMEOUT_SECONDS" \
    VERTEX_RETRY_MAX_ATTEMPTS="$VERTEX_RETRY_MAX_ATTEMPTS" \
    VERTEX_RETRY_INITIAL_MS="$VERTEX_RETRY_INITIAL_MS" \
    VERTEX_RETRY_MAX_MS="$VERTEX_RETRY_MAX_MS" \
    RUN_LIVE_VERTEX_TESTS=1 \
    LIVE_VERTEX_MODEL="$VERIFY_MODEL" \
    GOCACHE="$ROOT_DIR/.cache/go-build" \
    go test ./test/e2e -run TestLiveVertexGenerateExplicitModel -count=1 -v
elif [ "$SKIP_VERIFY" -eq 0 ]; then
  warn "Skipped live generation verification because MODEL was not provided"
fi

step "Done"
ok "Production setup complete"
note "Service account: $SA_EMAIL"
note "Key path: $KEY_PATH"
