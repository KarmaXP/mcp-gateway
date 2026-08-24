#!/usr/bin/env bash
# Smoke one multi-upstream scenario: start the gateway against its config, open one MCP
# session, and assert every tool the scenario promises. One table per scenario.
#
#   bash scripts/scenario_smoke.sh demo    # gateway.example.yaml + alpha/beta mocks
#   bash scripts/scenario_smoke.sh sre     # gateway.sre.example.yaml + k8s/prom/gh mocks
#
# Mocks must already be listening: bash scripts/mock_upstreams.sh <scenario> start
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SCENARIO="${1:-}"
GATEWAY_PORT="${GATEWAY_PORT:-8080}"
GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
PROBE_ROUTER=0

# tool,marker
case "${SCENARIO}" in
  demo)
    CONFIG="${MCP_GATEWAY_CONFIG:-deployments/gateway.example.yaml}"
    MOCK_PORTS=(3101 3102)
    EXPECT=("${DEMO_EXPECT_TOOL:-alpha__echo},${DEMO_EXPECT_TEXT:-alpha-ok}")
    ;;
  sre)
    CONFIG="${MCP_GATEWAY_CONFIG:-deployments/gateway.sre.example.yaml}"
    MOCK_PORTS=(3201 3202 3203)
    EXPECT=("k8s__get_pod_logs,k8s-ok" "prom__query_instant,prom-ok" "gh__list_prs,gh-ok")
    PROBE_ROUTER=1
    ;;
  *)
    echo "usage: $0 demo|sre" >&2
    exit 2
    ;;
esac

GW_PID=""
SSE_PID=""
SSE_HDR=""
SSE_OUT=""

cleanup() {
  [[ -n "${SSE_PID}" ]] && kill "${SSE_PID}" 2>/dev/null || true
  [[ -n "${GW_PID}" ]] && kill "${GW_PID}" 2>/dev/null || true
  [[ -n "${SSE_HDR}" && -f "${SSE_HDR}" ]] && rm -f "${SSE_HDR}"
  [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]] && rm -f "${SSE_OUT}"
}
trap cleanup EXIT

for port in "${MOCK_PORTS[@]}"; do
  if ! lsof -ti ":${port}" >/dev/null 2>&1; then
    echo "${SCENARIO} mock not listening on ${port} — run: bash scripts/mock_upstreams.sh ${SCENARIO} start" >&2
    exit 1
  fi
done

ROUTER_MODE_EFFECTIVE="off"
if [[ "${PROBE_ROUTER}" -eq 1 ]]; then
  QDRANT_URL="${QDRANT_URL:-http://127.0.0.1:6333}"
  EMBED_URL="${EMBED_URL:-http://127.0.0.1:8001}"
  if curl -sf --max-time 3 "${QDRANT_URL}/healthz" >/dev/null 2>&1 \
    && curl -sf --max-time 5 "${EMBED_URL}/healthz" >/dev/null 2>&1; then
    ROUTER_MODE_EFFECTIVE="on"
  elif [[ "${SCENARIO_SMOKE_REQUIRE_ROUTER:-}" == "1" ]]; then
    echo "Qdrant/embed required (SCENARIO_SMOKE_REQUIRE_ROUTER=1) — run: make sre-up" >&2
    exit 1
  else
    echo "WARN: Qdrant/embed not reachable; router off for this run (use make sre-up for router=on)"
  fi
fi

echo "==> gateway PORT=${GATEWAY_PORT} config=${CONFIG} router=${ROUTER_MODE_EFFECTIVE}"
MCP_GATEWAY_CONFIG="${CONFIG}" AUTH_MODE=none PORT="${GATEWAY_PORT}" \
  ROUTER_MODE="${ROUTER_MODE_EFFECTIVE}" \
  go run ./cmd/gateway &
GW_PID=$!
disown "${GW_PID}" 2>/dev/null || true
for _ in $(seq 1 60); do
  curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf "${GATEWAY_URL}/healthz" >/dev/null
if [[ "${ROUTER_MODE_EFFECTIVE}" == "on" ]]; then
  curl -sf "${GATEWAY_URL}/readyz" >/dev/null || {
    echo "readyz failed (Qdrant/embed must be healthy when router=on)" >&2
    exit 1
  }
fi

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
disown "${SSE_PID}" 2>/dev/null || true
sleep 0.6
SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
[[ -n "${SID}" ]] || { echo "no Mcp-Session-Id" >&2; cat "${SSE_HDR}" >&2; exit 1; }

post_rpc() {
  curl -sS -o /dev/null -w "HTTP %{http_code}\n" \
    -H "Mcp-Session-Id: ${SID}" \
    -H "Content-Type: application/json" \
    -d "$1" \
    "${GATEWAY_URL}/mcp/rpc"
}

post_rpc "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"${SCENARIO}-smoke\",\"version\":\"1\"}}}"
sleep 0.4
grep -q '"result"' "${SSE_OUT}" || { head -30 "${SSE_OUT}" >&2; exit 1; }
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}'
post_rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
sleep 0.5

id=2
for entry in "${EXPECT[@]}"; do
  IFS=, read -r tool marker <<<"${entry}"
  id=$((id + 1))
  grep -q "${tool}" "${SSE_OUT}" || { echo "missing ${tool} in tools/list" >&2; cat "${SSE_OUT}" >&2; exit 1; }
  post_rpc "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"tools/call\",\"params\":{\"name\":\"${tool}\",\"arguments\":{}}}"
  sleep 0.4
  grep -q "${marker}" "${SSE_OUT}" || { echo "missing ${marker} for ${tool}" >&2; cat "${SSE_OUT}" >&2; exit 1; }
  echo "  OK ${tool} -> ${marker}"
done

tools=""
for entry in "${EXPECT[@]}"; do
  tools="${tools}${tools:+, }${entry%%,*}"
done
echo "$(echo "${SCENARIO}" | tr '[:lower:]' '[:upper:]') SMOKE OK (${tools})"
