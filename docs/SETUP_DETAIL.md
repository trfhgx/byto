# Setup Detail

This document keeps the deeper setup notes out of the README.

## Requirements

- Go 1.22+
- Google Cloud CLI
- Docker, if you want container/cloud deploys
- A Google Cloud project that can call Vertex AI

## Local Interactive Setup

```bash
make setup
```

The local setup flow checks dependencies, installs or guides `gcloud` when needed, writes `.env`, helps with Google auth, sets the ADC quota project, enables Vertex AI, verifies the gateway, and shows an interactive action menu.

During setup, choose one gateway access mode:

- `Protect with API key`: setup generates an API key, writes it to `.env`, and copies it to your clipboard.
- `Open access`: `/v1/*` endpoints accept requests without `Authorization`. Use this only behind private networking, Cloud Run IAM, or another trusted access layer.

## Production Service Account Setup

```bash
make setup production
```

On Windows, use the native PowerShell setup:

```powershell
.\scripts\setup-production-windows.ps1
```

Production setup creates or reuses a Google service account, grants Vertex access, creates an ignored key under `secrets/`, writes `.env`, copies the gateway API key to your clipboard, and can verify the gateway through the Go server.

The production path is the right default for Docker, VPS, and other long-running server deployments because service-account auth does not expire like a local access token.

If Google Cloud CLI is missing, interactive setup offers to install it.

## Switch Vertex Auth Mode

```bash
make switch
```

Interactive mode lets you choose:

- `service account JSON`: uses `GOOGLE_APPLICATION_CREDENTIALS` and clears `VERTEX_ACCESS_TOKEN`.
- `gcloud access token`: writes a fresh `VERTEX_ACCESS_TOKEN` and clears `GOOGLE_APPLICATION_CREDENTIALS`.

The service-account mode is durable. The access-token mode is useful for short local checks.

## Cloud Run Setup

```bash
make setup-cloud
```

Cloud setup checks Google auth, checks Docker, writes `.env`, enables required APIs, creates a Cloud Run service account, grants IAM roles, builds the Docker image, and runs live Vertex verification.

Deploy during setup:

```bash
make setup-cloud DEPLOY=1
```

## Docker Compose

Docker Compose uses whichever auth mode is active in `.env`.

```bash
docker compose up --build
```

## Configuration

The Makefile loads `.env` automatically.

```bash
GOOGLE_CLOUD_PROJECT=your-project-id
GOOGLE_CLOUD_LOCATION=global
GATEWAY_API_KEYS=comma,separated,service,keys
GATEWAY_ALLOW_UNAUTHENTICATED=false

MODEL_CATALOG_PATH=config/models.json
MODEL_CATALOG_REFRESH_ON_START=true
ALLOW_ANY_GEMINI_MODEL=false
MODEL_ALIASES=

VERTEX_BASE_URL=https://aiplatform.googleapis.com
GOOGLE_APPLICATION_CREDENTIALS=secrets/llm-gateway-sa.json
VERTEX_ACCESS_TOKEN=

PORT=8080
LOG_PATH=logs/requests.jsonl
LOG_MAX_BYTES=104857600
REQUEST_TIMEOUT_SECONDS=180

VERTEX_RETRY_MAX_ATTEMPTS=3
VERTEX_RETRY_INITIAL_MS=250
VERTEX_RETRY_MAX_MS=2000

ADAPTIVE_CONCURRENCY_ENABLED=true
ADAPTIVE_CONCURRENCY_MIN=1
ADAPTIVE_CONCURRENCY_INITIAL=4
ADAPTIVE_CONCURRENCY_MAX=32
```

## Model Catalog

There is no default model. Requests must send `model`.

Byto resolves models in this order:

1. Reject empty `model`.
2. Apply `MODEL_ALIASES`, if configured.
3. Accept enabled and available models from `MODEL_CATALOG_PATH`.
4. If `ALLOW_ANY_GEMINI_MODEL=true`, accept any resolved model that starts with `gemini-`.
5. Reject everything else.

The catalog stores enabled state, live availability, supported actions, generation parameters, reasoning tiers, and reasoning-budget mappings. Startup refresh syncs against the current supported Google Gemini endpoint model list, marks stale IDs unavailable, and checks each supported candidate with Vertex `countTokens`. Passing candidates are enabled for your project/location; hard failures such as `404`, `403`, `401`, and `400` are disabled. Transient failures such as `429`, `5xx`, or timeout keep the previous catalog state.

## Logs

Default log path:

```text
logs/requests.jsonl
```

JSONL records include request ID, method/path/IP/user-agent for access logs, optional `X-App-ID`, requested model, resolved Vertex model, service tier, traffic type, reasoning effort, token counts, upstream status classification, and error text when present.

## CI Production Setup E2E

The repository includes `test/e2e/setup_production_fake_gcloud.sh`.

It runs the real `make setup production` path with `gcloud` forced missing. The installer runs in dry-run mode, chooses the platform-specific install path, creates a fake `gcloud` shim, and then the production setup continues. This lets CI verify the Linux and Windows installer plus shell/Makefile flow without requiring real Google credentials or creating cloud resources.

This test checks that setup:

- enters the Google Cloud CLI install path
- selects the expected installer branch for the OS
- calls the expected Google Cloud commands
- creates the service account key file
- writes `.env`
- leaves the gateway in service-account mode

Live Google verification remains separate and requires real credentials.
