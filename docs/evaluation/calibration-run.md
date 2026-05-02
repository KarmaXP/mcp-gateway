# Calibration run (embedding + Qdrant)

Repeatable procedure to record **live** router and gateway latency numbers with your real embedding endpoint and Qdrant (not synthetic in-tree defaults). Use this after `docs/architecture/mcp_gateway.plan.md` §6 and §3.B.8.

## Prerequisites

- `make docker-up` (or equivalent): Qdrant, embed sidecar, OTel collector, optional Prometheus/Grafana.
- Gateway configured with `ROUTER_MODE=on` or `assist_list` or `filter_list`, `QDRANT_URL`, `EMBED_URL`, and a representative `MCP_GATEWAY_CONFIG` catalog (≥20 tools recommended for router eval).

## Steps

1. **Health:** `curl -sf http://127.0.0.1:<PORT>/healthz` (adjust host/port).
2. **Unit / synthetic eval (CI-style):** from `mcp-gateway/` module root:
   ```bash
   go test -race ./internal/router/...
   go test -race ./internal/router/eval/...
   ```
3. **Integration (real Qdrant + embed):** with Compose up:
   ```bash
   make test-integration
   ```
   (Uses `-tags=integration` as defined in the repo Makefile.)
4. **Optional load / soak:** if you use `scripts/loadtest`, document the command line, duration, and concurrent clients you ran.

## Record (template)

Fill in after each run. Do not invent numbers—leave cells blank if a step was not executed.

| Metric / artifact | Command or source | Value | Notes |
| ----------------- | ----------------- | ----- | ----- |
| Catalog size (tools) | | | |
| recall@k (synthetic or labeled set) | `go test ./internal/router/eval/... -run …` or custom eval | | Define k and dataset id |
| Router decision p95 (embed+vector path) | Prometheus / Grafana from `mcp.gateway.semantic_router.duration_seconds` or traces | | Layer breakdown (`exact` vs `vector`) |
| Internal hop p95 | `mcp.gateway.internal.duration_seconds` by `phase` (`parse`, `security`, `router`, `mux`) | | Align with plan §6 < 50 ms p95 goal for gateway-only work |
| Embed service p95 | Your sidecar metrics or traces | | |
| Qdrant query p95 | Qdrant metrics or gateway `mcp.router.semantic` span children | | |

## Prometheus (OTel → Prom naming)

The application registers `mcp.gateway.internal.duration_seconds`. After OTel → Prometheus translation, series often appear as `mcp_gateway_internal_duration_seconds_bucket` (histogram). Use your scrape/relayer’s actual metric names when writing queries.

## References

- `docs/architecture/mcp_gateway.plan.md` — §3.B modes (`filter_list`), §3.D metrics, §6 latency budget.
- `README.md` — `ROUTER_MODE`, observability, integration tests.
