# MCP capabilities

What the gateway exposes to hosts and how each MCP method family behaves.

Wire contract: [OpenAPI](artifacts/openapi/openapi.yaml). Scope boundaries: [ADR 0004](adr/0004-gateway-scope.md).

---

## Host transport

| Endpoint | Role |
|----------|------|
| `GET /mcp/sse` | Open session; response header **`Mcp-Session-Id`** required on later POSTs |
| `POST /mcp/rpc` | Send one JSON-RPC **2.0** request or notification per call (**202** when accepted) |
| `GET /healthz` | Liveness (always OK when process is up) |
| `GET /readyz` | Readiness; probes Qdrant + embed when semantic router is active |

Wire format: JSON-RPC **2.0**; `initialize` uses MCP protocol version **2024-11-05** (see [DEVELOPER.md — Protocol](DEVELOPER.md#protocol-and-dependencies)).

Results for requests with an `id` are delivered on the SSE stream as `event: jsonrpc` with a single-line `data:` JSON-RPC object. Notifications (no `id`) are accepted with **202** and produce no SSE response.

---

## Method matrix

| MCP method | Aggregated across upstreams | Namespacing | JWT / RAR allow-list | Semantic router |
|------------|---------------------------|-------------|----------------------|-------------------|
| `initialize` | Yes (merged) | N/A | AuthN only | No |
| `notifications/initialized` | Forwarded | N/A | AuthN only | No |
| `tools/list` | Yes | `prefix__name` | **Yes**, filters visible tools | `filter_list` mode narrows by `X-MCP-Intent` |
| `tools/call` | Routed to one backend | Strip prefix on forward | **Yes**, denies if not allowed | Optional rewrite when name is ambiguous |
| `resources/list` | Yes | `prefix__` on URI | AuthN only (no tool allow-list) | No |
| `resources/read` | Routed | URI encoding for opaque `__` | AuthN only | No |
| `prompts/list` | Yes | `prefix__name` | AuthN only | No |
| `prompts/get` | Routed | Strip prefix | AuthN only | No |

**Tools** are the primary security and routing surface. **Resources** and **prompts** are aggregated and forwarded after authentication; they do not use `mcp_tools` / RAR filtering in the current release.

---

## Namespacing

- Catalog entries use **`{prefix}__{native_name}`** (e.g. `k8s__get_pod_logs`).
- On `tools/call`, the gateway strips the prefix and sends the **native** name to the selected upstream.
- Resource URIs may contain `__`; the gateway uses opaque encoding when needed (`namespace.JoinOpaque`, see OpenAPI notes).

---

## Handshake requirement

Until the session completes:

1. `initialize`
2. `notifications/initialized` (or host `initialized` notification per your MCP revision)

…calls such as `tools/list` and `tools/call` return **`HandshakeIncomplete` (-32001)**.

Full sequence: [Connecting agents — Session flow](CONNECTING_AGENTS.md#session-flow-required).

---

## Aggregation behavior

Default: if an upstream fails during `initialize` or a **list** RPC, that upstream is **omitted** from the merge (partial catalog).

Optional strict mode (`aggregation.strict_initialize` / `strict_list`): any upstream failure → **`StrictAggregationFailed` (-32005)** to the host.

Details: [Adding upstreams — Aggregation](ADDING_UPSTREAMS.md#aggregation-and-failures).

---

## Notifications

| Notification | Gateway behavior |
|--------------|------------------|
| `notifications/tools/list_changed` | When `forward_tools_list_changed` is enabled: invalidate tools cache, reindex router, broadcast to SSE clients |
| `resources/list_changed`, `prompts/list_changed` | When `forward_tools_list_changed` is enabled: **SSE relay only** (no tools-cache invalidation or router reindex). Default off: not delivered. |

---

## Optional request headers

| Header | Applies to | Purpose |
|--------|------------|---------|
| `Authorization: Bearer …` | SSE + POST | JWT when `AUTH_MODE=jwt` |
| `Mcp-Session-Id` | POST | Binds RPC to SSE session |
| `X-MCP-Intent` | POST | Natural-language hint for router / `filter_list` |
| `X-Agent-Tokens-Used` | POST | Recorded on trace span only (not a metric label) |
| `traceparent` / `tracestate` | POST | W3C trace propagation to HTTP upstreams |

---

## Related docs

- [configuration.md](configuration.md): enable router, policy, aggregation
- [errors.md](errors.md): error codes
- [ADDING_UPSTREAMS.md](ADDING_UPSTREAMS.md): register upstreams
