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
- **Intent for routing:** Optional header **`X-MCP-Intent`** on `POST /mcp/rpc` is stored in `hostctx` and forwarded as `RoutingSignal.IntentText` for **`tools/call`** and, when `ROUTER_MODE=filter_list`, for **`tools/list`** subsetting. Omitted header means empty intent (see below).
- **Core:** `internal/gateway/multiplex` (`Multiplexer`) merges `initialize` and `tools/list`; `tools/call` is forwarded with stable namespacing (`prefix__tool`).
- **Router:** `internal/router` optionally rewrites ambiguous tool names using embeddings + vector search (see [ADR 0001](docs/adr/0001-architecture-decisions.md)). **`filter_list`** (intent-filtered `tools/list`) is **implemented** behind `ROUTER_MODE=filter_list` / `router.mode: filter_list` — [ADR 0002](docs/adr/0002-tools-list-filter-list-deferred.md). With **`filter_list`**, empty intent returns the **full** merged catalog (after JWT/RAR allow-list only), same as `assist_list` for that request; there is no reuse of intent from earlier RPCs. Vector/embed failures or catalog/index mismatch **degrade** to the full allow-listed catalog (warn logs).
- **Security (RAR ∩ JWT, fail mode):** [ADR 0003](docs/adr/0003-security-rar-jwt-merge-failmode.md) documents the canonical `authorization_details` shape for MCP tools, merge rules for JWT vs RAR allow lists, and fail-closed vs opt-in degradation.

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

OpenAPI: [`docs/artifacts/openapi/openapi.yaml`](docs/artifacts/openapi/openapi.yaml) — documents required headers (`Authorization` when `AUTH_MODE=jwt`), optional `X-MCP-Intent`, W3C **`traceparent` / `tracestate`**, JWT claims (`iss`, `aud`, `exp`, `mcp_tools`, `authorization_details` / RAR), HTTP status semantics vs JSON-RPC errors on SSE, and example **`tools/call`** allow/deny payloads. Gateway error code names match [`internal/gateway/errcodes`](internal/gateway/errcodes/codes.go).

### API contract validation (OpenAPI)

From the repo root, lint the spec with Redocly (no repo dependency; uses [`docs/artifacts/openapi/redocly.yaml`](docs/artifacts/openapi/redocly.yaml)):

```bash
npx --yes @redocly/cli@1 lint --config docs/artifacts/openapi/redocly.yaml docs/artifacts/openapi/openapi.yaml
```

## Configuration highlights

- **Backends:** `MCP_GATEWAY_CONFIG` (YAML) and/or `MCP_GATEWAY_BACKENDS` (JSON list). Each entry has `id`, `prefix`, and either `url` (HTTP+SSE MCP) or `command` (stdio). See [`deployments/gateway.example.yaml`](deployments/gateway.example.yaml).
- **Auth:** `AUTH_MODE=none|jwt` — JWT validates RS256 (PEM or JWKS). Health paths are skipped by default. JWKS fetch or signature failure is **fail-closed** (401). Optional JWT claim **`mcp_tools`** (array of namespaced tool ids): restricts **`tools/list`** to that subset (metadata leak reduction), enforces the same allow-list on **`tools/call`** (returns **`PermissionDenied` (-32003)** if the tool is not listed), and feeds the semantic router vector filter. Optional **`authorization_details`** (RAR-style) and **`mcp_tool_groups`** merge per `internal/policy` and gateway YAML `policy:`. **`tools/call`** arguments are bounded (size/depth/keys) then validated against each tool’s aggregated **`inputSchema`** when present; tools listed under **`policy.elevated_tools`** require a compiled schema (**SEC3**). Schema validation errors returned to the host omit instance values (**SEC5**). **`POST /mcp/rpc`** uses **`MaxBytesReader`** (413 if over limit). Optional **`RATE_LIMIT_*`** (see `.env.example`) applies a per-subject or per-IP token bucket after auth. **`GET /mcp/sse`** emits periodic **comment keepalives** (`: keepalive`) for proxy/TCP hygiene. See OpenAPI `bearerAuth` and `.env.example`.
- **Semantic router:** `ROUTER_MODE=on`, `assist_list`, or **`filter_list`** requires **`QDRANT_URL`**; tune with `ROUTER_*` env vars or the `router:` block in YAML (`top_k`, `score_min`, `allow_auto_rename`, timeouts, `vector_dim`). `EMBED_URL` / `embed.url` points at the embedding sidecar. Modes: **`on`** / **`assist_list`** — full `tools/list`; **`filter_list`** — `tools/list` narrowed by `X-MCP-Intent` when the header is non-empty (policy and vector filters still apply).
- **Telemetry:** `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g. `http://127.0.0.1:4318`) — traces and metrics export via OTLP HTTP.
- **Policy reload:** sending **`SIGHUP`** to the gateway process re-reads **`MCP_GATEWAY_CONFIG`** (full `config.Load()`), rebuilds the in-memory **`policy.Engine`**, and atomically swaps it for new HTTP requests. Each **`POST /mcp/rpc`** re-runs JWT middleware with the **current** engine, so allow-list changes apply on the **next** authenticated RPC without requiring a new SSE connection; hosts that cache **`tools/list`** locally may still want to refresh after a policy change. See [ADR 0003](docs/adr/0003-security-rar-jwt-merge-failmode.md) §Decision 4. Structured policy audit events go through **`policy.AuditSink`** (default: slog + metrics); use **`policy.SetAuditSink`** for tests or alternate sinks.

