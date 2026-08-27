#!/usr/bin/env bash
# Fails when the dependency table in docs/DEVELOPER.md disagrees with go.mod.
set -euo pipefail

cd "$(dirname "$0")/.."

table="docs/DEVELOPER.md"

stale=0
rows=0
while IFS='|' read -r _ name _ version _; do
  module="$(echo "${name}" | grep -oE '`[^`]+`' | head -1 | tr -d '`')"
  claimed="$(echo "${version}" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[^ `)]*' | head -1)"
  [[ -z "${module}" || -z "${claimed}" ]] && continue
  rows=$((rows + 1))
  actual="$(grep -oE "(^|[[:space:]])${module//./\\.} v[0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*" go.mod | awk '{print $NF}' | head -1 || true)"
  if [[ -z "${actual}" ]]; then
    echo "${table}: ${module} is in the table but not in go.mod" >&2
    stale=$((stale + 1))
    continue
  fi
  if [[ "${claimed}" != "${actual}" ]]; then
    echo "${table}: ${module} says ${claimed}, go.mod has ${actual}" >&2
    stale=$((stale + 1))
  fi
done < <(grep -E '^\| `[a-z0-9./-]+`.*\| `v[0-9]' "${table}")

if [[ "${rows}" -eq 0 ]]; then
  echo "check-doc-versions: parsed no version rows from ${table}; the gate is checking nothing." >&2
  exit 1
fi
if [[ "${stale}" -gt 0 ]]; then
  echo "check-doc-versions: ${stale} stale version(s) in the dependency table." >&2
  exit 1
fi
echo "check-doc-versions: the dependency table matches go.mod."
