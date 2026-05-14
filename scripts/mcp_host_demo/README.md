# MCP host demo

Minimal reproducible MCP host client against the gateway transport:

1. `GET /mcp/sse`
2. `POST /mcp/rpc` `initialize`
3. `POST /mcp/rpc` `notifications/initialized`
4. `POST /mcp/rpc` `tools/list`
5. `POST /mcp/rpc` `tools/call`

The script reads `Mcp-Session-Id` from the SSE response headers, keeps the stream open, and waits for matching JSON-RPC responses by `id`.

## Usage

From `mcp-gateway/`:

```bash
GATEWAY_URL=http://127.0.0.1:8080 go run ./scripts/mcp_host_demo
```

Optional env vars:

- `GATEWAY_URL`: Gateway base URL (default `http://127.0.0.1:8080`)
- `TOOL_NAME`: Tool to call after `tools/list`. If empty, the first tool returned by `tools/list` is used.

## Example output

```text
session: 53fb19f9-188f-4904-84ef-9e1944f95f50
initialize response: {"jsonrpc":"2.0","id":...,"result":...}
sent notifications/initialized
tools/list response: {"jsonrpc":"2.0","id":...,"result":{"tools":[...]}}
tools/call name: alpha__echo
tools/call response: {"jsonrpc":"2.0","id":...,"result":...}
```
