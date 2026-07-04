# API Reference

Byto exposes an OpenAI-compatible HTTP API for Gemini models hosted on Vertex AI.

Base URL for local development:

```text
http://localhost:8080
```

## Authentication

By default, mutating and Vertex-backed `/v1/*` endpoints require a service API key:

```http
Authorization: Bearer <gateway-api-key>
```

Configure valid keys with:

```bash
GATEWAY_API_KEYS=key-one,key-two
```

To intentionally run open behind another access layer, set:

```bash
GATEWAY_ALLOW_UNAUTHENTICATED=true
GATEWAY_API_KEYS=
```

Open mode disables Byto's bearer-token check for protected `/v1/*` endpoints. Keep it behind private networking, Cloud Run IAM, or another trusted gateway.

`GET /healthz` and `GET /v1/models` do not require authentication.

## Common Headers

| Header | Required | Description |
| --- | --- | --- |
| `Authorization` | Yes for protected endpoints unless open mode is enabled | `Bearer <gateway-api-key>`. |
| `Content-Type` | Yes for JSON requests | Use `application/json`. |
| `X-Request-ID` | No | Request ID to echo back. Byto generates one if omitted. |
| `X-App-ID` | No | App/service name written to request logs. |

Every response includes `X-Request-ID`.

## Endpoints

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/healthz` | No | Health check. |
| `GET` | `/v1/models` | No | Lists configured models. |
| `GET` | `/v1/models/{model}` | Yes | Returns one configured model and known gateway/catalog metadata. |
| `POST` | `/v1/caches` | Yes | Creates Vertex cached content. |
| `GET` | `/v1/caches` | Yes | Lists Vertex cached content. |
| `GET` | `/v1/caches/{cache}` | Yes | Gets one Vertex cached content resource. |
| `DELETE` | `/v1/caches/{cache}` | Yes | Deletes one Vertex cached content resource. |
| `POST` | `/v1/chat/completions` | Yes | Creates a non-streaming or streaming chat completion. |
| `POST` | `/v1/chat/jobs` | Yes | Creates an explicit async non-streaming chat job. |
| `GET` | `/v1/chat/jobs/{id}` | Yes | Polls an async chat job. |
| `DELETE` | `/v1/chat/jobs/{id}` | Yes | Cancels a queued or running async chat job when possible. |

## `GET /healthz`

Returns service health.

### Response

```json
{
  "status": "ok"
}
```

## `GET /v1/models`

Returns enabled model IDs visible to callers.

### Request

```bash
curl -s http://localhost:8080/v1/models | jq
```

### Response

```json
{
  "object": "list",
  "data": [
    {
      "id": "gemini-2.5-flash",
      "object": "model",
      "owned_by": "google",
      "enabled": true,
      "available": true,
      "launch_stage": "GA",
      "supported_parameters": [
        "model",
        "messages",
        "stream",
        "temperature",
        "top_p",
        "max_tokens",
        "frequency_penalty",
        "presence_penalty",
        "stop",
        "seed"
      ],
      "capabilities": {
        "input": ["text"],
        "output": ["text"],
        "streaming": true
      }
    }
  ]
}
```

The OpenAI-compatible fields (`id`, `object`, `owned_by`) are always present. Extra fields are gateway extensions populated from `MODEL_CATALOG_PATH` when available.

## `GET /v1/models/{model}`

Returns one model plus catalog-backed metadata.

### Request

```bash
curl -s http://localhost:8080/v1/models/gemini-2.5-flash \
  -H "Authorization: Bearer <gateway-api-key>" | jq
```

### Response

```json
{
  "id": "gemini-2.5-flash",
  "object": "model",
  "owned_by": "google",
  "enabled": true,
  "available": true,
  "launch_stage": "GA",
  "supported_parameters": [
    "model",
    "messages",
    "max_tokens",
    "stop"
  ],
  "capabilities": {
    "reasoning_effort": ["low", "medium"],
    "input": ["text"],
    "output": ["text"],
    "streaming": true
  }
}
```

`supported_parameters` means parameters this gateway is prepared to accept and map for that model. Google's supported endpoint model list tells us which model IDs are current for a location, but it does not return a per-model list of supported generation arguments such as `frequencyPenalty` or `responseSchema`. Keep per-model parameter notes in `config/models.json` after reviewing Google model docs and live behavior.

## Vertex Context Caches

Byto exposes thin management endpoints for Vertex `cachedContents`. The gateway does not decide what to cache, when to cache, or how cache IDs are stored by your product. Apps own that logic. The gateway only authenticates the caller, forwards the cache request to Vertex, normalizes simple Gemini model IDs on create, and returns Vertex's JSON response.

### `POST /v1/caches`

Creates a Vertex cached content resource.

The request body is the Vertex `CachedContent` JSON shape. `model` may be either a full Vertex model resource name or a Gemini model ID such as `gemini-2.5-flash`; simple model IDs are expanded to `projects/PROJECT/locations/LOCATION/publishers/google/models/MODEL`.

```bash
curl -s http://localhost:8080/v1/caches \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "displayName": "product-docs-v1",
    "model": "gemini-2.5-flash",
    "contents": [
      {
        "role": "user",
        "parts": [
          { "text": "Large shared context to cache..." }
        ]
      }
    ],
    "ttl": "3600s"
  }' | jq
