#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT_DIR/VERSION"
POLICY_URL="${BYTO_VERSION_POLICY_URL:-https://raw.githubusercontent.com/trfhgx/vertex-gemini-openai-gateway/main/.github/version-policy.json}"
SKIP="${BYTO_SKIP_VERSION_CHECK:-${SKIP_VERSION_CHECK:-0}}"
NON_INTERACTIVE="${NON_INTERACTIVE:-0}"
HAS_TTY=0
if [ -t 0 ] && [ -r /dev/tty ]; then
  HAS_TTY=1
fi

RESET="$(printf '\033[0m')"
BOLD="$(printf '\033[1m')"
DIM="$(printf '\033[2m')"
RED="$(printf '\033[31m')"
YELLOW="$(printf '\033[33m')"
CYAN="$(printf '\033[36m')"
CLEAR_LINE="$(printf '\033[2K')"

fail() {
  echo "${RED}error:${RESET} $*" >&2
  exit 1
}

warn() {
  echo "${YELLOW}warning:${RESET} $*" >&2
}

select_menu() {
  local title="$1"
  shift
  local selected="$1"
  shift
  local options=("$@")
  local key=""
  local i

  if [ "$NON_INTERACTIVE" = "1" ] || [ "$NON_INTERACTIVE" = "true" ] || [ "$NON_INTERACTIVE" = "yes" ]; then
    printf '%s' "$selected"
    return
  fi
  if [ "$HAS_TTY" -ne 1 ]; then
    printf '%s' "$selected"
    return
  fi

  printf '%s\n' "$title" >/dev/tty
  tput civis >/dev/tty 2>/dev/null || true
  trap 'tput cnorm >/dev/tty 2>/dev/null || true' RETURN
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

version_lt() {
  python3 - "$1" "$2" <<'PY'
import re
import sys

def parts(value):
    out = []
    for token in re.split(r"[.+_-]", value.lstrip("v")):
        if token.isdigit():
            out.append(int(token))
        else:
            out.append(token)
    return out

left = parts(sys.argv[1])
right = parts(sys.argv[2])
limit = max(len(left), len(right))
left.extend([0] * (limit - len(left)))
right.extend([0] * (limit - len(right)))
sys.exit(0 if left < right else 1)
PY
}

fetch_policy() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 2 --max-time 5 "$POLICY_URL"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO- --timeout=5 "$POLICY_URL"
    return
  fi
  return 1
}

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin).get(sys.argv[1], ""))' "$1"
}

json_contains_deprecated() {
  python3 -c 'import json,sys; p=json.load(sys.stdin); sys.exit(0 if sys.argv[1] in p.get("deprecated", []) else 1)' "$1"
}

json_contains_obsolete() {
  python3 -c 'import json,sys; p=json.load(sys.stdin); sys.exit(0 if sys.argv[1] in p.get("obsolete", []) else 1)' "$1"
}

run_update() {
  local update_command="$1"
  if [ ! -d "$ROOT_DIR/.git" ]; then
    fail "This checkout is not a git repository. Download the latest Byto Gateway manually."
  fi
  echo "${BOLD}Updating Byto Gateway...${RESET}"
  (cd "$ROOT_DIR" && sh -c "$update_command")
}

if [ "$SKIP" = "1" ] || [ "$SKIP" = "true" ] || [ "$SKIP" = "yes" ]; then
  exit 0
fi

if [ ! -f "$VERSION_FILE" ]; then
  warn "VERSION file is missing; skipping remote version policy check."
  exit 0
fi

if ! command -v python3 >/dev/null 2>&1; then
  warn "python3 is not installed; skipping remote version policy check."
  exit 0
fi

LOCAL_VERSION="$(tr -d '[:space:]' <"$VERSION_FILE")"
if [ -z "$LOCAL_VERSION" ]; then
  warn "VERSION is empty; skipping remote version policy check."
  exit 0
fi

