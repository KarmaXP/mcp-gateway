#!/usr/bin/env bash
# End-to-end MCP smoke (SSE + JSON-RPC) against a running gateway.
#
#   bash scripts/smoke_e2e.sh
#   GATEWAY_URL=http://127.0.0.1:8080 SMOKE_EXPECT_TOOL=alpha__echo SMOKE_EXPECT_TEXT=ok bash scripts/smoke_e2e.sh
#   SMOKE_JWT="$(go run ./tools/gen-jwt ...)" bash scripts/smoke_e2e.sh
#   SMOKE_EXPECT_RPC_ERROR=-32003 ...  # JWT deny
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8080}"
SMOKE_EXPECT_TOOL="${SMOKE_EXPECT_TOOL:-smoke__echo}"
SMOKE_EXPECT_TEXT="${SMOKE_EXPECT_TEXT:-smoke-ok}"
SMOKE_EXPECT_RPC_ERROR="${SMOKE_EXPECT_RPC_ERROR:-}"
SMOKE_TOOL_ARGS="${SMOKE_TOOL_ARGS:-\{\}}"

SSE_HDR=""
SSE_OUT=""
SSE_PID=""
SID=""

curl_auth() {
  if [[ -n "${SMOKE_JWT:-}" ]]; then
    curl -H "Authorization: Bearer ${SMOKE_JWT}" "$@"
  else
    curl "$@"
  fi
}

cleanup() {
  if [[ -n "${SSE_PID:-}" ]]; then
    kill "${SSE_PID}" 2>/dev/null || true
    SSE_PID=""
  fi
  [[ -n "${SSE_HDR}" && -f "${SSE_HDR}" ]] && rm -f "${SSE_HDR}"
  [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]] && rm -f "${SSE_OUT}"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

dump_sse() {
  if [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]]; then
    echo "--- SSE output (first 160 lines) ---" >&2
    sed -n '1,160p' "${SSE_OUT}" >&2
  fi
}

wait_sse_contains() {
  local pattern="$1"
  local label="$2"
  for _ in $(seq 1 50); do
    if grep -Fq -- "${pattern}" "${SSE_OUT}"; then
      return 0
    fi
    sleep 0.15
  done
  echo "Expected ${label} on SSE stream." >&2
  dump_sse
  fail "SSE assertion failed for: ${label}"
}

post_rpc() {
  local body="$1"
  local step="$2"
  local http_code=""
  if ! http_code="$(
    curl_auth -sS -o /dev/null -w "%{http_code}" \
      -H "Mcp-Session-Id: ${SID}" \
      -H "Content-Type: application/json" \
      -d "${body}" \
      "${GATEWAY_URL}/mcp/rpc"
  )"; then
    fail "RPC POST failed during '${step}' to ${GATEWAY_URL}/mcp/rpc"
  fi

  if [[ "${http_code}" != "202" ]]; then
    dump_sse
    fail "Unexpected HTTP ${http_code} during '${step}' (expected 202)"
  fi
  echo "HTTP ${http_code}"
}

echo "==> GET /healthz"
curl_auth -sfS "${GATEWAY_URL}/healthz" >/dev/null || \
  fail "gateway not reachable at ${GATEWAY_URL}"

echo "==> GET /readyz"
curl_auth -sfS "${GATEWAY_URL}/readyz" >/dev/null || \
  fail "gateway not ready at ${GATEWAY_URL}"

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
echo "==> GET ${GATEWAY_URL}/mcp/sse (background) -> ${SSE_OUT}"
curl_auth -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" \
  "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6

SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
if [[ -z "${SID}" ]]; then
  echo "Failed to read Mcp-Session-Id from SSE response headers." >&2
  sed -n '1,80p' "${SSE_HDR}" >&2
  fail "SSE session handshake failed at ${GATEWAY_URL}/mcp/sse"
fi
echo "==> session id: ${SID}"

echo "==> initialize"
post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke-e2e","version":"1.0.0"}}}' "initialize"
wait_sse_contains '"id":1' 'initialize response id'
wait_sse_contains '"result"' 'initialize result'

echo "==> notifications/initialized"
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}' "notifications/initialized"

echo "==> tools/list"
post_rpc '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' "tools/list"
wait_sse_contains '"id":3' 'tools/list response id'
if [[ -z "${SMOKE_EXPECT_RPC_ERROR}" ]]; then
  wait_sse_contains "${SMOKE_EXPECT_TOOL}" "namespaced tool ${SMOKE_EXPECT_TOOL}"
fi

echo "==> tools/call ${SMOKE_EXPECT_TOOL}"
TOOLS_CALL_BODY="$(printf '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"%s","arguments":%s}}' "${SMOKE_EXPECT_TOOL}" "${SMOKE_TOOL_ARGS}")"
post_rpc "${TOOLS_CALL_BODY}" "tools/call"
wait_sse_contains '"id":4' 'tools/call response id'
if [[ -n "${SMOKE_EXPECT_RPC_ERROR}" ]]; then
  wait_sse_contains "${SMOKE_EXPECT_RPC_ERROR}" "tools/call JSON-RPC error ${SMOKE_EXPECT_RPC_ERROR}"
  echo
  echo "SMOKE OK — tools/call denied as expected (JSON-RPC ${SMOKE_EXPECT_RPC_ERROR})."
else
  wait_sse_contains "${SMOKE_EXPECT_TEXT}" "tools/call payload containing ${SMOKE_EXPECT_TEXT}"
  echo
  echo "SMOKE OK — full MCP handshake + tools/list + tools/call validated against running gateway."
fi
