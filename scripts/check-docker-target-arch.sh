#!/usr/bin/env bash
# Fails when the Dockerfile pins a build architecture instead of taking the builder's target,
# which produces an image whose binary does not match its own metadata and rootfs.
set -euo pipefail

cd "$(dirname "$0")/.."

dockerfile="Dockerfile"

if [[ ! -f "${dockerfile}" ]]; then
  echo "check-docker-target-arch: ${dockerfile} not found; the gate is checking nothing." >&2
  exit 1
fi

build_lines="$(grep -nE '^[[:space:]]*RUN .*go build|^[[:space:]]*RUN .*GOARCH=' "${dockerfile}" || true)"
if [[ -z "${build_lines}" ]]; then
  echo "check-docker-target-arch: no 'go build' line found in ${dockerfile}; the gate is checking nothing." >&2
  exit 1
fi

problems=0

while IFS= read -r line; do
  number="${line%%:*}"
  if grep -qE 'GOARCH=(386|amd64|arm|arm64|ppc64le|riscv64|s390x)([[:space:]]|$|\\)' <<<"${line}"; then
    echo "${dockerfile}:${number}: GOARCH is pinned to a literal; use \${TARGETARCH} so the binary matches the image." >&2
    problems=$((problems + 1))
  fi
  if grep -qE 'GOOS=(darwin|linux|windows)([[:space:]]|$|\\)' <<<"${line}"; then
    echo "${dockerfile}:${number}: GOOS is pinned to a literal; use \${TARGETOS} so the binary matches the image." >&2
    problems=$((problems + 1))
  fi
done <<<"${build_lines}"

if ! grep -qE '^[[:space:]]*ARG[[:space:]]+TARGETARCH[[:space:]]*$' "${dockerfile}"; then
  echo "${dockerfile}: no 'ARG TARGETARCH'; the build stage cannot see the target architecture." >&2
  problems=$((problems + 1))
fi

if ! grep -qE '^[[:space:]]*ARG[[:space:]]+TARGETOS[[:space:]]*$' "${dockerfile}"; then
  echo "${dockerfile}: no 'ARG TARGETOS'; the build stage cannot see the target OS." >&2
  problems=$((problems + 1))
fi

if [[ "${problems}" -gt 0 ]]; then
  echo "check-docker-target-arch: ${problems} problem(s); the image would carry a binary for the wrong platform." >&2
  exit 1
fi

echo "check-docker-target-arch: the build stage follows the builder's target platform."
