# Byto

<p align="center">
  <img src="assets/byto_banner.jpg" alt="Byto API Gateway Banner" width="100%" />
</p>

Byto is a Go gateway that exposes an OpenAI-compatible API for Vertex AI Gemini. It gives your apps one internal LLM endpoint while keeping model selection explicit.

```text
your apps -> Byto -> Vertex AI Gemini
```

## What It Does

- Serves `POST /v1/chat/completions`
- Serves `GET /v1/models`
- Serves `GET /healthz`
- Requires service API keys with `Authorization: Bearer ...`
- Requires callers to send a model on every completion request
- Translates OpenAI-style chat payloads to Vertex Gemini `generateContent`
- Supports streaming responses with server-sent events
- Writes JSONL request logs
- Refreshes the local Gemini model catalog from Vertex on startup

Full endpoint documentation lives in [docs/API.md](docs/API.md).

## Requirements

- Go 1.22+
- Google Cloud CLI
- Docker, for cloud setup and container builds
- A Google Cloud project that can call Vertex AI

## Local Setup

Use one command:

```bash
make setup PROJECT=your-project-id
```

The setup flow checks dependencies, writes `.env`, guides Google auth, sets the ADC quota project, enables Vertex AI, runs local verification, and then shows an interactive action menu.

Run the gateway:

```bash
make run
```

Call it:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{ "role": "user", "content": "Reply with only: ok" }]
  }' | jq
```

## Cloud Setup

Use the cloud path when you want Docker and Cloud Run readiness:

```bash
make setup-cloud PROJECT=your-project-id MODEL=gemini-2.5-flash
```

Equivalent alias:

```bash
make setup cloud PROJECT=your-project-id MODEL=gemini-2.5-flash
```

Cloud setup checks Google auth, checks Docker, writes `.env`, enables the required Google Cloud APIs, creates the Cloud Run service account, grants IAM roles, builds the Docker image, and runs live Vertex e2e verification.

Deploy during setup:

```bash
make setup-cloud PROJECT=your-project-id MODEL=gemini-2.5-flash DEPLOY=1
```

## Configuration

The runtime reads `.env` automatically through the Makefile.

```bash
GOOGLE_CLOUD_PROJECT=your-project-id
GOOGLE_CLOUD_LOCATION=global
GATEWAY_API_KEYS=comma,separated,service,keys
MODEL_CATALOG_PATH=config/models.json
MODEL_CATALOG_REFRESH_ON_START=true
ALLOW_ANY_GEMINI_MODEL=false
VERTEX_BASE_URL=https://aiplatform.googleapis.com
PORT=8080
LOG_PATH=logs/requests.jsonl
REQUEST_TIMEOUT_SECONDS=180
```

## Models

There is no default model. If `model` is missing or empty, Byto returns `400`.

Byto resolves models in this order:

1. Reject empty `model`.
2. Apply `MODEL_ALIASES`, if configured.
3. Accept enabled and available models from `config/models.json`.
4. If `ALLOW_ANY_GEMINI_MODEL=true`, accept any resolved model that starts with `gemini-`.
5. Reject everything else.

`config/models.json` stores model metadata such as enabled state, live availability, supported actions, and reasoning-effort tiers. Startup refresh adds newly discovered Vertex Gemini models as disabled entries for review.

## Logs

Default request log path:

```text
logs/requests.jsonl
```

Each JSONL record includes request ID, optional `X-App-ID`, requested model, resolved Vertex model, stream flag, HTTP status, latency, token counts, and error text when present.
