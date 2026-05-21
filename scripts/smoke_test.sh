#!/usr/bin/env bash
# End-to-end smoke: MCP HTTP+SSE through the gateway to scripts/smoke_upstream.
#
# Usage:
#   make test
#   SMOKE_AUTO_START_GATEWAY=1 ./scripts/smoke_test.sh
#
# Or with an already-running gateway (same shell exports as below):
#   export MCP_GATEWAY_CONFIG=/path/to/gateway.yaml   # must include backend url http://127.0.0.1:${SMOKE_UPSTREAM_PORT}, prefix smoke
#   export AUTH_MODE=none
#   ./scripts/smoke_test.sh
#
# Ports: SMOKE_UPSTREAM_PORT (default 31400). With SMOKE_AUTO_START_GATEWAY=1, gateway uses
# SMOKE_GATEWAY_PORT (default 18081). Manual mode uses PORT or 8080 unless SMOKE_GATEWAY_PORT is set.
# Logs: JSON slog on stdout when auto-starting the gateway; use grep on your terminal for
#       "mcp.dispatch", "initialize", or OTel exporter output if configured.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

UPSTREAM_PORT="${SMOKE_UPSTREAM_PORT:-31400}"
# Auto-start uses 18081 by default so we do not collide with a dev gateway on 8080.
if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
  GATEWAY_PORT="${SMOKE_GATEWAY_PORT:-18081}"
else
  GATEWAY_PORT="${SMOKE_GATEWAY_PORT:-${PORT:-8080}}"
fi
GATEWAY_URL="${SMOKE_GATEWAY_URL:-http://127.0.0.1:${GATEWAY_PORT}}"

TMPYAML=""
USE_DEMO_CONFIG=0
SSE_HDR=""
SSE_OUT=""
UP_PID=""
GW_PID=""
SSE_PID=""

cleanup() {
  [[ -n "${SSE_PID:-}" ]] && kill "${SSE_PID}" 2>/dev/null || true
  [[ -n "${UP_PID:-}" ]] && kill "${UP_PID}" 2>/dev/null || true
  [[ -n "${GW_PID:-}" ]] && kill "${GW_PID}" 2>/dev/null || true
  if [[ "${USE_DEMO_CONFIG}" -eq 0 && -n "${TMPYAML}" && -f "${TMPYAML}" ]]; then
    rm -f "${TMPYAML}"
  fi
  [[ -n "${SSE_HDR}" && -f "${SSE_HDR}" ]] && rm -f "${SSE_HDR}"
  [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]] && rm -f "${SSE_OUT}"
}
trap cleanup EXIT

wait_http_ok() {
  local url="$1"
  local attempts="${2:-40}"
  local delay="${3:-0.15}"
  local i
  for ((i = 1; i <= attempts; i++)); do
    if curl -sf "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${delay}"
  done
  return 1
}

wait_sse_contains() {
  local file="$1"
  local pattern="$2"
  local attempts="${3:-30}"
  local delay="${4:-0.15}"
  local i
  for ((i = 1; i <= attempts; i++)); do
    if grep -q "${pattern}" "${file}" 2>/dev/null; then
      return 0
    fi
    sleep "${delay}"
  done
  return 1
}

# Best-effort free upstream (stale smoke_upstream) and dedicated auto-start gateway port.
if [[ "${SMOKE_CLEAN_PORTS:-1}" == "1" ]]; then
  lsof -ti ":${UPSTREAM_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
    lsof -ti ":${GATEWAY_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  fi
fi

if [[ -n "${MCP_GATEWAY_CONFIG:-}" && -f "${MCP_GATEWAY_CONFIG}" ]]; then
  echo "==> using MCP_GATEWAY_CONFIG=${MCP_GATEWAY_CONFIG}"
  USE_DEMO_CONFIG=1
else
  TMPYAML="$(mktemp)"
  cat >"${TMPYAML}" <<EOF
backends:
  - id: smoke
    prefix: smoke
    url: http://127.0.0.1:${UPSTREAM_PORT}
    max_concurrency: 4
router:
  mode: off
embed:
  url: http://127.0.0.1:8001
qdrant:
  collection: mcp_tool_catalog
EOF
  export MCP_GATEWAY_CONFIG="${TMPYAML}"
fi

echo "==> starting smoke upstream on 127.0.0.1:${UPSTREAM_PORT}"
go run ./scripts/smoke_upstream/main.go -listen "127.0.0.1:${UPSTREAM_PORT}" &
UP_PID=$!
sleep 0.5

if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
  :
