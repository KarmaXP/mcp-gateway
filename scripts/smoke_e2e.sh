#!/usr/bin/env bash
# Smoke checks against a running gateway (AUTH_MODE=none by default).
# JWT: set AUTH_MODE=jwt and JWT_* on the gateway, mint a token with tools/gen-jwt, pass Authorization: Bearer.
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8080}"

echo "==> GET /healthz"
curl -sfS "${GATEWAY_URL}/healthz" | head -c 200 || {
  echo "FAIL: gateway not reachable at ${GATEWAY_URL}" >&2
  exit 1
}
echo
echo "==> GET /readyz"
curl -sfS "${GATEWAY_URL}/readyz"
echo

echo "==> SSE (first lines, 3s max)"
curl -sfS -N --max-time 3 "${GATEWAY_URL}/mcp/sse" | head -n 5 || true

echo "OK smoke finished."