## Observability (Prometheus · Grafana · Tempo)

With `make docker-up`, Compose brings up a minimal observability stack (exact service names and ports are in `deployments/docker-compose.yaml`):

- **Prometheus** scrapes gateway-relevant targets where configured.
- **Grafana** dashboards for metrics; **Tempo** receives traces from the OpenTelemetry Collector.
- Application metrics include semantic router outcomes and latency (`mcp.gateway.semantic_router.*`) with **`layer`** labels (`exact`, `rules`, `vector`, …), **`indexed_tools`** (gauge after each successful catalog reindex), and an **active SSE sessions** gauge (`mcp.gateway.active_sse_sessions`). **Internal hop** time (gateway-only, excludes upstream MCP I/O) is recorded as a histogram **`mcp.gateway.internal.duration_seconds`** with labels **`method`** (JSON-RPC method when known; `unknown` during JWT on `POST /mcp/rpc` before parse) and **`phase`**: `parse`, `security`, `router`, `mux`. Use phase histograms to track **p95** against the plan §6 internal budget (50 ms p95 goal for gateway-only work; see [calibration runbook](docs/evaluation/calibration-run.md)); Prometheus scrape names may appear as `mcp_gateway_internal_duration_seconds_*` after translation.
- Traces: child span **`mcp.security.authn`** wraps JWT validation (under **`mcp.host.request`** for `POST /mcp/rpc` when `AUTH_MODE=jwt`); **`mcp.security.authz`** remains on `tools/call` allow-list enforcement in the multiplexer.
- **Security-oriented counters** (low-cardinality labels only; no user IDs or request IDs — O5): `mcp.gateway.policy.decisions` (`outcome`: `allow` \| `deny`, `reason`: `allow_list_match` \| `not_in_allow_list` \| `policy_eval_failed` \| `other`), `mcp.gateway.auth.jwks.lookups` (`result`: `hit` \| `refresh` \| `error_fetch` \| `error_missing_kid` \| `error_unknown_kid`), `mcp.gateway.tool_args.validation` (`stage`: `limits` \| `schema`, `result`: `pass` \| `fail`), `mcp.gateway.ratelimit.events` (`result`: `allowed` \| `throttled`), `mcp.gateway.payload.bytes_rejected` (`reason`: `http_body_too_large` \| `tool_args_too_large`). Label enums are defined in `internal/defaults/metrics.go`. Import [`docs/artifacts/grafana/mcp-gateway-observability.json`](docs/artifacts/grafana/mcp-gateway-observability.json) for a **Security** row with PromQL regex matchers.

