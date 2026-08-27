#!/usr/bin/env bash
set -euo pipefail

# Checks gateway p95 latency against a threshold.
# Preferred source: Prometheus histogram (gateway internal phases).
# Fallback source: cmd/loadtest client-side latency_p95_ms approximation.

P95_THRESHOLD_MS="${P95_THRESHOLD_MS:-50}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
PROM_LOOKBACK="${PROM_LOOKBACK:-5m}"
SKIP_IF_NO_METRICS="${SKIP_IF_NO_METRICS:-0}"
ALLOW_LOADTEST_FALLBACK="${ALLOW_LOADTEST_FALLBACK:-1}"
GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8080}"
LOADTEST_MODE="${LOADTEST_MODE:-direct}"
LOADTEST_WORKERS="${LOADTEST_WORKERS:-6}"
LOADTEST_DURATION="${LOADTEST_DURATION:-30s}"
LOADTEST_WARMUP="${LOADTEST_WARMUP:-2}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

PROM_QUERY="$(cat <<EOF
max(
  histogram_quantile(
    0.95,
    sum by (le, phase) (
      rate({__name__=~"mcp(_mcp)?_gateway_internal_duration_seconds_bucket"}[${PROM_LOOKBACK}])
    )
  )
)*1000
EOF
)"

log() {
  printf '[check_gateway_p95] %s\n' "$*"
}

parse_prometheus_value_ms() {
  local response="$1"
  python3 - "$response" <<'PY'
import json
import sys

raw = sys.argv[1]
if not raw:
    print("")
    raise SystemExit(0)

data = json.loads(raw)
if data.get("status") != "success":
    print("")
    raise SystemExit(0)

result = data.get("data", {}).get("result", [])
if not result:
    print("")
    raise SystemExit(0)

try:
    print(result[0]["value"][1])
except Exception:
    print("")
PY
}

check_threshold() {
  local observed_ms="$1"
  local source="$2"
  python3 - "$observed_ms" "$P95_THRESHOLD_MS" "$source" <<'PY'
import sys

observed = float(sys.argv[1])
threshold = float(sys.argv[2])
source = sys.argv[3]

if observed <= threshold:
    print(f"PASS source={source} observed_p95_ms={observed:.3f} threshold_ms={threshold:.3f}")
    raise SystemExit(0)

print(f"FAIL source={source} observed_p95_ms={observed:.3f} threshold_ms={threshold:.3f}")
raise SystemExit(1)
PY
}

prometheus_query() {
  curl -fsS --get \
    --data-urlencode "query=${PROM_QUERY}" \
    "${PROMETHEUS_URL}/api/v1/query"
}

run_loadtest_fallback() {
  log "Running loadtest fallback against ${GATEWAY_URL}"
  local output
  output="$(cd "${REPO_ROOT}" && go run ./cmd/loadtest \
    -url "${GATEWAY_URL}" \
    -mode "${LOADTEST_MODE}" \
    -workers "${LOADTEST_WORKERS}" \
    -duration "${LOADTEST_DURATION}" \
    -warmup "${LOADTEST_WARMUP}")"
  printf '%s\n' "${output}"
  local approx
  approx="$(printf '%s\n' "${output}" | awk -F= '/^latency_p95_ms=/{print $2; exit}')"
  if [[ -z "${approx}" ]]; then
    log "Could not parse latency_p95_ms from loadtest output"
    return 2
  fi
  check_threshold "${approx}" "loadtest_approximation"
}

main() {
  log "Target threshold: ${P95_THRESHOLD_MS}ms"
  log "Trying Prometheus query at ${PROMETHEUS_URL}"
  local prom_output=""
  if prom_output="$(prometheus_query 2>/dev/null)"; then
    local prom_p95
    prom_p95="$(parse_prometheus_value_ms "${prom_output}")"
    if [[ -n "${prom_p95}" ]]; then
      check_threshold "${prom_p95}" "prometheus_internal_histogram"
      return 0
    fi
    log "Prometheus reachable but internal histogram series not present yet"
  else
    log "Prometheus query failed"
  fi

  if [[ "${ALLOW_LOADTEST_FALLBACK}" == "1" ]]; then
    log "Falling back to loadtest p95 approximation"
    run_loadtest_fallback
    return $?
  fi

  if [[ "${SKIP_IF_NO_METRICS}" == "1" ]]; then
    log "SKIP no usable internal metrics; set ALLOW_LOADTEST_FALLBACK=1 to approximate with loadtest"
    return 0
  fi

  log "No usable metrics source and fallback disabled"
  return 2
}

main "$@"
