# Calibration results

Canonical recorded lab results. Procedure: [`calibration-run.md`](calibration-run.md) (baseline) and [`integration-checklist.md`](integration-checklist.md) profile B.

**Rule:** do not invent numbers. If a metric was not measured or is not reliable, mark it **Not measured** or **Not used** with a reason — do not leave table cells blank.

| Run | Date (UTC) | Scope |
| ----- | ---------- | ----- |
| Baseline calibration | 2026-05-18 | Router recall, unit benchmarks, client loadtest (`AUTH_MODE=none`, demo mocks) |
| **Integrated lab run** | 2026-05-30 | Real MCP backends (stdio), JWT, OTLP→Prometheus, smoke load |

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
| Gateway commit | `a889bff` (includes `gateway.real.yaml`; prior lab session on `cb0a5aa`) |
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

## Summary

| Claim | Run | Evidence |
| ----- | --- | -------- |
| Router recall@1/@3 on eval catalog | Baseline + integrated regression | 1.000 (26/26) both dates |
| Lexical ranking baseline | Baseline | MRR=1.000, nDCG@5=0.907 |
| Real multibackend MCP + namespacing | Integrated lab | smoke_e2e ×3 (prom/k8s/gh) |
| JWT allow-list enforcement | Integrated lab | allow OK; deny -32003 |
| Internal gateway work ≪ 50 ms | Integrated lab | Prom **mean** by phase |
| Client-observed throughput/latency (no JWT) | Baseline | loadtest direct p95 ≈ 1.24 ms (includes SSE client path) |
