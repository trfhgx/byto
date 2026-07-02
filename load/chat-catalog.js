import http from "k6/http";
import { check, sleep } from "k6";

const url = __ENV.URL || "http://localhost:8080";
const apiKey = __ENV.API_KEY || "";
const vus = Number(__ENV.VUS || "5");
const duration = __ENV.DURATION || "30s";
const maxModels = Number(__ENV.MAX_MODELS || "20");

export const options = {
  scenarios: {
    chat_catalog: {
      executor: "constant-vus",
      vus,
      duration,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.05"],
    http_req_duration: ["p(95)<30000"],
  },
};

export function setup() {
  const res = http.get(`${url}/v1/models`, {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (res.status !== 200) {
    throw new Error(`/v1/models returned ${res.status}: ${res.body}`);
  }
  const body = JSON.parse(res.body);
  const models = (body.data || []).map((m) => m.id).filter(Boolean).slice(0, maxModels);
  if (models.length === 0) {
    throw new Error("No enabled models returned by /v1/models");
  }
  return { models };
}

export default function (data) {
  const model = data.models[__ITER % data.models.length];
  const payload = JSON.stringify({
    model,
    messages: [{ role: "user", content: "Say exactly ok" }],
    max_tokens: 64,
  });
  const res = http.post(`${url}/v1/chat/completions`, payload, {
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
      "X-App-ID": "k6-load-chat-catalog",
    },
    timeout: "120s",
  });
  check(res, {
    "chat status is 200": (r) => r.status === 200,
    "chat body has choices": (r) => r.body && r.body.includes('"choices"'),
  });
  sleep(0.1);
}
