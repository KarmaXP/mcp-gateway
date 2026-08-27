# Calibration run (full stack: Qdrant + embed + gateway + OTel)

Repeatable procedure to record **live** router and gateway latency numbers with real Qdrant + embedding + telemetry wiring (not synthetic in-tree defaults). See the architecture plan for the latency budget and semantic router acceptance criteria.

## End-to-end boot sequence

From `mcp-gateway/`, choose one boot path:

1. **Compose dependencies + host gateway**
  ```bash
  make docker-up
  ```
2. **Full compose stack (includes gateway container)**
  ```bash
  make docker-up-full
  ```
  (Equivalent helper target: `make calibration-up`.)

Wait for compose health:

```bash
docker compose -f deployments/docker-compose.yaml --env-file .env ps
```

Verify dependency endpoints from host:

```bash
curl -sf http://127.0.0.1:6333/healthz
curl -sf http://127.0.0.1:8001/healthz
curl -sf http://127.0.0.1:4318/
```

If you are running gateway on host (`make docker-up` path), export calibration env first:

```bash
export AUTH_MODE=none
export MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml
export ROUTER_MODE=${ROUTER_MODE:-assist_list}
export QDRANT_URL=${QDRANT_URL:-http://127.0.0.1:6333}
export EMBED_URL=${EMBED_URL:-http://127.0.0.1:8001}
export OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4318}
export PORT=${PORT:-8080}
make run
```

Recorded multiupstream benchmark and LangGraph agent integration runs in [calibration-results.md](calibration-results.md) used **`PORT=18080`** on the host gateway (compose gateway may still bind **8080**).

Gateway health/readiness check (works with current or stricter future readiness):

```bash
# compose gateway:
curl -sf http://127.0.0.1:${HOST_PORT_GATEWAY:-8080}/healthz
curl -fsS "http://127.0.0.1:${HOST_PORT_GATEWAY:-8080}/readyz" >/dev/null \
 || curl -fsS "http://127.0.0.1:${HOST_PORT_GATEWAY:-8080}/healthz" >/dev/null

# host gateway:
curl -sf "http://127.0.0.1:${PORT}/healthz"
curl -fsS "http://127.0.0.1:${PORT}/readyz" >/dev/null \
 || curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null
```

## Calibration checks

1. **Unit / synthetic eval (CI-style):**
  ```bash
  go test -race ./internal/router/...
  go test -race ./internal/routertest/...
  ```
2. **Integration (real Qdrant + embed):**
  ```bash
  make test-integration
  ```
  (Uses `-tags=integration` as defined in the Makefile.)
3. **Vector recall (MiniLM + Qdrant):** run the integration recall harness (recall@1 + recall@3):
  ```bash
  QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:8001 \
   go test -tags=integration -race ./internal/routertest -run TestRouterEvalVectorRecallMiniLM -v
  ```
  - Optional catalog override: set `ROUTER_EVAL_CATALOG_PATH=/absolute/path/to/router-eval-catalog.json`.
  - Default file in-repo: `docs/evaluation/router-eval-catalog.json` (`make gen-router-eval-catalog`).
  - If no catalog file is found, the test falls back to `SyntheticCatalog()`.
4. **Optional load / soak:** if you use `cmd/loadtest`, document command line, duration, and concurrent clients.

## Metrics definitions (GoldenCases)

Use these when reporting retrieval quality from `go test ./internal/routertest/...` and especially `TestGoldenCasesMRRAndNDCG`.

- **MRR@5 (Mean Reciprocal Rank):** for each case, find the first rank position `r <= 5` of the highest-relevance tool and score `1/r` (or `0` if not present), then average across all cases.
- **nDCG@5 (Normalized Discounted Cumulative Gain):** compute DCG over the top 5 candidates with graded relevance:
 `DCG@5 = Σ((2^rel_i - 1) / log2(i + 1))`, then divide by ideal DCG@5 (best possible ordering for that case), and average across cases.
- **Relevance labels in this harness:** target tool relevance is highest (`3`), same-prefix alternatives are weakly relevant (`1`), all others are `0`.

Quick run:

```bash
go test ./internal/routertest/... -run TestGoldenCasesMRRAndNDCG -v
```

## Load testing (direct vs semantic)

Run from the `mcp-gateway/` module root in two terminals.

Examples default to **`PORT=8080`**. To reproduce recorded multiupstream and LangGraph agent integration run numbers, use **`PORT=18080`** (or `GATEWAY_URL=http://127.0.0.1:18080` in smoke scripts).

### Go MCP loadtest (`cmd/loadtest/main.go`)

See also [cmd/loadtest/README.md](../../cmd/loadtest/README.md). Default direct tool is **`alpha__echo`** (`gateway.example.yaml` + `make demo-upstreams`).

```bash
# Terminal 1: direct path (exact tool name), router off
make demo-upstreams
MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml PORT=8080 make run
```

```bash
# Terminal 2
go run ./cmd/loadtest -url http://127.0.0.1:8080 -mode direct -workers 10 -duration 45s
```

