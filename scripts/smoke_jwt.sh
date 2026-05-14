#!/usr/bin/env bash
# JWT smoke: MCP HTTP+SSE through gateway with AUTH_MODE=jwt (no docker).
# Verifies:
#  - JWT-authenticated initialize + tools/list + allowed tools/call
#  - denied tools/call when target is outside JWT mcp_tools allow-list
#
# Usage:
#   SMOKE_AUTO_START_GATEWAY=1 bash scripts/smoke_jwt.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

UPSTREAM_PORT="${SMOKE_UPSTREAM_PORT:-31400}"
if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
  GATEWAY_PORT="${SMOKE_GATEWAY_PORT:-18082}"
else
  GATEWAY_PORT="${SMOKE_GATEWAY_PORT:-${PORT:-8080}}"
fi
GATEWAY_URL="${SMOKE_GATEWAY_URL:-http://127.0.0.1:${GATEWAY_PORT}}"
JWT_ISS="${SMOKE_JWT_ISS:-https://smoke.jwt.local}"
JWT_AUD="${SMOKE_JWT_AUD:-mcp-gateway-smoke}"
JWT_KID="${SMOKE_JWT_KID:-smoke-k1}"
ALLOW_TOOL="${SMOKE_ALLOW_TOOL:-smoke__echo}"
DENY_TOOL="${SMOKE_DENY_TOOL:-smoke__blocked}"

TMPYAML=""
SSE_HDR=""
SSE_OUT=""
JWT_PRIVATE_KEY_FILE=""
JWT_PUBLIC_KEY_FILE=""
UP_PID=""
GW_PID=""
SSE_PID=""

cleanup() {
  [[ -n "${SSE_PID:-}" ]] && kill "${SSE_PID}" 2>/dev/null || true
  [[ -n "${UP_PID:-}" ]] && kill "${UP_PID}" 2>/dev/null || true
  [[ -n "${GW_PID:-}" ]] && kill "${GW_PID}" 2>/dev/null || true
  [[ -n "${TMPYAML}" && -f "${TMPYAML}" ]] && rm -f "${TMPYAML}"
  [[ -n "${SSE_HDR}" && -f "${SSE_HDR}" ]] && rm -f "${SSE_HDR}"
  [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]] && rm -f "${SSE_OUT}"
  [[ -n "${JWT_PRIVATE_KEY_FILE}" && -f "${JWT_PRIVATE_KEY_FILE}" ]] && rm -f "${JWT_PRIVATE_KEY_FILE}"
  [[ -n "${JWT_PUBLIC_KEY_FILE}" && -f "${JWT_PUBLIC_KEY_FILE}" ]] && rm -f "${JWT_PUBLIC_KEY_FILE}"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

dump_sse() {
  if [[ -n "${SSE_OUT}" && -f "${SSE_OUT}" ]]; then
    echo "--- SSE output (first 200 lines) ---" >&2
    sed -n '1,200p' "${SSE_OUT}" >&2
  fi
}

wait_sse_contains() {
  local pattern="$1"
  local label="$2"
  for _ in $(seq 1 60); do
    if grep -Fq -- "${pattern}" "${SSE_OUT}"; then
      return 0
    fi
    sleep 0.15
  done
  echo "Expected ${label} on SSE stream." >&2
  dump_sse
  fail "SSE assertion failed for: ${label}"
}

gen_token() {
  local allowed_tools="$1"
  local token=""
  if ! token="$(
    go run ./tools/gen-jwt \
      -issuer "${JWT_ISS}" \
      -audience "${JWT_AUD}" \
      -key "${JWT_PRIVATE_KEY_FILE}" \
      -kid "${JWT_KID}" \
      -mcp-tools "${allowed_tools}"
  )"; then
    fail "failed generating JWT"
  fi
  printf '%s' "${token}"
}

