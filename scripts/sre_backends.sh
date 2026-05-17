#!/usr/bin/env bash
# Start/stop k8s (3201), prom (3202), gh (3203) MCP mock upstreams for gateway.sre.example.yaml.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PIDFILE="${SRE_BACKENDS_PIDFILE:-/tmp/mcp-gateway-sre-backends.pids}"
K8S_PORT="${MOCK_K8S_PORT:-3201}"
PROM_PORT="${MOCK_PROM_PORT:-3202}"
GH_PORT="${MOCK_GH_PORT:-3203}"

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
  for port in "${K8S_PORT}" "${PROM_PORT}" "${GH_PORT}"; do
    lsof -ti ":${port}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  done
}

wait_ports() {
  local ready=0
  for _ in $(seq 1 60); do
    if lsof -ti ":${K8S_PORT}" >/dev/null 2>&1 \
      && lsof -ti ":${PROM_PORT}" >/dev/null 2>&1 \
      && lsof -ti ":${GH_PORT}" >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [[ "${ready}" -ne 1 ]]; then
    echo "SRE mocks failed to bind :${K8S_PORT}, :${PROM_PORT}, or :${GH_PORT} within 15s"
    stop_pids
    exit 1
  fi
}

case "${1:-}" in
  start)
    stop_pids
    rm -f "${PIDFILE}"
    echo "Starting SRE mocks k8s:${K8S_PORT} prom:${PROM_PORT} gh:${GH_PORT}"
    start_one "${K8S_PORT}" "get_pod_logs,list_pods,describe_pod,list_events" "k8s-ok" "k8s-upstream"
    start_one "${PROM_PORT}" "query_instant,query_range,alerts,targets" "prom-ok" "prom-upstream"
    start_one "${GH_PORT}" "list_prs,get_pr" "gh-ok" "gh-upstream"
    wait_ports
    echo "SRE backends running (pidfile ${PIDFILE})"
    ;;
  stop)
    echo "Stopping SRE backends"
    stop_pids
    echo "SRE backends stopped"
    ;;
  *)
    echo "usage: $0 start|stop"
    exit 2
    ;;
esac
