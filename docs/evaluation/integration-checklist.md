# Integration checklist (gateway + backends, before your agent)

Use this **single session** to validate the gateway with real or mock upstreams **before** wiring LangGraph, Cursor, or another MCP host. Run everything with one gateway process up; do not restart between steps unless noted.

For recording measured numbers, use [calibration-results.md](calibration-results.md).

---

## Prerequisites

| Step | Command / check |
|------|-----------------|
| Dependencies | From `mcp-gateway/`: `make docker-up` (Qdrant + embed when using semantic router) |
| SRE mocks (optional) | `make sre-up` — k8s/prom/gh mocks on 3201–3203 |
| Config | `MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml` or your own YAML with real upstreams |
| Router | `ROUTER_MODE=on`, `QDRANT_URL`, `EMBED_URL` set on the **gateway** process |
| Embed probe | `POST /embed` with JSON field `texts` (not `inputs`) must succeed |

Start the gateway once:

```bash
export AUTH_MODE=none
export MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml
export ROUTER_MODE=on
export QDRANT_URL=http://127.0.0.1:6333
export EMBED_URL=http://127.0.0.1:8001
export PORT=8080
make run
```

Check: `curl -sf http://127.0.0.1:${PORT}/readyz` (or `/healthz`).

---

## 1. Upstreams and namespaced tools

Confirm merged catalog and at least one successful call.

| Check | Done |
|-------|------|
| `tools/list` returns namespaced tools (`prefix__name`) | |
| `tools/call` succeeds for a known tool | |
| Upstream URLs or `command` documented in your YAML | |

Manual flow: [scenario-sre-multibackend.md](scenario-sre-multibackend.md).  
Automated: `make sre-smoke` (with mocks and router healthy).

---

## 2. Minimal MCP host (same protocol as your agent)

Proves SSE + `Mcp-Session-Id` + initialize handshake without an LLM.

```bash
GATEWAY_URL=http://127.0.0.1:8080 go run ./scripts/mcp_host_demo
```

| Check | Done |
|-------|------|
| Session id received on SSE | |
| `tools/list` and `tools/call` complete on the SSE stream | |

Details: [scripts/mcp_host_demo/README.md](../../scripts/mcp_host_demo/README.md).

---

## 3. JWT and allow-list (only if you use `AUTH_MODE=jwt`)

Skip this section if you run with `AUTH_MODE=none` and document that choice in your deployment notes.

Follow [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md).

| Check | Done |
|-------|------|
| Allowed tools appear in `tools/list` | |
| Disallowed `tools/call` returns permission denied | |

---

## 4. Latency smoke (direct vs semantic routing)

With the same gateway still running:

```bash
go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode direct -workers 10 -duration 45s
go run ./scripts/loadtest -url http://127.0.0.1:8080 -mode semantic -workers 10 -duration 45s
```

Semantic mode needs a catalog that matches your intents (SRE mocks or tools from your real backends). Copy `latency_p95_ms` and sample counts into [calibration-results.md](calibration-results.md).

| Mode | samples | errors | p95 (ms) |
|------|---------|--------|----------|
| direct | | | |
| semantic | | | |

More detail: [calibration-run.md](calibration-run.md) and [scripts/loadtest/README.md](../../scripts/loadtest/README.md).

---

## 5. Observability by phase (optional)

If Prometheus and the OTel stack are up (`make docker-up`), generate traffic (step 4), then query internal phase latency per [calibration-run.md](calibration-run.md). Optional Tempo traces: [router-trace-capture.md](router-trace-capture.md).

If you skip this, note “metrics not collected for this run” in your results file.

---

## 6. Wire your agent

After steps 1–2 (and 3 if using JWT):

1. Point your host at the same `GATEWAY_URL`.
2. Use namespaced tool names from `tools/list`.
3. Send `X-MCP-Intent` on ambiguous `tools/call` when the semantic router is enabled.

Host integration: [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

---

## Quick reference

| Goal | Doc / command |
|------|----------------|
| Add a real upstream | [ADDING_BACKENDS.md](../ADDING_BACKENDS.md) |
| SRE three-backend walkthrough | [scenario-sre-multibackend.md](scenario-sre-multibackend.md) |
| Record recall/latency | [calibration-run.md](calibration-run.md) |
| CI-style unit tests | `make ci` from `mcp-gateway/` |
