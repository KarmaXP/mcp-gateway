# Calibration results

Record **live** router recall and gateway latency here after following [`calibration-run.md`](calibration-run.md).

Empty cells are normal until you complete a calibration run.

---

## Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | |
| Gateway commit | |
| Environment (host / Docker / CI) | |
| Catalog source (`ROUTER_EVAL_CATALOG_PATH` or default `router-eval-catalog.json`) | |
| Qdrant URL | |
| Embed URL | |
| Gateway mode (`ROUTER_MODE`) | |
| Load profile (tool, workers, duration) | |

---

## Vector recall (MiniLM + Qdrant)

Command:

```bash
QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:8001 \
 go test -tags=integration -race ./internal/router/eval -run TestRouterEvalVectorRecallMiniLM -v
```

| Metric | Value |
| ------ | ----- |
| recall@1 | |
| recall@3 | |
| Cases (hits / total) | |
| Notes | |

Optional supporting metrics (from Prometheus after a semantic load run):

| Metric | Value |
| ------ | ----- |
| Router decision p95 (`mcp.gateway.semantic_router.duration_seconds`) | |
| Internal hop p95 (`mcp.gateway.internal.duration_seconds`) | |

---

## Micro-benchmarks (unit; no Docker)

Measured with:

```bash
go test -run '^$' -bench 'Benchmark(ParseRequest|Namespace(Add|Strip))$' -benchmem ./internal/rpc ./internal/gateway/namespace
```

Sample output (orders of magnitude):

```text
BenchmarkParseRequest-14   ~10^3 ns/op
BenchmarkNamespaceAdd-14   ~10^1 ns/op
BenchmarkNamespaceStrip-14  ~10^1 ns/op
```

---

## Internal latency by phase (Prometheus)

PromQL (5m lookback; adjust metric name if your OTel → Prometheus translation differs):

```promql
histogram_quantile(0.95, sum by (le, phase) (rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])))
```

| Phase | p50 (ms) | p95 (ms) | p99 (ms) | Query time (UTC) | Notes |
| ----- | -------- | -------- | -------- | ---------------- | ----- |
| parse | | | | | |
| security | | | | | |
| router | | | | | |
| mux | | | | | |

---

## Gateway vs upstream (Tempo)

TraceQL seed:

```traceql
{ name = "mcp.host.request" && span.mcp.method = "tools/call" }
```

| Trace id | Tool | Backend(s) | T_total (ms) | T_upstream (ms) | T_internal (ms) | Path | Notes |
| -------- | ---- | ---------- | ------------ | --------------- | --------------- | ---- | ----- |
| | | | | | | | |

Cross-check:

| Check | Result | Notes |
| ----- | ------ | ----- |
| p95 internal hop vs 50 ms design goal | | |
| Traces consistent with Prometheus | | |

---

## Summary table

| Scope | recall@1 | recall@3 | MRR | nDCG@5 | p95 internal (ms) | Evidence |
| ----- | -------- | -------- | --- | ------ | ----------------- | -------- |
| golden-cases (lexical, unit) | n/a | n/a | 1.000 | 0.917 | n/a | `go test ./internal/router/eval/... -run TestGoldenCasesMRRAndNDCG` |
| parse | | | | | | calibration run |
| security | | | | | | calibration run |
| router | | | | | | calibration run |
| mux | | | | | | calibration run |

Do not invent numbers. Each filled cell must map to a dated run in the metadata section above.
