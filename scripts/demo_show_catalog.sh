#!/usr/bin/env bash
# Merged tools/list for demo recording (optional intent as $1 or DEMO_INTENT).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8080}"
SMOKE_JWT="${SMOKE_JWT:-${JWT_ADMIN_FULL:-${JWT_ADMIN:-}}}"
DEMO_INTENT="${DEMO_INTENT:-${1:-}}"

if [[ -z "${SMOKE_JWT}" ]]; then
  echo "Set SMOKE_JWT, JWT_ADMIN_FULL, or JWT_ADMIN (e.g. eval \"\$(bash scripts/lab_jwt_keys.sh env)\")" >&2
  exit 1
fi

SSE_HDR=""
SSE_OUT=""
SSE_PID=""
SID=""

curl_auth() {
  curl -H "Authorization: Bearer ${SMOKE_JWT}" "$@"
}

OUT_FILE=""

cleanup() {
  if [[ -n "${SSE_PID:-}" ]]; then
    kill "${SSE_PID}" 2>/dev/null || true
    SSE_PID=""
  fi
  [[ -n "${SSE_HDR}" && -f "${SSE_HDR}" ]] && rm -f "${SSE_HDR}"
  [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]] && rm -f "${SSE_OUT}"
  [[ -n "${OUT_FILE}" && -f "${OUT_FILE}" ]] && rm -f "${OUT_FILE}"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

check_gateway_ready() {
  local code
  code="$(curl_auth -sS -o /dev/null -w "%{http_code}" "${GATEWAY_URL}/readyz" || echo "000")"
  if [[ "${code}" == "200" ]]; then
    return 0
  fi

  echo "Gateway /readyz returned HTTP ${code} (expected 200)." >&2
  echo "With ROUTER_MODE=on|filter_list the gateway waits for Qdrant + embed." >&2
  echo >&2
  echo "Check dependencies:" >&2
  if curl -sfS "http://127.0.0.1:6333/healthz" >/dev/null 2>&1; then
    echo "  Qdrant  http://127.0.0.1:6333/healthz  OK" >&2
  else
    echo "  Qdrant  http://127.0.0.1:6333/healthz  FAIL — run: make docker-up" >&2
  fi
  if curl -sfS "http://127.0.0.1:8001/healthz" >/dev/null 2>&1; then
    echo "  Embed   http://127.0.0.1:8001/healthz   OK" >&2
  elif curl -sfS "http://127.0.0.1:18001/healthz" >/dev/null 2>&1; then
    echo "  Embed   http://127.0.0.1:18001/healthz  OK (alternate port)" >&2
    echo "  Fix: restart gateway with make run (.env loads EMBED_URL from HOST_PORT_EMBED)" >&2
  else
    echo "  Embed   http://127.0.0.1:8001/healthz   FAIL" >&2
    echo "          http://127.0.0.1:18001/healthz  (also try when HOST_PORT_EMBED=18001 in .env)" >&2
    echo "          First boot: docker logs mcp-gateway-embed (may take ~60s)" >&2
  fi
  if curl -sfS "${GATEWAY_URL}/healthz" >/dev/null 2>&1; then
    echo "  Gateway ${GATEWAY_URL}/healthz  OK (process up, deps not ready)" >&2
  else
    echo "  Gateway ${GATEWAY_URL}/healthz  FAIL — start gateway: export PORT=18080 ... && make run" >&2
  fi
  echo >&2
  echo "Gateway env should include QDRANT_URL=http://127.0.0.1:6333 and EMBED_URL matching HOST_PORT_EMBED in .env" >&2
  echo "Run: bash scripts/demo_lab_preflight.sh  (before make run)" >&2
  fail "gateway not ready at ${GATEWAY_URL}"
}

wait_sse_contains() {
  local pattern="$1"
  local label="$2"
  local attempts="${3:-60}"
  local delay="${4:-0.15}"
  for _ in $(seq 1 "${attempts}"); do
    if grep -Fq -- "${pattern}" "${SSE_OUT}"; then
      return 0
    fi
    sleep "${delay}"
  done
  fail "SSE timeout waiting for ${label}"
}

post_rpc() {
  local body="$1"
  local step="$2"
  local http_code=""
  if [[ -n "${DEMO_INTENT}" ]]; then
    if ! http_code="$(
      curl_auth -sS -o /dev/null -w "%{http_code}" \
        -H "Mcp-Session-Id: ${SID}" \
        -H "Content-Type: application/json" \
        -H "X-MCP-Intent: ${DEMO_INTENT}" \
        -d "${body}" \
        "${GATEWAY_URL}/mcp/rpc"
    )"; then
      fail "RPC POST failed during '${step}'"
    fi
  else
    if ! http_code="$(
      curl_auth -sS -o /dev/null -w "%{http_code}" \
        -H "Mcp-Session-Id: ${SID}" \
        -H "Content-Type: application/json" \
        -d "${body}" \
        "${GATEWAY_URL}/mcp/rpc"
    )"; then
      fail "RPC POST failed during '${step}'"
    fi
  fi
  if [[ "${http_code}" != "202" ]]; then
    fail "Unexpected HTTP ${http_code} during '${step}' (expected 202)"
  fi
}

curl_auth -sfS "${GATEWAY_URL}/healthz" >/dev/null || fail "gateway not reachable at ${GATEWAY_URL} (is make run active on PORT=18080?)"
check_gateway_ready

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
curl_auth -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6

SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | head -1 | sed 's/[^:]*: *//' | tr -d '\r\n' || true)"
[[ -n "${SID}" ]] || fail "no Mcp-Session-Id from SSE"

post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"demo-show-catalog","version":"1.0.0"}}}' "initialize"
wait_sse_contains '"id":1' 'initialize'

post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}' "notifications/initialized"
post_rpc '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' "tools/list"
echo "waiting for tools/list (cold stdio backends can take ~60s)..." >&2
wait_sse_contains '"id":3' 'tools/list' 180 0.5

OUT_FILE="$(mktemp)"

if ! python3 - "${SSE_OUT}" "${OUT_FILE}" <<'PY'
import json
import sys

sse_path, out_path = sys.argv[1], sys.argv[2]
text = open(sse_path, encoding="utf-8", errors="replace").read()
names = []
for line in text.splitlines():
    line = line.strip()
    if not line.startswith("data:"):
        continue
    payload = line[5:].strip()
    if not payload:
        continue
    try:
        obj = json.loads(payload)
    except json.JSONDecodeError:
        continue
    if obj.get("id") != 3:
        continue
    result = obj.get("result") or {}
    for tool in result.get("tools") or []:
        name = tool.get("name")
        if isinstance(name, str) and name:
            names.append(name)
    break

if not names:
    sys.exit("no tools/list result with id=3 found on SSE stream")

with open(out_path, "w", encoding="utf-8") as fh:
    for name in sorted(set(names)):
        fh.write(name + "\n")
PY
then
  fail "could not parse tools/list from SSE stream"
fi

if [[ -n "${DEMO_INTENT}" ]]; then
  echo "=== tools/list (intent: ${DEMO_INTENT}) ==="
else
  echo "=== tools/list ==="
fi

cat "${OUT_FILE}"
echo "---"
total="$(wc -l <"${OUT_FILE}" | tr -d ' ')"
echo "Total: ${total} tools"
echo "k8s__:  $(grep -c '^k8s__' "${OUT_FILE}" || true)"
echo "prom__: $(grep -c '^prom__' "${OUT_FILE}" || true)"
echo "gh__:   $(grep -c '^gh__' "${OUT_FILE}" || true)"
