#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-}"
LOCATION="${GOOGLE_CLOUD_LOCATION:-global}"
VERIFY_MODEL="${VERIFY_MODEL:-}"
MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-config/models.json}"
MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-true}"
ALLOWED_MODELS="${ALLOWED_MODELS:-}"
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
HAS_TTY=0

usage() {
  cat <<'EOF'
Usage:
  make setup PROJECT=my-gcp-project

This script is an internal runner for make setup. Prefer the Make commands below.

Make options:
  PROJECT=PROJECT_ID       Google Cloud project ID.
  LOCATION=LOCATION        Vertex AI location, default: global.
  API_KEY=KEY              Gateway API key for local calls.
  VERIFY_MODEL=MODEL       Model used for setup verification/examples.
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
      VERIFY_MODEL="${2:-}"
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

if [ -r /dev/tty ] && [ -w /dev/tty ]; then
  HAS_TTY=1
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
  if [ "$HAS_TTY" -ne 1 ]; then
    fail "Interactive setup needs a terminal. Rerun from a terminal or use NON_INTERACTIVE=1."
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
    fail "Interactive setup needs a terminal for selection. Rerun from a terminal or use NON_INTERACTIVE=1."
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

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

catalog_models() {
  if [ -f "$MODEL_CATALOG_PATH" ]; then
    awk -F'"' '/"id"[[:space:]]*:/ {print $4}' "$MODEL_CATALOG_PATH" | paste -sd, -
    return
  fi
  printf '%s' "$ALLOWED_MODELS"
}

first_allowed_model() {
  if [ -z "$ALLOWED_MODELS" ]; then
    ALLOWED_MODELS="$(catalog_models)"
  fi
  local first="${ALLOWED_MODELS%%,*}"
  trim "$first"
}

first_api_key() {
  local first="${API_KEYS%%,*}"
  trim "$first"
}

choose_model() {
  local title="${1:-Choose model}"
  if [ -n "$VERIFY_MODEL" ]; then
    printf '%s' "$VERIFY_MODEL"
    return
  fi

  local models=()
  local part
  IFS=',' read -r -a models <<< "$ALLOWED_MODELS"
  for i in "${!models[@]}"; do
    models[$i]="$(trim "${models[$i]}")"
  done

  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    first_allowed_model
    return
  fi

  local options=()
  for part in "${models[@]}"; do
    if [ -n "$part" ]; then
      options+=("$part")
    fi
  done
  options+=("Enter another Gemini model ID")

  local choice
  choice="$(select_menu "$title" 0 "${options[@]}")"
  if [ "$choice" -eq "$((${#options[@]} - 1))" ]; then
    prompt "Model ID" "$(first_allowed_model)"
    return
  fi
  printf '%s' "${options[$choice]}"
}

run_foreground() {
  local label="$1"
  shift
  if [ "$HAS_TTY" -ne 1 ]; then
    fail "$label needs an interactive terminal."
  fi
  echo "  $label"
  "$@" </dev/tty >/dev/tty 2>&1
  ok "$label"
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

ensure_command_on_path() {
  local cmd="$1"
  shift
  if command -v "$cmd" >/dev/null 2>&1; then
    return 0
  fi
  local candidate
  for candidate in "$@"; do
    if [ -x "$candidate" ]; then
      local dir
      dir="$(dirname "$candidate")"
      export PATH="$dir:$PATH"
      ok "Added $dir to PATH for this setup run"
      persist_path "$dir"
      return 0
    fi
  done
  return 1
}

persist_path() {
  local dir="$1"
  local shell_name
  shell_name="$(basename "${SHELL:-}")"
  local line="export PATH=\"$dir:\$PATH\""
  local files=()
  case "$shell_name" in
    zsh) files=("$HOME/.zshrc" "$HOME/.zprofile") ;;
    bash) files=("$HOME/.bashrc" "$HOME/.bash_profile") ;;
    *) files=("$HOME/.profile") ;;
  esac
  local rc
  for rc in "${files[@]}"; do
    if [ -f "$rc" ] && grep -F "$line" "$rc" >/dev/null 2>&1; then
      continue
    fi
    printf '\n# Added by go-llm-gateway setup for Google Cloud CLI\n%s\n' "$line" >> "$rc"
    ok "Persisted $dir in $rc"
  done
}

