# Calibration results

Canonical recorded lab results. Procedure: [`calibration-run.md`](calibration-run.md) (baseline) and [`integration-checklist.md`](integration-checklist.md) profile B.

**Rule:** do not invent numbers. If a metric was not measured or is not reliable, mark it **Not measured** or **Not used** with a reason — do not leave table cells blank.

| Run | Date (UTC) | Scope |
| ----- | ---------- | ----- |
| Baseline calibration | 2026-05-18 | Router recall, unit benchmarks, client loadtest (`AUTH_MODE=none`, demo mocks) |
| **Integrated lab run** | 2026-05-30 | Real MCP backends (stdio), JWT, OTLP→Prometheus, smoke load |
| **Full lab session** | 2026-06-08 | Profile C — B plus MCP host demo + LangGraph agent + Tempo + JWT loadtest |

---

## Baseline calibration (2026-05-18)

Hyperparameters: `deployments/gateway.example.yaml` (`top_k=8`, `score_min=0.35`, `hybrid_alpha=0.2`).

### Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | 2026-05-18T18:56:21Z |
| Gateway commit | `67251c25a68247c7ed934863fbc8e946a76ea99c` |
| Environment | Host gateway (`PORT=18080`) + Docker deps (`make docker-up`) + demo mocks (`make demo-backends`) |
| Catalog | default `docs/evaluation/router-eval-catalog.json` |
| Qdrant URL | `http://127.0.0.1:6333` |
| Embed URL | `http://127.0.0.1:18001` (`.env` `HOST_PORT_EMBED=18001`) |
| `ROUTER_MODE` | `assist_list` (env override) |
| Load profile | loadtest L2: `alpha__echo` direct, workers=10, duration=45s |

### Vector recall (MiniLM + Qdrant)

```bash
QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:18001 \
 go test -tags=integration -race -count=1 \
 ./internal/router/eval -run TestRouterEvalVectorRecallMiniLM -v
```

| Metric | Value |
| ------ | ----- |
| recall@1 | 1.000 (26/26) |
| recall@3 | 1.000 (26/26) |
| Notes | PASS; thresholds ≥0.60 / ≥0.85 |

### Micro-benchmarks (unit; no Docker)

```bash
go test -run '^$' -bench 'Benchmark(ParseRequest|Namespace(Add|Strip))$' -benchmem ./internal/rpc ./internal/gateway/namespace
```

| Benchmark | ns/op | MB/s | allocs/op |
| --------- | ----- | ---- | --------- |
| ParseRequest | 869.0 | 135.80 | 11 |
| NamespaceAdd | 32.58 | 798.11 | 1 |
| NamespaceStrip | 20.92 | 1242.65 | 0 |

Golden cases (lexical): MRR=1.000, nDCG@5=0.907 (`TestGoldenCasesMRRAndNDCG`).

### Client-observed load test (`AUTH_MODE=none`)

Gateway: `AUTH_MODE=none ROUTER_MODE=assist_list` + `make demo-backends`.

**Direct (`alpha__echo`):**

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

**Semantic:** 0 / 1333 samples — catalog did not match semantic loadtest intents.

### Baseline — not measured (documented, not repeated)

| Artifact | Status | Reason |
| -------- | ------ | ------ |
| Prom internal phase p50/p95/p99 | **Not measured** | Histogram series had 0 increments after host-gateway loadtest (NaN at query time 2026-05-18T18:56:10Z) |
| Tempo trace decomposition | **Not measured** | Optional step not executed |
| Semantic loadtest latency | **Not measured** | Zero successful samples |

Baseline supplies recall, unit benchmarks, and direct loadtest reference. Operational latency under JWT + real backends is in the **integrated lab run** below.

---

## Integrated lab run (2026-05-30)

**Scope:** single session — real MCP backends (stdio), semantic router (`ROUTER_MODE=on`), JWT, OTLP→Prometheus. No SRE HTTP mocks (`make sre-up` not used).

Config: `deployments/gateway.real.yaml`. Procedure: [`integration-checklist.md`](integration-checklist.md) profile B, [`scenario-real-backends-jwt.md`](scenario-real-backends-jwt.md).

### Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | 2026-05-30 |
| Gateway commit | `a889bff2779a7ac8630dc4224b2d44ab56a99fe5` (includes `gateway.real.yaml`; prior lab session on `cb0a5aa`) |
| Environment | Host gateway (`PORT=18080`) + Docker deps (`make docker-up`); macOS / OrbStack |
| `MCP_GATEWAY_CONFIG` | `deployments/gateway.real.yaml` |
| `AUTH_MODE` | `jwt` (`JWT_ISS=https://tfm.local`, `JWT_AUD=mcp-gateway`, key `/tmp/mcp-tfm-jwt.key`) |
| `ROUTER_MODE` | `on` |
| `GATEWAY_URL` | `http://127.0.0.1:18080` |
| `QDRANT_URL` / `EMBED_URL` | `http://127.0.0.1:6333` / `http://127.0.0.1:18001` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://127.0.0.1:4318` |
| Backends (real, stdio) | `@modelcontextprotocol/server-everything` → `k8s`; `server-filesystem` → `prom` (root `/private/tmp/mcp-tfm-tribunal`); `server-memory` → `gh` |
| JWT traffic | `scripts/smoke_e2e.sh` with admin JWT: 60× parallel + 20× sequential (`prom__read_text_file`) |

