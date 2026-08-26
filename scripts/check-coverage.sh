#!/usr/bin/env bash
# Fails when internal coverage drops below the floor.
#   COVERAGE_FLOOR=80 bash scripts/check-coverage.sh    # raise the floor
set -euo pipefail

FLOOR="${COVERAGE_FLOOR:-75}"
PROFILE="${COVERAGE_PROFILE:-bin/coverage.out}"

if [[ ! -f "${PROFILE}" ]]; then
  echo "check-coverage: ${PROFILE} not found (run: make test-cover)" >&2
  exit 1
fi

# internal/upstream/mock is test scaffolding: its own coverage says nothing about the gateway.
filtered="$(mktemp)"
trap 'rm -f "${filtered}"' EXIT
head -n 1 "${PROFILE}" > "${filtered}"
grep -v '/internal/upstream/mock/' "${PROFILE}" | tail -n +2 >> "${filtered}" || true

total="$(go tool cover -func="${filtered}" | awk '/^total:/ {print $NF}' | tr -d '%')"

if awk -v t="${total}" -v f="${FLOOR}" 'BEGIN { exit !(t < f) }'; then
  echo "check-coverage: ${total}% is below the ${FLOOR}% floor" >&2
  exit 1
fi
echo "check-coverage: ${total}% (floor ${FLOOR}%)"
