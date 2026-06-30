# Go LLM Gateway for Vertex AI Gemini

Production-oriented Go gateway exposing an OpenAI-compatible API over Vertex AI Gemini.

```text
Your apps -> /v1/chat/completions -> Go Gateway -> Vertex AI Gemini
```

This project is designed for people building multiple LLM-powered apps who want one reusable internal completion URL without using LiteLLM or OpenRouter.

## What it does

- Exposes `POST /v1/chat/completions`
- Exposes `GET /v1/models`
- Exposes `GET /healthz`
- Accepts OpenAI-style chat requests
- Calls Vertex AI Gemini `generateContent`
- Supports Vertex AI `streamGenerateContent` via SSE
- Uses real Gemini model IDs such as `gemini-3.1-pro-preview`
- Supports service-level API keys
- Writes persistent JSONL logs
- Passes explicit Gemini cached content IDs through to Vertex
- Keeps prompt formatting deterministic for implicit caching

## What it does not do

- It does not manage your product users.
- It does not manage your app credits/subscriptions.
- It does not decide product limits.
- It does not use LiteLLM.
- It does not hide model selection unless you configure aliases.

Each application should own its own business logic. This gateway only owns LLM provider mechanics.

## Requirements

- Go 1.22+
- Docker, optional
- Google Cloud SDK, for GCP setup and local auth
- Google Cloud project with billing attached
- Vertex AI API enabled

## Quick local setup

```bash
make setup PROJECT=your-project-id
```

The setup flow checks Go, creates `.env`, generates a local gateway API key, runs the non-live test suite, and prints the exact verification/run/curl commands. Interactive setup uses a small arrow-key menu for optional Google Cloud CLI installation and keeps long package-manager/test output in `.cache/setup/` logs.

You can also run it interactively:

```bash
make setup
```

For CI or scripted setup:

```bash
make setup PROJECT=your-project-id NON_INTERACTIVE=1
```

If `gcloud` is not installed, interactive setup offers to install it. For scripted setup:

```bash
make setup PROJECT=your-project-id INSTALL_GCLOUD=1
```

The resulting `.env` contains:

```bash
GOOGLE_CLOUD_PROJECT=your-project-id
GOOGLE_CLOUD_LOCATION=global
GATEWAY_API_KEYS=<generated-local-key>
DEFAULT_MODEL=gemini-3.1-pro-preview
ALLOWED_MODELS=gemini-3.1-pro-preview,gemini-3.1-pro-preview-customtools,gemini-3-flash-preview
```

Then authenticate locally:

```bash
gcloud auth login
gcloud auth application-default login
gcloud config set project "$GOOGLE_CLOUD_PROJECT"
```

Run:

```bash
make run
```

`make run` automatically loads `.env`.

Test:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer dev-local-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gemini-3.1-pro-preview",
    "messages":[{"role":"user","content":"Reply with only: ok"}]
  }' | jq
```

## CLI-based GCP setup

Most tedious GCP setup is automated with `gcloud`.

```bash
export GOOGLE_CLOUD_PROJECT="your-project-id"
export GOOGLE_CLOUD_LOCATION="global"

gcloud auth login
gcloud config set project "$GOOGLE_CLOUD_PROJECT"

