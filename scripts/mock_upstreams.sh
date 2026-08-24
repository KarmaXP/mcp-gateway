#!/usr/bin/env bash
# Start/stop a named set of MCP mock upstreams. One table per set; everything else is shared.
#
#   bash scripts/mock_upstreams.sh demo start     # alpha 3101, beta 3102 (gateway.example.yaml)
#   bash scripts/mock_upstreams.sh sre stop       # k8s 3201, prom 3202, gh 3203 (gateway.sre.example.yaml)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SET_NAME="${1:-}"
ACTION="${2:-}"

# port,tools,marker,name
case "${SET_NAME}" in
  demo)
    MOCKS=(
      "${MOCK_ALPHA_PORT:-3101},echo,alpha-ok,alpha-upstream"
      "${MOCK_BETA_PORT:-3102},echo|ping,beta-ok,beta-upstream"
    )
    ;;
  sre)
    MOCKS=(
      "${MOCK_K8S_PORT:-3201},get_pod_logs|list_pods|describe_pod|list_events,k8s-ok,k8s-upstream"
      "${MOCK_PROM_PORT:-3202},query_instant|query_range|alerts|targets,prom-ok,prom-upstream"
      "${MOCK_GH_PORT:-3203},list_prs|get_pr,gh-ok,gh-upstream"
    )
    ;;
  *)
    echo "usage: $0 demo|sre start|stop" >&2
    exit 2
    ;;
esac

PIDFILE="${MOCK_UPSTREAMS_PIDFILE:-/tmp/mcp-gateway-${SET_NAME}-backends.pids}"
LOCKDIR="${MOCK_UPSTREAMS_LOCKDIR:-/tmp/mcp-gateway-${SET_NAME}-backends.lock.d}"

set_ports() {
  local entry
  for entry in "${MOCKS[@]}"; do
    echo "${entry%%,*}"
  done
}

start_one() {
  local port="$1" tools="$2" marker="$3" name="$4"
  go run ./scripts/mock_upstream/main.go \
    -listen "127.0.0.1:${port}" \
    -tools "${tools//|/,}" \
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
  local port
  for port in $(set_ports); do
    lsof -ti ":${port}" 2>/dev/null | xargs kill -9 2>/dev/null || true
  done
}

all_listening() {
  local port
  for port in $(set_ports); do
    lsof -ti ":${port}" >/dev/null 2>&1 || return 1
  done
  return 0
}

case "${ACTION}" in
  start)
    if ! mkdir "${LOCKDIR}" 2>/dev/null; then
      echo "${SET_NAME} mocks already starting or running (lock ${LOCKDIR})"
      exit 1
    fi
    stop_pids
    echo "Starting ${SET_NAME} mock upstreams on $(set_ports | tr '\n' ' ')"
    for entry in "${MOCKS[@]}"; do
      IFS=, read -r port tools marker name <<<"${entry}"
      start_one "${port}" "${tools}" "${marker}" "${name}"
    done
    for _ in $(seq 1 60); do
      all_listening && break
      sleep 0.25
    done
    if ! all_listening; then
      echo "${SET_NAME} mocks failed to bind within 15s"
      stop_pids
      rmdir "${LOCKDIR}" 2>/dev/null || true
      exit 1
    fi
    echo "${SET_NAME} mocks running (pidfile ${PIDFILE})"
    ;;
  stop)
    echo "Stopping ${SET_NAME} mocks"
    stop_pids
    rmdir "${LOCKDIR}" 2>/dev/null || true
    echo "${SET_NAME} mocks stopped"
    ;;
  *)
    echo "usage: $0 demo|sre start|stop" >&2
    exit 2
    ;;
esac
