# Queue Sizing Guide

This guide explains how to size the synchronous adaptive queue and when to use async jobs instead. For endpoint shapes, headers, status codes, and environment variables, see [API.md](API.md).

## Mental Model

The synchronous queue is a short shock absorber. It is not a durable work backlog.

When a request reaches `/v1/chat/completions`, the gateway resolves the model and attempts to acquire that model's adaptive concurrency slot:

1. If in-flight calls are below the current AIMD limit, the request runs immediately.
2. If the model is at its current limit, the request waits in that model's bounded queue.
3. If the queue is full, the request returns `429` with `code=queue_full`.
4. If queue wait exceeds `ADAPTIVE_QUEUE_MAX_WAIT_MS`, the request returns `429` with `code=queue_timeout`.
5. If the request is admitted and Vertex later returns resource exhaustion, the response is still a `429`, usually with `code=temporary_resource_exhausted`.

The queue and AIMD limit are per resolved model and in-process. Model aliases that resolve to the same Vertex model share the same limiter. Multiple gateway processes do not share limiter state.

## Retry Interaction

Vertex retry/backoff changes queue behavior.

With `VERTEX_RETRY_MAX_ATTEMPTS > 1`, an admitted request that hits retryable Vertex pressure holds its model permit while backing off and retrying. That protects callers from transient failures, but it also means the queue drains more slowly under overload.

AIMD reacts after the admitted request completes:

1. request is admitted at current limit
2. Vertex returns resource exhaustion after one or more attempts
3. gateway releases the permit
4. AIMD lowers the model limit
5. later queued requests drain at the lower limit

This means AIMD helps recover from overload, but it cannot prevent the first burst from overshooting.

## Retry-Enabled 8s Queue-Wait Benchmarks

These live k6 benchmarks used:

- `VERTEX_RETRY_MAX_ATTEMPTS=3`
- `VERTEX_RETRY_INITIAL_MS=250`
- `VERTEX_RETRY_MAX_MS=2000`
- `ADAPTIVE_CONCURRENCY_INITIAL=4`
- `ADAPTIVE_CONCURRENCY_MAX=32`
- `ADAPTIVE_QUEUE_MAX_DEPTH=2048`
- stop condition: first tested burst where any request waited more than 8000ms in the gateway queue

| Model | Max burst with queue wait <=8s | Max all-200 burst | First level exceeding 8s wait |
| --- | ---: | ---: | ---: |
| `gemini-3.5-flash` | `48` | `32` | `64` |
| `gemini-3.1-flash-lite` | `24` | `24` | `32` |
| `gemini-2.5-flash` | `12` | `8` | `16` |
| `gemini-2.5-flash-lite` | `12` | `8` | `16` |
| `gemini-2.5-flash-lite-preview-09-2025` | `12` | `8` | `16` |
| `gemini-2.5-pro` | `12` | `8` | `16` |
| `gemini-3-flash-preview` | `12` | `8` | `16` |
| `gemini-3.1-pro-preview` | `8` | `8` | `12` |

These numbers are not universal. They depend on project quota, region, model availability, prompt shape, token counts, retry settings, service tier, and concurrent traffic from other apps.

## Practical Queue Caps

For an 8 second maximum queue-wait target, start with conservative synchronous caps:

| Model group | Suggested sync cap |
| --- | ---: |
| `gemini-3.5-flash` | `32` clean, up to `48` if retried 429s are acceptable |
| `gemini-3.1-flash-lite` | `24` |
| most other flash/lite/pro models | `8` clean, up to `12` if retried 429s are acceptable |
| slow or preview pro models | `8` or async-only for bursts |

`ADAPTIVE_QUEUE_MAX_DEPTH=2048` is a high ceiling, not a recommended SLA-sized queue. It prevents immediate local rejection during extreme bursts, but it can create long waits. If your product needs an 8 second queue-wait SLA, set a lower per-deployment queue depth or enforce app-level admission before calling the gateway.

## Fan-Out Workloads

Do not model a fan-out product workflow as many independent synchronous user requests.

For example, a branding app comparing 40 candidate names should usually use one of these patterns:

- Batch all 40 names in one structured prompt when the task fits the context window.
- Chunk into a small number of requests, for example 4 requests with 10 names each.
- Use a two-stage pipeline: a fast model scores all names, then a stronger model reviews only finalists.
- Use `POST /v1/chat/jobs` for async processing when the work can exceed the caller's HTTP timeout or queue-wait target.

If you must fan out to many model calls, run a worker pool with model-specific concurrency limits instead of letting the public HTTP queue absorb the entire burst.

## Benchmark Harness

The repository includes a local k6 harness used to produce the benchmark report:

- `.cache/live-benchmark/model-bench.js`
- `.cache/live-benchmark/generate_report.py`

The generated HTML report is intentionally not committed by default. Re-run the harness for your deployment and open `.cache/live-benchmark/report.html` locally.
