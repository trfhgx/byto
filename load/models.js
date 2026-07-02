import http from "k6/http";
import { check } from "k6";

const url = __ENV.URL || "http://localhost:8080";
const apiKey = __ENV.API_KEY || "";
const vus = Number(__ENV.VUS || "500");
const duration = __ENV.DURATION || "20s";

export const options = {
  scenarios: {
    models: {
      executor: "constant-vus",
      vus,
      duration,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

export default function () {
  const res = http.get(`${url}/v1/models`, {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  check(res, {
    "models status is 200": (r) => r.status === 200,
    "models body has data": (r) => r.body && r.body.includes('"data"'),
  });
}
