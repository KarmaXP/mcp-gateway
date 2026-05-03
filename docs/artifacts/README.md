# API and observability artifacts

Operator and build details: **[DEVELOPER.md](../DEVELOPER.md)**.

| Path | Purpose |
|------|---------|
| `openapi/openapi.yaml` | Host HTTP API (health, SSE, JSON-RPC POST) |
| `grafana/mcp-gateway-observability.json` | Grafana dashboard (import + map Prometheus datasource) |

Load testing: [`../../scripts/loadtest/`](../../scripts/loadtest/) (Go client for MCP latency).

CI: [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) at the repository root.
