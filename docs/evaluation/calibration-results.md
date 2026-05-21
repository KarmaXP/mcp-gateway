# Calibration results

Record **live** router recall and gateway latency here after following [`calibration-run.md`](calibration-run.md).

**Recorded run:** 2026-05-18 (baseline hyperparameters in `deployments/gateway.example.yaml`: `top_k=8`, `score_min=0.35`, `hybrid_alpha=0.2`).

---

## Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | 2026-05-18T18:56:21Z |
| Gateway commit | `67251c25a68247c7ed934863fbc8e946a76ea99c` |
| Environment (host / Docker / CI) | host gateway (`PORT=18080`) + Docker deps (`make docker-up`) + demo mocks (`make demo-backends`) |
| Catalog source (`ROUTER_EVAL_CATALOG_PATH` or default `router-eval-catalog.json`) | default `docs/evaluation/router-eval-catalog.json` |
| Qdrant URL | `http://127.0.0.1:6333` |
| Embed URL | `http://127.0.0.1:18001` (`.env` `HOST_PORT_EMBED=18001`; host port 8001 was already in use) |
| Gateway mode (`ROUTER_MODE`) | `assist_list` (env override over `gateway.example.yaml`) |
| Load profile (tool, workers, duration) | loadtest L2: `alpha__echo` direct, workers=10, duration=45s, URL `http://127.0.0.1:18080` |

Procedure and probes: [`calibration-run.md`](calibration-run.md). Embed sidecar JSON field: `texts` (see `deployments/embed/server.py`).

---

## Vector recall (MiniLM + Qdrant)

Command:

```bash
QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:18001 \
 go test -tags=integration -race -count=1 \
 ./internal/router/eval -run TestRouterEvalVectorRecallMiniLM -v
```

| Metric | Value |
| ------ | ----- |
| recall@1 | 1.000 |
| recall@3 | 1.000 |
| Cases (hits / total) | 26/26 @1, 26/26 @3 |
| Notes | PASS; test thresholds ≥0.60 / ≥0.85; catalog `docs/evaluation/router-eval-catalog.json` |

Test output (`minilm_integration_test.go:99`):

```text
MiniLM+Qdrant router eval (../../../docs/evaluation/router-eval-catalog.json): recall@1=1.000 (26/26) recall@3=1.000 (26/26)
```

Optional supporting metrics (from Prometheus after a semantic load run):

| Metric | Value |
| ------ | ----- |
| Router decision p95 (`mcp.gateway.semantic_router.duration_seconds`) | not measured (semantic load test had 0 successful samples; see load test section) |
| Internal hop p95 (`mcp.gateway.internal.duration_seconds`) | Prometheus NaN over 5m window (no scrape increments during host-gateway run) |

---

## Micro-benchmarks (unit; no Docker)

Measured with:

```bash
go test -run '^$' -bench 'Benchmark(ParseRequest|Namespace(Add|Strip))$' -benchmem ./internal/rpc ./internal/gateway/namespace
```

Sample output (2026-05-18, darwin/arm64 Apple M4 Max):

```text
BenchmarkParseRequest-14    	 1381596	       869.0 ns/op	 135.80 MB/s	     448 B/op	      11 allocs/op
BenchmarkNamespaceAdd-14      	36700941	        32.58 ns/op	 798.11 MB/s	      32 B/op	       1 allocs/op
BenchmarkNamespaceStrip-14    	56601212	        20.92 ns/op	1242.65 MB/s	       0 B/op	       0 allocs/op
```

Golden cases (lexical, no Docker):

```bash
go test ./internal/router/eval/... -run TestGoldenCasesMRRAndNDCG -v
```

```text
golden metrics lexical baseline: MRR=1.000 nDCG@5=0.907
```

---

## Internal latency by phase (Prometheus)

PromQL (5m lookback; OTel→Prom metric: `mcp_mcp_gateway_internal_duration_seconds_bucket`):

```promql
histogram_quantile(0.95, sum by (le, phase) (rate(mcp_mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])))
```

| Phase | p50 (ms) | p95 (ms) | p99 (ms) | Query time (UTC) | Notes |
| ----- | -------- | -------- | -------- | ---------------- | ----- |
| parse | | | | 2026-05-18T18:56:10Z | NaN — 0 samples `increase(...[10m])` after host loadtest |
| security | | | | 2026-05-18T18:56:10Z | NaN |
| router | | | | 2026-05-18T18:56:10Z | NaN |
| mux | | | | 2026-05-18T18:56:10Z | NaN |

---

## Client-observed load test

Gateway: `AUTH_MODE=none ROUTER_MODE=assist_list QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:18001 PORT=18080 make run` + `make demo-backends`.

### Direct (`alpha__echo`)

```bash
go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode direct -workers 10 -duration 45s
```

| Field | Value |
| ----- | ----- |
| throughput_rps_est | 5.00 |
| latency_p50_ms | 0.266 |
| latency_p95_ms | 1.242 |
| latency_p99_ms | 3.591 |
| samples / errors | 225 / 1367 |

### Semantic (vague intent → router)

```bash
go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode semantic -workers 10 -duration 45s
```

| Field | Value |
| ----- | ----- |
| samples / errors | 0 / 1333 |
| Notes | no successful samples; live alpha/beta catalog does not match semantic loadtest intents |

---

## Gateway vs upstream (Tempo)

TraceQL seed:

```traceql
{ name = "mcp.host.request" && span.mcp.method = "tools/call" }
```

| Trace id | Tool | Backend(s) | T_total (ms) | T_upstream (ms) | T_internal (ms) | Path | Notes |
| -------- | ---- | ---------- | ------------ | --------------- | --------------- | ---- | ----- |
| | | | | | | | not run (optional; see `calibration-run.md` Tempo section) |

Cross-check:

| Check | Result | Notes |
| ----- | ------ | ----- |
| p95 internal hop vs 50 ms design goal | PASS (loadtest approx) | direct `latency_p95_ms=1.242`; includes client SSE+JSON-RPC |
| Traces consistent with Prometheus | n/a | Prom phase histograms had no samples in this run |
| `check_gateway_p95.sh` | FAIL | `observed_p95_ms=nan`; Prometheus phase histograms empty in this run |

---

## Summary table

| Scope | recall@1 | recall@3 | MRR | nDCG@5 | p95 internal (ms) | Evidence |
| ----- | -------- | -------- | --- | ------ | ----------------- | -------- |
| golden-cases (lexical, unit) | n/a | n/a | 1.000 | 0.907 | n/a | `go test ./internal/router/eval/... -run TestGoldenCasesMRRAndNDCG -v` |
| MiniLM+Qdrant (integration) | 1.000 | 1.000 | n/a | n/a | n/a | `TestRouterEvalVectorRecallMiniLM` |
| parse | n/a | n/a | n/a | n/a | | Prom NaN |
| security | n/a | n/a | n/a | n/a | | Prom NaN |
| router | n/a | n/a | n/a | n/a | | Prom NaN |
| mux | n/a | n/a | n/a | n/a | | Prom NaN |
| loadtest direct (E2E client) | n/a | n/a | n/a | n/a | 1.242 | `scripts/loadtest -mode direct` |

Do not invent numbers. Each filled cell maps to a dated run in the metadata section above.