**macOS note:** filesystem MCP allowed root is `/private/tmp/...`; tool paths must use `/private/tmp/mcp-tfm-tribunal/...`, not `/tmp/...`.

### Vector recall (regression check, same catalog)

Re-run on 2026-05-30 to confirm no regression with stack up:

```bash
QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:18001 \
 go test -tags=integration -race -count=1 \
 ./internal/router/eval -run TestRouterEvalVectorRecallMiniLM -v
```

| Metric | Value |
| ------ | ----- |
| recall@1 | 1.000 (26/26) |
| recall@3 | 1.000 (26/26) |
| Notes | Same catalog as baseline; independent of JWT/backend YAML in the gateway session |

### Functional E2E (`scripts/smoke_e2e.sh`, JWT)

| Silo | Namespaced tool | Result |
| ---- | --------------- | ------ |
| prom (filesystem) | `prom__read_text_file` | SMOKE OK |
| k8s (everything) | `k8s__echo` | SMOKE OK |
| gh (memory) | `gh__create_entities` | SMOKE OK |

### JWT allow-list

| Check | Result |
| ----- | ------ |
| Token restricted (`-mcp-tools prom__read_text_file`) + `prom__read_text_file` | SMOKE OK |
| Same token + `prom__list_directory` | JSON-RPC **-32003** — `tool "prom__list_directory" not allowed for this principal` |

### Internal latency by phase (Prometheus, `tools/call`)

Metric (OTel→Prom): `mcp_mcp_gateway_internal_duration_seconds_{sum,count}`.

**Primary evidence for the 50 ms internal budget:** mean latency per phase (reliable with sub-ms samples). Histogram `histogram_quantile` p95 can mis-report when the first bucket starts at 5 s; see [calibration-run.md](calibration-run.md).

```promql
sum(rate(mcp_mcp_gateway_internal_duration_seconds_sum{method="tools/call",phase="<phase>"}[5m]))
/
sum(rate(mcp_mcp_gateway_internal_duration_seconds_count{method="tools/call",phase="<phase>"}[5m]))
* 1000
```

| Phase | Mean (ms) | vs 50 ms design goal |
| ----- | --------- | -------------------- |
| parse | 0.0075 | PASS |
| security | 0.0022 | PASS |
| mux | 0.0060 | PASS |
| router | 0.0307 | PASS |

### Integrated run — not measured (protocol choice)

| Artifact | Status | Reason |
| -------- | ------ | ------ |
| `scripts/loadtest` direct/semantic | **Not measured** | Tool does not send `Authorization: Bearer`; session used `AUTH_MODE=jwt`. Substitute: JWT smoke traffic (80 calls) + Prom means above. |
| Tempo `T_total` / `T_upstream` / `T_internal` | **Not measured** | Tempo is not published on the host in `docker-compose.yaml` (in-cluster only). |
| Histogram p95 per phase | **Not used** | Artefact ~4750 ms from bucket layout; `check_gateway_p95.sh` FAIL on this series is expected for sub-ms samples. |

---

## Full lab session (profile C, 2026-06-08)

**Scope:** single session extending profile B in the same gateway config — MCP host demo with JWT, a LangGraph agent host, Tempo trace capture, and a JWT-aware loadtest. **Profile C extends B; it does not replace the B numbers above**, which remain the primary gateway benchmark.

Procedure: [integration-checklist.md](integration-checklist.md) profile C, [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md). Internal notes and command log: `local/mcp-gateway/full-lab-session-results.md`.

### Run metadata

| Field | Value |
| ----- | ----- |
| Date (UTC) | 2026-06-08 |
| Gateway commit | `6e2de406cf64ab190cf2230ea432b2b677ef276e` (+ profile C client changes uncommitted; `make ci` OK 2026-06-08) |
| Environment | Host gateway (`PORT=18080`) + Docker deps (`make docker-up`); macOS / OrbStack |
| `MCP_GATEWAY_CONFIG` / `AUTH_MODE` / `ROUTER_MODE` | `deployments/gateway.real.yaml` / `jwt` / `on` |
| Backends (real, stdio) | `server-everything`→`k8s`; `server-filesystem`→`prom` (root `/private/tmp/mcp-tfm-tribunal`); `server-memory`→`gh` |
| Agent host | `langgraph-demo/` (sibling project): stdlib MCP client + LangGraph `StateGraph` |

### Results

