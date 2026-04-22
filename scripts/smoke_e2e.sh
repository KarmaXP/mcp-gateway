#!/usr/bin/env bash
# Smoke checks against a running gateway (AUTH_MODE=none by default).
# For JWT mode: export AUTH_MODE=jwt, set JWT_* env on the gateway, then:
#   TOKEN="$(go run ./tools/gen-jwt -issuer "$JWT_ISS" -audience "$JWT_AUD" -key "$JWT_DEV_PRIVATE_KEY_PEM_FILE")"
#   curl -H "Authorization: Bearer $TOKEN" ...
#
# Qdrant: the Go gateway currently indexes tools in an in-memory vector store by default.
# Verifying Qdrant payload/search requires a future Qdrant-backed store implementation.
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

echo "==> SSE (first line only, 3s max)"
# Read until we get Mcp-Session-Id or timeout
curl -sfS -N --max-time 3 "${GATEWAY_URL}/mcp/sse" | head -n 5 || true

echo "OK smoke finished (SSE may truncate — expected with head/max-time)."
