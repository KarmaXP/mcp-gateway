# Integration checklist (gateway + backends + optional agent)

Use a **single session** to validate the gateway (and optionally the full host stack) with one gateway process up; do not restart between steps unless noted.

Recorded numbers: [calibration-results.md](calibration-results.md) — *Integrated lab run* (profile B, 2026-05-30); *Full lab session* (profile C, 2026-06-08).

---

## Choose a profile

| Profile | Config | Auth | Backends | Scope |
|---------|--------|------|----------|--------|
| **A — Mocks + optional loadtest** | `gateway.sre.example.yaml` | `AUTH_MODE=none` | HTTP mocks (`make sre-up`) | Gateway + loadtest without JWT |
| **B — Real stdio + JWT (integrated lab)** | `gateway.real.yaml` | `AUTH_MODE=jwt` | stdio MCP via `npx` | Gateway evidence (smoke, JWT, Prom means, recall) — [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |
| **C — Full lab session** | Same as B | `AUTH_MODE=jwt` | Same as B | **B plus** MCP host demo, agent (e.g. LangGraph), Tempo capture, JWT load — section [Profile C](#profile-c--full-lab-session) below |

Profile B **does not** use `make sre-up`. Profile B primary load evidence uses JWT smoke traffic + Prometheus internal means (documented in calibration-results). JWT-aware `scripts/loadtest` is supported via `-token` and was measured in profile C.

Profile C **extends** B in the same session (or immediately after, same gateway config). Record outcomes under *Full lab session* in [calibration-results.md](calibration-results.md).

---

## Prerequisites

| Step | Profile A | Profile B |
|------|-----------|-----------|
| Dependencies | `make docker-up` | `make docker-up` (no `sre-up`) |
| Mocks (optional) | `make sre-up` | — |
| Config | `MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml` | `MCP_GATEWAY_CONFIG=deployments/gateway.real.yaml` |
| Router | `ROUTER_MODE=on`, `QDRANT_URL`, `EMBED_URL` | same |
| OTLP (Prom/Grafana) | `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318` | **required** |
| Embed probe | `POST /embed` with JSON field `texts` (not `inputs`) | same |

### Profile A — start gateway

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

### Profile B — start gateway

See [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) (JWT keys, `PORT=18080`, macOS filesystem path).

Check: `curl -sf http://127.0.0.1:${PORT}/readyz` (or `/healthz`).

---

## 1. Upstreams and namespaced tools

| Check | Profile A | Profile B |
|-------|-----------|-----------|
| `tools/list` returns `prefix__name` | ✓ | ✓ |
| `tools/call` on three silos | `make sre-smoke` or manual | smoke_e2e ×3 (prom/k8s/gh) |
| YAML documented | `gateway.sre.example.yaml` | `gateway.real.yaml` |

Manual flow (A): [scenario-sre-multibackend.md](scenario-sre-multibackend.md).

---

## 2. Minimal MCP host (same protocol as your agent)

Proves SSE + `Mcp-Session-Id` + initialize handshake without an LLM.

```bash
GATEWAY_URL=http://127.0.0.1:${PORT} go run ./scripts/mcp_host_demo
```

With JWT (profile B): set `GATEWAY_JWT` or use `scripts/smoke_e2e.sh` with `SMOKE_JWT`.

| Check | Done |
|-------|------|
| Session id received on SSE | |
| `tools/list` and `tools/call` complete on the SSE stream | |

Details: [scripts/mcp_host_demo/README.md](../../scripts/mcp_host_demo/README.md).

---

## 3. JWT and allow-list

| Profile | Action |
|---------|--------|
| A | Skip if `AUTH_MODE=none`; document skip |
| B | **Required** — [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md) + [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |

| Check | Expected (profile B, recorded) |
|-------|--------------------------------|
| Allowed `tools/call` | SMOKE OK |
| Disallowed `tools/call` | JSON-RPC **-32003** |

---

## 4. Latency and load

### Profile A — loadtest (`AUTH_MODE=none`)

```bash
go run ./scripts/loadtest -url http://127.0.0.1:${PORT} -mode direct -workers 10 -duration 45s
go run ./scripts/loadtest -url http://127.0.0.1:${PORT} -mode semantic -workers 10 -duration 45s
```

Copy `latency_p95_ms` into [calibration-results.md](calibration-results.md) (baseline section). Semantic mode needs a catalog matching your intents.

### Profile B — JWT smoke load

Repeat `scripts/smoke_e2e.sh` (parallel + sequential) with `SMOKE_JWT`. Record Prometheus **mean** internal latency per phase — see [calibration-results.md](calibration-results.md). Do **not** rely on histogram p95 for sub-ms phases.

| Mode | Profile B status (2026-05-30) |
|------|-------------------------------|
| loadtest direct/semantic under JWT | Not measured in profile B (historical); measured in profile C with `-token` |
| JWT smoke | 60× parallel + 20× sequential |

More detail: [calibration-run.md](calibration-run.md), [scripts/loadtest/README.md](../../scripts/loadtest/README.md).

---

## 5. Observability by phase

With `make docker-up` and OTLP exported from the gateway:

1. Generate traffic (step 4).
2. Query internal phase latency — **mean** recommended when samples are sub-ms ([calibration-results.md](calibration-results.md)).
3. Optional Tempo: [router-trace-capture.md](router-trace-capture.md) (Tempo is in-cluster only in default Compose; host capture via Grafana).

If skipped, record **Not measured** with reason in calibration-results.

---

## 6. Wire your agent

After steps 1–2 (and 3 for JWT):

1. Point your host at the same `GATEWAY_URL`.
2. Use namespaced tool names from `tools/list`.
3. Send `Authorization: Bearer` when using JWT.
4. Send `X-MCP-Intent` on ambiguous `tools/call` when the semantic router is enabled.

Host integration: [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

---

## Profile C — Full lab session

Run **after** profile B checks (or repeat B sanity first). Goal: close gaps left by B — MCP host without smoke-only path, conversational agent, optional Tempo, JWT-aware load.

### C.0 — Same prerequisites as B

[scenario-real-backends-jwt.md](scenario-real-backends-jwt.md): `make docker-up`, JWT keys, `gateway.real.yaml`, `ROUTER_MODE=on`, OTLP, three stdio backends. Keep `PORT=18080` stable.

### C.1 — MCP host demo (no LLM)

Same protocol as step [2. Minimal MCP host](#2-minimal-mcp-host-same-protocol-as-your-agent), with JWT on every HTTP call via `GATEWAY_JWT`.

| Check | Expected |
|-------|----------|
| SSE session id | Received |
| `tools/list` | Namespaced tools |
| `tools/call` | At least one success on a namespaced tool |

See [scripts/mcp_host_demo/README.md](../../scripts/mcp_host_demo/README.md) for `GATEWAY_JWT` and `TOOL_ARGS`.

### C.2 — Agent (LangGraph or equivalent)

| Check | Expected |
|-------|----------|
| Same `GATEWAY_URL` as B | No config drift |
| `Authorization: Bearer` | When `AUTH_MODE=jwt` |
| `tools/call` via agent | ≥1 success; capture log or trace id for the evaluation record |

Optional: `X-MCP-Intent` on an ambiguous call when the router is on.

Details: [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

### C.3 — Observability (Tempo)

1. Generate traffic (agent or smoke).
2. Open Grafana (Compose stack), Prometheus + Tempo datasources.
3. Follow [router-trace-capture.md](router-trace-capture.md).
4. Store one trace link or screenshot next to results.

If skipped: **Not measured** + reason in *Full lab session* table.

### C.4 — Load under JWT

| Option | Command / approach |
|--------|-------------------|
| **Preferred** | `scripts/loadtest` with `-token` / `LOADTEST_JWT`, `-tool`, `-args` (use `-workers 1` under JWT; see [errors.md](../errors.md#known-limitations-multiplexing)) |
| **Alternative** | Repeat JWT `smoke_e2e` (parallel + sequential) as in B; record Prom **means** only |

### C.5 — Record results

Copy all measured values into [calibration-results.md](calibration-results.md) → **Full lab session**. Do not invent numbers. Mark **Not measured** with a one-line reason for every skipped row.

### What profile C does not claim

- Production Kubernetes / Prometheus / GitHub APIs (still reference MCP servers over stdio).
- Replacing profile B numbers. B remains the primary recorded gateway benchmark.
- Fixing OTel histogram p95 artefacts for sub-ms internal phases (still use means unless buckets change).

---

## Quick reference

| Goal | Doc / command |
|------|----------------|
| Real stdio + JWT (integrated lab) | [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |
| Full lab (B + agent + Tempo + JWT load) | Profile C above |
| Add a real upstream | [ADDING_BACKENDS.md](../ADDING_BACKENDS.md) |
| SRE three-backend mocks | [scenario-sre-multibackend.md](scenario-sre-multibackend.md) |
| Record recall/latency | [calibration-results.md](calibration-results.md) |
| CI-style unit tests | `make ci` from `mcp-gateway/` |
