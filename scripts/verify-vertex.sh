#!/usr/bin/env bash
set -euo pipefail

MODEL="${1:-}"
PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-}"
LOCATION="${GOOGLE_CLOUD_LOCATION:-global}"
BASE_URL="${VERTEX_BASE_URL:-https://aiplatform.googleapis.com}"

if [ -z "$PROJECT_ID" ]; then
  echo "GOOGLE_CLOUD_PROJECT is required"
  exit 1
fi

if [ -z "$MODEL" ]; then
  echo "model argument is required"
  echo "Usage: make verify-gcp MODEL=gemini-3.1-pro-preview"
  exit 1
fi

TOKEN="${VERTEX_ACCESS_TOKEN:-}"
if [ -z "$TOKEN" ]; then
  TOKEN="$(gcloud auth application-default print-access-token)"
fi

URL="$BASE_URL/v1/projects/$PROJECT_ID/locations/$LOCATION/publishers/google/models/$MODEL:generateContent"

echo "Calling Vertex model: $MODEL"
echo "Location: $LOCATION"

curl -sS -X POST "$URL" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Reply with only: ok"}]}]}' | python3 -m json.tool
