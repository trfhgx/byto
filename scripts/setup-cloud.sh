#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${APP_NAME:-llm-gateway}"
PROJECT_ID="${PROJECT:-${GOOGLE_CLOUD_PROJECT:-}}"
LOCATION="${LOCATION:-${GOOGLE_CLOUD_LOCATION:-global}}"
MODEL="${MODEL:-${VERIFY_MODEL:-}}"
REGION="${REGION:-${CLOUD_RUN_REGION:-us-central1}}"
SERVICE="${SERVICE:-${CLOUD_RUN_SERVICE:-llm-gateway}}"
SERVICE_ACCOUNT_NAME="${SERVICE_ACCOUNT_NAME:-llm-gateway-sa}"
API_KEYS="${GATEWAY_API_KEYS:-}"
GATEWAY_ALLOW_UNAUTHENTICATED="${GATEWAY_ALLOW_UNAUTHENTICATED:-false}"
MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-config/models.json}"
MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-true}"
ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-false}"
VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"
REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
VERTEX_RETRY_MAX_ATTEMPTS="${VERTEX_RETRY_MAX_ATTEMPTS:-3}"
VERTEX_RETRY_INITIAL_MS="${VERTEX_RETRY_INITIAL_MS:-250}"
VERTEX_RETRY_MAX_MS="${VERTEX_RETRY_MAX_MS:-2000}"
DEPLOY=0
NON_INTERACTIVE=0
SETUP_LOG_DIR="$ROOT_DIR/.cache/setup"
HAS_TTY=0

usage() {
  cat <<'EOF'
Usage:
  make setup-cloud PROJECT=my-gcp-project MODEL=gemini-2.5-flash
  make setup cloud PROJECT=my-gcp-project MODEL=gemini-2.5-flash

Options:
  PROJECT=PROJECT_ID       Google Cloud project ID.
  LOCATION=LOCATION        Vertex AI location, default: global.
  MODEL=MODEL              Explicit model for live generation verification.
  REGION=REGION            Cloud Run region, default: us-central1.
  SERVICE=NAME             Cloud Run service name, default: llm-gateway.
  DEPLOY=1                 Deploy to Cloud Run after setup.
  NON_INTERACTIVE=1        Do not prompt.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) PROJECT_ID="${2:-}"; shift 2 ;;
    --location) LOCATION="${2:-}"; shift 2 ;;
    --model) MODEL="${2:-}"; shift 2 ;;
    --region) REGION="${2:-}"; shift 2 ;;
    --service) SERVICE="${2:-}"; shift 2 ;;
    --deploy) DEPLOY=1; shift ;;
    --non-interactive) NON_INTERACTIVE=1; shift ;;
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

is_placeholder_project() {
  [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "your-project-id" ] || [ "$PROJECT_ID" = "test-project" ]
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
    fail "Interactive cloud setup needs a terminal. Rerun from a terminal or use NON_INTERACTIVE=1."
  fi
  read -r -p "$label [$default]: " value </dev/tty
  if [ -z "$value" ]; then
    value="$default"
  fi
  printf '%s' "$value"
}

