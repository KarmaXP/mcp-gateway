#!/usr/bin/env bash
# Collect p50/p95/p99 by gateway internal phase from Prometheus.
#
# Usage:
#   PROM_URL=http://127.0.0.1:9090 WINDOW=5m scripts/collect_internal_latency.sh
#   PROM_URL=http://127.0.0.1:9090 WINDOW=10m METRIC_PREFIX='mcp(_mcp)?_gateway_internal_duration_seconds' scripts/collect_internal_latency.sh
#
# Output:
#   Markdown table with parse/security/router/mux rows and quantiles in milliseconds.
set -euo pipefail

PROM_URL="${PROM_URL:-http://127.0.0.1:9090}"
WINDOW="${WINDOW:-5m}"
METRIC_PREFIX="${METRIC_PREFIX:-mcp(_mcp)?_gateway_internal_duration_seconds}"
PHASE_RE="${PHASE_RE:-parse|security|router|mux}"

query_quantile() {
  local quantile="$1"
  local query
  query="histogram_quantile(${quantile}, sum by (le, phase) (rate({__name__=~\"${METRIC_PREFIX}_bucket\",phase=~\"${PHASE_RE}\"}[${WINDOW}])))"
  curl -fsS -G "${PROM_URL%/}/api/v1/query" --data-urlencode "query=${query}"
}

tmp50="$(mktemp)"
tmp95="$(mktemp)"
tmp99="$(mktemp)"
trap 'rm -f "$tmp50" "$tmp95" "$tmp99"' EXIT

query_quantile "0.50" >"$tmp50"
query_quantile "0.95" >"$tmp95"
query_quantile "0.99" >"$tmp99"

python3 - "$tmp50" "$tmp95" "$tmp99" <<'PY'
import json
import sys
from datetime import datetime, timezone

phases = ["parse", "security", "router", "mux"]

def read_phase_values(path):
    with open(path, "r", encoding="utf-8") as f:
        payload = json.load(f)
    if payload.get("status") != "success":
        raise SystemExit(f"Prometheus query failed for {path}: {payload}")
    out = {}
    for item in payload.get("data", {}).get("result", []):
        phase = item.get("metric", {}).get("phase")
        value = float(item.get("value", [None, "nan"])[1])
        if phase:
            out[phase] = value
    return out

q50 = read_phase_values(sys.argv[1])
q95 = read_phase_values(sys.argv[2])
q99 = read_phase_values(sys.argv[3])
ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

def to_ms(v):
    if v is None:
        return ""
    return f"{v * 1000:.2f}"

print(f"# Internal latency by phase ({ts})")
print("")
print("| Phase | p50 (ms) | p95 (ms) | p99 (ms) |")
print("| ----- | -------- | -------- | -------- |")
for phase in phases:
    print(
        f"| {phase} | {to_ms(q50.get(phase))} | {to_ms(q95.get(phase))} | {to_ms(q99.get(phase))} |"
    )
PY

