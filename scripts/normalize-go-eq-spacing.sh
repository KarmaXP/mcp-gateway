#!/usr/bin/env bash
# Collapse column-aligned spaces before "=" on tab-indented lines (const/var blocks).
# gofmt re-introduces alignment; run this after gofmt — see `make fmt`.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
find . -name '*.go' \
	-not -path './vendor/*' \
	-exec perl -i -pe 's/^(\t\S.*?)\s{2,}= /$1 = /' {} +
