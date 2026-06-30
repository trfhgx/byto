#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${GOOGLE_CLOUD_PROJECT:-}"
LOCATION="${GOOGLE_CLOUD_LOCATION:-global}"
SERVICE_ACCOUNT_NAME="${SERVICE_ACCOUNT_NAME:-llm-gateway-sa}"
SERVICE_ACCOUNT_DISPLAY="LLM Gateway Service Account"

if [ -z "$PROJECT_ID" ]; then
  echo "GOOGLE_CLOUD_PROJECT is required"
  echo "Example: export GOOGLE_CLOUD_PROJECT=my-project-id"
  exit 1
fi

if ! command -v gcloud >/dev/null 2>&1; then
  echo "gcloud CLI is required. Install Google Cloud SDK first."
  exit 1
fi

echo "Using project: $PROJECT_ID"
gcloud config set project "$PROJECT_ID" >/dev/null

echo "Enabling required APIs..."
gcloud services enable \
  aiplatform.googleapis.com \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com \
  iam.googleapis.com \
  iamcredentials.googleapis.com \
  logging.googleapis.com \
  monitoring.googleapis.com \
  serviceusage.googleapis.com

SA_EMAIL="$SERVICE_ACCOUNT_NAME@$PROJECT_ID.iam.gserviceaccount.com"

if gcloud iam service-accounts describe "$SA_EMAIL" >/dev/null 2>&1; then
  echo "Service account already exists: $SA_EMAIL"
else
  echo "Creating service account: $SA_EMAIL"
  gcloud iam service-accounts create "$SERVICE_ACCOUNT_NAME" \
    --display-name="$SERVICE_ACCOUNT_DISPLAY"
fi

echo "Granting IAM roles..."
for ROLE in \
  roles/aiplatform.user \
  roles/logging.logWriter \
  roles/monitoring.metricWriter \
  roles/serviceusage.serviceUsageConsumer; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$SA_EMAIL" \
    --role="$ROLE" \
    --quiet >/dev/null
  echo "Granted $ROLE"
done

cat > .env.generated <<EOF_ENV
GOOGLE_CLOUD_PROJECT=$PROJECT_ID
GOOGLE_CLOUD_LOCATION=$LOCATION
GATEWAY_API_KEYS=dev-local-key-change-me
ALLOWED_MODELS=gemini-3.1-pro-preview,gemini-3.1-pro-preview-customtools,gemini-3-flash-preview
ALLOW_ANY_GEMINI_MODEL=false
VERTEX_BASE_URL=https://aiplatform.googleapis.com
LOG_PATH=logs/requests.jsonl
REQUEST_TIMEOUT_SECONDS=180
EOF_ENV

echo "Wrote .env.generated"
echo "Service account: $SA_EMAIL"
echo "Local auth: run 'gcloud auth application-default login'"
echo "Verify: make verify-gcp MODEL=gemini-3.1-pro-preview"
