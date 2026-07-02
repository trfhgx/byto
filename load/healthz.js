import http from "k6/http";
import { check } from "k6";

const url = __ENV.URL || "http://localhost:8080";
const vus = Number(__ENV.VUS || "1000");
const duration = __ENV.DURATION || "20s";

export const options = {
  scenarios: {
    healthz: {
      executor: "constant-vus",
      vus,
      duration,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<250"],
  },
};

export default function () {
  const res = http.get(`${url}/healthz`);
  check(res, {
    "healthz status is 200": (r) => r.status === 200,
  });
}
