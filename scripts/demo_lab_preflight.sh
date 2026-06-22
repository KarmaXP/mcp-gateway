#!/usr/bin/env bash
# Integrated lab preflight: bash scripts/demo_lab_preflight.sh [--full]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FULL=0
[[ "${1:-}" == "--full" ]] && FULL=1

GATEWAY_URL="${GATEWAY_URL:-}"
PORT="${PORT:-18080}"
EMBED_URL="${EMBED_URL:-}"
HOST_PORT_EMBED="${HOST_PORT_EMBED:-}"
FIXTURE="/private/tmp/mcp-gateway-lab/readme.txt"

ok()  { printf '  OK  %s\n' "$*"; }
die() { printf 'FAIL  %s\n' "$*" >&2; exit 1; }
warn() { printf 'WARN  %s\n' "$*" >&2; }

load_env() {
  if [[ -f .env ]]; then
    set -a
    # shellcheck disable=SC1091
    source ./.env
    set +a
  fi
  GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:${PORT:-18080}}"
  PORT="${PORT:-18080}"
}

embed_health_url() {
  if [[ -n "${HOST_PORT_EMBED}" ]]; then
    echo "http://127.0.0.1:${HOST_PORT_EMBED}/healthz"
  elif [[ -n "${EMBED_URL}" ]]; then
    local base="${EMBED_URL%/}"
    echo "${base}/healthz"
  else
    echo "http://127.0.0.1:8001/healthz"
  fi
}

sync_embed_expectation() {
  if [[ -n "${HOST_PORT_EMBED}" ]]; then
    local expected="http://127.0.0.1:${HOST_PORT_EMBED}"
    if [[ -n "${EMBED_URL}" && "${EMBED_URL}" != "${expected}" ]]; then
      warn ".env EMBED_URL=${EMBED_URL} but HOST_PORT_EMBED=${HOST_PORT_EMBED} — make run forces ${expected}"
    fi
    EMBED_URL="${expected}"
  fi
}

load_env
sync_embed_expectation

echo "=== demo lab preflight ==="
echo "gateway: ${GATEWAY_URL}"
echo "embed:   ${EMBED_URL:-<default :8001>}"
echo

curl -sfS "http://127.0.0.1:6333/healthz" >/dev/null \
  || die "Qdrant down — run: make docker-up"
ok "Qdrant http://127.0.0.1:6333/healthz"

embed_hz="$(embed_health_url)"
if ! curl -sfS "${embed_hz}" >/dev/null 2>&1; then
  die "Embed down at ${embed_hz} — wait ~60s after make docker-up or check: docker logs mcp-gateway-embed"
fi
ok "Embed ${embed_hz}"

if curl -sfS "http://127.0.0.1:3000/api/health" >/dev/null 2>&1; then
  ok "Grafana http://127.0.0.1:3000"
else
  warn "Grafana not reachable on :3000 (scene 5 only)"
fi

mkdir -p /private/tmp/mcp-gateway-lab
if [[ ! -f "${FIXTURE}" ]]; then
  echo 'fixture-ok' >"${FIXTURE}"
  ok "created ${FIXTURE}"
else
  content="$(tr -d '\n' <"${FIXTURE}" || true)"
  [[ "${content}" == "fixture-ok" ]] \
    || die "${FIXTURE} must contain exactly 'fixture-ok' (got: ${content:-<empty>})"
  ok "fixture ${FIXTURE}"
fi

chmod +x scripts/lab_jwt_keys.sh
bash scripts/lab_jwt_keys.sh keys >/dev/null
eval "$(bash scripts/lab_jwt_keys.sh env)"
[[ -n "${JWT_ADMIN:-}" ]] || die "JWT_ADMIN empty — run: eval \"\$(bash scripts/lab_jwt_keys.sh env)\""
[[ -n "${JWT_ADMIN_FULL:-}" ]] || die "JWT_ADMIN_FULL empty"
[[ -n "${JWT_RESTRICTED:-}" ]] || die "JWT_RESTRICTED empty"
ok "JWT tokens (admin, full, restricted)"

