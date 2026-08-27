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

# The README states a number a reader will believe, so it may understate but never overstate.
README="${COVERAGE_README:-README.md}"
claimed="$( { grep -oE 'img\.shields\.io/badge/coverage-[0-9]+(\.[0-9]+)?%25' "${README}" || true; } \
  | head -1 | sed -E 's|.*/coverage-||; s|%25$||')"

if [[ -z "${claimed}" ]]; then
  echo "check-coverage: no coverage badge found in ${README}; the badge check is checking nothing." >&2
  exit 1
fi

if awk -v c="${claimed}" -v t="${total}" 'BEGIN { exit !(c > t) }'; then
  echo "check-coverage: ${README} claims ${claimed}% but coverage is ${total}%" >&2
  exit 1
fi

echo "check-coverage: ${total}% (floor ${FLOOR}%, README claims ${claimed}%)"
