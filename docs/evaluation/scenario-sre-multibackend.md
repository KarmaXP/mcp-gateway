# SRE scenario: multi-backend namespaced routing

This scenario validates gateway behavior when multiple MCP backends are exposed behind namespaced tools (canonical names: `k8s__get_pod_logs`, `prom__query_instant`, `gh__list_prs`) with semantic routing enabled.

Use this runbook to exercise multi-backend namespaced routing with the existing smoke flow (`scripts/smoke_e2e.sh`) or your MCP host client. For agent integration, see [CONNECTING_AGENTS.md](../CONNECTING_AGENTS.md).

## 1) Topology and config shape

Follow the same `backends` layout used in `deployments/gateway.example.yaml` (`id`, `prefix`, `url`, `max_concurrency` per backend). For this scenario, use 2-3 backends with distinct prefixes:

- `k8s` backend serving Kubernetes tools (`get_pod_logs`, `list_pods`, ...).
- `prom` backend serving Prometheus query tools (`query_instant`, `query_range`, ...).
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

## 2) Walkthrough (host client or smoke scripts)

Use one client session over MCP SSE + JSON-RPC. The sequence below mirrors `scripts/smoke_e2e.sh` semantics, replacing the tool names with multi-backend targets.

1. Start gateway with the multi-backend config and verify `GET /healthz` and `GET /readyz`.
2. Open `GET /mcp/sse` and capture `Mcp-Session-Id`.
3. Send `initialize`, then `notifications/initialized`.
4. Send `tools/list`.
5. Send three `tools/call` requests in the same session:
   - `k8s__get_pod_logs` (cluster diagnostics)
   - `prom__query_instant` (service SLI snapshot)
   - `gh__list_prs` (deployment/code-change context)

One-shot smoke (requires `make sre-up` or mocks + Qdrant/embed):

```bash
make sre-smoke
```

Manual sequence with `scripts/smoke_e2e.sh` (three runs):

```bash
SMOKE_EXPECT_TOOL=k8s__get_pod_logs SMOKE_EXPECT_TEXT=k8s-ok bash scripts/smoke_e2e.sh
SMOKE_EXPECT_TOOL=prom__query_instant SMOKE_EXPECT_TEXT=prom-ok bash scripts/smoke_e2e.sh
SMOKE_EXPECT_TOOL=gh__list_prs SMOKE_EXPECT_TEXT=gh-ok bash scripts/smoke_e2e.sh
```

## 3) Expected router behavior (`ROUTER_MODE=on`)

- `tools/list` remains the full merged namespaced catalog (subject to auth/policy allow-lists). It is not intent-filtered in `on` mode.
- `tools/call` uses the semantic router decision path before multiplex dispatch.
- Exact namespaced match (for example `k8s__get_pod_logs`) takes the deterministic shortcut and should not require vector search.
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

## 5) Evidence checklist

- Multi-backend `tools/list` shows namespaced catalog entries from at least two backends.
- Successful `tools/call` for 2-3 namespaced tools across different backend prefixes.
- Traces/metrics show `parse/security/router/mux` phase coverage for the same session.
- Router telemetry confirms exact-path or vector-path behavior under `ROUTER_MODE=on`.