post_rpc() {
  local body="$1"
  local step="$2"
  local token="$3"
  local http_code=""
  if ! http_code="$(
    curl -sS -o /dev/null -w "%{http_code}" \
      -H "Authorization: Bearer ${token}" \
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

# Best-effort free upstream (stale smoke_upstream) and dedicated auto-start gateway port.
if [[ "${SMOKE_CLEAN_PORTS:-1}" == "1" ]]; then
  lsof -ti ":${UPSTREAM_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
    lsof -ti ":${GATEWAY_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  fi
fi

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

JWT_PRIVATE_KEY_FILE="$(mktemp)"
JWT_PUBLIC_KEY_FILE="$(mktemp)"
cat >"${JWT_PRIVATE_KEY_FILE}" <<'EOF'
-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQC/oRWzgc5y3ctc
G9XfH6z+BDR06QAwZLMHlgUGbk4LbavsmTTb1McASXyC7gPZT3LDcEaPSzHwH1mW
C9+Z42T/0H6i+17i6LgVFGZuPe6fMLVxGZLXO8ez2REJUDnpkSPf0Wyg6UNpVSYT
zmsiFa9TnrFzHNymZnict3ddwpxoBdNIxYenabm6jaq3BCdtq5Ttuo4CwmO2PXSv
WIepPJBE9qN/3hwUW65zVW/JvsgaNM9P6kL7Jsy8Hy5L0Jg0Ne06mNzUjDz8lSR8
5BERG5xspC5YTfYV191SMn1BA7gEjiygLCMEKRQxWwmV6RqFHl/+Xkqs7FRdCq1C
fSCxHgX5AgMBAAECggEAEmztNsDk9mOAIb+lbVpQ2m3SE2mx9HVCR5jzq74gb/Xg
IZRLolWPuuXV/IrhQNxkRwl9J1sOFq4VAZnrqpLUS8qi2o37/iptRM2c2b3Hu3PG
BnV0ipB7b74P5srZfq1Pez5aSRCUxESFMENZRsI6BPrNyik8yB0zPLJhXlkPi+rU
wn+Uf3KZw/AdzxDhBAZkJReyz1oGpC3aHoxuEx1bIivumSSesULmmmqUzsh05JN3
GYMoNpnetrtk+pNGibWf8CnB6Wne/X3k0F16vII5y927hvVhoMbUFb21zGxKYPTq
jo/RMttMvC9gfR9WNIIc65PsZcC8wUSwDRjTZaZmXwKBgQDu1WC6tSIpMnvQ6N5/
u8GVw2crlUUrLsoqS55GMr1NzikgHLF0khkscGy/TgMp4rDxYEs62XdcQd64d+rH
2LEeJTHtPqrIHQn4GPixzTb8FjBZRnkAXB9yv0ZVAom0Ehu3WJgC7YpCXVU5ykPG
Rq1K3qDh0rD0j9Ywe810idG2TwKBgQDNZyGlrlRvItDr7+D0VqIFr7Rr/i7A4SF/
9T7FMD0SLgpReepr6poZ9QxZaJujiZV4iUvNQU8F2jl8BjjY6ONWfwURDcEaOTqF
FcPMjC/u4gTzNLEqXo8gWUiZLUSphojSX414+CbJ4vfexBOINbwpL2MPXkItuxF0
mVAU3ba1NwKBgBPhIOcJkqlZMWMnLvX0290qYZkIGLTKdTtmBeuT55vlUBkDKmYo
jv3a8cJOrQa8frvopvpkBYJhXTd/i8RMrhlzQR+dOrvjZuQGuBScnzoGYsnbitDT
2i5D64fB6VJau4HcVvLPcNWrTR+9TTzgvyXfOAbz8ZS5sDti4qwTmKgTAoGAKxdo
yq5xDkO6mtTfV8NZCGJdMo7H1jUk5whXW90L4uV/yqoOEQfNvoZXSeaVSFDT5869
9VivMGYgyzEu+eqZzwqk0HgXO94ntcXkJuR+JdqK+U7joCToV/wDLAeAMSSFTcU4
E9nToWUZZUWzZ08Go4lKee3nalqlhdWoJEiDTS8CgYB1kJGSSPqZA6Y45sz5BnN8
UnivsP7wadnX18U95DBFQ1W6f8o0YsoQ5uda7HyoUuzyUjnTsW1I5mRoOkrhwAWc
wQDu1QfqWPbN1cyon16lVGifcWAE4T/T6CTvf0m4OjviHr7balvRZ2CnnKIou3vt
of2GbdwOiciZpvNSrjCNhg==
-----END PRIVATE KEY-----
EOF
cat >"${JWT_PUBLIC_KEY_FILE}" <<'EOF'
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv6EVs4HOct3LXBvV3x+s
/gQ0dOkAMGSzB5YFBm5OC22r7Jk029THAEl8gu4D2U9yw3BGj0sx8B9ZlgvfmeNk
/9B+ovte4ui4FRRmbj3unzC1cRmS1zvHs9kRCVA56ZEj39FsoOlDaVUmE85rIhWv
U56xcxzcpmZ4nLd3XcKcaAXTSMWHp2m5uo2qtwQnbauU7bqOAsJjtj10r1iHqTyQ
RPajf94cFFuuc1Vvyb7IGjTPT+pC+ybMvB8uS9CYNDXtOpjc1Iw8/JUkfOQRERuc
bKQuWE32FdfdUjJ9QQO4BI4soCwjBCkUMVsJlekahR5f/l5KrOxUXQqtQn0gsR4F
+QIDAQAB
-----END PUBLIC KEY-----
EOF

ALLOW_JWT="$(gen_token "${ALLOW_TOOL}")"
[[ -n "${ALLOW_JWT}" ]] || fail "allow token was empty"

echo "==> starting smoke upstream on 127.0.0.1:${UPSTREAM_PORT}"
go run ./scripts/smoke_upstream/main.go -listen "127.0.0.1:${UPSTREAM_PORT}" &
UP_PID=$!
sleep 0.5

if [[ "${SMOKE_AUTO_START_GATEWAY:-}" == "1" ]]; then
  echo "==> auto-starting gateway AUTH_MODE=jwt PORT=${GATEWAY_PORT} (JSON logs on stdout)"
  MCP_GATEWAY_CONFIG="${TMPYAML}" \
  AUTH_MODE=jwt \
  JWT_ISS="${JWT_ISS}" \
  JWT_AUD="${JWT_AUD}" \
  JWT_PUBLIC_KEY_PEM="$(<"${JWT_PUBLIC_KEY_FILE}")" \
  PORT="${GATEWAY_PORT}" \
    go run ./cmd/gateway/main.go &
  GW_PID=$!
  for _ in $(seq 1 40); do
    if curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 0.15
  done
  curl -sf "${GATEWAY_URL}/healthz" >/dev/null || fail "gateway failed to become healthy"
else
  curl -sf "${GATEWAY_URL}/healthz" >/dev/null 2>&1 || fail "gateway not healthy at ${GATEWAY_URL}"
fi

SSE_HDR="$(mktemp)"
SSE_OUT="$(mktemp)"
echo "==> GET ${GATEWAY_URL}/mcp/sse (JWT allow token, background) -> ${SSE_OUT}"
curl -sS -N -o "${SSE_OUT}" -D "${SSE_HDR}" \
  -H "Authorization: Bearer ${ALLOW_JWT}" \
  "${GATEWAY_URL}/mcp/sse" &
SSE_PID=$!
sleep 0.6

SID="$(grep -i '^mcp-session-id:' "${SSE_HDR}" | sed -n '1s/[^:]*: *//p' | tr -d '\r\n' || true)"
if [[ -z "${SID}" ]]; then
  echo "failed to read Mcp-Session-Id from headers:" >&2
  sed -n '1,80p' "${SSE_HDR}" >&2
  fail "SSE handshake failed"
fi
echo "==> session id: ${SID}"

echo "==> initialize (allow token)"
post_rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke-jwt","version":"1.0.0"}}}' "initialize allow" "${ALLOW_JWT}"
wait_sse_contains '"id":1' 'initialize response id'
wait_sse_contains '"result"' 'initialize result'

echo "==> notifications/initialized (allow token)"
post_rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}' "notifications/initialized allow" "${ALLOW_JWT}"

