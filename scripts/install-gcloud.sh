#!/usr/bin/env bash
set -euo pipefail

PREFIX="${BYTO_GCLOUD_INSTALL_PREFIX:-$HOME/google-cloud-sdk}"
DRY_RUN="${BYTO_GCLOUD_INSTALL_DRY_RUN:-0}"
LOG_PATH="${BYTO_GCLOUD_INSTALL_LOG:-}"

log() {
  if [ -n "$LOG_PATH" ]; then
    printf '%s\n' "$*" >> "$LOG_PATH"
  fi
}

os_name() {
  if [ -n "${BYTO_TEST_OS:-}" ]; then
    printf '%s' "$BYTO_TEST_OS"
    return
  fi
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT) printf 'windows' ;;
    *) uname -s | tr '[:upper:]' '[:lower:]' ;;
  esac
}

sudo_cmd() {
  if [ "$(id -u 2>/dev/null || echo 1)" -eq 0 ]; then
    "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo "$@"
    return
  fi
  echo "This install step needs root privileges, but sudo is not available." >&2
  return 1
}

write_fake_gcloud() {
  mkdir -p "$PREFIX/bin"
  cat > "$PREFIX/bin/gcloud" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

log="${FAKE_GCLOUD_LOG:-}"
if [ -n "$log" ]; then
  printf '%q ' "$@" >> "$log"
  printf '\n' >> "$log"
fi

write_key() {
  local path="$1"
  local email="$2"
  mkdir -p "$(dirname "$path")"
  cat > "$path" <<EOF_KEY
{
  "type": "service_account",
  "project_id": "ci-project",
  "private_key_id": "fake-key-id",
  "private_key": "-----BEGIN PRIVATE KEY-----\\nFAKE\\n-----END PRIVATE KEY-----\\n",
  "client_email": "$email",
  "client_id": "1234567890",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/$email",
  "universe_domain": "googleapis.com"
}
EOF_KEY
}

case "${1:-}" in
  --version|version)
    echo "Google Cloud SDK fake-ci"
    exit 0
    ;;
  config)
    if [ "${2:-}" = "set" ] && [ "${3:-}" = "project" ]; then exit 0; fi
    ;;
  auth)
    if [ "${2:-}" = "list" ]; then echo "ci-user@example.com"; exit 0; fi
    if [ "${2:-}" = "login" ]; then exit 0; fi
    if [ "${2:-}" = "application-default" ] && [ "${3:-}" = "print-access-token" ]; then echo "fake-token"; exit 0; fi
    ;;
  services)
    if [ "${2:-}" = "enable" ]; then exit 0; fi
    ;;
  iam)
    if [ "${2:-}" = "service-accounts" ] && [ "${3:-}" = "describe" ]; then exit 1; fi
    if [ "${2:-}" = "service-accounts" ] && [ "${3:-}" = "create" ]; then exit 0; fi
    if [ "${2:-}" = "service-accounts" ] && [ "${3:-}" = "keys" ] && [ "${4:-}" = "create" ]; then
      key_path="${5:-}"
      iam_account=""
      shift 5 || true
      for arg in "$@"; do
        case "$arg" in
          --iam-account=*) iam_account="${arg#--iam-account=}" ;;
        esac
      done
      write_key "$key_path" "$iam_account"
      exit 0
    fi
    ;;
  projects)
    if [ "${2:-}" = "add-iam-policy-binding" ]; then exit 0; fi
    if [ "${2:-}" = "get-ancestors" ]; then echo "organization 123456789"; exit 0; fi
    ;;
  resource-manager)
    if [ "${2:-}" = "org-policies" ]; then exit 0; fi
    ;;
  organizations)
    if [ "${2:-}" = "add-iam-policy-binding" ]; then exit 0; fi
    ;;
esac

echo "fake gcloud: unsupported command: $*" >&2
exit 2
EOF
  chmod +x "$PREFIX/bin/gcloud"
}

dry_run_install() {
  local os
  os="$(os_name)"
  log "dry-run os=$os"
  case "$os" in
    darwin) log "installer=macos-tarball" ;;
    linux) log "installer=linux" ;;
    windows) log "installer=windows" ;;
    *) log "installer=unsupported" ;;
  esac
  log "dry-run prefix=$PREFIX"
  write_fake_gcloud
}

install_macos() {
  local arch package tmpdir
  arch="$(uname -m)"
  package="google-cloud-cli-darwin-x86_64.tar.gz"
  if [ "$arch" = "arm64" ]; then
    package="google-cloud-cli-darwin-arm.tar.gz"
  fi
  log "installer=macos-tarball package=$package"
  if [ -x "$PREFIX/bin/gcloud" ]; then
    return
  fi
  tmpdir="$(mktemp -d /tmp/gcloud-install.XXXXXX)"
  trap 'rm -rf "$tmpdir"' RETURN
  (
    cd "$tmpdir"
    curl -fsSLO "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/$package"
    tar -xzf "$package"
    mv google-cloud-sdk "$PREFIX"
    "$PREFIX/install.sh" --quiet --path-update=false --command-completion=false --usage-reporting=false || true
  )
}

install_linux() {
  if command -v apt-get >/dev/null 2>&1; then
    log "installer=linux-apt"
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
    return
  fi
  if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    local installer="dnf"
    command -v dnf >/dev/null 2>&1 || installer="yum"
    log "installer=linux-$installer"
    sudo_cmd "$installer" install -y google-cloud-cli
    return
  fi
  if command -v snap >/dev/null 2>&1; then
    log "installer=linux-snap"
    sudo_cmd snap install google-cloud-cli --classic
    return
  fi
  echo "No supported Linux package manager found for automatic Google Cloud CLI install." >&2
  return 1
}

install_windows() {
  if command -v winget >/dev/null 2>&1; then
    log "installer=windows-winget"
    winget install --id Google.CloudSDK --exact --silent --accept-source-agreements --accept-package-agreements
    return
  fi
  if command -v choco >/dev/null 2>&1; then
    log "installer=windows-choco"
    choco install gcloudsdk -y --no-progress
    return
  fi
  log "installer=windows-powershell"
  powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
    "Invoke-WebRequest -Uri https://dl.google.com/dl/cloudsdk/channels/rapid/GoogleCloudSDKInstaller.exe -OutFile \$env:TEMP\\GoogleCloudSDKInstaller.exe; Start-Process \$env:TEMP\\GoogleCloudSDKInstaller.exe -ArgumentList '/S' -Wait"
}

main() {
  if [ "$DRY_RUN" = "1" ] || [ "$DRY_RUN" = "true" ]; then
    dry_run_install
    exit 0
  fi
  case "$(os_name)" in
    darwin) install_macos ;;
    linux) install_linux ;;
    windows) install_windows ;;
    *)
      echo "Automatic Google Cloud CLI install is not supported on $(uname -s)." >&2
      exit 1
      ;;
  esac
}

main "$@"