```

The response is the Vertex cached content object. Store its `name` in your app and pass it later through `extra_body.google.cached_content`.

### `GET /v1/caches`

Lists Vertex cached content for the configured project/location.

```bash
curl -s 'http://localhost:8080/v1/caches?page_size=20' \
  -H "Authorization: Bearer <gateway-api-key>" | jq
```

Supported query aliases:

| Query | Vertex Query |
| --- | --- |
| `page_size` | `pageSize` |
| `page_token` | `pageToken` |

Existing Vertex query names such as `pageSize` and `pageToken` are also passed through.

### `GET /v1/caches/{cache}`

Gets a single cache. `{cache}` may be the cache ID or a full Vertex resource name.

```bash
curl -s http://localhost:8080/v1/caches/CACHE_ID \
  -H "Authorization: Bearer <gateway-api-key>" | jq
```

### `DELETE /v1/caches/{cache}`

Deletes a single cache. `{cache}` may be the cache ID or a full Vertex resource name.

```bash
curl -s -X DELETE http://localhost:8080/v1/caches/CACHE_ID \
  -H "Authorization: Bearer <gateway-api-key>" | jq
```

## `POST /v1/chat/completions`

Creates a chat completion using an explicit Gemini model ID.

### Request Body

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `model` | string | Yes | Gemini model ID or configured alias. Empty values are rejected. |
| `messages` | array | Yes | Ordered chat messages. Must contain at least one item. |
| `messages[].role` | string | Yes | `system`, `user`, or `assistant`. |
| `messages[].content` | string or array | Yes | Text content. Arrays may contain text parts only. |
| `stream` | boolean | No | When `true`, returns server-sent events. Default `false`. |
| `service_tier` | string | No | Vertex consumption lane. Defaults to `priority`. Accepted values: `priority`, `standard`, `flex`, `dedicated`. |
| `reasoning_effort` | string | No | OpenAI-style reasoning control. Accepted values: `off`, `low`, `medium`, `high`. Mapped through the model catalog to Vertex `thinkingConfig.thinkingBudget`. |
| `temperature` | number | No | Passed to Vertex `generationConfig.temperature`. |
| `top_p` | number | No | Passed to Vertex `generationConfig.topP`. |
| `max_tokens` | integer | No | Passed to Vertex `generationConfig.maxOutputTokens`. |
| `frequency_penalty` | number | No | Passed to Vertex `generationConfig.frequencyPenalty`. |
| `presence_penalty` | number | No | Passed to Vertex `generationConfig.presencePenalty`. |
| `stop` | string or array | No | Passed to Vertex `generationConfig.stopSequences`. Empty strings are ignored. |
| `seed` | integer | No | Passed to Vertex `generationConfig.seed`. |
| `extra_body.google.cached_content` | string | No | Vertex cached content resource name. |
| `extra_body.google.reasoning_effort` | string | No | Google-scoped alias for `reasoning_effort`. Conflicts with a different top-level value are rejected. |
| `extra_body.google.thinking_budget` | integer | No | Explicit Vertex `thinkingConfig.thinkingBudget`. Overrides named reasoning-budget mapping. |
| `extra_body.google.include_thoughts` | boolean | No | Explicit Vertex `thinkingConfig.includeThoughts`. Off by default. |

Vertex `generationConfig` also documents fields such as `topK`, `candidateCount`, `responseMimeType`, `responseSchema`, `responseLogprobs`, `logprobs`, and `audioTimestamp`. This gateway only accepts the fields listed above in the OpenAI-compatible request body right now. Add new mappings deliberately because some Vertex fields need response-shape work (`candidateCount` / OpenAI `n`) or model-specific behavior (JSON schema).

For Gemini 3 models, Google documents that sampling parameters (`temperature`, `topP`, and `topK`) are deprecated and recommends omitting them so the model controls sampling automatically.

`service_tier` maps to Vertex request headers:

| `service_tier` | Vertex headers | Behavior |
| --- | --- | --- |
| omitted, `auto`, `high`, `priority` | `X-Vertex-AI-LLM-Request-Type: shared`, `X-Vertex-AI-LLM-Shared-Request-Type: priority` | Priority PayGo. Google can still downgrade to Standard PayGo under ramp/capacity pressure. |
| `standard`, `default`, `on_demand` | `X-Vertex-AI-LLM-Request-Type: shared` | Standard PayGo. |
| `flex` | `X-Vertex-AI-LLM-Request-Type: shared`, `X-Vertex-AI-LLM-Shared-Request-Type: flex` | Flex PayGo for latency-tolerant work. |
| `dedicated`, `provisioned`, `provisioned_throughput` | `X-Vertex-AI-LLM-Request-Type: dedicated` | Provisioned Throughput only. |

The response includes `usage.traffic_type` when Vertex returns it. Use this value to verify what actually happened (`ON_DEMAND_PRIORITY`, `ON_DEMAND`, `ON_DEMAND_FLEX`, or `PROVISIONED_THROUGHPUT`).

`reasoning_effort` remains named at the API boundary. Per-model numeric budgets live in `MODEL_CATALOG_PATH` under `capabilities.reasoning_budgets`. This lets one model map `high` differently from another while callers keep using `off`, `low`, `medium`, and `high`. Vertex returns thinking token usage as `usageMetadata.thoughtsTokenCount`; the gateway maps that to `usage.completion_tokens_details.reasoning_tokens`.

### Message Content

String content:

```json
{
  "role": "user",
  "content": "Say ok"
}
```

Text-part content:

```json
{
  "role": "user",
  "content": [
    { "type": "text", "text": "Say ok" }
  ]
}
```

Only text content is supported in this version. Image URLs, audio, files, and tool calls are not accepted.

### Non-Streaming Request

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -H "X-App-ID: billing-api" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      { "role": "system", "content": "You are concise." },
      { "role": "user", "content": "Reply with only: ok" }
    ],
    "reasoning_effort": "low",
    "temperature": 0.2,
    "frequency_penalty": 0.1,
    "presence_penalty": 0.1,
    "stop": ["END"],
    "seed": 7,
    "max_tokens": 32
  }' | jq
```