echo "==> tools/list (allow token)"
post_rpc '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' "tools/list allow" "${ALLOW_JWT}"
wait_sse_contains '"id":3' 'tools/list response id'
wait_sse_contains "${ALLOW_TOOL}" "tools/list contains ${ALLOW_TOOL}"

echo "==> tools/call ${ALLOW_TOOL} (allow token)"
ALLOW_CALL_BODY="$(printf '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"%s","arguments":{}}}' "${ALLOW_TOOL}")"
post_rpc "${ALLOW_CALL_BODY}" "tools/call allow" "${ALLOW_JWT}"
wait_sse_contains '"id":4' 'tools/call allow response id'
wait_sse_contains 'smoke-ok' 'tools/call allow payload smoke-ok'

echo "==> tools/call ${DENY_TOOL} (allow token, tool not in mcp_tools, expect -32003)"
DENY_CALL_BODY="$(printf '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"%s","arguments":{}}}' "${DENY_TOOL}")"
post_rpc "${DENY_CALL_BODY}" "tools/call deny" "${ALLOW_JWT}"
wait_sse_contains '"id":5' 'tools/call deny response id'
wait_sse_contains '"code":-32003' 'permission denied error code'
wait_sse_contains 'not allowed for this principal' 'permission denied message'

echo
echo "JWT SMOKE OK — authenticated MCP flow passed and deny-path returned -32003."
