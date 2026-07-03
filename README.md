<p align="center">
  <img src="assets/byto_banner.jpg" alt="Byto API Gateway Banner" width="100%" style="border-radius: 8px;" />
</p>

# Byto

<p align="center">
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blueviolet?style=flat-square" alt="License" /></a>
  <a href="https://www.docker.com"><img src="https://img.shields.io/badge/Platform-Docker%20%7C%20Cloud%20Run-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker" /></a>
  <a href="https://cloud.google.com/vertex-ai"><img src="https://img.shields.io/badge/Provider-Vertex%20Gemini-4285F4?style=flat-square&logo=google-cloud&logoColor=white" alt="Vertex AI" /></a>
</p>

Byto is a Go gateway that turns your own Vertex AI Gemini access into an OpenAI-compatible API.

```text
your apps -> Byto -> Vertex AI Gemini
```

It is built for explicit model selection, service API keys, production service-account auth, priority PayGo headers, reasoning controls, JSONL logs, and Docker/server deployments.

---

## Quick Start

Clone the repo first:

```bash
git clone https://github.com/trfhgx/vertex-gemini-openai-gateway.git
cd vertex-gemini-openai-gateway
```

Prerequisites:

- Go 1.22 or newer. Install this yourself before running the gateway.
- A Google Cloud project with Vertex AI access.
- Google Cloud CLI (`gcloud`). Interactive setup can install it for you when it is missing.
- Docker. Optional, only needed for the Docker path.
- `make`. Optional; macOS/Linux often have it or can install it easily, Windows usually does not.
- PowerShell 7 or Windows PowerShell 5.1 on Windows.

### macOS and Linux

The setup scripts use Bash. Use either the `make` commands or the direct script commands.

With `make`:

```bash
make setup
make run
```

Production service-account setup with `make`:

```bash
make setup production PROJECT=your-gcp-project MODEL=gemini-2.5-flash
```

Without `make`:

```bash
./setup.sh
go run ./cmd/gateway
./scripts/setup-production.sh --project your-gcp-project --model gemini-2.5-flash
```

### Windows

Use PowerShell. `make`, Bash, Docker, Git Bash, and WSL are not required for the
local Windows path.

Clone:

```powershell
git clone https://github.com/trfhgx/vertex-gemini-openai-gateway.git
cd vertex-gemini-openai-gateway
```

Run setup:

```powershell
.\scripts\setup-windows.ps1
```

If PowerShell blocks local scripts, run this once for your user account:

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
```

Then run:

```powershell
go run ./cmd/gateway
```

Production service-account setup still uses the Bash script today. Run it from
WSL, or configure `GOOGLE_APPLICATION_CREDENTIALS` in `.env` yourself after
creating a service-account key in Google Cloud Console.

### Docker

On any platform with Docker:

```bash
docker compose up --build
```

### Call It

List available models:

```bash
curl -s http://localhost:8080/v1/models
```

Send a chat request:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{ "role": "user", "content": "Reply with only: ok" }]
  }'
```

---

## What You Get

- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /healthz`
- OpenAI-style `model`, `messages`, `stream`, `service_tier`, and `reasoning_effort`
- Vertex cache endpoints under `/v1/caches`
- API-key gateway auth
- Durable service-account auth for production
- Startup model-catalog refresh with Vertex `countTokens` availability checks
- JSONL access/request logs with token usage, traffic type, reasoning tokens, and upstream status

Full API docs: [docs/API.md](docs/API.md)

Detailed setup docs: [docs/SETUP_DETAIL.md](docs/SETUP_DETAIL.md)

---

## Model Rules

There is no default model. If `model` is missing or empty, Byto returns `400`.

Allowed models come from [config/models.json](config/models.json), aliases, or `ALLOW_ANY_GEMINI_MODEL=true`. Startup refresh syncs the catalog against the current supported Google Gemini endpoint model list, then checks each candidate with Vertex `countTokens`. Models that pass are enabled for your project/location; hard failures like `404`/`403` stay disabled.

---

## Tests

```bash
make test
```

Live Vertex checks require real Google auth:

```bash
make test-live MODEL=gemini-2.5-flash
```

CI also runs a fake-cloud production setup e2e on Linux and Windows so the `make setup production` path stays portable.

---

## License

MIT. See [LICENSE](LICENSE).