Example PromQL (Grafana “Security” row, rate over 5m):

```promql
sum by (outcome, reason) (rate(mcp_gateway_policy_decisions_total[5m]))
sum by (result) (rate(mcp_gateway_auth_jwks_lookups_total[5m]))
sum by (stage, result) (rate(mcp_gateway_tool_args_validation_total[5m]))
sum by (result) (rate(mcp_gateway_ratelimit_events_total[5m]))
sum by (reason) (rate(mcp_gateway_payload_bytes_rejected_total[5m]))
```

(Prometheus scrape names may use `_total` suffix and dots mapped to underscores depending on exporter config; align queries with your OTel → Prometheus translation.)

Structured logs use `log/slog` with a handler that attaches **trace_id** when a span is present.

## Project layout

Standard Go layout: `cmd/`, `internal/`, `deployments/`, `docs/`, `scripts/`. Public reusable libraries would live under `pkg/` if added; this module is primarily an application.

## Code quality guardrail (defaults, performance, structure)

- Runtime numeric defaults and limits are centralized in `internal/defaults`.
- MCP wire/protocol strings are centralized in `internal/gateway/mcpwire`.
- Do not introduce raw numeric literals for tunable behavior in production code; use named constants/defaults.
- CI lint includes `mnd` to catch newly introduced magic numbers in non-test Go code.
- **Performance on hot paths** (`tools/list`, `tools/call`, semantic reindex, vector query): avoid unmarshaling or marshaling the same catalog payload twice when maps or rows are already in memory; use appropriate sorting (`sort.Slice` vs nested loops), preallocate when size is known, and reuse derived text (e.g. formatted tool docs) across embedding and rerank. See `.ai/rules/first-pass-quality.md` section 7.
- **Readability:** prefer short functions with one responsibility and split large types across focused files in the same package (e.g. `tools_list.go`, `semantic_router_resolve.go`) rather than growing a single source file. See `.ai/rules/first-pass-quality.md` section 8 and `.ai/rules/go.md`.

## Continuous integration

GitHub Actions: [`.github/workflows/ci.yml`](.github/workflows/ci.yml).

- **Every push and pull request:** `golangci-lint`, `go vet ./...`, and `go test -race` (full module).
- **`main` branch pushes and `workflow_dispatch`:** an **integration** job starts **Qdrant**, **embed**, and **otel-collector** from [`deployments/docker-compose.yaml`](deployments/docker-compose.yaml) and runs `go test -tags=integration -race` for `internal/gateway/httpserver`, `internal/router`, and `internal/telemetry`. Pull requests do not run this job automatically; use **`Actions → CI → Run workflow`** for a manual run, or execute **`make test-integration`** locally (see below).

## Full integration tests locally

`make test-integration` runs `go vet` and integration-tagged tests with defaults aimed at compose-mapped ports (`QDRANT_URL`, `EMBED_URL`, `OTEL_EXPORTER_OTLP_ENDPOINT`).

1. **Dependencies (recommended):** `make docker-up` — brings up Qdrant, embed sidecar, OTel collector (and the rest of the dev stack). Wait until Qdrant (`http://127.0.0.1:6333/healthz`) and embed (`http://127.0.0.1:8001/healthz`) are healthy.
2. **Run:** `make test-integration`.
3. **Behavior:**
   - **`internal/gateway/httpserver`:** JWT **`mcp_tools`** allow-list denial for **`tools/call`** is always exercised (in-process `httptest`; no external services).
   - **`internal/router`:** semantic routing against live Qdrant + embed runs when both are reachable; if either is down and **`CI` is unset**, the test **skips** with a clear message (on CI, missing deps **fail** the job).
   - **`internal/telemetry`:** OTLP shutdown test runs when the collector answers at `OTEL_EXPORTER_OTLP_ENDPOINT`; otherwise it **skips**.

Use **`go test -tags=integration -short ./...`** to skip tests that call `testing.Short()` (currently the JWT policy integration test).

## License

See [LICENSE](LICENSE).