select_menu() {
  local title="$1"
  shift
  local selected="$1"
  shift
  local options=("$@")
  local key=""
  local i

  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    printf '%s' "$selected"
    return
  fi
  if [ "$HAS_TTY" -ne 1 ]; then
    fail "Interactive cloud setup needs a terminal for selection."
  fi

  printf '%s\n' "$title" >/dev/tty
  tput civis >/dev/tty 2>/dev/null || true
  while true; do
    for i in "${!options[@]}"; do
      printf '%s\r' "$CLEAR_LINE" >/dev/tty
      if [ "$i" -eq "$selected" ]; then
        printf '  %s> %s%s\n' "$CYAN" "${options[$i]}" "$RESET" >/dev/tty
      else
        printf '    %s\n' "${options[$i]}" >/dev/tty
      fi
    done

    IFS= read -rsn1 key </dev/tty
    if [ "$key" = "" ]; then
      tput cnorm >/dev/tty 2>/dev/null || true
      printf '\n' >/dev/tty
      printf '%s' "$selected"
      return
    fi

    if [ "$key" = $'\033' ]; then
      IFS= read -rsn2 key </dev/tty || true
      case "$key" in
        "[A")
          selected=$((selected - 1))
          if [ "$selected" -lt 0 ]; then
            selected=$((${#options[@]} - 1))
          fi
          ;;
        "[B")
          selected=$((selected + 1))
          if [ "$selected" -ge "${#options[@]}" ]; then
            selected=0
          fi
          ;;
      esac
    fi

    printf '\033[%dA' "${#options[@]}" >/dev/tty
  done
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
      printf '\r%s  %sOK%s %s\n' "$CLEAR_LINE" "$GREEN" "$RESET" "$label"
    else
      ok "$label"
    fi
    return 0
  fi

  if [ -t 1 ]; then
    printf '\r%s  %sERROR%s %s\n' "$CLEAR_LINE" "$RED" "$RESET" "$label"
  else
    warn "$label failed"
  fi
  warn "Details saved to $log"
  return "$status"
}

generate_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

catalog_path_for_tests() {
  case "$MODEL_CATALOG_PATH" in
    /*) printf '%s' "$MODEL_CATALOG_PATH" ;;
    *) printf '%s/%s' "$ROOT_DIR" "$MODEL_CATALOG_PATH" ;;
  esac
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
  GATEWAY_ALLOW_UNAUTHENTICATED="${GATEWAY_ALLOW_UNAUTHENTICATED:-false}"
  MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-config/models.json}"
  MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-true}"
  ALLOW_ANY_GEMINI_MODEL="${ALLOW_ANY_GEMINI_MODEL:-false}"
  VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"
  REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
  VERTEX_RETRY_MAX_ATTEMPTS="${VERTEX_RETRY_MAX_ATTEMPTS:-3}"
  VERTEX_RETRY_INITIAL_MS="${VERTEX_RETRY_INITIAL_MS:-250}"
  VERTEX_RETRY_MAX_MS="${VERTEX_RETRY_MAX_MS:-2000}"
}

write_env() {
  if [ -f .env ]; then
    local backup=".env.backup.$(date +%Y%m%d%H%M%S)"
    cp .env "$backup"
    ok "Backed up existing .env to $backup"
  fi

  cat > .env <<EOF_ENV
GOOGLE_CLOUD_PROJECT=$PROJECT_ID
GOOGLE_CLOUD_LOCATION=$LOCATION
GATEWAY_API_KEYS=$API_KEYS
GATEWAY_ALLOW_UNAUTHENTICATED=$GATEWAY_ALLOW_UNAUTHENTICATED
MODEL_CATALOG_PATH=$MODEL_CATALOG_PATH
MODEL_CATALOG_REFRESH_ON_START=$MODEL_CATALOG_REFRESH_ON_START
ALLOW_ANY_GEMINI_MODEL=$ALLOW_ANY_GEMINI_MODEL
MODEL_ALIASES=
VERTEX_BASE_URL=$VERTEX_BASE_URL
VERTEX_ACCESS_TOKEN=
PORT=8080
LOG_PATH=logs/requests.jsonl
REQUEST_TIMEOUT_SECONDS=$REQUEST_TIMEOUT_SECONDS
VERTEX_RETRY_MAX_ATTEMPTS=$VERTEX_RETRY_MAX_ATTEMPTS
VERTEX_RETRY_INITIAL_MS=$VERTEX_RETRY_INITIAL_MS
VERTEX_RETRY_MAX_MS=$VERTEX_RETRY_MAX_MS
EOF_ENV
  ok "Wrote .env"
}

ensure_command() {
  local cmd="$1"
  local help="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "$cmd found"
    return
  fi
  fail "$cmd is required. $help"
}

install_docker_desktop() {
  if [ "$(uname -s)" != "Darwin" ]; then
    fail "Automatic Docker install is only supported on macOS for now. Install Docker, then rerun make setup-cloud."
  fi
  ensure_command brew "Install Homebrew or install Docker Desktop manually."
  run_quiet "Install Docker Desktop" brew install --cask docker
}

wait_for_docker() {
  local tries=90
  local i
  for i in $(seq 1 "$tries"); do
    if docker info >/dev/null 2>&1; then
      ok "Docker daemon is running"
      return 0
    fi
    sleep 1
  done
  return 1
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    local choice=1
    if [ "$NON_INTERACTIVE" -eq 0 ]; then
      choice="$(select_menu "Docker is required for cloud setup:" 0 "Install Docker Desktop now" "Abort setup")"
    fi
    if [ "$choice" -eq 0 ]; then
      install_docker_desktop
    else
      fail "Docker is required for cloud setup."
    fi
  fi

  if docker info >/dev/null 2>&1; then
    ok "Docker daemon is running"
    return
  fi

  if [ "$(uname -s)" = "Darwin" ] && command -v open >/dev/null 2>&1; then
    warn "Docker is installed but not running."
    open -a Docker >/dev/null 2>&1 || true
    run_quiet "Wait for Docker Desktop" wait_for_docker
    return
  fi
  fail "Docker is installed but the daemon is not running."
}

ensure_gcloud_auth() {
  ensure_command gcloud "Run make setup first or install Google Cloud CLI."
  if ! gcloud auth list --filter=status:ACTIVE --format='value(account)' | grep -q .; then
    if [ "$NON_INTERACTIVE" -eq 1 ]; then
      fail "gcloud is not authenticated. Run make setup first."
    fi
    gcloud auth login </dev/tty >/dev/tty 2>&1
  fi
  if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
    if [ "$NON_INTERACTIVE" -eq 1 ]; then
      fail "Application Default Credentials are missing. Run make setup first."
    fi
    gcloud auth application-default login </dev/tty >/dev/tty 2>&1
  fi
}

run_live_e2e() {
  if [ -n "$MODEL" ]; then
    run_quiet "Live Vertex e2e ($MODEL)" env \
      GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
      GOOGLE_CLOUD_LOCATION="$LOCATION" \
      MODEL_CATALOG_PATH="$(catalog_path_for_tests)" \
      MODEL_CATALOG_REFRESH_ON_START="$MODEL_CATALOG_REFRESH_ON_START" \
      GATEWAY_API_KEYS="$API_KEYS" \
      GATEWAY_ALLOW_UNAUTHENTICATED="$GATEWAY_ALLOW_UNAUTHENTICATED" \
      make test-live MODEL="$MODEL"
    return
  fi
  run_quiet "Live Vertex catalog e2e" env \
    GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
    GOOGLE_CLOUD_LOCATION="$LOCATION" \
    MODEL_CATALOG_PATH="$(catalog_path_for_tests)" \
    MODEL_CATALOG_REFRESH_ON_START="$MODEL_CATALOG_REFRESH_ON_START" \
    GATEWAY_API_KEYS="$API_KEYS" \
    GATEWAY_ALLOW_UNAUTHENTICATED="$GATEWAY_ALLOW_UNAUTHENTICATED" \
    RUN_LIVE_VERTEX_TESTS=1 \
    GOCACHE="$ROOT_DIR/.cache/go-build" \
    go test ./test/e2e -run 'TestLiveVertexPublisherModels|TestLiveVertexStartupCatalogRefresh' -count=1 -v
}

deploy_cloud_run() {
  run_quiet "Deploy Cloud Run service" env \
    GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
    GOOGLE_CLOUD_LOCATION="$LOCATION" \
    CLOUD_RUN_REGION="$REGION" \
    CLOUD_RUN_SERVICE="$SERVICE" \
    SERVICE_ACCOUNT_NAME="$SERVICE_ACCOUNT_NAME" \
    GATEWAY_API_KEYS="$API_KEYS" \
    GATEWAY_ALLOW_UNAUTHENTICATED="$GATEWAY_ALLOW_UNAUTHENTICATED" \
    MODEL_CATALOG_PATH="$MODEL_CATALOG_PATH" \
    MODEL_CATALOG_REFRESH_ON_START="$MODEL_CATALOG_REFRESH_ON_START" \
    ALLOW_ANY_GEMINI_MODEL="$ALLOW_ANY_GEMINI_MODEL" \
    VERTEX_BASE_URL="$VERTEX_BASE_URL" \
    REQUEST_TIMEOUT_SECONDS="$REQUEST_TIMEOUT_SECONDS" \
    VERTEX_RETRY_MAX_ATTEMPTS="$VERTEX_RETRY_MAX_ATTEMPTS" \
    VERTEX_RETRY_INITIAL_MS="$VERTEX_RETRY_INITIAL_MS" \
    VERTEX_RETRY_MAX_MS="$VERTEX_RETRY_MAX_MS" \
    ./scripts/cloud-run-deploy.sh
}

step "Byto Cloud Setup"
note "One path: project auth, cloud APIs, service account, Docker build, live verification, optional Cloud Run deploy."

load_existing_env

if is_placeholder_project; then
  PROJECT_ID="$(prompt "Google Cloud project ID" "your-project-id")"
fi
if is_placeholder_project; then
  fail "A real Google Cloud project is required for cloud setup."
fi
LOCATION="$(prompt "Vertex AI location" "$LOCATION")"
REGION="$(prompt "Cloud Run region" "$REGION")"
SERVICE="$(prompt "Cloud Run service" "$SERVICE")"
if [ -z "$MODEL" ]; then
  MODEL="$(prompt "Live verification model" "")"
fi
if [ "$GATEWAY_ALLOW_UNAUTHENTICATED" = "true" ]; then
  API_KEYS=""
  warn "Gateway auth is open because GATEWAY_ALLOW_UNAUTHENTICATED=true"
elif [ -z "$API_KEYS" ] || [ "$API_KEYS" = "dev-local-key" ] || [ "$API_KEYS" = "dev-local-key-change-me" ]; then
  API_KEYS="$(generate_key)"
  ok "Generated gateway API key"
fi

step "Checking Tools"
ensure_gcloud_auth
ensure_docker

step "Writing Environment"
write_env

step "Preparing Google Cloud"
run_quiet "Set gcloud project" gcloud config set project "$PROJECT_ID"
run_quiet "Set ADC quota project" gcloud auth application-default set-quota-project "$PROJECT_ID"
run_quiet "Enable APIs and IAM" env \
  GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
  GOOGLE_CLOUD_LOCATION="$LOCATION" \
  SERVICE_ACCOUNT_NAME="$SERVICE_ACCOUNT_NAME" \
  GATEWAY_API_KEYS="$API_KEYS" \
  GATEWAY_ALLOW_UNAUTHENTICATED="$GATEWAY_ALLOW_UNAUTHENTICATED" \
  MODEL_CATALOG_PATH="$MODEL_CATALOG_PATH" \
  MODEL_CATALOG_REFRESH_ON_START="$MODEL_CATALOG_REFRESH_ON_START" \
  ALLOW_ANY_GEMINI_MODEL="$ALLOW_ANY_GEMINI_MODEL" \
  VERTEX_BASE_URL="$VERTEX_BASE_URL" \
  REQUEST_TIMEOUT_SECONDS="$REQUEST_TIMEOUT_SECONDS" \
  VERTEX_RETRY_MAX_ATTEMPTS="$VERTEX_RETRY_MAX_ATTEMPTS" \
  VERTEX_RETRY_INITIAL_MS="$VERTEX_RETRY_INITIAL_MS" \
  VERTEX_RETRY_MAX_MS="$VERTEX_RETRY_MAX_MS" \
  ./scripts/setup-gcp.sh

step "Docker"
run_quiet "Build Docker image" docker build -t "$APP_NAME:cloud-ready" .

step "Live Verification"
run_live_e2e

if [ "$DEPLOY" -eq 1 ]; then
  step "Cloud Run"
  deploy_cloud_run
else
  if [ "$NON_INTERACTIVE" -eq 0 ]; then
    choice="$(select_menu "Cloud Run deploy:" 0 "Deploy now" "Finish without deploy" "Show deploy command")"
    case "$choice" in
      0)
        step "Cloud Run"
        deploy_cloud_run
        ;;
      2)
        echo
        echo "make setup-cloud PROJECT=$PROJECT_ID MODEL=$MODEL DEPLOY=1"
        ;;
    esac
  fi
fi

ok "Cloud setup complete"
echo "Project: $PROJECT_ID"
echo "Service: $SERVICE"
echo "Region: $REGION"
echo "Docker image: $APP_NAME:cloud-ready"
