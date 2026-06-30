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
INSTALL_GCLOUD=0
SETUP_LOG_DIR="$ROOT_DIR/.cache/setup"

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
  INSTALL_GCLOUD=1         Install Google Cloud CLI if missing.

Examples:
  make setup
  make setup PROJECT=my-gcp-project
  make setup PROJECT=my-gcp-project INSTALL_GCLOUD=1
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
    --install-gcloud)
      INSTALL_GCLOUD=1
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
  read -r -p "$label [$default]: " value
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

  if [ "$NON_INTERACTIVE" -eq 1 ] || [ ! -t 0 ] || [ ! -t 1 ]; then
    printf '%s' "$selected"
    return
  fi

  printf '%s\n' "$title" >&2
  tput civis 2>/dev/null || true
  while true; do
    for i in "${!options[@]}"; do
      printf '%s\r' "$CLEAR_LINE" >&2
      if [ "$i" -eq "$selected" ]; then
        printf '  %s> %s%s\n' "$CYAN" "${options[$i]}" "$RESET" >&2
      else
        printf '    %s\n' "${options[$i]}" >&2
      fi
    done

    IFS= read -rsn1 key
    if [ "$key" = "" ]; then
      tput cnorm 2>/dev/null || true
      printf '\n' >&2
      printf '%s' "$selected"
      return
    fi

    if [ "$key" = $'\033' ]; then
      IFS= read -rsn2 key || true
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

    printf '\033[%dA' "${#options[@]}" >&2
  done
}

run_quiet() {
  local label="$1"
  shift
  mkdir -p "$SETUP_LOG_DIR"
  local safe_label
  safe_label="$(printf '%s' "$label" | tr '[:upper:] ' '[:lower:]-' | tr -cd 'a-z0-9_-')"
  local log="$SETUP_LOG_DIR/$(date +%Y%m%d%H%M%S)-$safe_label.log"
  local frames=("|" "/" "-" "\\")
  local frame=0
  local status=0

  if [ -t 1 ]; then
    printf '  %s %s' "${frames[$frame]}" "$label"
  else
    echo "RUN $label"
  fi

  "$@" >"$log" 2>&1 &
  local pid=$!
  while kill -0 "$pid" >/dev/null 2>&1; do
    if [ -t 1 ]; then
      frame=$(((frame + 1) % 4))
      printf '\r%s  %s %s' "$CLEAR_LINE" "${frames[$frame]}" "$label"
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

sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo "$@"
    return
  fi
  fail "This install step needs root privileges, but sudo is not available."
}

ensure_sudo_session() {
  if [ "$(id -u)" -eq 0 ]; then
    return
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    fail "This install step needs root privileges, but sudo is not available."
  fi
  note "Admin password may be requested once for package installation."
  sudo -v
}

install_gcloud_macos_cmd() {
  if ! command -v brew >/dev/null 2>&1; then
    warn "Homebrew is not installed, so setup cannot auto-install Google Cloud CLI on macOS."
    echo "Install Homebrew first, then rerun: make setup INSTALL_GCLOUD=1"
    return 1
  fi
  brew install --cask google-cloud-sdk
}

install_gcloud_apt_cmd() {
  if ! command -v curl >/dev/null 2>&1; then
    sudo_cmd apt-get update
    sudo_cmd apt-get install -y curl
  fi
  sudo_cmd apt-get update
  sudo_cmd apt-get install -y apt-transport-https ca-certificates gnupg curl
  sudo_cmd rm -f /usr/share/keyrings/cloud.google.gpg
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo_cmd gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
  echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo_cmd tee /etc/apt/sources.list.d/google-cloud-sdk.list >/dev/null
  sudo_cmd apt-get update
  sudo_cmd apt-get install -y google-cloud-cli
}

install_gcloud_yum_family_cmd() {
  local installer="$1"
  local el_major="9"
  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    el_major="${VERSION_ID:-9}"
    el_major="${el_major%%.*}"
    if [ "$el_major" != "8" ] && [ "$el_major" != "9" ]; then
      el_major="9"
    fi
  fi
  cat <<'EOF_REPO' | sudo_cmd tee /etc/yum.repos.d/google-cloud-sdk.repo >/dev/null
[google-cloud-cli]
name=Google Cloud CLI
baseurl=__BASEURL__
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg
EOF_REPO
  sudo_cmd sed -i.bak "s#__BASEURL__#https://packages.cloud.google.com/yum/repos/cloud-sdk-el${el_major}-x86_64#g" /etc/yum.repos.d/google-cloud-sdk.repo
  sudo_cmd "$installer" install -y google-cloud-cli
}

install_gcloud_linux_cmd() {
  if command -v apt-get >/dev/null 2>&1; then
    install_gcloud_apt_cmd
    return
  fi
  if command -v dnf >/dev/null 2>&1; then
    install_gcloud_yum_family_cmd dnf
    return
  fi
  if command -v yum >/dev/null 2>&1; then
    install_gcloud_yum_family_cmd yum
    return
  fi
  if command -v snap >/dev/null 2>&1; then
    sudo_cmd snap install google-cloud-cli --classic
    return
  fi
  warn "No supported Linux package manager found for automatic Google Cloud CLI install."
  echo "Supported auto-install paths: apt-get, dnf, yum, or snap."
  return 1
}

install_gcloud() {
  step "Installing Google Cloud CLI"
  case "$(uname -s)" in
    Darwin)
      run_quiet "Installing Google Cloud CLI with Homebrew" install_gcloud_macos_cmd || return 1
      ;;
    Linux)
      ensure_sudo_session
      run_quiet "Installing Google Cloud CLI" install_gcloud_linux_cmd || return 1
      ;;
    *)
      warn "Automatic Google Cloud CLI install is not supported on $(uname -s)."
      return 1
      ;;
  esac
  hash -r
  if command -v gcloud >/dev/null 2>&1; then
    ok "Installed gcloud CLI"
    return 0
  fi
  warn "Install command finished, but gcloud is not on PATH yet."
  echo "Open a new terminal or update PATH, then rerun: make setup"
  return 1
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
    warn "gcloud is not installed. It is required for live Vertex verification and local ADC auth."
    local choice=1
    if [ "$INSTALL_GCLOUD" -eq 1 ]; then
      choice=0
    elif [ "$NON_INTERACTIVE" -eq 1 ]; then
      choice=1
    else
      choice="$(select_menu "Choose how to continue:" 1 "Install Google Cloud CLI now" "Skip for now" "Abort setup")"
    fi
    if [ "$choice" -eq 0 ]; then
      if install_gcloud; then
        check_gcloud
        return
      fi
      warn "Continuing without gcloud. Live Vertex setup will not work until it is installed."
      return
    fi
    if [ "$choice" -eq 2 ]; then
      fail "Setup aborted before installing gcloud."
    fi
    warn "Skipped Google Cloud CLI install. Live Vertex setup will not work until gcloud is installed."
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
  run_quiet "Local Go tests" env GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}" go test ./... -count=1
}

step "Go LLM Gateway Setup"
note "One path: make setup -> make verify-gcp -> make run"

load_existing_env

step "Checking Dependencies"
check_go
check_gcloud

step "Configuring Local Environment"
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

step "Running Local Verification"
run_tests

step "Next Commands"
echo "1. Authenticate Google Cloud:"
echo "   gcloud auth login && gcloud auth application-default login"
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