./scripts/setup-gcp.sh
```

The setup script enables APIs, creates a service account, grants required roles, and writes `.env.generated`.

It enables:

- Vertex AI API
- Cloud Run API
- Cloud Build API
- Artifact Registry API
- IAM API
- IAM Service Account Credentials API
- Cloud Logging API
- Cloud Monitoring API
- Service Usage API

It creates:

```text
llm-gateway-sa@YOUR_PROJECT_ID.iam.gserviceaccount.com
```

It grants:

- `roles/aiplatform.user`
- `roles/logging.logWriter`
- `roles/monitoring.metricWriter`
- `roles/serviceusage.serviceUsageConsumer`

## What still requires manual setup

Some things cannot be reliably automated without your billing/org permissions:

- Create or choose the Google Cloud project
- Attach billing / free trial credits
- Accept any model/API terms Google prompts for
- Create billing budget alerts
- Request quota increases if needed

## Verify Vertex model access

```bash
./scripts/verify-vertex.sh gemini-3.1-pro-preview
```

Gemini 3.1 Pro Preview is available on global endpoints, so use:

```bash
GOOGLE_CLOUD_LOCATION=global
```

## Production deploy to Cloud Run

Set a real app key first:

```bash
export GATEWAY_API_KEYS="replace-with-long-random-secret"
export GOOGLE_CLOUD_PROJECT="your-project-id"
export GOOGLE_CLOUD_LOCATION="global"
```

Deploy:

```bash
./scripts/cloud-run-deploy.sh
```

Cloud Run should run as:

```text
llm-gateway-sa@YOUR_PROJECT_ID.iam.gserviceaccount.com
```

No JSON service account key is needed in production.

## Docker Compose

```bash
cp .env.example .env
# edit .env
docker compose up --build
```

For local Docker, easiest auth is to set `VERTEX_ACCESS_TOKEN` manually:

```bash
export VERTEX_ACCESS_TOKEN="$(gcloud auth application-default print-access-token)"
```

Then put it in `.env` or pass it to Docker Compose.

## API

### `POST /v1/chat/completions`

Non-streaming:

```json
{
  "model": "gemini-3.1-pro-preview",
  "messages": [
    { "role": "system", "content": "You are concise." },
    { "role": "user", "content": "Say ok" }
  ]
}
```

Streaming:

```json
{
  "model": "gemini-3.1-pro-preview",
  "stream": true,
  "messages": [
    { "role": "user", "content": "Write one sentence." }
  ]
}
```

Explicit cache passthrough:

```json
{
  "model": "gemini-3.1-pro-preview",
  "messages": [
    { "role": "user", "content": "Use the cached context and summarize." }
  ],
  "extra_body": {
    "google": {
      "cached_content": "projects/PROJECT/locations/global/cachedContents/CACHE_ID"
    }
  }
}
```

### `GET /v1/models`

Returns configured allowed model IDs.

## Logs

Default log path:

```text
logs/requests.jsonl
```

Each line includes:

```json
{
  "timestamp": "...",
  "request_id": "...",
  "app_id": "...",
  "model": "gemini-3.1-pro-preview",
  "vertex_model": "gemini-3.1-pro-preview",
  "stream": false,
  "status": 200,
  "latency_ms": 123,
  "prompt_tokens": 10,
  "completion_tokens": 5,
  "total_tokens": 15,
  "cached_tokens": 8
}
```

Add `X-App-ID` from each product service to make logs easier to split by app.

## Model configuration

By default, services send real model IDs. Example:

```json
{ "model": "gemini-3.1-pro-preview" }
```

Allowed models are configured through:

```bash
ALLOWED_MODELS=gemini-3.1-pro-preview,gemini-3.1-pro-preview-customtools,gemini-3-flash-preview
```

You can allow future Gemini model IDs without redeploying code:

```bash
ALLOW_ANY_GEMINI_MODEL=true
```

Optional aliases:

```bash
MODEL_ALIASES=pro=gemini-3.1-pro-preview,tools=gemini-3.1-pro-preview-customtools
```

Aliases are optional. The recommended production path is to send real model IDs from the calling service.

## Tests

```bash
make test
make test-race
```

Optional live Vertex test:

```bash
RUN_LIVE_VERTEX_TESTS=1 make test-live
```

## Official Google docs referenced

- Gemini 3.1 Pro model IDs: https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/3-1-pro
- Gemini inference API: https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/inference
- Vertex AI service account role: https://docs.cloud.google.com/workflows/docs/tutorials/use-vertex-ai-models
- Service account creation via gcloud: https://docs.cloud.google.com/iam/docs/service-accounts-create
