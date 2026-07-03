# Project Goal

This project is a production-oriented Go gateway that lets multiple applications call Gemini on Vertex AI through an OpenAI-compatible HTTP API.

The goal is to build a reusable internal LLM provider layer:

```text
Your apps -> Go LLM Gateway -> Vertex AI Gemini
```

Each app keeps its own business logic:

- end-user authentication
- credits
- subscriptions
- product limits
- prompts
- product-specific workflows

The gateway only handles provider mechanics:

- `/v1/chat/completions` compatibility
- `/v1/models`
- service-level API key auth
- Vertex AI authentication
- request translation to Gemini `generateContent`
- streaming translation from Gemini `streamGenerateContent`
- persistent request logging
- explicit cache passthrough
- explicit cache lifecycle forwarding
- transient Vertex retry handling
- model allow-listing by real Gemini model name

## Non-goals

This gateway does not manage end-user credits, product plans, subscriptions, or application-specific limits. Those belong inside each product service.

This gateway is also not a replacement for Vertex AI. Vertex remains the model provider and inference engine.

## Model selection rule

Calling services should send real Gemini/Vertex model IDs, for example:

```json
{
  "model": "gemini-3.1-pro-preview",
  "messages": [
    { "role": "user", "content": "Hello" }
  ]
}
```

The gateway validates the model against `config/models.json`. Optional aliases exist, but they are disabled by default and should not be the main production path if you want services to explicitly choose models.

There is no gateway default model. If a request omits `model`, the gateway returns an error. Model choice belongs to the calling service.

The model catalog carries per-model metadata such as enabled/available state, supported actions, and reasoning-effort tiers. On startup, the gateway refreshes that catalog against the current supported Google Gemini endpoint model list, then verifies candidates with Vertex `countTokens`. Candidates that pass become available for the configured project/location; stale IDs and hard failures stay disabled.

## Caching rule

Vertex AI handles implicit caching automatically when prompt prefixes repeat. The gateway must not inject dynamic values such as request IDs, timestamps, app IDs, or trace IDs into the prompt body.

For explicit Gemini cache objects, services can pass:

```json
{
  "extra_body": {
    "google": {
      "cached_content": "projects/PROJECT/locations/global/cachedContents/CACHE_ID"
    }
  }
}
```

The gateway maps this to Vertex `cachedContent`. For cache lifecycle operations, services can use the gateway's `/v1/caches` endpoints to create, list, inspect, and delete Vertex `cachedContents` resources. Services still own cache policy and storage of cache resource names.

## Current v1 limitations

- Text-only message content.
- No `image_url` support yet.
- No OpenAI tool-call roundtrip yet.
- Persistent logs are JSONL files, not a database.
- Streaming parser is best-effort for Vertex REST stream chunks.

## Intended next milestones

1. Add multimodal input support.
2. Add OpenAI tool-call mapping to Gemini function declarations.
3. Add optional BigQuery or Postgres logging sink.
4. Add structured output support.
5. Add load tests and benchmarks.