if ! POLICY_JSON="$(fetch_policy)"; then
  warn "Could not reach GitHub version policy; continuing with local version $LOCAL_VERSION."
  exit 0
fi

LATEST="$(printf '%s' "$POLICY_JSON" | json_field latest)"
DEPRECATED_BELOW="$(printf '%s' "$POLICY_JSON" | json_field deprecated_below)"
OBSOLETE_BELOW="$(printf '%s' "$POLICY_JSON" | json_field obsolete_below)"
MESSAGE="$(printf '%s' "$POLICY_JSON" | json_field message)"
UPDATE_COMMAND="$(printf '%s' "$POLICY_JSON" | json_field update_command)"
if [ -z "$UPDATE_COMMAND" ]; then
  UPDATE_COMMAND="git pull --ff-only origin main"
fi

IS_OBSOLETE=0
IS_EXACT_OBSOLETE=0
if [ -n "$OBSOLETE_BELOW" ] && version_lt "$LOCAL_VERSION" "$OBSOLETE_BELOW"; then
  IS_OBSOLETE=1
fi
if printf '%s' "$POLICY_JSON" | json_contains_obsolete "$LOCAL_VERSION"; then
  IS_OBSOLETE=1
  IS_EXACT_OBSOLETE=1
fi

if [ "$IS_OBSOLETE" -eq 1 ]; then
  echo >&2
  echo "${RED}${BOLD}================================================================${RESET}" >&2
  echo "${RED}${BOLD}  THIS BYTO GATEWAY VERSION IS OBSOLETE AND MUST BE UPDATED${RESET}" >&2
  echo "${RED}${BOLD}================================================================${RESET}" >&2
  echo >&2
  echo "Installed: $LOCAL_VERSION" >&2
  if [ "$IS_EXACT_OBSOLETE" -eq 1 ]; then
    echo "Blocked:   this exact release is marked obsolete" >&2
  elif [ -n "$OBSOLETE_BELOW" ]; then
    echo "Required:  $OBSOLETE_BELOW or newer" >&2
  fi
  [ -n "$LATEST" ] && echo "Latest:    $LATEST" >&2
  [ -n "$MESSAGE" ] && echo >&2 && echo "$MESSAGE" >&2
  echo >&2
  run_update "$UPDATE_COMMAND"
  exit 0
fi

IS_DEPRECATED=0
if [ -n "$DEPRECATED_BELOW" ] && version_lt "$LOCAL_VERSION" "$DEPRECATED_BELOW"; then
  IS_DEPRECATED=1
fi
if printf '%s' "$POLICY_JSON" | json_contains_deprecated "$LOCAL_VERSION"; then
  IS_DEPRECATED=1
fi

if [ "$IS_DEPRECATED" -eq 1 ]; then
  echo >&2
  echo "${YELLOW}${BOLD}================================================================${RESET}" >&2
  echo "${YELLOW}${BOLD}  THIS BYTO GATEWAY VERSION IS DEPRECATED${RESET}" >&2
  echo "${YELLOW}${BOLD}================================================================${RESET}" >&2
  echo >&2
  echo "Installed: $LOCAL_VERSION" >&2
  [ -n "$DEPRECATED_BELOW" ] && echo "Recommended: $DEPRECATED_BELOW or newer" >&2
  [ -n "$LATEST" ] && echo "Latest:    $LATEST" >&2
  [ -n "$MESSAGE" ] && echo >&2 && echo "$MESSAGE" >&2
  echo >&2

  if [ "$HAS_TTY" -eq 1 ] && [ "$NON_INTERACTIVE" != "1" ] && [ "$NON_INTERACTIVE" != "true" ] && [ "$NON_INTERACTIVE" != "yes" ]; then
    choice="$(select_menu "Update now?" 0 "Update now" "Continue without updating")"
    if [ "$choice" = "0" ]; then
      run_update "$UPDATE_COMMAND"
    fi
  else
    warn "Non-interactive shell; continuing without update. Run: $UPDATE_COMMAND"
  fi
fi
