#!/usr/bin/env bash
# Fails when a version the documentation states disagrees with the source it came from:
# the dependency table in docs/DEVELOPER.md against go.mod, and the README's MCP badge
# against mcpwire.MCPProtocolVersion.
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

readme="README.md"
source_version="$(grep -oE 'MCPProtocolVersion = "[^"]+"' internal/gateway/mcpwire/protocol.go \
  | head -1 | sed -E 's|.*"(.*)"|\1|')"
badge_version="$( { grep -oE 'img\.shields\.io/badge/MCP-[0-9]{4}(--[0-9]{2}){2}' "${readme}" || true; } \
  | head -1 | sed -E 's|.*/MCP-||; s|--|-|g')"

if [[ -z "${source_version}" ]]; then
  echo "check-doc-versions: MCPProtocolVersion not found in mcpwire; the badge check is checking nothing." >&2
  exit 1
fi
if [[ -z "${badge_version}" ]]; then
  echo "check-doc-versions: no MCP protocol badge found in ${readme}; the badge check is checking nothing." >&2
  exit 1
fi
if [[ "${badge_version}" != "${source_version}" ]]; then
  echo "check-doc-versions: ${readme} shows MCP ${badge_version}, mcpwire speaks ${source_version}" >&2
  exit 1
fi

echo "check-doc-versions: the dependency table matches go.mod, and the MCP badge says ${source_version}."