link_gcloud_shim() {
  local src="$1"
  local dir
  for dir in /opt/homebrew/bin /usr/local/bin; do
    if [ -d "$dir" ] && [ -w "$dir" ] && printf ':%s:' "$PATH" | grep -F ":$dir:" >/dev/null 2>&1; then
      ln -sf "$src" "$dir/gcloud"
      ok "Linked gcloud into $dir"
      return 0
    fi
  done
  return 1
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
  install_gcloud_macos_tarball_cmd
}

install_gcloud_macos_tarball_cmd() {
  local arch
  arch="$(uname -m)"
  local package="google-cloud-cli-darwin-x86_64.tar.gz"
  if [ "$arch" = "arm64" ]; then
    package="google-cloud-cli-darwin-arm.tar.gz"
  fi
  local install_dir="$HOME/google-cloud-sdk"
  if [ -x "$install_dir/bin/gcloud" ]; then
    return
  fi
  if [ -e "$install_dir" ]; then
    warn "$install_dir exists but does not contain bin/gcloud."
    return 1
  fi
  local tmpdir
  tmpdir="$(mktemp -d /tmp/gcloud-install.XXXXXX)"
  trap 'rm -rf "$tmpdir"' RETURN
  (
    cd "$tmpdir"
    curl -fsSLO "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/$package"
    tar -xzf "$package"
    mv google-cloud-sdk "$install_dir"
    "$install_dir/install.sh" --quiet --path-update=false --command-completion=false --usage-reporting=false || true
  )
  test -x "$install_dir/bin/gcloud"
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
      run_quiet "Installing Google Cloud CLI" install_gcloud_macos_cmd || return 1
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
  if [ -x "$HOME/google-cloud-sdk/bin/gcloud" ]; then
    link_gcloud_shim "$HOME/google-cloud-sdk/bin/gcloud" || true
  fi
  ensure_command_on_path gcloud \
    "/opt/homebrew/bin/gcloud" \
    "/usr/local/bin/gcloud" \
    "/opt/google-cloud-cli/bin/gcloud" \
    "$HOME/google-cloud-sdk/bin/gcloud" \
    "/opt/homebrew/Caskroom/google-cloud-sdk/latest/google-cloud-sdk/bin/gcloud" \
    "/usr/local/Caskroom/google-cloud-sdk/latest/google-cloud-sdk/bin/gcloud" || true
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
  MODEL_CATALOG_PATH="${MODEL_CATALOG_PATH:-$MODEL_CATALOG_PATH}"
  MODEL_CATALOG_REFRESH_ON_START="${MODEL_CATALOG_REFRESH_ON_START:-$MODEL_CATALOG_REFRESH_ON_START}"
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

# Model behavior: services must send real Gemini model IDs or configured aliases.
MODEL_CATALOG_PATH=$MODEL_CATALOG_PATH
MODEL_CATALOG_REFRESH_ON_START=$MODEL_CATALOG_REFRESH_ON_START
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
  ensure_command_on_path gcloud \
    "/opt/homebrew/bin/gcloud" \
    "/usr/local/bin/gcloud" \
    "/opt/google-cloud-cli/bin/gcloud" \
    "$HOME/google-cloud-sdk/bin/gcloud" \
    "/opt/homebrew/share/google-cloud-sdk/bin/gcloud" \
    "/usr/local/share/google-cloud-sdk/bin/gcloud" \
    "/opt/homebrew/Caskroom/google-cloud-sdk/latest/google-cloud-sdk/bin/gcloud" \
    "/usr/local/Caskroom/google-cloud-sdk/latest/google-cloud-sdk/bin/gcloud" >/dev/null 2>&1 || true

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
}