```bash
# Semantic path (vector router), router on + embed sidecar
make docker-up
make demo-upstreams
ROUTER_MODE=on EMBED_URL=http://127.0.0.1:8001 QDRANT_URL=http://127.0.0.1:6333 \
  MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml PORT=8080 make run
```

```bash
go run ./cmd/loadtest -url http://127.0.0.1:8080 -mode semantic -workers 10 -duration 45s
```

Record the printed `throughput_rps_est`, `latency_p95_ms`, and `latency_p99_ms` for both modes.

### HTTP baseline (`cmd/loadtest/k6_http_baseline.js`)

The k6 script probes `GET /healthz` and `GET /readyz`. Run it once with gateway in direct mode and once with gateway in semantic mode to compare baseline HTTP overhead.

```bash
# Direct-mode gateway baseline
BASE_URL=http://127.0.0.1:8080 k6 run --vus 30 --duration 60s cmd/loadtest/k6_http_baseline.js
```

```bash
# Semantic-mode gateway baseline (same k6 script, gateway started with ROUTER_MODE=on)
BASE_URL=http://127.0.0.1:8080 k6 run --vus 30 --duration 60s cmd/loadtest/k6_http_baseline.js
```

## Versioned index text template

When preparing a calibration catalog, keep the embedding document format pinned to the template version in `internal/router/index/format.go` (`TemplateVersion`).

Current template (`v1`):

```text
Tool: <namespaced_tool_name>
Description: <tool_description>
Parameters: <sorted_comma_separated_param_keys_or_(none)>
Template: v1
```

Recommended query text shape for eval intents:

```text
Intent: <normalized_intent_text_or_(none)>
ToolName: <incoming_tool_name_or_(none)>
ArgumentKeys: <sorted_comma_separated_argument_keys_or_(none)>
```

For reproducible runs, record both the catalog identifier and template version together (for example: `catalog=router-eval-sre template=v1`).

## Record (template for new runs)

Use when capturing a **new** calibration session. Canonical numbers for calibration (2026-05-18), multiupstream benchmark (2026-05-30), and LangGraph agent integration run (2026-06-08) are already in [calibration-results.md](calibration-results.md). Record measured values only; mark **Not measured** / **Not used** with reason when skipped.

| Metric / artifact | Command or source | Value | Notes |
| ----------------- | ----------------- | ----- | ----- |
| Catalog size (tools) | | | |
| recall@k (synthetic or labeled set) | `go test ./internal/routertest/... -run …` or custom eval | | Define k and dataset id |
| MRR@5 (GoldenCases) | `go test ./internal/routertest/... -run TestGoldenCasesMRRAndNDCG -v` | | Reciprocal rank of first highest-relevance hit |
| nDCG@5 (GoldenCases) | `go test ./internal/routertest/... -run TestGoldenCasesMRRAndNDCG -v` | | Graded ranking quality (normalized to ideal order) |
| Router decision p95 (embed+vector path) | Prometheus / Grafana from `mcp.gateway.semantic_router.duration_seconds` or traces | | Layer breakdown (`exact` vs `vector`) |
| Internal hop p95 | `mcp.gateway.internal.duration_seconds` by `phase` (`parse`, `security`, `router`, `mux`) | | Align with architecture latency budget for gateway-only work |
| Embed service p95 | Your sidecar metrics or traces | | |
| Qdrant query p95 | Qdrant metrics or gateway `mcp.router.semantic` span children | | |

Use `docs/evaluation/calibration-results.md` as the canonical place to copy the measured recall values from `TestRouterEvalVectorRecallMiniLM` and keep run metadata (date, env, catalog source).

## Internal p95 gate script

Run from `mcp-gateway/`:

```bash
# Default gate (50ms)
bash scripts/check_gateway_p95.sh
```

```bash
# Custom threshold / Prometheus endpoint
P95_THRESHOLD_MS=60 PROMETHEUS_URL=http://127.0.0.1:9090 bash scripts/check_gateway_p95.sh
```

Behavior:

- Primary source: Prometheus histogram query over `mcp.gateway.internal.duration_seconds` (translated Prom name usually `mcp_gateway_internal_duration_seconds_bucket`, collector namespace may prepend an extra `mcp_`).
- Fallback (default enabled): if histogram data is unavailable, script runs `go run ./cmd/loadtest` and gates on printed `latency_p95_ms`.
- Limitation: loadtest fallback is **approximate** and includes end-to-end client-observed latency (SSE + JSON-RPC roundtrip), so it is not a pure gateway-internal phase metric.
- CI/manual runs that should not fail on missing metrics can set `SKIP_IF_NO_METRICS=1` and `ALLOW_LOADTEST_FALLBACK=0`.

## Histogram exemplars (Grafana to Tempo)

Operator checklist to confirm trace exemplars are linked from histogram panels:

