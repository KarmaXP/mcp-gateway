# ADR 0004: Gateway scope and MCP method boundaries

## Status

Accepted

## Context

The gateway is positioned as an SRE/platform MCP broker that aggregates backend capabilities while preserving transparent MCP behavior for hosts. Recent features (JWT/RAR policy, list aggregation, list-change forwarding) require an explicit scope boundary so operators and reviewers understand which methods are policy-enforced, forwarded, or intentionally unsupported.

## Decision

### 1) Product scope: SRE tools broker

- The gateway's primary role is brokering and observing MCP access to operational tool backends (Kubernetes, metrics, GitHub, runbook/RAG, etc.).
- It is not a generic policy engine for every MCP primitive in this phase; method-level enforcement is intentionally scoped.

### 2) AuthN vs AuthZ boundary

- **Authentication (AuthN)** applies to the session ingress (`GET /mcp/sse`, `POST /mcp/rpc`) in JWT mode.
- **Authorization (AuthZ)** from JWT claims / RAR allow-lists is **tools-only**:
 - `tools/list` is filtered by effective allowed tools.
 - `tools/call` is denied when target tool is outside the effective allowed tools.
- `resources/*` and `prompts/*` are currently **pass-through after AuthN** (no JWT/RAR allow-list filtering at this layer).

### 3) Supported vs out-of-scope MCP methods

| MCP method / group | Gateway status | Notes |
|---|---|---|
| `initialize` | Supported | Aggregated negotiation across backends. |
| `notifications/initialized` / `initialized` | Supported | Required handshake step before operational RPCs. |
| `ping` | Supported | Forwarded/handled as standard utility method. |
| `tools/list` | Supported (aggregated + policy-enforced) | Merged catalog; AuthZ filtering applies. |
| `tools/call` | Supported (routed + policy-enforced) | Namespaced routing; AuthZ + validation path. |
| `resources/list`, `resources/read` | Supported (pass-through after AuthN) | Aggregated/forwarded; no JWT/RAR allow-list enforcement in this phase. |
| `prompts/list`, `prompts/get` | Supported (pass-through after AuthN) | Aggregated/forwarded; no JWT/RAR allow-list enforcement in this phase. |
| `notifications/tools/list_changed` | Supported (optional forward) | `aggregation.forward_tools_list_changed` forwards catalog-change notifications to hosts. |
| `notifications/resources/list_changed`, `notifications/prompts/list_changed` | Out of scope for cache/router side-effects | When `aggregation.forward_tools_list_changed` is enabled: **SSE relay only** (no tools-cache invalidation or router reindex). Default off: no handler registered. |
| Other MCP methods not listed above | Out of scope | Returned as method not supported by gateway contract. |

### 4) `list_changed` side-effects are tools-only

When forwarding list-change notifications is enabled, **tools catalog events only** trigger gateway side-effects:

- merged tools catalog cache invalidation,
- semantic router reindex trigger,
- SSE broadcast to connected host sessions for tool-catalog change visibility.

Equivalent side-effects for `resources/*` and `prompts/*` list-change notifications are explicitly out of scope in this phase.

## Consequences

- Security and consent semantics stay clear: granular least-privilege is guaranteed for tool execution, while resources/prompts remain transport pass-through once authenticated.
- Operators treat `resources/*` and `prompts/*` access control as backend-side responsibility in this phase; gateway-level AuthZ for those method families is **out of scope** (see table above).
- Method support and notification behavior are now explicit for OpenAPI, deployment comments, and security review references.

## References

- `docs/architecture/mcp_gateway.plan.md`, orchestrator scope and method behavior
- `docs/adr/0003-security-rar-jwt-merge-failmode.md`, JWT/RAR merge and fail modes
- `docs/artifacts/openapi/openapi.yaml`, host-facing HTTP/MCP contract
- `docs/DEVELOPER.md`, operator-facing configuration semantics
