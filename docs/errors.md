# Gateway errors

How the gateway reports failures to MCP hosts: **HTTP status** on `POST /mcp/rpc` and **JSON-RPC errors** on the SSE stream (`event: jsonrpc`).

Authoritative HTTP details: [OpenAPI](artifacts/openapi/openapi.yaml). Source constants: [`internal/gateway/errcodes`](../internal/gateway/errcodes/codes.go).

---

## HTTP responses (`POST /mcp/rpc`)

| Status | Meaning |
|--------|---------|
| **202 Accepted** | Request accepted; read the matching JSON-RPC response on the open SSE connection (same `id`). |
| **400 Bad Request** | Missing or invalid `Mcp-Session-Id`, malformed JSON-RPC body. |
| **401 Unauthorized** | `AUTH_MODE=jwt` and missing/invalid bearer, or policy evaluation failed (fail-closed). |
| **404 Not Found** | Unknown or expired session id. |
| **413 Payload Too Large** | Body exceeds gateway limit (`MaxBytesReader`). |
| **429 Too Many Requests** | Rate limit exhausted (`rate_limit` / `RATE_LIMIT_*`). |
| **503 Service Unavailable** | `/readyz` only, dependency unhealthy when router requires Qdrant/embed. |

Notifications (no JSON-RPC `id`) also return **202** when accepted.

---

## JSON-RPC errors (on SSE)

Gateway-specific codes use the implementation-defined range **-32000 … -32099** (JSON-RPC 2.0).

| Code | Name | Typical cause | What to do |
|------|------|---------------|------------|
| **-32001** | `HandshakeIncomplete` | `tools/*` (or other gated RPCs) before `initialize` + `notifications/initialized` | Complete the [session handshake](CONNECTING_AGENTS.md#session-flow-required). |
| **-32002** | `RequestRejected` | Request rejected by gateway middleware (e.g. policy gate before dispatch) | Check logs; verify auth and request shape. |
| **-32003** | `PermissionDenied` | Tool not allowed by JWT `mcp_tools` / RAR / policy | Adjust token claims or [policy](configuration.md#policy-block); see [JWT scenario](evaluation/scenario-jwt-allowlist.md). |
| **-32004** | `ToolRoutingAmbiguous` | Semantic router could not pick a single tool | Send an exact namespaced tool name, or a clearer `X-MCP-Intent` header. |
| **-32005** | `StrictAggregationFailed` | `aggregation.strict_initialize` or `strict_list` and an upstream failed | Fix upstream health or disable strict mode; see [backend-down scenario](evaluation/scenario-backend-down.md). |
| **-32000** | `GatewayInternal` | Upstream call failed, multiplex error, or unexpected gateway fault | Check backend logs and traces (`mcp.backend.call`). |

Standard JSON-RPC errors may also appear (from gateway or forwarded upstream):

| Code | Name | Typical cause |
|------|------|---------------|
| **-32601** | `MethodNotFound` | Unknown JSON-RPC method |
| **-32602** | `InvalidParams` | Invalid arguments (including JSON Schema validation on `tools/call`) |
| **-32603** | `InternalError` | Generic internal error |

Schema validation errors on `tools/call` **must not** echo argument values in the message (SEC5).

---

## Quick troubleshooting

| Symptom | See |
|---------|-----|
| POST **202** but no SSE payload | SSE connection closed or not read in parallel. See [Connecting agents](CONNECTING_AGENTS.md). |
| **401** on every RPC | JWT mode, keys, or `authorization_details`. See [configuration, Auth section](configuration.md#authentication). |
| Tool missing from `tools/list` | Backend down, `MethodNotFound` on list, or JWT filter. See [Adding backends](ADDING_BACKENDS.md). |
| Router ignores intent | `ROUTER_MODE` off, or exact tool name shortcut. See [configuration, Router section](configuration.md#semantic-router). |
