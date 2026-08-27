#!/usr/bin/env bash
# Fails when a script prints an RPC example a reader cannot run. An example is an echoed curl
# against /mcp/rpc; to work it needs both handshake steps beside it, and the reader has to be
# told the reply arrives on the SSE stream rather than in the POST response.
set -euo pipefail

cd "$(dirname "$0")/.."

with_examples=()
for file in scripts/*.sh; do
  printed="$(grep -E '^[[:space:]]*echo ' "${file}" || true)"
  if grep -q 'curl' <<<"${printed}" && grep -q 'mcp/rpc' <<<"${printed}"; then
    with_examples+=("${file}")
  fi
done

if [[ "${#with_examples[@]}" -eq 0 ]]; then
  echo "check-demo-example: no printed /mcp/rpc example found; the gate is checking nothing." >&2
  exit 1
fi

problems=0

for file in "${with_examples[@]}"; do
  printed="$(grep -E '^[[:space:]]*echo ' "${file}")"

  if ! grep -q 'method.*initialize' <<<"${printed}"; then
    echo "${file}: prints an RPC example with no initialize step; running it answers -32001." >&2
    problems=$((problems + 1))
  fi

  if ! grep -q 'notifications/initialized' <<<"${printed}"; then
    echo "${file}: prints an RPC example with no notifications/initialized; running it answers -32001." >&2
    problems=$((problems + 1))
  fi

  if ! grep -qiE 'sse stream|on the stream' <<<"${printed}"; then
    echo "${file}: prints an RPC example without saying the reply arrives on the SSE stream." >&2
    problems=$((problems + 1))
  fi
done

if [[ "${problems}" -gt 0 ]]; then
  echo "check-demo-example: ${problems} problem(s); a reader copying the example would see nothing work." >&2
  exit 1
fi

echo "check-demo-example: every printed RPC example carries its handshake."
