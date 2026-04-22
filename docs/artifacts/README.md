# Thesis delivery artifacts

| Path | Purpose |
|------|---------|
| `openapi/openapi.yaml` | Host HTTP API (health, SSE, JSON-RPC POST) |
| `grafana/mcp-gateway-observability.json` | Grafana dashboard (import + map Prometheus datasource) |

Load testing lives in **`../../scripts/loadtest/`** (Go MCP latency + optional k6 baseline).

CI workflow: **`../../../.github/workflows/ci.yml`** (Git root = `tfm/`). If your remote repository root is only `mcp-gateway/`, move that workflow to `mcp-gateway/.github/workflows/ci.yml`, drop the `mcp-gateway/` prefix from paths, and set `working-directory: .` / `go-version-file: go.mod`.
