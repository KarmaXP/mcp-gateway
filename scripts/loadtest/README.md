# Load testing

## Go MCP end-to-end (`main.go`)

Measures **tools/call** latency from POST `202 Accepted` until the matching JSON-RPC payload arrives on the **SSE** stream. Reports estimated **RPS**, **p50 / p95 / p99** (client-side).

| Mode       | Gateway config | `tools/call` name |
|-----------|----------------|-------------------|
| `direct`  | Any (exact match shortcut when router on) | `alpha__echo` |
| `semantic`| `ROUTER_MODE=on` + healthy `EMBED_URL`   | Long vague string → vector routing |

Examples:

```bash
# Terminal 1: gateway with router off (direct path still uses exact name)
go run ./cmd/gateway

# Terminal 2
go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode direct -workers 10 -duration 45s
```

```bash
# Semantic comparison (embed + Qdrant optional; in-memory store is enough for router)
ROUTER_MODE=on EMBED_URL=http://127.0.0.1:8001 go run ./cmd/gateway

go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode semantic -workers 10 -duration 45s
```

Compare the printed **p95 / p99** lines across runs. Throughput is approximate (successful iterations / wall time).

## k6 HTTP baseline (`k6_http_baseline.js`)

Exercises **GET /healthz** and **GET /readyz** with default k6 percentile summaries (`http_req_duration`).

```bash
k6 run --vus 30 --duration 60s scripts/loadtest/k6_http_baseline.js
BASE_URL=http://127.0.0.1:18080 k6 run --vus 30 --duration 60s scripts/loadtest/k6_http_baseline.js
```
