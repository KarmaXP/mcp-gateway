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

### JWT (profile B / C)

Pass `-token` (or set `LOADTEST_JWT`) to send `Authorization: Bearer` on the SSE
GET and every POST, and `-tool` / `-args` to target a real namespaced tool:

```bash
go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode direct -workers 10 -duration 45s \
  -token "$JWT_ADMIN" \
  -tool prom__read_text_file \
  -args '{"path":"/private/tmp/mcp-tfm-tribunal/readme.txt"}'
```

| Flag | Default | Purpose |
|------|---------|---------|
| `-token` | `""` (or `LOADTEST_JWT`) | JWT bearer for `AUTH_MODE=jwt` |
| `-tool` | `alpha__echo` | direct-mode namespaced tool name |
| `-args` | `{}` | direct-mode `tools/call` arguments (JSON object) |
| `-semantic-tool` | echo description | semantic-mode natural-language tool |

The client-observed p95 here includes the SSE round-trip and (under JWT) auth on
every request; it is **not** the gateway's internal phase latency. For internal
overhead use Prometheus phase **means** ([calibration-results.md](../../docs/evaluation/calibration-results.md)),
which stay reliable when samples are sub-ms. If you do not run a JWT loadtest,
the documented substitute is repeated `scripts/smoke_e2e.sh` traffic + those means.

## k6 HTTP baseline (`k6_http_baseline.js`)

Exercises **GET /healthz** and **GET /readyz** with default k6 percentile summaries (`http_req_duration`).

```bash
k6 run --vus 30 --duration 60s scripts/loadtest/k6_http_baseline.js
BASE_URL=http://127.0.0.1:8080 k6 run --vus 30 --duration 60s scripts/loadtest/k6_http_baseline.js
```
