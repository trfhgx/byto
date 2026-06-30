#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

if [ ! -f .env ]; then
  cp .env.example .env
  echo "Created .env from .env.example"
else
  echo ".env already exists"
fi

echo "Checking Go..."
if ! command -v go >/dev/null 2>&1; then
  echo "Go is not installed. Install Go 1.22+ first."
  exit 1
fi

echo "Running tests..."
go test ./... -count=1

echo "Local setup complete."
echo "Next: edit .env, then run: make run"
echo "For GCP automation: export GOOGLE_CLOUD_PROJECT=your-project-id && ./scripts/setup-gcp.sh"
