# Calibration results (MiniLM + Qdrant)

Store measured router recall and latency from real integration dependencies here.

## Run metadata

- Date:
- Commit:
- Environment (host / Docker / CI):
- Catalog source (`PHASE2_CATALOG_PATH` or fallback):
- Qdrant URL:
- Embed URL:

## B1.3 vector recall

- Test: `TestPhase2VectorRecallMiniLM`
- Command:
  ```bash
  QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:8001 \
    go test -tags=integration -race ./internal/router/eval -run TestPhase2VectorRecallMiniLM -v
  ```

### Results

- recall@1: `TODO`
- recall@3: `TODO`
- Cases (hits/total): `TODO`
- Notes: `TODO`

## Optional supporting metrics

- Router decision p95 (`mcp.gateway.semantic_router.duration_seconds`): `TODO`
- Internal hop p95 (`mcp.gateway.internal.duration_seconds`): `TODO`
# Calibration results

## B2.3 benchmark snapshot

Measured with:

```bash
go test -run '^$' -bench 'Benchmark(ParseRequest|Namespace(Add|Strip))$' -benchmem ./internal/rpc ./internal/gateway/namespace
```

Orders of magnitude (`ns/op`):

- `BenchmarkParseRequest`: ~`10^3` ns/op (sub-microsecond to ~1 microsecond class)
- `BenchmarkNamespaceAdd`: ~`10^1` ns/op (tens of nanoseconds)
- `BenchmarkNamespaceStrip`: ~`10^1` ns/op (tens of nanoseconds)

Sample output:

```text
BenchmarkParseRequest-14          1382940	       896.0 ns/op	 131.70 MB/s	     448 B/op	      11 allocs/op
BenchmarkNamespaceAdd-14         28541556	        53.91 ns/op	 482.33 MB/s	      32 B/op	       1 allocs/op
BenchmarkNamespaceStrip-14       37070932	        45.72 ns/op	 568.67 MB/s	       0 B/op	       0 allocs/op
```
# Calibration results template

Use this file as the canonical output sheet after following `docs/evaluation/calibration-run.md`.

## Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | |
| Gateway commit | |
| Gateway mode (`ROUTER_MODE`) | |
| Catalog size (tools) | |
| Load profile (tool + workers + duration) | |
| Prometheus lookback window | `5m` (or record override) |
| Tempo datasource / environment | |

## B2.1 Internal latency quantiles by phase (Prometheus)

PromQL used for all phases:

```promql
histogram_quantile(0.50, sum by (le, phase) (rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])))
histogram_quantile(0.95, sum by (le, phase) (rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])))
histogram_quantile(0.99, sum by (le, phase) (rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])))
```

> If your scrape pipeline uses dot metric names, replace `mcp_gateway_internal_duration_seconds_*` with your translated equivalent.

Record values in milliseconds:

| Phase | p50 (ms) | p95 (ms) | p99 (ms) | Query timestamp (UTC) | Notes |
| ----- | -------- | -------- | -------- | --------------------- | ----- |
| parse | | | | | |
| security | | | | | |
| router | | | | | |
| mux | | | | | |

## B2.2 Gateway internal vs upstream split (Tempo)

TraceQL seed query:

```traceql
{ name = "mcp.host.request" && span.mcp.method = "tools/call" }
```

Use span attributes for filtering (`span.mcp.jsonrpc.id`, `span.mcp.session.id`, `span.mcp.tool.name`, `span.mcp.backend.id`) as documented in `docs/DEVELOPER.md` and `internal/telemetry/attrs.go`.

Formulae:

- `T_total` = duration of `mcp.host.request`.
- `T_upstream` = sum of `mcp.backend.call` durations.
- Single-backend path: `T_internal = T_total - T_upstream`.
- Fan-out path: report `T_upstream_sum` and `T_internal_exclusive = T_total - backend critical path`.

| Trace link / id | Tool | Backend(s) | T_total (ms) | T_upstream_sum (ms) | T_internal_exclusive (ms) | Path type (single/fan-out) | Notes |
| --------------- | ---- | ---------- | ------------ | ------------------- | ------------------------- | --------------------------- | ----- |
| | | | | | | | |
| | | | | | | | |
| | | | | | | | |

## Cross-check and decision notes

| Check | Result | Notes |
| ----- | ------ | ----- |
| p95 internal hop under 50 ms target | | |
| Trace decomposition consistent with Prometheus trend | | |
| Outliers explained (phase/upstream specific) | | |

# Calibration results (master table)

Source runbook: `docs/evaluation/calibration-run.md`.

Do not invent numbers. Keep `TBD` (or equivalent placeholders) until each metric is measured and traceable to a concrete run.

| Phase | recall@1 | recall@3 | MRR | nDCG@5 | p95 internal (ms) | Status / evidence |
| ----- | -------- | -------- | --- | ------ | ----------------- | ----------------- |
| golden-cases (lexical baseline) | N/A | N/A | 1.000 | 0.917 | N/A | `go test ./internal/router/eval/... -run TestGoldenCasesMRRAndNDCG -v` |
| parse | TBD | TBD | TBD | TBD | TBD | Pendiente de ejecucion |
| security | TBD | TBD | TBD | TBD | TBD | Pendiente de ejecucion |
| router | TBD | TBD | TBD | TBD | TBD | Pendiente de ejecucion |
| mux | TBD | TBD | TBD | TBD | TBD | Pendiente de ejecucion |
