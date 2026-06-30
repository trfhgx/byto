#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-}"
LOCATION="${GOOGLE_CLOUD_LOCATION:-global}"
DEFAULT_MODEL="${DEFAULT_MODEL:-gemini-3.1-pro-preview}"
ALLOWED_MODELS="${ALLOWED_MODELS:-gemini-3.1-pro-preview,gemini-3.1-pro-preview-customtools,gemini-3-flash-preview}"
ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-false}"
API_KEYS="${GATEWAY_API_KEYS:-}"
VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"
PORT="${PORT:-8080}"
LOG_PATH="${LOG_PATH:-logs/requests.jsonl}"
REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
NON_INTERACTIVE=0
SKIP_TESTS=0

usage() {
  cat <<'EOF'
Usage:
  make setup PROJECT=my-gcp-project

This script is an internal runner for make setup. Prefer the Make commands below.

Make options:
  PROJECT=PROJECT_ID       Google Cloud project ID.
  LOCATION=LOCATION        Vertex AI location, default: global.
  API_KEY=KEY              Gateway API key for local calls.
  MODEL=MODEL              Default Gemini model.
  NON_INTERACTIVE=1        Do not prompt; use env/default values.
  SKIP_TESTS=1             Do not run the local Go test suite.

Examples:
  make setup
  make setup PROJECT=my-gcp-project
  make setup PROJECT=my-gcp-project NON_INTERACTIVE=1
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --project)
      PROJECT_ID="${2:-}"
      shift 2
      ;;
    --location)
      LOCATION="${2:-}"
      shift 2
      ;;
    --api-key)
      API_KEYS="${2:-}"
      shift 2
      ;;
    --model)
      DEFAULT_MODEL="${2:-}"
      shift 2
      ;;
    --non-interactive)
      NON_INTERACTIVE=1
      shift
      ;;
    --skip-tests)
      SKIP_TESTS=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      usage
      exit 1
      ;;
  esac
done

if [ -t 1 ]; then
  BOLD="$(printf '\033[1m')"
  GREEN="$(printf '\033[32m')"
  YELLOW="$(printf '\033[33m')"
  RED="$(printf '\033[31m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""
  GREEN=""
  YELLOW=""
  RED=""
  RESET=""
fi

step() {
  echo
  echo "${BOLD}==> $*${RESET}"
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

prompt() {
  local label="$1"
  local default="$2"
  local value=""
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    printf '%s' "$default"
    return
  fi
  read -r -p "$label [$default]: " value
  if [ -z "$value" ]; then
    value="$default"
  fi
  printf '%s' "$value"
}

generate_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

load_existing_env() {
  if [ ! -f .env ]; then
    return
  fi
  set -a
  # shellcheck disable=SC1091
  source ./.env
  set +a
  PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-$PROJECT_ID}"
  LOCATION="${GOOGLE_CLOUD_LOCATION:-$LOCATION}"
  DEFAULT_MODEL="${DEFAULT_MODEL:-gemini-3.1-pro-preview}"
  ALLOWED_MODELS="${ALLOWED_MODELS:-$ALLOWED_MODELS}"
  ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-$ALLOW_ANY_GEMINI_MODEL}"
  API_KEYS="${GATEWAY_API_KEYS:-$API_KEYS}"
  VERTEX_BASE_URL="${VERTEX_BASE_URL:-$VERTEX_BASE_URL}"
  PORT="${PORT:-$PORT}"
  LOG_PATH="${LOG_PATH:-$LOG_PATH}"
  REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-$REQUEST_TIMEOUT_SECONDS}"
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

# Gateway auth: comma-separated API keys your apps use when calling this gateway.
GATEWAY_API_KEYS=$API_KEYS

# Model behavior: services should send real Gemini model IDs.
DEFAULT_MODEL=$DEFAULT_MODEL
ALLOWED_MODELS=$ALLOWED_MODELS
ALLOW_ANY_GEMINI_MODEL=$ALLOW_ANY_GEMINI_MODEL