elif curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1; then
  echo "==> gateway already up at ${GATEWAY_URL}"
  echo "    (backend must be http://127.0.0.1:${UPSTREAM_PORT} with prefix smoke)"
  GW_PID=""
else
  if [[ "${SMOKE_AUTO_START_GATEWAY:-}" != "1" ]]; then
    echo "Gateway not healthy at ${GATEWAY_URL}."
    echo "Start it (example):"
    echo "  export MCP_GATEWAY_CONFIG=${TMPYAML} AUTH_MODE=none PORT=${GATEWAY_PORT}"
    echo "  go run ./cmd/gateway/main.go"
    echo "Or run: SMOKE_AUTO_START_GATEWAY=1 $0"
    exit 1
  fi
fi

if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
  echo "==> auto-starting gateway PORT=${GATEWAY_PORT} config=${MCP_GATEWAY_CONFIG} (JSON logs on stdout)"
  AUTH_MODE=none PORT="${GATEWAY_PORT}" \
    go run ./cmd/gateway/main.go &
  GW_PID=$!
  wait_http_ok "${GATEWAY_URL}/healthz" || {
    echo "gateway failed to become healthy"
    exit 1
  }
fi

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
echo "==> GET ${GATEWAY_URL}/mcp/sse (background) -> ${SSE_OUT}"
curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6

SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
if [[ -z "${SID}" ]]; then
  echo "failed to read Mcp-Session-Id:"
  cat "${SSE_HDR}"
  exit 1
fi
echo "==> session id: ${SID}"

post_rpc() {
  local body="$1"
  curl -sS -o /dev/null -w "HTTP %{http_code}\n" \
    -H "Mcp-Session-Id: ${SID}" \
    -H "Content-Type: application/json" \
    -d "${body}" \
    "${GATEWAY_URL}/mcp/rpc"
}

echo "==> initialize"
post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1.0.0"}}}'
wait_sse_contains "${SSE_OUT}" '"result"' || {
  echo "expected initialize result on SSE stream:"
  head -30 "${SSE_OUT}"
  exit 1
}
echo "==> initialize: JSON-RPC result seen on SSE (proves host session + multiplexer)"

echo "==> notifications/initialized"
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}'

echo "==> ping (MCP spec)"
post_rpc '{"jsonrpc":"2.0","id":2,"method":"ping"}'
wait_sse_contains "${SSE_OUT}" '"id":2' || {
  echo "expected ping response id 2 on SSE stream:"
  head -40 "${SSE_OUT}"
  exit 1
}
wait_sse_contains "${SSE_OUT}" '"result":{}' || {
  echo "expected ping empty result object on SSE stream:"
  head -40 "${SSE_OUT}"
  exit 1
}
echo "==> ping: empty JSON-RPC result (spec compliance)"

echo "==> tools/list"
post_rpc '{"jsonrpc":"2.0","id":3,"method":"tools/list"}'
wait_sse_contains "${SSE_OUT}" 'smoke__echo' || {
  echo "expected aggregated namespaced tool smoke__echo:"
  cat "${SSE_OUT}"
  exit 1
}
echo "==> tools/list: smoke__echo present (proves upstream tools/list via mcphttp)"

echo "==> tools/call smoke__echo"
post_rpc '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"smoke__echo","arguments":{}}}'
wait_sse_contains "${SSE_OUT}" 'smoke-ok' || {
  echo "expected upstream text smoke-ok:"
  cat "${SSE_OUT}"
  exit 1
}
echo "==> tools/call: smoke-ok (proves strip-prefix forward to upstream)"

echo ""
echo "SMOKE OK — inspect gateway stdout for slog lines (e.g. mcp.dispatch, semantic spans) or OTLP if enabled."

if [[ "${DEMO_PRINT_HELP:-}" == "1" ]]; then
  echo ""
  echo "=== Demo complete ==="
  echo "Gateway URL:  ${GATEWAY_URL}"
  echo "Health:       curl -s ${GATEWAY_URL}/healthz"
  echo "MCP SSE:      curl -N ${GATEWAY_URL}/mcp/sse"
  echo "Example RPC:  curl -s -H 'Mcp-Session-Id: <from-sse>' -H 'Content-Type: application/json' \\"
  echo "                -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"smoke__echo\",\"arguments\":{}}}' \\"
  echo "                ${GATEWAY_URL}/mcp/rpc"
  echo "Config:       ${MCP_GATEWAY_CONFIG}"
  echo "Stop gateway: make stop   (frees PORT/GATEWAY_PORT from .env; demo auto-start used ${GATEWAY_PORT})"
fi
