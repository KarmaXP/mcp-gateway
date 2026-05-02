#!/usr/bin/env bash
# Fail if any Go source still has two or more spaces before "=" on a tab-indented line.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
bad=0
while IFS= read -r -d '' f; do
	if ! perl -0777 -ne 'exit(/^\t\S.*\s{2,}= /m ? 1 : 0)' "$f"; then
		echo "column-aligned '=' before value (run: make fmt in mcp-gateway): $f" >&2
		bad=1
	fi
done < <(find . -name '*.go' -not -path './vendor/*' -print0)
exit "$bad"