### Non-Streaming Response

```json
{
  "id": "chatcmpl-2f0f2d4fb9f12a1c51bd6f83",
  "object": "chat.completion",
  "created": 1793364660,
  "model": "gemini-2.5-flash",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "ok"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 1,
    "total_tokens": 28,
    "cached_tokens": 0,
    "completion_tokens_details": {
      "reasoning_tokens": 12
    },
    "traffic_type": "ON_DEMAND_PRIORITY"
  }
}
```

### Streaming Request

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "stream": true,
    "messages": [
      { "role": "user", "content": "Write one short sentence." }
    ]
  }'
```

### Streaming Response

The response is `text/event-stream`.

```text
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1793364660,"model":"gemini-2.5-flash","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1793364660,"model":"gemini-2.5-flash","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1793364660,"model":"gemini-2.5-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

## Async Chat Jobs

Normal `POST /v1/chat/completions` requests stay synchronous. Byto may hold that HTTP request open only while it waits briefly for an adaptive concurrency slot. It does not silently turn normal requests into background jobs.

Use `POST /v1/chat/jobs` when the caller explicitly wants async work. The request body is the same as non-streaming `/v1/chat/completions`; `stream=true` is rejected. The create response is `202 Accepted` with a job ID:

```bash
curl -s http://localhost:8080/v1/chat/jobs \
  -H "Authorization: Bearer <gateway-api-key>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: invoice-123-summary" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{ "role": "user", "content": "Summarize invoice 123." }]
  }' | jq
```

```json
{
  "id": "chatjob-...",
  "object": "chat.job",
  "status": "queued",
  "created_at": "2026-07-04T16:00:00Z",
  "updated_at": "2026-07-04T16:00:00Z",
  "model": "gemini-2.5-flash"
}
```

Poll with `GET /v1/chat/jobs/{id}`. Terminal statuses are `succeeded`, `failed`, and `canceled`. Successful jobs include a normal chat completion response under `response`; failed or canceled jobs include `error`.