run_tests() {
  if [ "$SKIP_TESTS" -eq 1 ]; then
    warn "Skipped tests"
    return
  fi
  mkdir -p .cache/go-build
  run_quiet "Local Go tests" env GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}" go test ./... -count=1
}

active_gcloud_account() {
  gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | head -n 1 || true
}

configured_gcloud_project() {
  gcloud config get-value project 2>/dev/null | head -n 1 || true
}

adc_is_ready() {
  gcloud auth application-default print-access-token >/dev/null 2>&1
}

authenticate_google_cloud() {
  if ! command -v gcloud >/dev/null 2>&1; then
    warn "Cannot authenticate Google Cloud because gcloud is not installed."
    return 1
  fi
  if [ "$PROJECT_ID" = "your-project-id" ]; then
    warn "Set a real PROJECT value before Google Cloud auth."
    return 1
  fi
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    warn "Skipped Google auth in non-interactive mode."
    return 0
  fi

  local active_account
  local configured_project
  local adc_status="missing"
  active_account="$(active_gcloud_account)"
  configured_project="$(configured_gcloud_project)"
  if adc_is_ready; then
    adc_status="ready"
  fi

  echo "  Account: ${active_account:-not signed in}"
  echo "  Application Default Credentials: $adc_status"
  echo "  gcloud project: ${configured_project:-not set}"
  echo "  Target project: $PROJECT_ID"

  if [ -n "$active_account" ] && [ "$adc_status" = "ready" ] && [ "$configured_project" = "$PROJECT_ID" ]; then
    ok "Google Cloud auth is ready"
    return 0
  fi

  local choice
  choice="$(select_menu "Google Cloud auth:" 0 "Run full Google auth now" "Set gcloud project only" "Skip auth" "Abort setup")"
  case "$choice" in
    0)
      run_foreground "Google account login" gcloud auth login
      run_foreground "Application Default Credentials login" gcloud auth application-default login
      run_quiet "Set gcloud project" gcloud config set project "$PROJECT_ID"
      if adc_is_ready; then
        ok "Application Default Credentials are available"
      else
        warn "Application Default Credentials are still not ready. Rerun make setup and choose Google auth."
      fi
      ;;
    1)
      run_quiet "Set gcloud project" gcloud config set project "$PROJECT_ID"
      ;;
    2)
      warn "Skipped Google auth. Vertex verification may fail until you authenticate."
      ;;
    3)
      fail "Setup aborted during Google auth."
      ;;
  esac
}

verify_vertex_access() {
  if ! command -v gcloud >/dev/null 2>&1; then
    warn "Cannot verify Vertex access because gcloud is not installed."
    return 1
  fi
  local model
  model="$(choose_model "Choose model to verify against Vertex:")"
  run_quiet "Verify Vertex access ($model)" env \
    GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
    GOOGLE_CLOUD_LOCATION="$LOCATION" \
    VERTEX_BASE_URL="$VERTEX_BASE_URL" \
    ./scripts/verify-vertex.sh "$model"
}

gateway_env() {
  env \
    GOOGLE_CLOUD_PROJECT="$PROJECT_ID" \
    GOOGLE_CLOUD_LOCATION="$LOCATION" \
    GATEWAY_API_KEYS="$API_KEYS" \
    MODEL_CATALOG_PATH="$MODEL_CATALOG_PATH" \
    MODEL_CATALOG_REFRESH_ON_START="$MODEL_CATALOG_REFRESH_ON_START" \
    ALLOW_ANY_GEMINI_MODEL="$ALLOW_ANY_GEMINI_MODEL" \
    MODEL_ALIASES="${MODEL_ALIASES:-}" \
    VERTEX_BASE_URL="$VERTEX_BASE_URL" \
    PORT="$PORT" \
    LOG_PATH="$LOG_PATH" \
    REQUEST_TIMEOUT_SECONDS="$REQUEST_TIMEOUT_SECONDS" \
    "$@"
}

