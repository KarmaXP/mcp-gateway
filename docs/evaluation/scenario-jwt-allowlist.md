# JWT allow-list walkthrough (`AUTH_MODE=jwt`)

This scenario validates JWT-based tool authorization in gateway mode:

- `tools/list` is filtered to the effective allow-list.
- `tools/call` is denied (`PermissionDenied`, `-32003`) for tools outside that allow-list.

**Full multibackend benchmark** (real stdio backends + OTLP + Prometheus): [scenario-real-backends-jwt.md](scenario-real-backends-jwt.md).

The claim model follows [ADR 0003](../adr/0003-security-rar-jwt-merge-failmode.md):

- `mcp_tools` contains namespaced tool ids.
- `authorization_details` entries for MCP tools use `type: "mcp_tool"` with exactly one of `tool_name` or `tool_pattern`.
- When both are present, effective allow-list is the intersection of JWT tools and RAR-expanded tools.

## Prerequisites

- Gateway running and reachable at `http://127.0.0.1:8080`.
- Catalog includes at least `alpha__echo` and `alpha__other`.
- Gateway is configured for JWT verification:
  - `AUTH_MODE=jwt`
  - `JWT_PUBLIC_KEY_PEM` (or JWKS)
  - `JWT_ISS`, `JWT_AUD` aligned with token claims

## Step 1: Create local RSA keys

```bash
openssl genrsa -out /tmp/mcp-jwt.key 2048
openssl rsa -in /tmp/mcp-jwt.key -pubout -out /tmp/mcp-jwt.pub.pem
export JWT_PUBLIC_KEY_PEM="$(cat /tmp/mcp-jwt.pub.pem)"
export JWT_ISS="https://dev.local"
export JWT_AUD="mcp-gateway"
```

## Step 2: Generate a base token with `tools/gen-jwt`

```bash
# From the mcp-gateway module root:
BASE_JWT="$(go run ./tools/gen-jwt -issuer "${JWT_ISS}" -audience "${JWT_AUD}" -key /tmp/mcp-jwt.key)"
echo "${BASE_JWT}" | cut -c1-32
```

`tools/gen-jwt` gives a signed RS256 token with registered claims (`iss`, `aud`, `exp`, `iat`).

## Step 3: Use JWT claims for this scenario (ADR 0003 shape)

Use a token that includes this payload subset:

```json
{
  "mcp_tools": ["alpha__echo"],
  "authorization_details": [
    { "type": "mcp_tool", "tool_pattern": "alpha__*" }
  ]
}
```

Expected effective allow-list:

- `mcp_tools` => `["alpha__echo"]`
- `authorization_details` => matches `alpha__echo`, `alpha__other`, ...
- intersection => `["alpha__echo"]`

If your IdP mints custom claims directly, request the payload above. For local tests, you can mint an equivalent signed token fixture from your test harness/dev tooling using the same private key as step 1.

## Step 4: Open SSE session with bearer token

```bash
JWT="${BASE_JWT}" # replace with your claimful token from step 3
SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"

curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" \
  -H "Authorization: Bearer ${JWT}" \
  "http://127.0.0.1:8080/mcp/sse" &

SSE_PID=$!
sleep 1
SID="$(awk 'BEGIN{IGNORECASE=1}/^mcp-session-id:/{gsub("\r",""); print $2; exit}' "${SSE_HDR}")"
```

## Step 5: Handshake (`initialize` + `notifications/initialized`)

```bash
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer ${JWT}" \
  -H "Mcp-Session-Id: ${SID}" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"jwt-allowlist-walkthrough","version":"1.0.0"}}}' \
  "http://127.0.0.1:8080/mcp/rpc"

curl -sS -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer ${JWT}" \
  -H "Mcp-Session-Id: ${SID}" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  "http://127.0.0.1:8080/mcp/rpc"
```

Both requests should return `202`.

## Step 6: Validate `tools/list` filtering

```bash
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer ${JWT}" \
  -H "Mcp-Session-Id: ${SID}" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' \
  "http://127.0.0.1:8080/mcp/rpc"

sleep 1
rg -n '"id":3|alpha__echo|alpha__other' "${SSE_OUT}"
```

Expected:

- `alpha__echo` appears in the `tools/list` response.
- `alpha__other` does not appear in that response.

## Step 7: Validate `tools/call` deny for disallowed tool

```bash
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer ${JWT}" \
  -H "Mcp-Session-Id: ${SID}" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"alpha__other","arguments":{}}}' \
  "http://127.0.0.1:8080/mcp/rpc"

sleep 1
rg -n '"id":7|"code":-32003|PermissionDenied|alpha__other' "${SSE_OUT}"
```

Expected:

- HTTP `202` on POST (transport accepted).
- SSE JSON-RPC error for `id: 7` with code `-32003` (`PermissionDenied`).
- Backend tool execution is skipped for that denied call.

## Cleanup

```bash
kill "${SSE_PID}" 2>/dev/null || true
rm -f "${SSE_HDR}" "${SSE_OUT}"
```

## Automated check

CI runs `scripts/smoke_jwt.sh` on every push (see [DEVELOPER.md — Continuous integration](../DEVELOPER.md#continuous-integration)).