if [[ ! -f "${JWT_PUBLIC_KEY_FILE:-/tmp/mcp-lab-jwt.pub.pem}" ]]; then
  die "JWT public key missing — make lab-jwt-keys"
fi
ok "JWT public key ${JWT_PUBLIC_KEY_FILE:-/tmp/mcp-lab-jwt.pub.pem}"

if [[ -f .env ]]; then
  if grep -q 'gateway.real.yaml' .env 2>/dev/null; then
    ok ".env uses gateway.real.yaml"
  else
    warn ".env MCP_GATEWAY_CONFIG is not gateway.real.yaml — demo needs deployments/gateway.real.yaml"
  fi
  if grep -q 'AUTH_MODE=jwt' .env 2>/dev/null; then
    ok ".env AUTH_MODE=jwt"
  else
    warn ".env missing AUTH_MODE=jwt"
  fi
else
  warn "No .env — copy from .env.example or run make bootstrap"
fi

if [[ "${FULL}" -eq 0 ]]; then
  echo
  echo "Preflight OK. Next:"
  echo "  make run"
  echo "  bash scripts/demo_lab_preflight.sh --full"
  exit 0
fi

ready_code="$(curl -sS -o /dev/null -w '%{http_code}' "${GATEWAY_URL}/readyz" 2>/dev/null || echo 000)"
[[ "${ready_code}" == "200" ]] \
  || die "Gateway /readyz HTTP ${ready_code} — start: make run (embed must match HOST_PORT_EMBED)"
ok "Gateway ${GATEWAY_URL}/readyz"

export GATEWAY_URL SMOKE_JWT="${JWT_ADMIN_FULL}"

echo
echo "=== scene 1: catalog (JWT_ADMIN_FULL → ~36 tools) ==="
catalog_log="$(mktemp)"
bash scripts/demo_show_catalog.sh >"${catalog_log}" 2>&1
cat "${catalog_log}"
total="$(awk '/^Total:/{print $2}' "${catalog_log}")"
rm -f "${catalog_log}"
[[ "${total:-0}" -ge 30 ]] || die "Expected ≥30 tools in catalog, got: ${total:-0}"
ok "catalog total=${total}"

echo
echo "=== scene 2a: JWT allow ==="
GATEWAY_URL="${GATEWAY_URL}" SMOKE_JWT="${JWT_ADMIN}" \
  SMOKE_EXPECT_TOOL=prom__read_text_file \
  SMOKE_EXPECT_TEXT=fixture-ok \
  SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}' \
  bash scripts/smoke_e2e.sh
ok "JWT allow smoke"

echo
echo "=== scene 2b: JWT deny -32003 ==="
GATEWAY_URL="${GATEWAY_URL}" SMOKE_JWT="${JWT_RESTRICTED}" \
  SMOKE_EXPECT_TOOL=prom__list_directory \
  SMOKE_EXPECT_RPC_ERROR=-32003 \
  SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab"}' \
  bash scripts/smoke_e2e.sh
ok "JWT deny smoke"

echo
echo "=== scene 4: LangGraph agent ==="
LG_ROOT="$(cd "${ROOT}/../langgraph-demo" 2>/dev/null && pwd || true)"
if [[ -z "${LG_ROOT}" || ! -f "${LG_ROOT}/agent.py" ]]; then
  warn "langgraph-demo/ not found — skip agent check"
else
  (
    cd "${LG_ROOT}"
    export GATEWAY_URL GATEWAY_JWT="${JWT_ADMIN}"
    export AGENT_REQUEST="Read the lab readme file from the filesystem backend"
    out="$(python3 agent.py 2>&1)"
    echo "${out}"
    echo "${out}" | grep -q 'fixture-ok' || die "LangGraph agent did not return fixture-ok"
    echo "${out}" | grep -q '\[agent\] OK' || die "LangGraph agent did not print [agent] OK"
  )
  ok "LangGraph agent"
fi

echo
echo "=== ALL DEMO CHECKS PASSED ==="
echo "Optional: Ctrl+C gateway → make run-filter-list → re-run catalog with intent (scene 3)"