run_local_smoke_test() {
  if ! command -v curl >/dev/null 2>&1; then
    warn "curl is required for local smoke tests."
    return 1
  fi

  mkdir -p "$SETUP_LOG_DIR"
  local log="$SETUP_LOG_DIR/$(date +%Y%m%d%H%M%S)-gateway-smoke.log"
  local api_key
  api_key="$(first_api_key)"
  local base_url="http://localhost:$PORT"

  echo "  Starting gateway temporarily on :$PORT"
  gateway_env go run ./cmd/gateway >"$log" 2>&1 &
  local pid=$!
  local ready=0

  for _ in $(seq 1 60); do
    if curl -fsS "$base_url/healthz" >/dev/null 2>&1; then
      ready=1
      break
    fi
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      break
    fi
    sleep 0.2
  done

  if [ "$ready" -ne 1 ]; then
    kill "$pid" >/dev/null 2>&1 || true
    warn "Gateway did not become ready. Details saved to $log"
    return 1
  fi

  if ! curl -fsS "$base_url/healthz" >/dev/null; then
    kill "$pid" >/dev/null 2>&1 || true
    warn "Health check failed. Details saved to $log"
    return 1
  fi
  if ! curl -fsS "$base_url/v1/models" -H "Authorization: Bearer $api_key" >/dev/null; then
    kill "$pid" >/dev/null 2>&1 || true
    warn "Model listing check failed. Details saved to $log"
    return 1
  fi
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
  ok "Local gateway smoke test passed"
}

print_curl_example() {
  local model
  model="$(choose_model "Choose model for curl example:")"
  local api_key
  api_key="$(first_api_key)"
  echo
  echo "curl -s http://localhost:$PORT/v1/chat/completions \\"
  echo "  -H \"Authorization: Bearer $api_key\" \\"
  echo "  -H \"Content-Type: application/json\" \\"
  echo "  -d '{\"model\":\"$model\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with only: ok\"}]}' | jq"
}

post_setup_actions() {
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    note "Run make setup without NON_INTERACTIVE=1 for guided auth, verification, and smoke-test actions."
    return
  fi

  while true; do
    local choice
    choice="$(select_menu "What do you want to do now?" 0 \
      "Verify Vertex model access" \
      "Run local gateway smoke test" \
      "Start gateway now" \
      "Show curl example" \
      "Finish")"
    case "$choice" in
      0)
        verify_vertex_access || true
        ;;
      1)
        run_local_smoke_test || true
        ;;
      2)
        echo "Starting gateway. Press Ctrl-C to stop."
        exec make run
        ;;
      3)
        print_curl_example
        ;;
      4)
        return
        ;;
    esac
  done
}

step "Go LLM Gateway Setup"
note "One path: make setup. The gateway requires every request to include a model."

load_existing_env

step "Checking Dependencies"
check_go
check_gcloud

step "Configuring Local Environment"
if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "your-project-id" ]; then
  PROJECT_ID="$(prompt "Google Cloud project ID" "your-project-id")"
fi
LOCATION="$(prompt "Vertex AI location" "$LOCATION")"
MODEL_CATALOG_PATH="$(prompt "Model catalog path" "$MODEL_CATALOG_PATH")"
if [ -z "$API_KEYS" ] || [ "$API_KEYS" = "dev-local-key" ] || [ "$API_KEYS" = "dev-local-key-change-me" ]; then
  API_KEYS="$(generate_key)"
  ok "Generated local gateway API key"
fi

write_env

if [ "$PROJECT_ID" = "your-project-id" ]; then
  warn ".env still uses the placeholder project ID. Edit GOOGLE_CLOUD_PROJECT before live Vertex calls."
fi

step "Google Cloud Auth"
authenticate_google_cloud || true

step "Running Local Verification"
run_tests

step "Setup Actions"
post_setup_actions

ok "Setup complete"
