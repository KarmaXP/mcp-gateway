# SRE scenario: multi-backend namespaced routing

This scenario validates gateway behavior when multiple MCP backends are exposed behind namespaced tools (for example `k8s__get_logs`, `prom__query`, `gh__list_prs`) with semantic routing enabled.

Use this runbook to exercise the B3.2 narrative path with the existing smoke flow (`scripts/smoke_e2e.sh`) or your B3.1 client equivalent.

## 1) Topology and config shape

Follow the same `backends` layout used in `deployments/gateway.example.yaml` (`id`, `prefix`, `url`, `max_concurrency` per backend). For this scenario, use 2-3 backends with distinct prefixes:

- `k8s` backend serving Kubernetes tools (`get_logs`, `list_pods`, ...).
- `prom` backend serving Prometheus query tools (`query`, `query_range`, ...).
- `gh` backend serving GitHub tools (`list_prs`, `get_pr`, ...).

Example (adapt URLs and ports to your deployment):

```yaml
backends:
  - id: backend-k8s
    prefix: k8s
    url: http://127.0.0.1:3201
    max_concurrency: 8
  - id: backend-prom
    prefix: prom
    url: http://127.0.0.1:3202
    max_concurrency: 8
  - id: backend-gh
    prefix: gh
    url: http://127.0.0.1:3203
    max_concurrency: 4
router:
  mode: on
```

Set runtime env consistent with semantic routing:

- `ROUTER_MODE=on`
- `QDRANT_URL=<your-qdrant-url>`
- `EMBED_URL=<your-embed-url>`

## 2) Walkthrough (B3.1 client or smoke scripts)

Use one client session over MCP SSE + JSON-RPC. The sequence below mirrors `scripts/smoke_e2e.sh` semantics, replacing the tool names with multi-backend targets.

1. Start gateway with the multi-backend config and verify `GET /healthz` and `GET /readyz`.
2. Open `GET /mcp/sse` and capture `Mcp-Session-Id`.
3. Send `initialize`, then `notifications/initialized`.
4. Send `tools/list`.
5. Send three `tools/call` requests in the same session:
   - `k8s__get_logs` (cluster diagnostics)
   - `prom__query` (service SLI snapshot)
   - `gh__list_prs` (deployment/code-change context)

If you are using `scripts/smoke_e2e.sh`, run it three times with backend-specific expectations (or extend your local copy to issue all three calls in one run):

```bash
SMOKE_EXPECT_TOOL=k8s__get_logs SMOKE_EXPECT_TEXT=<expected-k8s-marker> bash scripts/smoke_e2e.sh
SMOKE_EXPECT_TOOL=prom__query SMOKE_EXPECT_TEXT=<expected-prom-marker> bash scripts/smoke_e2e.sh
SMOKE_EXPECT_TOOL=gh__list_prs SMOKE_EXPECT_TEXT=<expected-gh-marker> bash scripts/smoke_e2e.sh
```

## 3) Expected router behavior (`ROUTER_MODE=on`)

- `tools/list` remains the full merged namespaced catalog (subject to auth/policy allow-lists). It is not intent-filtered in `on` mode.
- `tools/call` uses the semantic router decision path before multiplex dispatch.
- Exact namespaced match (for example `k8s__get_logs`) takes the deterministic shortcut and should not require vector search.
- Ambiguous or non-exact calls may use rules/vector layers when enabled by router settings.
- On embed/vector degradation, gateway falls back gracefully to exact/rules behavior rather than breaking the session.

## 4) Expected trace phases and spans

For each `POST /mcp/rpc`, expect gateway internal phase timing in `mcp.gateway.internal.duration_seconds` with:

- `phase=parse`
- `phase=security`
- `phase=router`
- `phase=mux`

For `tools/call`, expect additional routing and backend execution visibility:

- Root request span: `mcp.host.request`
- Router decision telemetry through semantic router outcomes/duration metrics (layer labels such as `exact`, `rules`, `vector`)
- Backend call span(s): `mcp.backend.call` associated with selected backend id (`backend-k8s`, `backend-prom`, `backend-gh`)

Operationally, this gives an SRE a single correlated trace to answer:

- Was failure/latency in parsing/security, routing decisioning, or backend execution?
- Which backend was selected by the router?
- Did the chosen backend/tool match the namespaced intent?

## 5) Evidence checklist for B3.2

- Multi-backend `tools/list` shows namespaced catalog entries from at least two backends.
- Successful `tools/call` for 2-3 namespaced tools across different backend prefixes.
- Traces/metrics show `parse/security/router/mux` phase coverage for the same session.
- Router telemetry confirms exact-path or vector-path behavior under `ROUTER_MODE=on`.