| Artifact | Status | Evidence / notes |
| -------- | ------ | ---------------- |
| `mcp_host_demo` + JWT | **Measured** | SSE session + `tools/list` (namespaced, allow-list filtered to 3 tools) + `tools/call` OK on all three silos: `prom__read_text_file`→"tfm smoke", `k8s__echo`→"Echo: …", `gh__create_entities`→entities created |
| Agent (LangGraph) `tools/call` | **Measured** | Same `GATEWAY_URL` + Bearer. Ran both as a real `langgraph.StateGraph` (`k8s__echo`→"Echo: …") and via a built-in fallback runner (`prom__read_text_file`→"tfm smoke"); ≥1 `tools/call` succeeded via the agent graph |
| Tempo trace / decomposition | **Measured** | Captured via the Grafana datasource proxy (Tempo not host-published). One representative `tools/call` trace (`553af62b…`): `mcp.security.authn` 0.049 ms · `mcp.multiplex.tools_list` 2.57 ms · `mcp.security.authz` 0.0025 ms · `mcp.router.semantic` 0.031 ms · `mcp.validate.json_schema` 0.0045 ms · `mcp.backend.call` (filesystem) 0.846 ms. Single-trace point samples, not percentiles. |
| `scripts/loadtest` with JWT | **Measured (workers=1)** | Bearer + namespaced tool now supported (`-token`/`-tool`/`-args`). `direct` mode, `prom__read_text_file`, 1 worker, 30 s: 10 594 samples, **0 errors**, p50 0.490 ms / p95 0.944 ms / p99 2.031 ms, ≈353 rps (client-observed, includes SSE round-trip + JWT per request). Higher concurrency is blocked by a gateway `tools/list` fan-out id collision (see limitation below). |
| Internal phase means under JWT | **Measured** | Low-rate burst window (`[1m]`, ≈40 `tools/call`): parse 0.0089 · security 0.0049 · mux 0.0064 · router 0.0460 ms — consistent with the integrated run, no regression. Under sustained load (`[5m]`, ≈353 rps loadtest): parse/security/mux < 0.005 ms, **router rises to ≈3.3 ms** (throughput/contention) — still ≪ 50 ms. Means, not histogram p95. |
| JWT deny (`-32003`) | **Measured** | Restricted principal: `prom__list_directory` is filtered out of `tools/list` (SEC2); a direct `tools/call` returns `-32003 "tool \"prom__list_directory\" not allowed for this principal"`. |
| `X-MCP-Intent` call (optional) | **Not measured** | Header is plumbed end-to-end (agent `AGENT_INTENT`→`X-MCP-Intent`), but `gateway.real.yaml` has `allow_auto_rename:false`, so exact names take the deterministic path and the intent does not rewrite the tool. Semantic rename not exercised. |
| Router recall regression | **Not re-measured** | Covered by baseline + integrated run (1.000, 26/26); optional sanity, not repeated this session. |

### Profile C limitations found (filed as follow-ups, not fixed this session)

- **Upstream id forwarding (tools/call):** the multiplexer forwards the host JSON-RPC `id` verbatim to upstreams. Node-based MCP servers round ids above 2^53, so the gateway cannot match the response → `-32000 "backend call failed"`. Worked around by switching the reference clients (`mcp_host_demo`, `loadtest`) to small monotonic ids; `smoke_e2e` (id=4) was always safe.
- **tools/list fan-out id collision (concurrency):** the list fan-out uses a constant per-backend id, so concurrent `tools/list` to the same upstream fail with `duplicate jsonrpc id`. This caps the JWT loadtest at 1 worker (10-worker run: 1 sample / 97 errors). Sequential traffic is unaffected (30/30 smoke OK).

Neither limitation affects the profile B evidence (single-stream smoke with small ids).

---

## Summary

| Claim | Run | Evidence |
| ----- | --- | -------- |
| Router recall@1/@3 on eval catalog | Baseline + integrated regression | 1.000 (26/26) both dates |
| Lexical ranking baseline | Baseline | MRR=1.000, nDCG@5=0.907 |
| Real multibackend MCP + namespacing | Integrated lab | smoke_e2e ×3 (prom/k8s/gh) |
| JWT allow-list enforcement | Integrated lab | allow OK; deny -32003 |
| Internal gateway work ≪ 50 ms | Integrated lab + full lab | Prom **mean** by phase (≪ 50 ms at low rate and under 353 rps) |
| Client-observed throughput/latency (no JWT) | Baseline | loadtest direct p95 ≈ 1.24 ms (includes SSE client path) |
| MCP host + agent (LangGraph) over JWT | Full lab (profile C) | host demo ×3 silos + LangGraph `StateGraph` `tools/call` OK |
| Client-observed latency **under JWT** | Full lab (profile C) | loadtest direct p95 ≈ 0.944 ms, 0 errors (1 worker, 10.6k samples) |
| Trace decomposition (internal vs backend) | Full lab (profile C) | one Tempo trace: internal spans sub-ms, backend.call ≈ 0.85 ms |
