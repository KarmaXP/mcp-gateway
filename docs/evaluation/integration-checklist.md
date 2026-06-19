# Integration checklist (gateway + backends + optional agent)

Reproducibility runbook for the three documented lab scenarios. **Canonical recorded results** for the multibackend and LangGraph agent assays are already in [calibration-results.md](calibration-results.md) (*Multibackend assay* 2026-05-30; *LangGraph agent assay* 2026-06-08). Use this checklist to re-run or verify; you do not need to re-record unless you change stack or config.

Use a **single session** to validate the gateway (and optionally the full host stack) with one gateway process up; do not restart between steps unless noted.

---

## Choose a scenario

| Scenario | Config | Auth | Backends | Scope |
|----------|--------|------|----------|--------|
| **SRE mock multibackend** | `gateway.sre.example.yaml` | `AUTH_MODE=none` | HTTP mocks (`make sre-up`) | Gateway + loadtest without JWT |
| **Multibackend assay** | `gateway.real.yaml` | `AUTH_MODE=jwt` | stdio MCP via `npx` | Gateway evidence (smoke, JWT, Prom means, recall) — [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |
| **LangGraph agent assay** | Same as multibackend assay | `AUTH_MODE=jwt` | Same as multibackend assay | Multibackend assay **plus** MCP host demo, agent (e.g. LangGraph), Tempo capture, JWT load — section [LangGraph agent assay](#langgraph-agent-assay) below |

The multibackend assay **does not** use `make sre-up`. Its primary load evidence uses JWT smoke traffic + Prometheus internal means (documented in calibration-results). JWT-aware `scripts/loadtest` is supported via `-token` and was measured in the LangGraph agent assay.

The LangGraph agent assay **extends** the multibackend assay in the same session (or immediately after, same gateway config). Record outcomes under *LangGraph agent assay* in [calibration-results.md](calibration-results.md).

---

## Prerequisites

| Step | SRE mock | Multibackend assay |
|------|----------|-------------------|
| Dependencies | `make docker-up` | `make docker-up` (no `sre-up`) |
| Mocks (optional) | `make sre-up` | — |
| Config | `MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml` | `MCP_GATEWAY_CONFIG=deployments/gateway.real.yaml` |
| Router | `ROUTER_MODE=on`, `QDRANT_URL`, `EMBED_URL` | same |
| OTLP (Prom/Grafana) | `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318` | **required** |
| Embed probe | `POST /embed` with JSON field `texts` (not `inputs`) | same |

### SRE mock — start gateway

```bash
export AUTH_MODE=none
export MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml
export ROUTER_MODE=on
export QDRANT_URL=http://127.0.0.1:6333
export EMBED_URL=http://127.0.0.1:8001
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export PORT=18080
make run
```

### Multibackend assay — start gateway

See [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) (JWT keys, `PORT=18080`, macOS filesystem path).

Check: `curl -sf http://127.0.0.1:${PORT}/readyz` (or `/healthz`).

---

## 1. Upstreams and namespaced tools

| Check | SRE mock | Multibackend assay |
|-------|----------|-------------------|
| `tools/list` returns `prefix__name` | ✓ | ✓ |
| `tools/call` on three silos | `make sre-smoke` or manual | smoke_e2e ×3 (prom/k8s/gh) |
| YAML documented | `gateway.sre.example.yaml` | `gateway.real.yaml` |

Manual flow (SRE mock): [scenario-sre-multibackend.md](scenario-sre-multibackend.md).

---

## 2. Minimal MCP host (same protocol as your agent)

Proves SSE + `Mcp-Session-Id` + initialize handshake without an LLM.

```bash
GATEWAY_URL=http://127.0.0.1:${PORT} go run ./scripts/mcp_host_demo
```

With JWT (multibackend assay): set `GATEWAY_JWT` or use `scripts/smoke_e2e.sh` with `SMOKE_JWT`.

| Check | Done |
|-------|------|
| Session id received on SSE | |
| `tools/list` and `tools/call` complete on the SSE stream | |

Details: [scripts/mcp_host_demo/README.md](../../scripts/mcp_host_demo/README.md).

---

## 3. JWT and allow-list

| Scenario | Action |
|----------|--------|
| SRE mock | Skip if `AUTH_MODE=none`; document skip |
| Multibackend assay | **Required** — [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md) + [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |

| Check | Expected (multibackend assay, recorded) |
|-------|----------------------------------------|
| Allowed `tools/call` | SMOKE OK |
| Disallowed `tools/call` | JSON-RPC **-32003** |

---

## 4. Latency and load

### SRE mock — loadtest (`AUTH_MODE=none`)

```bash
go run ./scripts/loadtest -url http://127.0.0.1:${PORT} -mode direct -workers 10 -duration 45s
go run ./scripts/loadtest -url http://127.0.0.1:${PORT} -mode semantic -workers 10 -duration 45s
```

Copy `latency_p95_ms` into [calibration-results.md](calibration-results.md) (calibration section). Semantic mode needs a catalog matching your intents.

### Multibackend assay — JWT smoke load

Repeat `scripts/smoke_e2e.sh` (parallel + sequential) with `SMOKE_JWT`. Record Prometheus **mean** internal latency per phase — see [calibration-results.md](calibration-results.md). Do **not** rely on histogram p95 for sub-ms phases.

| Mode | Multibackend assay status (2026-05-30) |
|------|--------------------------------------|
| loadtest direct/semantic under JWT | Not measured in that session (historical); measured in LangGraph agent assay with `-token` |
| JWT smoke | 60× parallel + 20× sequential |

More detail: [calibration-run.md](calibration-run.md), [scripts/loadtest/README.md](../../scripts/loadtest/README.md).

---

## 5. Observability by phase

With `make docker-up` and OTLP exported from the gateway:

1. Generate traffic (step 4).
2. Query internal phase latency — **mean** recommended when samples are sub-ms ([calibration-results.md](calibration-results.md)).
3. Optional Tempo: [router-trace-capture.md](router-trace-capture.md) (Tempo is in-cluster only in default Compose; host capture via Grafana).

During a re-run, if Prometheus/Tempo steps are skipped, mark **Not measured** with reason in [calibration-results.md](calibration-results.md). Multibackend assay (2026-05-30) means are already recorded there.

---

## 6. Wire your agent

After steps 1–2 (and 3 for JWT):

1. Point your host at the same `GATEWAY_URL`.
2. Use namespaced tool names from `tools/list`.
3. Send `Authorization: Bearer` when using JWT.
4. Send `X-MCP-Intent` on ambiguous `tools/call` when the semantic router is enabled.

Host integration: [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

---

## LangGraph agent assay

Run **after** multibackend assay checks (or repeat multibackend sanity first). Extends the multibackend assay with MCP host demo, conversational agent, optional Tempo, and JWT-aware load.

### Prerequisites (same as multibackend assay)

[scenario-real-backends-jwt.md](scenario-real-backends-jwt.md): `make docker-up`, JWT keys, `gateway.real.yaml`, `ROUTER_MODE=on`, OTLP, three stdio backends. Keep `PORT=18080` stable.

### MCP host demo (no LLM)

Same protocol as step [2. Minimal MCP host](#2-minimal-mcp-host-same-protocol-as-your-agent), with JWT on every HTTP call via `GATEWAY_JWT`.

| Check | Expected |
|-------|----------|
| SSE session id | Received |
| `tools/list` | Namespaced tools |
| `tools/call` | At least one success on a namespaced tool |

See [scripts/mcp_host_demo/README.md](../../scripts/mcp_host_demo/README.md) for `GATEWAY_JWT` and `TOOL_ARGS`.

### Agent (LangGraph or equivalent)

| Check | Expected |
|-------|----------|
| Same `GATEWAY_URL` as multibackend assay | No config drift |
| `Authorization: Bearer` | When `AUTH_MODE=jwt` |
| `tools/call` via agent | ≥1 success; capture log or trace id for the evaluation record |

Optional: `X-MCP-Intent` on an ambiguous call when the router is on.

Details: [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

### Observability (Tempo)

1. Generate traffic (agent or smoke).
2. Open Grafana (Compose stack), Prometheus + Tempo datasources.
3. Follow [router-trace-capture.md](router-trace-capture.md) (optional; 2026-06-08 LangGraph agent assay recorded one Tempo trace as **Measured** via Grafana proxy — see [calibration-results.md](calibration-results.md)).
4. On re-runs only: store one trace link or screenshot next to new results.

If Tempo capture is skipped during a re-run, mark **Not measured** with reason in the *LangGraph agent assay* table (2026-06-08 already records one trace as **Measured** via Grafana proxy; see [calibration-results.md](calibration-results.md)).

### Load under JWT

| Option | Command / approach |
|--------|-------------------|
| **Preferred** | `scripts/loadtest` with `-token` / `LOADTEST_JWT`, `-tool`, `-args` (use `-workers 1` under JWT; see [errors.md](../errors.md#known-limitations-multiplexing)) |
| **Alternative** | Repeat JWT `smoke_e2e` (parallel + sequential) as in the multibackend assay; record Prom **means** only |

### Record results (re-runs only)

When re-running the LangGraph agent assay after stack or config changes, copy measured values into [calibration-results.md](calibration-results.md) → **LangGraph agent assay**. Do not invent numbers. Mark **Not measured** with a one-line reason for every skipped row. The 2026-06-08 session is already recorded; reviewers can cite that table as-is.

### What the LangGraph agent assay does not claim

- Production Kubernetes / Prometheus / GitHub APIs (still reference MCP servers over stdio).
- Replacing multibackend assay numbers. The multibackend assay remains the primary recorded gateway benchmark.
- OTel histogram p95 artefacts for sub-ms internal phases (documented limitation; use **means** — see [calibration-results.md](calibration-results.md) and [errors.md](../errors.md#known-limitations-multiplexing)).

---

## Quick reference

| Goal | Doc / command |
|------|----------------|
| Real stdio + JWT (multibackend assay) | [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |
| LangGraph agent assay (host + agent + Tempo + JWT load) | [LangGraph agent assay](#langgraph-agent-assay) above |
| Add a real upstream | [ADDING_BACKENDS.md](../ADDING_BACKENDS.md) |
| SRE three-backend mocks | [scenario-sre-multibackend.md](scenario-sre-multibackend.md) |
| Record recall/latency | [calibration-results.md](calibration-results.md) |
| CI-style unit tests | `make ci` from `mcp-gateway/` |
