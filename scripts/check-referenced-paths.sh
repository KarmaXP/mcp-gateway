#!/usr/bin/env bash
# Fails when a tracked text file names a repository path that does not exist.
#   Covers paths under the source directories and the top-level project files.
set -euo pipefail

cd "$(dirname "$0")/.."

ignore_file="scripts/referenced-paths-ignore.txt"

is_ignored() {
  [[ -f "${ignore_file}" ]] || return 1
  grep -qxF "$1" <(grep -vE '^#|^$' "${ignore_file}")
}

missing=0
while IFS= read -r file; do
  case "${file}" in
    *.png|*.jpg|*.jpeg|*.gif|*.svg|*.ico|go.sum) continue ;;
  esac
  while IFS= read -r ref; do
    [[ -e "${ref}" ]] && continue
    is_ignored "${ref}" && continue
    echo "${file}: names ${ref}, which does not exist" >&2
    missing=$((missing + 1))
  done < <({
      grep -ohE '\b(docs|scripts|deployments|internal|cmd|tools)/[A-Za-z0-9._/-]+\.(md|sh|go|ya?ml|json)\b' "${file}" || true
      grep -ohE '\b(Makefile|Dockerfile|README\.md|CHANGELOG\.md|CONTRIBUTING\.md|SECURITY\.md|LICENSE|go\.mod|\.env\.example|\.golangci\.yml)\b' "${file}" || true
    } 2>/dev/null | sort -u)
done < <(git ls-files)

# Relative markdown links, which the repository-path patterns above cannot see.
while IFS= read -r file; do
  case "${file}" in *.md) ;; *) continue ;; esac
  dir="$(dirname "${file}")"
  while IFS= read -r ref; do
    [[ -e "${dir}/${ref}" ]] && continue
    is_ignored "${ref}" && continue
    echo "${file}: links ${ref}, which does not exist" >&2
    missing=$((missing + 1))
  done < <(grep -ohE '\]\(([A-Za-z0-9._/-]+\.md)(#[^)]*)?\)' "${file}" 2>/dev/null \
             | sed -E 's/^\]\(//; s/(#[^)]*)?\)$//' | grep -v '^http' | sort -u)
done < <(git ls-files '*.md')

if [[ "${missing}" -gt 0 ]]; then
  echo "check-referenced-paths: ${missing} dangling path reference(s)." >&2
  exit 1
fi
echo "check-referenced-paths: every referenced repository path exists."
