/**
 * k6 baseline: /healthz and /readyz throughput + latency percentiles.
 *
 * Full MCP (SSE + JSON-RPC) is covered by the Go loadtest (scripts/loadtest/main.go)
 * because k6 does not reliably stream SSE for bidirectional MCP sessions.
 *
 * Usage:
 *   k6 run --vus 20 --duration 60s scripts/loadtest/k6_http_baseline.js
 *   BASE_URL=http://127.0.0.1:8080 k6 run scripts/loadtest/k6_http_baseline.js
 */
import http from "k6/http";
import { check, sleep } from "k6";
import { Trend, Rate } from "k6/metrics";

const base = __ENV.BASE_URL || "http://127.0.0.1:8080";

const healthLatency = new Trend("healthz_latency_ms");
const readyLatency = new Trend("readyz_latency_ms");
const healthFail = new Rate("healthz_fail");

export const options = {
  thresholds: {
    http_req_failed: ["rate<0.01"],
    healthz_latency_ms: ["p(95)<500"],
    readyz_latency_ms: ["p(95)<500"],
  },
};

export default function () {
  let r = http.get(`${base}/healthz`);
  healthLatency.add(r.timings.duration);
  healthFail.add(r.status !== 200);
  check(r, { "health 200": (res) => res.status === 200 });

  r = http.get(`${base}/readyz`);
  readyLatency.add(r.timings.duration);
  check(r, { "ready 200": (res) => res.status === 200 });

  sleep(0.05);
}
