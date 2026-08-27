# Load testing

## Go MCP end-to-end (`main.go`)

Measures **tools/call** latency from POST `202 Accepted` until the matching JSON-RPC payload arrives on the **SSE** stream. Reports estimated **RPS**, **p50 / p95 / p99** (client-side).

| Mode       | Gateway config | `tools/call` name |
|-----------|----------------|-------------------|
| `direct`  | Any (exact match shortcut when router on) | `alpha__echo` |
| `semantic`| `ROUTER_MODE=on` + healthy `EMBED_URL`   | Long vague string → vector routing |

Examples (from `mcp-gateway/`):

```bash
# Terminal 1: alpha mock upstream + gateway (default direct tool is alpha__echo)
make demo-upstreams
MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml make run

# Terminal 2
go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode direct -workers 10 -duration 45s
```

Plain `go run ./cmd/gateway` without `MCP_GATEWAY_CONFIG` fails unless you have `gateway.yaml` in the cwd. `make run` defaults to `deployments/gateway.demo.yaml` (`smoke__echo`); override with `-tool smoke__echo` or use the example config above.

```bash
# Semantic comparison (embed sidecar + example config; Qdrant optional for in-memory dev paths)
make docker-up
make demo-upstreams
ROUTER_MODE=on EMBED_URL=http://127.0.0.1:8001 QDRANT_URL=http://127.0.0.1:6333 \
  MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml make run

go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode semantic -workers 10 -duration 45s
```

Compare the printed **p95 / p99** lines across runs. Throughput is approximate (successful iterations / wall time).

### JWT loadtest (LangGraph agent integration run)

The multibackend benchmark (2026-05-30) uses repeated `smoke_e2e.sh` + Prometheus means instead of loadtest (see [integration-checklist.md](../../docs/evaluation/integration-checklist.md)). The LangGraph agent integration run adds JWT loadtest below.

Pass `-token` (or set `LOADTEST_JWT`) to send `Authorization: Bearer` on the SSE
GET and every POST, and `-tool` / `-args` to target a real namespaced tool:

```bash
go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode direct -workers 1 -duration 30s \
  -token "$JWT_ADMIN" \
  -tool prom__read_text_file \
  -args '{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}'
```

Under JWT, use **one worker** because concurrent `tools/list` can collide (see [errors.md](../../docs/errors.md#known-limitations-multiplexing)).

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
