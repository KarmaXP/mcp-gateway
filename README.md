# MCP Gateway

Production-oriented **Model Context Protocol (MCP)** multiplexer and orchestrator in **Go**: one host-facing HTTP surface (SSE + JSON-RPC), many backends, optional **semantic routing** over **Qdrant**, and **OpenTelemetry**-first observability.

## Architecture

```mermaid
flowchart LR
  subgraph Host
    H[AI Host / Client]
  end
  subgraph Gateway["mcp-gateway (Go)"]
    HTTP[HTTPServer\nSSE + POST /mcp/rpc]
    Sess[Session Manager]
    Mpx[Multiplexer]
    R[Semantic Router]
    HTTP --> Sess --> Mpx
    Mpx --> R
  end
  subgraph Backends
    B1[MCP Backend A]
    B2[MCP Backend B]
  end
  subgraph DataPlane["Data plane (optional)"]
    E[Embed sidecar\nONNX / MiniLM]
    Q[(Qdrant)]
  end
  OTel[OTLP\nTempo · Prometheus · Grafana]
  H <-- text/event-stream + JSON-RPC --> HTTP
  Mpx --> B1
  Mpx --> B2
  R --> E
  R --> Q
  Gateway -. traces / metrics .-> OTel
```

- **Ingress:** `GET /mcp/sse` opens the session; JSON-RPC **responses** (for requests with `id`) are pushed as **SSE named events** `event: jsonrpc` with a single-line JSON-RPC object in `data:` (see OpenAPI). `POST /mcp/rpc` accepts one request per call (`202` when accepted).
- **Intent for routing:** Optional header **`X-MCP-Intent`** on `POST /mcp/rpc` is forwarded into the semantic router as `RoutingSignal.IntentText` for **`tools/call`** (improves recall when the tool name is vague). Omitted header means empty intent.
- **Core:** `internal/gateway/multiplex` (`Multiplexer`) merges `initialize` and `tools/list`; `tools/call` is forwarded with stable namespacing (`prefix__tool`).
- **Router:** `internal/router` optionally rewrites ambiguous tool names using embeddings + vector search (see [ADR 0001](docs/adr/0001-architecture-decisions.md)). **`filter_list`** (intent-filtered `tools/list`) is explicitly **deferred** to Phase 3 — [ADR 0002](docs/adr/0002-tools-list-filter-list-deferred.md).

## Why this stack

| Choice | Why |
|--------|-----|
| **Go** | Small static binary, excellent concurrency, fast JSON-RPC and HTTP, first-class context cancellation for timeouts and graceful shutdown—fits SRE-style services and low-latency gateways. |
| **Qdrant** | Fast filtered ANN search over tool vectors; clear HTTP API and Docker story; supports catalog versioning and explainable candidates. |
| **ONNX / local embeddings** | Keeps routing signals on-prem, removes per-call embedding API cost and tail latency from the public internet, and simplifies air-gapped or CI runs (privacy + predictability). |

## Quick start (Makefile)

Clone the repository and run targets from the **module root** (this directory):

```bash
make help
```

Typical flow:

```bash
make docker-up          # Qdrant, embed sidecar, OTel collector, Tempo, Prometheus, Grafana
make run                # gateway on PORT / GATEWAY_PORT (default 18080 with .env patterns)
make test               # vet + race + unit tests
make test-integration   # needs compose (Qdrant + embed); see Makefile
make lint
```

OpenAPI: [`docs/artifacts/openapi/openapi.yaml`](docs/artifacts/openapi/openapi.yaml).

## Configuration highlights

- **Backends:** `MCP_GATEWAY_CONFIG` (YAML) and/or `MCP_GATEWAY_BACKENDS` (JSON list). Each entry has `id`, `prefix`, and either `url` (HTTP+SSE MCP) or `command` (stdio). See [`deployments/gateway.example.yaml`](deployments/gateway.example.yaml).
- **Auth:** `AUTH_MODE=none|jwt` — JWT validates RS256 (PEM or JWKS). Health paths are skipped by default. Optional JWT claim **`mcp_tools`** (array of namespaced tool ids): restricts **`tools/list`** to that subset (metadata leak reduction), enforces the same allow-list on **`tools/call`** (returns `RequestRejected` if the tool is not listed), and feeds the semantic router vector filter. **`tools/call`** arguments are validated against each tool’s aggregated **`inputSchema`** (JSON Schema, draft from `$schema` or Draft 7 default) after the last successful **`tools/list`**. See OpenAPI `bearerAuth` and `.env.example`.
- **Semantic router:** `ROUTER_MODE=on` or `assist_list` requires **`QDRANT_URL`**; tune with `ROUTER_*` env vars or the `router:` block in YAML (`top_k`, `score_min`, `allow_auto_rename`, timeouts, `vector_dim`). `EMBED_URL` / `embed.url` points at the embedding sidecar.
- **Telemetry:** `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g. `http://127.0.0.1:4318`) — traces and metrics export via OTLP HTTP.

## Observability (Prometheus · Grafana · Tempo)

With `make docker-up`, Compose brings up a minimal observability stack (exact service names and ports are in `deployments/docker-compose.yaml`):

- **Prometheus** scrapes gateway-relevant targets where configured.
- **Grafana** dashboards for metrics; **Tempo** receives traces from the OpenTelemetry Collector.
- Application metrics include semantic router outcomes and latency (`mcp.gateway.semantic_router.*`) with **`layer`** labels (`exact`, `rules`, `vector`, …), **`indexed_tools`** (gauge after each successful catalog reindex), and an **active SSE sessions** gauge (`mcp.gateway.active_sse_sessions`).

Structured logs use `log/slog` with a handler that attaches **trace_id** when a span is present.

## Project layout

Standard Go layout: `cmd/`, `internal/`, `deployments/`, `docs/`, `scripts/`. Public reusable libraries would live under `pkg/` if added; this module is primarily an application.

## Code quality guardrail (no magic numbers)

- Runtime numeric defaults and limits are centralized in `internal/defaults`.
- MCP wire/protocol strings are centralized in `internal/gateway/mcpwire`.
- Do not introduce raw numeric literals for tunable behavior in production code; use named constants/defaults.
- CI lint includes `mnd` to catch newly introduced magic numbers in non-test Go code.

## Continuous integration

GitHub Actions: [`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push and pull request: `setup-go` with module cache, `golangci-lint`, unit tests, and a job that starts Qdrant + embed from Compose and runs `-tags=integration` tests.

## License

See [LICENSE](LICENSE).
