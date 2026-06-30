#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-}"
LOCATION="${GOOGLE_CLOUD_LOCATION:-global}"
REGION="${CLOUD_RUN_REGION:-us-central1}"
SERVICE="${CLOUD_RUN_SERVICE:-llm-gateway}"
SA_NAME="${SERVICE_ACCOUNT_NAME:-llm-gateway-sa}"
SA_EMAIL="$SA_NAME@$PROJECT_ID.iam.gserviceaccount.com"

if [ -z "$PROJECT_ID" ]; then
  echo "GOOGLE_CLOUD_PROJECT is required"
  exit 1
fi

if [ -z "${GATEWAY_API_KEYS:-}" ]; then
  echo "GATEWAY_API_KEYS is required for deployment"
  exit 1
fi

gcloud config set project "$PROJECT_ID" >/dev/null

gcloud run deploy "$SERVICE" \
  --source . \
  --region "$REGION" \
  --service-account "$SA_EMAIL" \
  --allow-unauthenticated \
  --set-env-vars "GOOGLE_CLOUD_PROJECT=$PROJECT_ID,GOOGLE_CLOUD_LOCATION=$LOCATION,GATEWAY_API_KEYS=$GATEWAY_API_KEYS,ALLOWED_MODELS=${ALLOWED_MODELS:-gemini-3.1-pro-preview,gemini-3.1-pro-preview-customtools,gemini-3-flash-preview},ALLOW_ANY_GEMINI_MODEL=${ALLOW_ANY_GEMINI_MODEL:-false},VERTEX_BASE_URL=${VERTEX_BASE_URL:-https://aiplatform.googleapis.com},LOG_PATH=/tmp/requests.jsonl,REQUEST_TIMEOUT_SECONDS=${REQUEST_TIMEOUT_SECONDS:-180}"
