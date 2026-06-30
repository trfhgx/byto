# API Reference

Byto exposes an OpenAI-compatible HTTP API for Gemini models hosted on Vertex AI.

Base URL for local development:

```text
http://localhost:8080
```

## Authentication

By default, all `/v1/*` endpoints require a service API key:

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

Open mode disables Byto's bearer-token check for `/v1/*`. Keep it behind private networking, Cloud Run IAM, or another trusted gateway.

`GET /healthz` does not require authentication.

## Common Headers

| Header | Required | Description |
| --- | --- | --- |
| `Authorization` | Yes for `/v1/*` unless open mode is enabled | `Bearer <gateway-api-key>`. |
| `Content-Type` | Yes for JSON requests | Use `application/json`. |
| `X-Request-ID` | No | Request ID to echo back. Byto generates one if omitted. |
| `X-App-ID` | No | App/service name written to request logs. |

Every response includes `X-Request-ID`.

## Endpoints

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/healthz` | No | Health check. |
| `GET` | `/v1/models` | Yes | Lists configured models. |
| `GET` | `/v1/models/{model}` | Yes | Returns one configured model and known gateway/catalog metadata. |
| `POST` | `/v1/chat/completions` | Yes | Creates a non-streaming or streaming chat completion. |

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
curl -s http://localhost:8080/v1/models \
  -H "Authorization: Bearer <gateway-api-key>"
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

`supported_parameters` means parameters this gateway is prepared to accept and map for that model. Google Vertex publisher-model metadata currently exposes model presence, launch stage, version state, and UI/actions metadata, but it does not return a per-model list of supported generation arguments such as `frequencyPenalty` or `responseSchema`. Keep per-model parameter notes in `config/models.json` after reviewing Google model docs and live behavior.

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
| `temperature` | number | No | Passed to Vertex `generationConfig.temperature`. |
| `top_p` | number | No | Passed to Vertex `generationConfig.topP`. |
| `max_tokens` | integer | No | Passed to Vertex `generationConfig.maxOutputTokens`. |
| `frequency_penalty` | number | No | Passed to Vertex `generationConfig.frequencyPenalty`. |
| `presence_penalty` | number | No | Passed to Vertex `generationConfig.presencePenalty`. |
| `stop` | string or array | No | Passed to Vertex `generationConfig.stopSequences`. Empty strings are ignored. |
| `seed` | integer | No | Passed to Vertex `generationConfig.seed`. |
| `extra_body.google.cached_content` | string | No | Vertex cached content resource name. |

Vertex `generationConfig` also documents fields such as `topK`, `candidateCount`, `responseMimeType`, `responseSchema`, `responseLogprobs`, `logprobs`, `audioTimestamp`, and `thinkingConfig`. This gateway only accepts the fields listed above in the OpenAI-compatible request body right now. Add new mappings deliberately because some Vertex fields need response-shape work (`candidateCount` / OpenAI `n`) or model-specific behavior (`thinkingConfig`, JSON schema).

For Gemini 3 models, Google documents that sampling parameters (`temperature`, `topP`, and `topK`) are deprecated and recommends omitting them so the model controls sampling automatically.

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
    "cached_tokens": 0
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

## Model Resolution

`model` is mandatory. Byto never chooses a default model.

Resolution order:

1. Reject missing or empty `model`.
2. Resolve `MODEL_ALIASES`, if configured.
3. Accept enabled and available entries from `MODEL_CATALOG_PATH`.
4. If `ALLOW_ANY_GEMINI_MODEL=true`, accept any `gemini-*` model ID.
5. Reject the request.

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
| `405` | `method_not_allowed` | Unsupported method for endpoint. |
| `500` | `server_error` | Server cannot stream the response. |
| `502` | `vertex_error` | Vertex returned an error or could not be reached. |

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
