#!/usr/bin/env bash
# Zero added comments. The only comment this gate accepts is a doc comment on an exported
# symbol, which Go and this repository both require. Everything else belongs in the PR body,
# an ADR, or a better name.
#
#   bash scripts/check-added-comments.sh              # against origin/main
#   COMMENT_GATE_BASE=HEAD~1 bash scripts/...         # against something else
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE="${COMMENT_GATE_BASE:-origin/main}"
if ! git rev-parse --verify --quiet "${BASE}^{commit}" >/dev/null; then
  echo "check-added-comments: ${BASE} not available, nothing to compare (fetch it to enable this gate)" >&2
  exit 0
fi

added_comment_lines() {
  git diff --unified=0 "${BASE}" -- '*.go' | awk '
    /^\+\+\+ b\// { file = substr($0, 7); next }
    /^@@/ {
      split($3, hunk, ",")
      line = substr(hunk[1], 2) + 0
      next
    }
    /^\+\+\+/ { next }
    /^\+/ {
      if ($0 ~ /^\+[[:space:]]*\/\//) print file ":" line
      line++
    }
  '
}

# The declaration a doc comment would document, skipping the rest of the comment block.
declaration_after() {
  awk -v start="$2" 'NR >= start && $0 !~ /^[[:space:]]*\/\// { print; exit }' "$1"
}

violations=0
while IFS=: read -r file line; do
  [[ -n "${file}" && -f "${file}" ]] || continue
  text="$(sed -n "${line}p" "${file}")"
  case "${text}" in
    *//go:*|*//nolint:*) continue ;;
  esac
  next="$(declaration_after "${file}" "${line}")"
  case "${next}" in
    "package "*) continue ;;
    "func "[A-Z]*|"type "[A-Z]*|"const "[A-Z]*|"var "[A-Z]*) continue ;;
    "func ("*") "[A-Z]*) continue ;;
    [[:space:]]*[A-Z]*" = "*|[[:space:]]*[A-Z]*" "[A-Za-z]*" = "*) continue ;;
  esac
  echo "${file}:${line}: added comment on an unexported symbol. Put it in the PR body, or rename until it is redundant." >&2
  violations=$((violations + 1))
done < <(added_comment_lines)

if [[ "${violations}" -gt 0 ]]; then
  echo "check-added-comments: ${violations} comment(s) added that this repository does not want." >&2
  exit 1
fi
echo "check-added-comments: no comments added outside exported doc comments."
