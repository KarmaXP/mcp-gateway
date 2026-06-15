#!/usr/bin/env bash
# Idempotent lab JWT key pair for integrated sessions (gateway.real.yaml).
# Keys live under /tmp so they survive without .env; regenerate with: rm /tmp/mcp-lab-jwt.*
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEY="${LAB_JWT_PRIVATE_KEY:-/tmp/mcp-lab-jwt.key}"
PUB="${LAB_JWT_PUBLIC_KEY:-/tmp/mcp-lab-jwt.pub.pem}"
ISS="${JWT_ISS:-https://lab.local}"
AUD="${JWT_AUD:-mcp-gateway}"
ADMIN_TOOLS="${LAB_JWT_ADMIN_TOOLS:-prom__read_text_file,k8s__echo,gh__create_entities}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [keys|admin|restricted|env|verify]

  keys       Ensure RSA key pair at $KEY and $PUB (default).
  admin      Print a signed admin JWT (requires keys).
  restricted Print a restricted JWT (prom__read_text_file only).
  env        Print export lines for JWT_PUBLIC_KEY_FILE and JWT_ADMIN.
  verify     Run crypto check: sign with private key, validate with gateway auth.

Override paths: LAB_JWT_PRIVATE_KEY, LAB_JWT_PUBLIC_KEY.
EOF
}

ensure_keys() {
  if [[ ! -f "$KEY" ]]; then
    openssl genrsa -out "$KEY" 2048
    chmod 600 "$KEY"
    echo "created $KEY"
  fi
  if [[ ! -f "$PUB" ]]; then
    openssl rsa -in "$KEY" -pubout -out "$PUB"
    chmod 644 "$PUB"
    echo "created $PUB"
  fi
}

gen_jwt() {
  local sub=$1 tools=$2
  (cd "$ROOT" && go run ./tools/gen-jwt \
    -key "$KEY" -iss "$ISS" -aud "$AUD" -sub "$sub" -mcp-tools "$tools")
}

cmd="${1:-keys}"
case "$cmd" in
  keys)
    ensure_keys
    echo "Lab JWT keys ready:"
    echo "  private: $KEY"
    echo "  public:  $PUB"
    echo "Gateway: export JWT_PUBLIC_KEY_FILE=$PUB JWT_ISS=$ISS JWT_AUD=$AUD"
    ;;
  admin)
    ensure_keys
    gen_jwt lab-admin "$ADMIN_TOOLS"
    ;;
  restricted)
    ensure_keys
    gen_jwt lab-restricted "prom__read_text_file"
    ;;
  env)
    ensure_keys
    printf 'export JWT_PUBLIC_KEY_FILE=%q\n' "$PUB"
    printf 'export JWT_ISS=%q\n' "$ISS"
    printf 'export JWT_AUD=%q\n' "$AUD"
    printf 'export JWT_ADMIN=%q\n' "$(gen_jwt lab-admin "$ADMIN_TOOLS")"
    ;;
  verify)
    ensure_keys
    LAB_JWT_PRIVATE_KEY="$KEY" LAB_JWT_PUBLIC_KEY="$PUB" \
      go test -count=1 -run TestLabJWTPairOnDisk ./tools/gen-jwt/
    echo "verify OK: lab JWT pair matches gateway validator (iss=$ISS aud=$AUD)"
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    echo "unknown command: $cmd" >&2
    usage >&2
    exit 2
    ;;
esac
