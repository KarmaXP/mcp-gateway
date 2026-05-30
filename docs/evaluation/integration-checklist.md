# Integration checklist (gateway + backends, before your agent)

Use this **single session** to validate the gateway with real or mock upstreams **before** wiring LangGraph, Cursor, or another MCP host. Run everything with one gateway process up; do not restart between steps unless noted.

Recorded numbers (integrated lab run, 2026-05-30): [calibration-results.md](calibration-results.md).

---

## Choose a profile

| Profile | Config | Auth | Backends | Walkthrough |
|---------|--------|------|----------|-------------|
| **A — Mocks + optional loadtest** | `gateway.sre.example.yaml` | `AUTH_MODE=none` | HTTP mocks (`make sre-up`) | Steps below (default commands) |
| **B — Real stdio + JWT (integrated lab)** | `gateway.real.yaml` | `AUTH_MODE=jwt` | stdio MCP via `npx` | [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |

Profile B **does not** use `make sre-up`. Profile B **does not** run `scripts/loadtest` (no Bearer header); use JWT smoke traffic + Prometheus internal means instead (documented in calibration-results).

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
export EMBED_URL=http://127.0.0.1:18001
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

With JWT (profile B): set `Authorization: Bearer` in your client or use `scripts/smoke_e2e.sh` with `SMOKE_JWT`.

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
| loadtest direct/semantic | Not measured (no JWT in loadtest) |
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

## Quick reference

| Goal | Doc / command |
|------|----------------|
| Real stdio + JWT (integrated lab) | [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) |
| Add a real upstream | [ADDING_BACKENDS.md](../ADDING_BACKENDS.md) |
| SRE three-backend mocks | [scenario-sre-multibackend.md](scenario-sre-multibackend.md) |
| Record recall/latency | [calibration-results.md](calibration-results.md) |
| CI-style unit tests | `make ci` from `mcp-gateway/` |
