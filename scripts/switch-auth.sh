#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

AUTH_MODE="${AUTH:-${MODE:-}}"
KEY_PATH="${KEY_PATH:-secrets/llm-gateway-sa.json}"
VERIFY_MODEL="${MODEL:-${VERIFY_MODEL:-}}"
NON_INTERACTIVE=0
SETUP_LOG_DIR="$ROOT_DIR/.cache/setup"
HAS_TTY=0

usage() {
  cat <<'EOF'
Usage:
  make switch
  make switch AUTH=service MODEL=gemini-2.5-flash
  make switch AUTH=token MODEL=gemini-2.5-flash

Options:
  AUTH=service          Use GOOGLE_APPLICATION_CREDENTIALS and clear VERTEX_ACCESS_TOKEN.
  AUTH=token            Use a freshly minted gcloud access token and clear GOOGLE_APPLICATION_CREDENTIALS.
  KEY_PATH=PATH         Service account JSON path for service mode.
  MODEL=MODEL           Optional live gateway verification model.
  NON_INTERACTIVE=1     Do not prompt.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --auth) AUTH_MODE="${2:-}"; shift 2 ;;
    --key-path) KEY_PATH="${2:-}"; shift 2 ;;
    --model) VERIFY_MODEL="${2:-}"; shift 2 ;;
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
    fail "Interactive switch needs a terminal. Use AUTH=service or AUTH=token."
  fi

  printf '%s\n' "$title" >/dev/tty
  tput civis >/dev/tty 2>/dev/null || true
  trap 'tput cnorm >/dev/tty 2>/dev/null || true' EXIT
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
      trap - EXIT
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
      printf '\r%s  %s %s\n' "$CLEAR_LINE" "${GREEN}OK${RESET}" "$label"
    else
      echo "OK $label"
    fi
    return
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

select_mode() {
  if [ -n "$AUTH_MODE" ]; then
    return
  fi
  if [ "$NON_INTERACTIVE" -eq 1 ]; then
    fail "AUTH is required in non-interactive mode. Use AUTH=service or AUTH=token."
  fi
  local selected=0
  if [ -z "${GOOGLE_APPLICATION_CREDENTIALS:-}" ] && [ -n "${VERTEX_ACCESS_TOKEN:-}" ]; then
    selected=1
  fi
  selected="$(select_menu "Choose gateway Vertex auth mode" "$selected" "Service account JSON" "gcloud access token")"
  case "$selected" in
    0) AUTH_MODE=service ;;
    1) AUTH_MODE=token ;;
    *) fail "Unknown selection: $selected" ;;
  esac
}

load_env() {
  if [ ! -f .env ]; then
    fail ".env not found. Run make setup or make setup production first."
  fi
  set -a
  # shellcheck disable=SC1091
  source ./.env
  set +a
}

set_env_var() {
  local key="$1"
  local value="$2"
  if grep -q "^$key=" .env; then
    python3 - "$key" "$value" <<'PY'
import sys
from pathlib import Path
key, value = sys.argv[1], sys.argv[2]
p = Path(".env")
lines = p.read_text().splitlines()
for i, line in enumerate(lines):
    if line.startswith(key + "="):
        lines[i] = f"{key}={value}"
        break
p.write_text("\n".join(lines) + "\n")
PY
  else
    printf '%s=%s\n' "$key" "$value" >> .env
  fi
}

absolute_key_path() {
  case "$KEY_PATH" in
    /*) printf '%s' "$KEY_PATH" ;;
    *) printf '%s/%s' "$ROOT_DIR" "$KEY_PATH" ;;
  esac
}

verify_gateway() {
  if [ -z "$VERIFY_MODEL" ]; then
    warn "Skipped live verification because MODEL was not provided"
    return
  fi
  run_quiet "Verify gateway auth mode" env \
    GOOGLE_CLOUD_PROJECT="$GOOGLE_CLOUD_PROJECT" \
    GOOGLE_CLOUD_LOCATION="${GOOGLE_CLOUD_LOCATION:-global}" \
    GATEWAY_API_KEYS="$GATEWAY_API_KEYS" \
    GATEWAY_ALLOW_UNAUTHENTICATED="${GATEWAY_ALLOW_UNAUTHENTICATED:-false}" \
    GOOGLE_APPLICATION_CREDENTIALS="${GOOGLE_APPLICATION_CREDENTIALS:-}" \
    VERTEX_ACCESS_TOKEN="${VERTEX_ACCESS_TOKEN:-}" \
    MODEL_CATALOG_PATH= \
    ALLOW_ANY_GEMINI_MODEL=true \
    VERTEX_BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}" \
    REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}" \
    VERTEX_RETRY_MAX_ATTEMPTS="${VERTEX_RETRY_MAX_ATTEMPTS:-3}" \
    VERTEX_RETRY_INITIAL_MS="${VERTEX_RETRY_INITIAL_MS:-250}" \
    VERTEX_RETRY_MAX_MS="${VERTEX_RETRY_MAX_MS:-2000}" \
    RUN_LIVE_VERTEX_TESTS=1 \
    LIVE_VERTEX_MODEL="$VERIFY_MODEL" \
    GOCACHE="$ROOT_DIR/.cache/go-build" \
    go test ./test/e2e -run TestLiveVertexGenerateExplicitModel -count=1 -v
}

load_env
select_mode

step "Switch Vertex Auth"
case "$AUTH_MODE" in
  service|service-account|service_account)
    if [ ! -s "$KEY_PATH" ]; then
      fail "Service account key not found at $KEY_PATH. Run make setup production first or pass KEY_PATH=..."
    fi
    set_env_var GOOGLE_APPLICATION_CREDENTIALS "$KEY_PATH"
    set_env_var VERTEX_ACCESS_TOKEN ""
    export GOOGLE_APPLICATION_CREDENTIALS
    GOOGLE_APPLICATION_CREDENTIALS="$(absolute_key_path)"
    export VERTEX_ACCESS_TOKEN=""
    ok "Active auth mode: service account"
    note "GOOGLE_APPLICATION_CREDENTIALS=$KEY_PATH"
    ;;
  token|access-token|access_token)
    if ! command -v gcloud >/dev/null 2>&1; then
      fail "gcloud is required to mint VERTEX_ACCESS_TOKEN"
    fi
    token="$(gcloud auth application-default print-access-token)"
    if [ -z "$token" ]; then
      fail "Could not mint gcloud ADC access token"
    fi
    set_env_var VERTEX_ACCESS_TOKEN "$token"
    set_env_var GOOGLE_APPLICATION_CREDENTIALS ""
    export VERTEX_ACCESS_TOKEN="$token"
    export GOOGLE_APPLICATION_CREDENTIALS=""
    ok "Active auth mode: access token"
    warn "Access tokens expire. Use service account mode for durable Docker/VPS production."
    ;;
  *)
    fail "Unknown AUTH mode: $AUTH_MODE. Use AUTH=service or AUTH=token."
    ;;
esac

verify_gateway

step "Done"
ok "Auth switch complete"
