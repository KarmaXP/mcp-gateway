# Security policy

## Supported versions

No tagged releases yet: **`main` is the only supported version.** Fixes land there, no backports.

## Reporting a vulnerability

**Do not open a public issue or pull request** — both are visible before a fix exists.

Use [**Security → Report a vulnerability**](https://github.com/KarmaXP/mcp-gateway/security/advisories/new),
which opens a private advisory visible only to the owners in
[`.github/CODEOWNERS`](.github/CODEOWNERS).

Most useful, in order: what an attacker gains and what access they need to start · a minimal
reproduction (usually a config plus a request sequence, since the surface is JSON-RPC over HTTP) ·
the commit you tested and whether `AUTH_MODE` was `jwt` or `none`.

Expect an acknowledgement within a few days. Small project, no paid security team.

## Keys in this repository

There are none, and there should never be. The two scripts that need an RSA pair generate it:

- `scripts/smoke_jwt.sh` generates a throwaway pair per run into `mktemp` files and deletes both on
  exit.
- `scripts/lab_jwt_keys.sh` is idempotent by design, so a lab session survives a shell restart: it
  creates `/tmp/mcp-lab-jwt.key` (mode 600) and `.pub.pem` once and reuses them. Nothing removes
  them — `rm /tmp/mcp-lab-jwt.*` to rotate. Override with `LAB_JWT_PRIVATE_KEY` /
  `LAB_JWT_PUBLIC_KEY`. These keys are for the local lab only; never point a deployment at one.

A private key in git is a private key everyone has, including after you delete it — history keeps
it. `scripts/smoke_jwt.sh` carried a hardcoded key until it was replaced with per-run generation;
that key is reachable in history and must be treated as public. It is not used anywhere.

## In scope

JWT and JWKS handling · the tool allow-list and RAR merge · JSON Schema argument validation · rate
limiting · resource exhaustion through any request path · secrets leaking into logs, spans or
container images.

## Not in scope

- **`upstreams[].command` is executed by design.** Whoever writes the gateway config already runs
  code as the gateway process; config is inside the trust boundary. Influencing that config *from
  outside* the boundary is very much in scope.
- **`AUTH_MODE=none` disables authentication.** It is a documented local-development mode.
  Exposing it on a reachable network is a deployment mistake, not a defect.
