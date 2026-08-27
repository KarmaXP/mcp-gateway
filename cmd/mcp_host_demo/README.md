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
GATEWAY_URL=http://127.0.0.1:8080 go run ./cmd/mcp_host_demo
```

Multi-backend (`gateway.example.yaml`):

```bash
make demo-upstreams
MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml make run   # other terminal
TOOL_NAME=alpha__echo GATEWAY_URL=http://127.0.0.1:8080 go run ./cmd/mcp_host_demo
```

Or one shot: `make demo-full`.

### Multiupstream benchmark / LangGraph agent integration run (real upstreams + JWT)

When the gateway runs with `AUTH_MODE=jwt` (see
[scenario-real-upstreams-jwt.md](../../docs/evaluation/scenario-real-upstreams-jwt.md)),
set `GATEWAY_JWT` so the client sends `Authorization: Bearer` on the SSE GET and
every POST, and pass `TOOL_ARGS` for tools that need arguments:

```bash
PORT=18080
TOOL_NAME=prom__read_text_file \
TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}' \
GATEWAY_URL=http://127.0.0.1:${PORT} \
GATEWAY_JWT="$JWT_ADMIN" \
go run ./cmd/mcp_host_demo
```

Without `GATEWAY_JWT` against a JWT-mode gateway the SSE GET returns `401`.

Optional env vars:

- `GATEWAY_URL`: Gateway base URL (default `http://127.0.0.1:8080`)
- `GATEWAY_JWT`: Bearer token sent on SSE and every POST. Required when the gateway runs with `AUTH_MODE=jwt`.
- `TOOL_NAME`: Tool to call after `tools/list`. If empty, the first tool returned by `tools/list` is used (e.g. `alpha__echo` with `make demo-upstreams`).
- `TOOL_ARGS`: JSON object passed as `tools/call` arguments (default `{}`). Example: `{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}`.

## Example output

```text
session: 53fb19f9-188f-4904-84ef-9e1944f95f50
initialize response: {"jsonrpc":"2.0","id":...,"result":...}
sent notifications/initialized
tools/list response: {"jsonrpc":"2.0","id":...,"result":{"tools":[...]}}
tools/call name: alpha__echo
tools/call response: {"jsonrpc":"2.0","id":...,"result":...}
```