Cancel with `DELETE /v1/chat/jobs/{id}`. Cancellation is best effort: queued jobs are marked canceled before they start, and running jobs receive context cancellation. If Vertex has already completed, the terminal result may already be `succeeded` or `failed`.

`Idempotency-Key` on `POST /v1/chat/jobs` prevents duplicate work after client/network retries. The key is scoped by the caller's bearer token when present, otherwise by `X-App-ID`, so two apps can reuse the same key without colliding. Retrying the same create request with the same key returns the existing job instead of creating another one.

The first implementation stores jobs in memory. That is intentionally non-durable: jobs are lost on process restart and are not shared across gateway replicas. The `jobStore` abstraction keeps the path open for a DB-backed implementation, where `jobs` and scoped idempotency keys would be persisted in a transactional table and workers would claim queued rows with leases.

### Explicit Vertex Cache

Pass a Vertex cached content resource through `extra_body.google.cached_content`.

```json
{
  "model": "gemini-2.5-flash",
  "messages": [
    { "role": "user", "content": "Use the cached context and summarize it." }
  ],
  "extra_body": {
    "google": {
      "cached_content": "projects/PROJECT/locations/global/cachedContents/CACHE_ID"
    }
  }
}
```

Vertex also performs implicit caching automatically for eligible repeated prompt prefixes. Explicit cache management is useful when an app wants predictable reuse of large shared context and wants to store/cache resource names itself.

## Model Resolution

`model` is mandatory. Byto never chooses a default model.

Resolution order:

1. Reject missing or empty `model`.
2. Resolve `MODEL_ALIASES`, if configured.
3. Accept enabled and available entries from `MODEL_CATALOG_PATH`.
4. If `ALLOW_ANY_GEMINI_MODEL=true`, accept any `gemini-*` model ID.
5. Reject the request.

Gemini catalog entries use Vertex `generateContent`. Catalog entries with `runtime: "vertex_openai"` use the Vertex / Gemini Enterprise Agent Platform OpenAI-compatible chat completions endpoint and are limited to the non-Gemini MaaS IDs that were live-proven for this project/workstream.

When startup catalog refresh is enabled, Byto updates only Gemini candidates in `MODEL_CATALOG_PATH` against the current supported Google Gemini endpoint model list, then verifies each supported Gemini candidate with Vertex `countTokens`. Passing candidates become enabled and available for the configured project/location. Hard failures such as `404`, `403`, `401`, and `400` disable the entry. Transient failures such as `429`, `5xx`, or timeout keep the previous state. MaaS entries are not verified with Gemini `countTokens`, and Byto does not accept Marketplace terms or make project/billing changes from code.

## Errors

Errors use this shape:

```json
{
  "error": {
    "message": "model is required",
    "type": "invalid_request_error",
    "code": ""
  }
}
```

| Status | Type | Common Cause |
| --- | --- | --- |
| `400` | `invalid_request_error` | Invalid JSON, missing `model`, missing `messages`, unsupported role/content. |
| `400` | `invalid_model` | Model is not enabled, available, aliased, or allowed. |
| `401` | `invalid_api_key` | Missing or invalid bearer token. |
| `401` or `403` | `provider_access_error` | Vertex rejected access to a MaaS provider/model for the configured Google Cloud account. |
| `405` | `method_not_allowed` | Unsupported method for endpoint. |
| `429` | `server_overloaded` | Local model queue is full (`queue_full`) or wait timed out (`queue_timeout`). |
| `429` | `server_overloaded` | Vertex returned temporary resource exhaustion (`temporary_resource_exhausted`) or quota/rate-limit pressure (`quota_or_rate_limit`). The gateway preserves this status so callers can back off or use a higher-priority Vertex consumption mode. |
| `500` | `server_error` | Server cannot stream the response. |
| `502` | `vertex_error` | Vertex returned an error or could not be reached. |

## Adaptive Concurrency

Byto can limit concurrent chat requests per resolved Vertex model and adjust that limit from recent outcomes.

The limiter starts at `ADAPTIVE_CONCURRENCY_INITIAL`, allows only that many in-flight requests for the model, increases slowly after clean completions, and reduces quickly when Vertex returns resource exhaustion. Requests that arrive while the model is already at its current limit may wait in a bounded per-model queue for up to `ADAPTIVE_QUEUE_MAX_WAIT_MS`. If the queue is full, Byto returns `429` with `code=queue_full`. If the wait expires, Byto returns `429` with `code=queue_timeout`. If the client disconnects while queued, that queued waiter is removed.

