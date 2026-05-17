# Evaluation and walkthroughs

Guides for validating gateway behavior beyond unit tests: scripted scenarios, load tests, and router/latency measurement.

---

## Operational walkthroughs

| Guide | What it exercises |
|-------|-------------------|
| [scenario-sre-multibackend.md](scenario-sre-multibackend.md) | Three backends, namespaced tools, semantic router, traces |
| [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md) | `AUTH_MODE=jwt`, `mcp_tools` filtering and deny on `tools/call` |
| [scenario-backend-down.md](scenario-backend-down.md) | Partial catalog when an upstream is unavailable |

Quick automated checks (from repo root):

```bash
make sre-smoke   # three SRE tool calls via mocks
make smoke     # single-backend MCP curl flow
```

Host client: [`scripts/mcp_host_demo/README.md`](../../scripts/mcp_host_demo/README.md).

---

## Performance and router quality

| Guide | Purpose |
|-------|---------|
| [calibration-run.md](calibration-run.md) | Procedure: Qdrant + embed + gateway + metrics |
| [calibration-results.md](calibration-results.md) | Template to record recall@k and latency numbers |
| [router-trace-capture.md](router-trace-capture.md) | Capture semantic-router spans in Tempo |

Load testing: [`scripts/loadtest/README.md`](../../scripts/loadtest/README.md).

---

## Catalog data

| File | Purpose |
|------|---------|
| [router-eval-catalog.json](router-eval-catalog.json) | Static tool catalog for integration recall tests |
| Regenerate | `make gen-router-eval-catalog` |

---

## Related

- [../README.md](../README.md): documentation index
- [../CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md): host integration
- [../DEVELOPER.md](../DEVELOPER.md): CI and observability