1. Start stack (`make docker-up`) and ensure Grafana, Prometheus, Tempo, and OTel collector are healthy.
2. Generate traffic (for example, one direct and one semantic load test from the section above) so histogram buckets receive samples.
3. In Grafana, open a histogram-backed panel/query such as `mcp_gateway_internal_duration_seconds_bucket` (or your translated name) and enable exemplar markers.
4. Click an exemplar marker on the histogram series and open the linked Tempo trace.
5. Verify the trace contains gateway spans for the same request window (`mcp.host.request` and child spans like security/router/mux).
6. If exemplar markers are absent, note datasource/link configuration and record the outcome in calibration-results.

If the full stack is not runnable in your environment, document this as: **"capture during full-stack calibration run"**.

## Prometheus (OTel → Prom naming)

The application registers `mcp.gateway.internal.duration_seconds`. After OTel → Prometheus translation, series often appear as `mcp_gateway_internal_duration_seconds_bucket` (histogram). Use your scrape/relayer’s actual metric names when writing queries.

### Internal phase quantiles (p50 / p95 / p99)

Use this procedure to report gateway-only latency for `parse`, `security`, `router`, and `mux`.

1. Confirm the histogram family exists in your Prometheus target:
  ```promql
  mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}
  ```
2. Pick one fixed lookback window for the run (for example `5m`) and keep it unchanged across comparisons.
3. Run the quantile queries below and export values either in seconds or multiplied by `1000` (ms).

Example PromQL (all phases in one query per quantile):

```promql
histogram_quantile(
 0.50,
 sum by (le, phase) (
  rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])
 )
)
```

```promql
histogram_quantile(
 0.95,
 sum by (le, phase) (
  rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])
 )
)
```

```promql
histogram_quantile(
 0.99,
 sum by (le, phase) (
  rate(mcp_gateway_internal_duration_seconds_bucket{phase=~"parse|security|router|mux"}[5m])
 )
)
```

Per-phase drill-down example (`router`, p95):

```promql
histogram_quantile(
 0.95,
 sum by (le) (
  rate(mcp_gateway_internal_duration_seconds_bucket{phase="router"}[5m])
 )
)
```

If your exporter keeps dot names instead of underscore mapping, adapt metric names to your OTel -> Prometheus translation.

### Gateway-internal vs upstream latency (Tempo)

Use Tempo traces to decompose each request latency into gateway internal work vs upstream waiting time.

Telemetry attribute references:

- `docs/DEVELOPER.md` (Observability section for span names and attributes).
- `internal/telemetry/attrs.go` (canonical attribute keys such as `mcp.method`, `mcp.tool.name`, `mcp.backend.id`).

Procedure:

1. Query request spans (TraceQL):
  ```traceql
  { name = "mcp.host.request" && span.mcp.method = "tools/call" }
  ```
2. Narrow to a specific request using one or more span attributes:
  - `span.mcp.jsonrpc.id`
  - `span.mcp.session.id`
  - `span.mcp.tool.name`
3. For the selected trace, record:
  - `T_total` = duration of `mcp.host.request`.
  - `T_upstream` = sum of child `mcp.backend.call` durations.
4. Compute:
  - Single upstream path: `T_internal = T_total - T_upstream`.
  - Fan-out path: report both
   - `T_upstream_sum = sum(all mcp.backend.call spans)`.
   - `T_internal_exclusive = T_total - upstream critical path`.
5. Cross-check that trace-derived internal latency trends match Prometheus phase quantiles from the internal phase quantiles section above.

For **new** calibration runs (canonical numbers already in [calibration-results.md](calibration-results.md)): store one Tempo trace URL or screenshot next to quantitative results so decomposition is auditable.

## Recorded results (repository)

Canonical numbers live in [calibration-results.md](calibration-results.md):

| Run | Date | Scenario |
|-----|------|----------|
| Calibration | 2026-05-18 | `gateway.example.yaml`, demo mocks, `AUTH_MODE=none`, recall + direct loadtest |
| Multiupstream benchmark | 2026-05-30 | `gateway.real.yaml`, stdio MCP, JWT, OTLP; see [scenario-real-upstreams-jwt.md](scenario-real-upstreams-jwt.md) |
| LangGraph agent integration run | 2026-06-08 | Multiupstream benchmark + MCP host demo, LangGraph agent, Tempo trace, JWT loadtest; see [integration-checklist.md](integration-checklist.md#langgraph-agent-integration-run) |

**Internal 50 ms budget (multiupstream benchmark):** evidence uses Prometheus **mean** latency per phase (`tools/call`), all ≪ 50 ms. Histogram p95 is **not used** when sub-ms samples fall into the first 5 s bucket (artefact ~4750 ms).

## Post-calibration (operator tuning, optional)

These are not required for the reference repository; calibration and multiupstream benchmark numbers are already recorded in [calibration-results.md](calibration-results.md).

- **Hybrid reranking:** RRF vs `hybrid_alpha` ablation when retuning router weights.
- **Vector index tuning:** Qdrant HNSW tuning when recall/latency goals change.

## References

- `docs/architecture/mcp_gateway.plan.md`: semantic router modes (`filter_list`), metrics, latency budget.
- `docs/DEVELOPER.md`, `ROUTER_MODE`, observability, integration tests.
