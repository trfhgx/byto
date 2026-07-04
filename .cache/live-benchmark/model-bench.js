import http from "k6/http";
import exec from "k6/execution";
import { check } from "k6";

const url = __ENV.URL || "http://localhost:18080";
const apiKey = __ENV.API_KEY || "";
const model = __ENV.MODEL;
const concurrency = Number(__ENV.CONCURRENCY || "1");
const iterations = Number(__ENV.ITERATIONS || String(concurrency));
const prompt = __ENV.PROMPT || "Reply with exactly: ok";
const maxTokens = Number(__ENV.MAX_TOKENS || "16");
const phase = __ENV.PHASE || "capacity";
const httpTimeout = __ENV.HTTP_TIMEOUT || "240s";

export const options = {
  scenarios: {
    probe: {
      executor: "shared-iterations",
      vus: concurrency,
      iterations,
      maxDuration: __ENV.MAX_DURATION || "15m",
    },
  },
};

function headers() {
  const h = {
    "Content-Type": "application/json",
    "X-App-ID": "k6-live-model-benchmark",
  };
  if (apiKey) h.Authorization = `Bearer ${apiKey}`;
  return h;
}

function errorCode(body) {
  if (!body) return "";
  try {
    return JSON.parse(body).error?.code || "";
  } catch {
    return "";
  }
}

export default function () {
  const payload = JSON.stringify({
    model,
    messages: [{ role: "user", content: prompt }],
    max_tokens: maxTokens,
  });
  const started = Date.now();
  const res = http.post(`${url}/v1/chat/completions`, payload, {
    headers: headers(),
    timeout: httpTimeout,
  });
  const queueWait = Number(res.headers["X-Byto-Queue-Wait-Ms"] || "0");
  const queueDepth = Number(res.headers["X-Byto-Model-Queue-Depth"] || "0");
  const queueMax = Number(res.headers["X-Byto-Model-Queue-Max"] || "0");
  const row = {
    ts: new Date().toISOString(),
    phase,
    model,
    concurrency,
    vu: exec.vu.idInTest,
    iter: exec.scenario.iterationInTest,
    status: res.status,
    duration_ms: Date.now() - started,
    queue_wait_ms: queueWait,
    queue_depth: queueDepth,
    queue_max: queueMax,
    in_flight: Number(res.headers["X-Byto-Model-In-Flight"] || "0"),
    limit: Number(res.headers["X-Byto-Model-Concurrency-Limit"] || "0"),
    error_code: errorCode(res.body),
    ok: res.status === 200 && res.body && res.body.includes('"choices"'),
  };
  console.log(`RESULT ${JSON.stringify(row)}`);
  check(res, {
    "status is 200 or overload": (r) => r.status === 200 || r.status === 429,
  });
}
