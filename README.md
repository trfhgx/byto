<p align="center">
  <img src="assets/byto_banner.jpg" alt="Byto API Gateway Banner" width="100%" style="border-radius: 8px;" />
</p>

# Byto

<p align="center">
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blueviolet?style=flat-square" alt="License" /></a>
  <a href="https://www.docker.com"><img src="https://img.shields.io/badge/Platform-Docker%20%7C%20Cloud%20Run-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker" /></a>
  <a href="https://cloud.google.com/vertex-ai"><img src="https://img.shields.io/badge/Provider-Vertex%20Gemini-4285F4?style=flat-square&logo=google-cloud&logoColor=white" alt="Vertex AI" /></a>
  <a href="https://github.com/trfhgx/byto/actions/workflows/test.yml"><img src="https://github.com/trfhgx/byto/actions/workflows/test.yml/badge.svg" alt="Tests" /></a>
  <a href="https://github.com/trfhgx/byto/releases"><img src="https://img.shields.io/github/v/release/trfhgx/byto?display_name=tag&sort=semver" alt="Latest Release" /></a>
</p>

Byto is a Go gateway that turns your own Vertex AI Gemini access into an OpenAI-compatible API.

```text
your apps -> Byto -> Vertex AI Gemini
```

It is built for explicit model selection, service API keys, production service-account auth, priority PayGo headers, reasoning controls, adaptive per-model concurrency, JSONL logs, and Docker/server deployments.

### Why Byto

- **Keep existing clients** — point OpenAI-compatible applications at one gateway instead of rewriting each integration for Vertex AI.
- **Centralize provider mechanics** — handle Google authentication, model availability, streaming translation, caching, and reasoning controls once.
- **Operate it directly** — deploy a small Go service locally, with Docker, or on Cloud Run while retaining explicit model and access policies.
- **Inspect real traffic** — record structured request, upstream, token, reasoning, and traffic-type data without changing application prompts.

---

## Quick Start

Download a tagged release from [GitHub Releases](https://github.com/trfhgx/byto/releases). Releases include standalone macOS, Linux, and Windows binaries for AMD64 and ARM64, plus SHA-256 checksums.

Alternatively, build and run from source:

Clone the repo first:

```bash
git clone https://github.com/trfhgx/byto.git
cd byto
```

Prerequisites:

- A Google Cloud project with Vertex AI access.
- Go 1.22 or newer. Setup can install it automatically.
- Google Cloud CLI (`gcloud`). Interactive setup can install it for you when a supported package manager is available.
- `make`. Optional; macOS/Linux often have it or can install it easily, Windows usually does not.
- PowerShell 7 or Windows PowerShell 5.1 on Windows.

### macOS and Linux

The setup scripts use Bash. Use either the `make` commands or the direct script commands.

Use `make setup production` if you do not want to log in every time.

With `make`:

```bash
make setup
make run
```

Production service-account setup with `make`:

```bash
make setup production
```

Without `make`:

```bash
./setup.sh
go run ./cmd/gateway
./scripts/setup-production.sh
```

### Windows

Use PowerShell. `make`, Bash, Docker, Git Bash, and WSL are not required for the
local Windows path.

Clone:

```powershell
git clone https://github.com/trfhgx/byto.git
cd byto
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

Production service-account setup:

```powershell
.\scripts\setup-production-windows.ps1
```

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
- `POST /v1/chat/jobs`, `GET /v1/chat/jobs/{id}`, `DELETE /v1/chat/jobs/{id}` for explicit async chat jobs
- `GET /v1/models`
- `GET /healthz`
- OpenAI-style `model`, `messages`, `stream`, `service_tier`, and `reasoning_effort`
- Vertex cache endpoints under `/v1/caches`
- API-key gateway auth
- Durable service-account auth for production
- Startup model-catalog refresh with Vertex `countTokens` availability checks
- Adaptive per-model concurrency with bounded per-model wait queues plus exponential backoff for Vertex resource exhaustion
- JSONL access/request logs with token usage, traffic type, reasoning tokens, and upstream status

Full API docs: [docs/API.md](docs/API.md)

Detailed setup docs: [docs/SETUP_DETAIL.md](docs/SETUP_DETAIL.md)

---

## Model Rules

There is no default model. If `model` is missing or empty, Byto returns `400`.

Allowed models come from [config/models.json](config/models.json), aliases, or `ALLOW_ANY_GEMINI_MODEL=true`. Gemini entries use Vertex `generateContent`; entries with `runtime: "vertex_openai"` use the Vertex / Gemini Enterprise Agent Platform OpenAI-compatible chat completions endpoint. Startup refresh syncs only the Gemini catalog candidates against the current supported Google Gemini endpoint model list, then checks each candidate with Vertex `countTokens`. Models that pass are enabled for your project/location; hard failures like `404`/`403` stay disabled.

The bundled MaaS catalog includes only non-Gemini models that were live-proven to reply for this project/workstream. If a listed MaaS model is not enabled for your Google Cloud account, Byto returns a provider access error instead of trying to accept Marketplace terms or change billing/project settings.

---

## Tests

```bash
make test
```

Live Vertex checks require real Google auth:

```bash
make test-live MODEL=gemini-2.5-flash
```

CI also runs fake-cloud production setup e2e checks for Linux, macOS/Linux-style Bash on Windows, and the native Windows PowerShell setup path.

---

## License

MIT. See [LICENSE](LICENSE).
