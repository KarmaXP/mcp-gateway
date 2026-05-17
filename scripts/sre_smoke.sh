#!/usr/bin/env bash
# Smoke: gateway + gateway.sre.example.yaml + SRE mocks; three tools/call in one session.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GATEWAY_PORT="${GATEWAY_PORT:-8080}"
GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
CONFIG="${MCP_GATEWAY_CONFIG:-deployments/gateway.sre.example.yaml}"
QDRANT_URL="${QDRANT_URL:-http://127.0.0.1:6333}"
EMBED_URL="${EMBED_URL:-http://127.0.0.1:8001}"

GW_PID=""
SSE_PID=""

cleanup() {
  [[ -n "${SSE_PID}" ]] && kill "${SSE_PID}" 2>/dev/null || true
  [[ -n "${GW_PID}" ]] && kill "${GW_PID}" 2>/dev/null || true
}
trap cleanup EXIT

for port in 3201 3202 3203; do
  if ! lsof -ti ":${port}" >/dev/null 2>&1; then
    echo "SRE mock not listening on ${port} — run: make sre-backends (or make sre-up)"
    exit 1
  fi
done

ROUTER_MODE_EFFECTIVE="on"
if ! curl -sf --max-time 3 "${QDRANT_URL}/healthz" >/dev/null 2>&1 \
  || ! curl -sf --max-time 5 "${EMBED_URL}/healthz" >/dev/null 2>&1; then
  if [[ "${SRE_SMOKE_REQUIRE_ROUTER:-}" == "1" ]]; then
    echo "Qdrant/embed required (SRE_SMOKE_REQUIRE_ROUTER=1) — run: make sre-up"
    exit 1
  fi
  echo "WARN: Qdrant/embed not reachable; ROUTER_MODE=off for multiplex smoke only (use make sre-up for router=on)"
  ROUTER_MODE_EFFECTIVE="off"
fi

echo "==> gateway PORT=${GATEWAY_PORT} config=${CONFIG} router=${ROUTER_MODE_EFFECTIVE}"
MCP_GATEWAY_CONFIG="${CONFIG}" AUTH_MODE=none PORT="${GATEWAY_PORT}" \
  ROUTER_MODE="${ROUTER_MODE_EFFECTIVE}" \
  QDRANT_URL="${QDRANT_URL}" EMBED_URL="${EMBED_URL}" \
  go run ./cmd/gateway/main.go &
GW_PID=$!
for _ in $(seq 1 60); do
  curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf "${GATEWAY_URL}/healthz" >/dev/null
if [[ "${ROUTER_MODE_EFFECTIVE}" == "on" ]]; then
  curl -sf "${GATEWAY_URL}/readyz" >/dev/null || {
    echo "readyz failed (Qdrant/embed must be healthy when router=on)"
    exit 1
  }
fi

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6
SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
[[ -n "${SID}" ]] || { echo "no Mcp-Session-Id"; exit 1; }

post_rpc() {
  curl -sS -o /dev/null -w "HTTP %{http_code}\n" \
    -H "Mcp-Session-Id: ${SID}" \
    -H "Content-Type: application/json" \
    -d "$1" \
    "${GATEWAY_URL}/mcp/rpc"
}

post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"sre-smoke","version":"1"}}}'
sleep 0.4
grep -q '"result"' "${SSE_OUT}" || { head -30 "${SSE_OUT}"; exit 1; }
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}'
post_rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
sleep 0.5

assert_tool_call() {
  local tool="$1"
  local marker="$2"
  local id="$3"
  grep -q "${tool}" "${SSE_OUT}" || { echo "missing ${tool} in tools/list"; cat "${SSE_OUT}"; exit 1; }
  post_rpc "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"tools/call\",\"params\":{\"name\":\"${tool}\",\"arguments\":{}}}"
  sleep 0.4
  grep -q "${marker}" "${SSE_OUT}" || { echo "missing ${marker} for ${tool}"; cat "${SSE_OUT}"; exit 1; }
  echo "  OK ${tool} -> ${marker}"
}

assert_tool_call "k8s__get_pod_logs" "k8s-ok" 3
assert_tool_call "prom__query_instant" "prom-ok" 4
assert_tool_call "gh__list_prs" "gh-ok" 5

echo "SRE SMOKE OK (k8s__get_pod_logs, prom__query_instant, gh__list_prs)"
