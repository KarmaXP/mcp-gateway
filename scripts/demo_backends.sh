#!/usr/bin/env bash
# Start/stop alpha (3101) and beta (3102) MCP mock upstreams for gateway.example.yaml.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PIDFILE="${DEMO_BACKENDS_PIDFILE:-/tmp/mcp-gateway-demo-backends.pids}"
LOCKDIR="${DEMO_BACKENDS_LOCKDIR:-/tmp/mcp-gateway-demo-backends.lock.d}"
ALPHA_PORT="${MOCK_ALPHA_PORT:-3101}"
BETA_PORT="${MOCK_BETA_PORT:-3102}"

start_one() {
  local listen="$1"
  local tools="$2"
  local marker="$3"
  local name="$4"
  go run ./scripts/mock_upstream/main.go \
    -listen "127.0.0.1:${listen}" \
    -tools "${tools}" \
    -marker "${marker}" \
    -name "${name}" &
  echo "$!" >>"${PIDFILE}"
}

stop_pids() {
  if [[ -f "${PIDFILE}" ]]; then
    while read -r pid; do
      [[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null || true
    done <"${PIDFILE}"
    rm -f "${PIDFILE}"
  fi
  lsof -ti ":${ALPHA_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  lsof -ti ":${BETA_PORT}" 2>/dev/null | xargs kill -9 2>/dev/null || true
}

case "${1:-}" in
  start)
    if ! mkdir "${LOCKDIR}" 2>/dev/null; then
      echo "demo backends already starting or running (lock ${LOCKDIR})"
      exit 1
    fi
    stop_pids
    rm -f "${PIDFILE}"
    echo "Starting mock upstream alpha on :${ALPHA_PORT} and beta on :${BETA_PORT}"
    start_one "${ALPHA_PORT}" "echo" "alpha-ok" "alpha-upstream"
    start_one "${BETA_PORT}" "echo,ping" "beta-ok" "beta-upstream"
    ready=0
    for _ in $(seq 1 60); do
      if lsof -ti ":${ALPHA_PORT}" >/dev/null 2>&1 && lsof -ti ":${BETA_PORT}" >/dev/null 2>&1; then
        ready=1
        break
      fi
      sleep 0.25
    done
    if [[ "${ready}" -ne 1 ]]; then
      echo "mock upstream failed to bind :${ALPHA_PORT} or :${BETA_PORT} within 15s"
      stop_pids
      rmdir "${LOCKDIR}" 2>/dev/null || true
      exit 1
    fi
    echo "demo backends running (pidfile ${PIDFILE})"
    ;;
  stop)
    echo "Stopping demo backends"
    stop_pids
    rmdir "${LOCKDIR}" 2>/dev/null || true
    echo "demo backends stopped"
    ;;
  *)
    echo "usage: $0 start|stop"
    exit 2
    ;;
esac
