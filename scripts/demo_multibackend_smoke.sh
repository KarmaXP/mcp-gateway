#!/usr/bin/env bash
# Smoke: gateway on :8080 with gateway.example.yaml and alpha/beta mocks (make demo-backends).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GATEWAY_PORT="${GATEWAY_PORT:-8080}"
GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
CONFIG="${MCP_GATEWAY_CONFIG:-deployments/gateway.example.yaml}"
EXPECT_TOOL="${DEMO_EXPECT_TOOL:-alpha__echo}"
EXPECT_TEXT="${DEMO_EXPECT_TEXT:-alpha-ok}"

GW_PID=""
SSE_PID=""

cleanup() {
  [[ -n "${SSE_PID}" ]] && kill "${SSE_PID}" 2>/dev/null || true
  [[ -n "${GW_PID}" ]] && kill "${GW_PID}" 2>/dev/null || true
}
trap cleanup EXIT

if ! lsof -ti ":3101" >/dev/null 2>&1; then
  echo "alpha mock not listening on 3101 — run: make demo-backends"
  exit 1
fi

echo "==> gateway PORT=${GATEWAY_PORT} config=${CONFIG}"
MCP_GATEWAY_CONFIG="${CONFIG}" AUTH_MODE=none PORT="${GATEWAY_PORT}" \
  go run ./cmd/gateway/main.go &
GW_PID=$!
for _ in $(seq 1 40); do
  curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1 && break
  sleep 0.15
done
curl -sf "${GATEWAY_URL}/healthz" >/dev/null

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6
SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
[[ -n "${SID}" ]] || { echo "no Mcp-Session-Id"; cat "${SSE_HDR}"; exit 1; }

post_rpc() {
  curl -sS -o /dev/null -w "HTTP %{http_code}\n" \
    -H "Mcp-Session-Id: ${SID}" \
    -H "Content-Type: application/json" \
    -d "$1" \
    "${GATEWAY_URL}/mcp/rpc"
}

post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"demo","version":"1"}}}'
sleep 0.35
grep -q '"result"' "${SSE_OUT}" || { head -20 "${SSE_OUT}"; exit 1; }
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}'
post_rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
sleep 0.35
grep -q "${EXPECT_TOOL}" "${SSE_OUT}" || { echo "missing ${EXPECT_TOOL}"; cat "${SSE_OUT}"; exit 1; }
post_rpc "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"${EXPECT_TOOL}\",\"arguments\":{}}}"
sleep 0.35
grep -q "${EXPECT_TEXT}" "${SSE_OUT}" || { echo "missing ${EXPECT_TEXT}"; cat "${SSE_OUT}"; exit 1; }

echo "DEMO MULTIBACKEND OK (${EXPECT_TOOL})"