Response headers on admitted chat requests are written before the upstream Vertex call, so they are present on successful responses and on admitted requests that later return an upstream error such as `429 temporary_resource_exhausted`. They are not present when the request never receives a model slot, for example `queue_full`, `queue_timeout`, or client cancellation while still waiting.

| Header | Description |
| --- | --- |
| `X-Byto-Queue-Wait-Ms` | Time spent waiting for an adaptive concurrency slot. |
| `X-Byto-Model-Queue-Depth` | Remaining queued requests for the model after this request was admitted. |
| `X-Byto-Model-Queue-Max` | Configured maximum queued synchronous requests per model. |
| `X-Byto-Model-In-Flight` | In-flight count for the model after this request was admitted. |
| `X-Byto-Model-Concurrency-Limit` | Current concurrency limit for the model when admitted. |

Limiter scope:

- The adaptive concurrency limiter is in-process and per resolved model. For example, `gemini-3.5-flash` and `gemini-3.1-flash-lite` have separate in-flight counts, queues, and learned AIMD limits.
- Model aliases are resolved before limiter lookup, so aliases that point at the same Vertex model share the same limiter.
- This is not a distributed limiter; multiple gateway processes each maintain their own local limiter state.

Settings:

| Environment | Default | Description |
| --- | --- | --- |
| `ADAPTIVE_CONCURRENCY_ENABLED` | `true` | Enable per-model adaptive concurrency. |
| `ADAPTIVE_CONCURRENCY_MIN` | `1` | Lowest learned concurrency limit. |
| `ADAPTIVE_CONCURRENCY_INITIAL` | `4` | Starting concurrency limit per model. |
| `ADAPTIVE_CONCURRENCY_MAX` | `32` | Highest learned concurrency limit. |
| `ADAPTIVE_QUEUE_MAX_DEPTH` | `2048` | Maximum queued synchronous requests per model. Set to `0` to fail immediately when all slots are busy. |
| `ADAPTIVE_QUEUE_MAX_WAIT_MS` | `30000` | Maximum time a synchronous request waits for a model slot. |
| `ASYNC_JOB_RETENTION_SECONDS` | `3600` | How long completed in-memory async jobs are retained for polling/idempotency. |
| `ASYNC_JOB_TIMEOUT_SECONDS` | `300` | Background execution timeout for async jobs. |

For queue sizing guidance and benchmark interpretation, see [Queue Sizing Guide](QUEUE_SIZING.md).

## Retries

Byto retries transient Vertex transport/upstream failures and Vertex resource exhaustion using exponential backoff with jitter. This keeps retry behavior in the gateway while product and business decisions stay in the calling apps.

Retried by default:

- network/request errors before a response is returned
- `408`
- `429`
- `500`
- `502`
- `503`
- `504`

Not retried:

- `400` invalid request/model/parameters
- `401` or `403` auth and permission errors
- model safety/content responses returned as successful Vertex responses
- response parsing errors after a streaming response has already started

Retry settings:

| Environment | Default | Description |
| --- | --- | --- |
| `VERTEX_RETRY_MAX_ATTEMPTS` | `3` | Total attempts including the first request. Set `1` to disable gateway retries. |
| `VERTEX_RETRY_INITIAL_MS` | `250` | Initial exponential backoff delay. |
| `VERTEX_RETRY_MAX_MS` | `2000` | Maximum backoff delay before jitter. |

If Vertex sends `Retry-After`, the gateway honors it.

## Unsupported OpenAI Fields

These OpenAI fields are not implemented in this version:

- `tools`
- `tool_choice`
- `response_format`
- `n`
- image, audio, or file content

These Vertex-specific `generationConfig` fields are not mapped yet:

- `topK`
- `candidateCount`
- `responseMimeType`
- `responseSchema`
- `responseLogprobs`
- `logprobs`
- `audioTimestamp`
- `thinkingConfig`

## Vertex References

- [Vertex/Gemini generateContent REST reference](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/projects.locations.publishers.models/generateContent)
- [Vertex AI Gemini inference examples and generationConfig shape](https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/inference)
- [Vertex AI OpenAI-compatible chat completions endpoint](https://cloud.google.com/vertex-ai/generative-ai/docs/start/openai)
- [Gemini Enterprise Agent Platform MaaS open model API guide](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/maas/call-open-model-apis)
