# Evaluation and walkthroughs

Guides for validating gateway behavior beyond unit tests: scripted scenarios, load tests, and router/latency measurement.

**Multibackend benchmark (2026-05-30):** [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) + [calibration-results.md](calibration-results.md).

**LangGraph agent integration run (2026-06-08):** [integration-checklist.md](integration-checklist.md#langgraph-agent-integration-run) + calibration-results *LangGraph agent integration run*.

---

## Operational walkthroughs

| Guide | What it exercises |
|-------|-------------------|
| [integration-checklist.md](integration-checklist.md) | **End-to-end validation** — SRE mocks, multibackend benchmark (real+JWT), LangGraph agent integration run |
| [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md) | **Real stdio MCP** (everything, filesystem, memory) + JWT + OTLP + Prom |
| [scenario-sre-multibackend.md](scenario-sre-multibackend.md) | Three HTTP mocks, namespaced tools, semantic router |
| [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md) | `AUTH_MODE=jwt`, `mcp_tools` filtering and deny on `tools/call` |
| [scenario-backend-down.md](scenario-backend-down.md) | Partial catalog when an upstream is unavailable |

Quick automated checks (from repo root):

```bash
make ci                    # unit + race tests (no Docker)
make sre-smoke             # SRE mock: k8s/prom/gh HTTP mocks
make smoke
GATEWAY_URL=http://127.0.0.1:8080 bash scripts/smoke_e2e.sh

# Multibackend benchmark (JWT + real stdio backends): see scenario-real-backends-jwt.md
```

Host client: [`scripts/mcp_host_demo/README.md`](../../scripts/mcp_host_demo/README.md).

---

## Performance and router quality

| Guide | Purpose |
|-------|---------|
| [calibration-run.md](calibration-run.md) | Procedure: Qdrant + embed + gateway + metrics (calibration, mocks) |
| [calibration-results.md](calibration-results.md) | **Canonical recorded numbers** — calibration 2026-05-18, multibackend benchmark 2026-05-30, LangGraph agent integration run 2026-06-08 |
| [router-trace-capture.md](router-trace-capture.md) | Capture semantic-router spans in Tempo (optional) |

Load testing: [`scripts/loadtest/README.md`](../../scripts/loadtest/README.md) (`AUTH_MODE=none` by default; pass `-token` or `LOADTEST_JWT` under JWT — see [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md)).

---

## Catalog data

| File | Purpose |
|------|---------|
| [router-eval-catalog.json](router-eval-catalog.json) | Static tool catalog for integration recall tests |
| Regenerate | `make gen-router-eval-catalog` |

Integration recall test: `TestRouterEvalVectorRecallMiniLM` — recall@1/@3 = 1.000 (26/26) as of 2026-05-18 and re-run 2026-05-30.

---

## Related

- [Documentation index](../README.md)
- [../CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md): host integration
- [../DEVELOPER.md](../DEVELOPER.md): CI and observability