# Optional aliases if you want them. Keep empty if you want strict real model names only.
MODEL_ALIASES=

# Vertex endpoint base. Keep default unless you know you need a regional endpoint.
VERTEX_BASE_URL=$VERTEX_BASE_URL

# Auth token source order:
# 1) VERTEX_ACCESS_TOKEN if set
# 2) Cloud Run/GCE metadata server
# 3) gcloud auth application-default print-access-token
VERTEX_ACCESS_TOKEN=

# Server
PORT=$PORT
LOG_PATH=$LOG_PATH
REQUEST_TIMEOUT_SECONDS=$REQUEST_TIMEOUT_SECONDS
EOF_ENV
  ok "Wrote .env"
}

check_go() {
  if ! command -v go >/dev/null 2>&1; then
    fail "Go 1.22+ is required. Install Go first, then rerun make setup"
  fi
  local version
  version="$(go version | awk '{print $3}' | sed 's/^go//')"
  local major minor
  major="$(printf '%s' "$version" | cut -d. -f1)"
  minor="$(printf '%s' "$version" | cut -d. -f2)"
  if [ "${major:-0}" -lt 1 ] || { [ "${major:-0}" -eq 1 ] && [ "${minor:-0}" -lt 22 ]; }; then
    fail "Go $version found, but Go 1.22+ is required."
  fi
  ok "Go $(go version | awk '{print $3}')"
}

check_gcloud() {
  if ! command -v gcloud >/dev/null 2>&1; then
    warn "gcloud is not installed. Install Google Cloud SDK before verifying live Vertex access."
    return
  fi
  ok "gcloud CLI found"
  if gcloud auth application-default print-access-token >/dev/null 2>&1; then
    ok "Application Default Credentials are available"
  else
    warn "Application Default Credentials are not ready. Run: gcloud auth application-default login"
  fi
}

run_tests() {
  if [ "$SKIP_TESTS" -eq 1 ]; then
    warn "Skipped tests"
    return
  fi
  mkdir -p .cache/go-build
  GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}" go test ./... -count=1
  ok "Local tests passed"
}

step "Go LLM Gateway setup"
echo "This prepares local config, checks dependencies, and runs the non-live test suite."

load_existing_env

step "Checking dependencies"
check_go
check_gcloud

step "Configuring .env"
if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "your-project-id" ]; then
  PROJECT_ID="$(prompt "Google Cloud project ID" "your-project-id")"
fi
LOCATION="$(prompt "Vertex AI location" "$LOCATION")"
DEFAULT_MODEL="$(prompt "Default Gemini model" "$DEFAULT_MODEL")"
if [ -z "$API_KEYS" ] || [ "$API_KEYS" = "dev-local-key" ] || [ "$API_KEYS" = "dev-local-key-change-me" ]; then
  API_KEYS="$(generate_key)"
  ok "Generated local gateway API key"
fi

write_env

if [ "$PROJECT_ID" = "your-project-id" ]; then
  warn ".env still uses the placeholder project ID. Edit GOOGLE_CLOUD_PROJECT before live Vertex calls."
fi

step "Running local verification"
run_tests

step "Next commands"
echo "1. If needed, authenticate Google Cloud:"
echo "   gcloud auth login"
echo "   gcloud auth application-default login"
echo "   gcloud config set project \"$PROJECT_ID\""
echo
echo "2. Verify Vertex model access:"
echo "   make verify-gcp"
echo
echo "3. Start the gateway:"
echo "   make run"
echo
echo "4. In another terminal, call it:"
echo "   curl -s http://localhost:$PORT/v1/chat/completions \\"
echo "     -H \"Authorization: Bearer $API_KEYS\" \\"
echo "     -H \"Content-Type: application/json\" \\"
echo "     -d '{\"model\":\"$DEFAULT_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with only: ok\"}]}' | jq"
echo
ok "Setup complete"
