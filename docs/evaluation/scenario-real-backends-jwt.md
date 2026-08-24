# Real MCP backends + JWT (multibackend benchmark)

Walkthrough for validating the gateway with **stdio MCP servers** (not HTTP mocks), **semantic router**, **JWT**, and **OTLP → Prometheus**. Recorded numbers: [calibration-results.md](calibration-results.md) (multibackend benchmark, 2026-05-30).

For mocks-only validation, use [scenario-sre-multibackend.md](scenario-sre-multibackend.md) or the **SRE mock** scenario in [integration-checklist.md](integration-checklist.md).

---

## Topology

| Silo prefix | Upstream (stdio) | Example tool |
|-------------|------------------|--------------|
| `k8s` | `@modelcontextprotocol/server-everything` | `k8s__echo` |
| `prom` | `@modelcontextprotocol/server-filesystem` | `prom__read_text_file` |
| `gh` | `@modelcontextprotocol/server-memory` | `gh__create_entities` |

Config file: [`deployments/gateway.real.yaml`](../../deployments/gateway.real.yaml).

---

## Prerequisites

```bash
cd mcp-gateway
make docker-up          # Qdrant, embed, OTel collector, Prometheus, Grafana — no make sre-up
```

Generate JWT key pair (once; idempotent):

```bash
make lab-jwt-keys
# or: bash scripts/lab_jwt_keys.sh keys
```

Creates `/tmp/mcp-lab-jwt.key` and `/tmp/mcp-lab-jwt.pub.pem` if missing (no `.env` required).
Issue tokens: `make lab-jwt-env` or `bash scripts/lab_jwt_keys.sh admin`.

Manual equivalent:

```bash
openssl genrsa -out /tmp/mcp-lab-jwt.key 2048
openssl rsa -in /tmp/mcp-lab-jwt.key -pubout -out /tmp/mcp-lab-jwt.pub.pem
```

Filesystem backend root (macOS): create allowed directory and a sample file:

```bash
mkdir -p /private/tmp/mcp-gateway-lab
echo 'fixture-ok' > /private/tmp/mcp-gateway-lab/readme.txt
```

On macOS, `/tmp/...` is **not** the same path as `/private/tmp/...` for the filesystem MCP server. Use `/private/tmp/mcp-gateway-lab/...` in tool arguments and in YAML.

---

## Start gateway (host, JWT, OTLP)

**Prefer `make run`** — it loads `.env` and aligns `EMBED_URL` with `HOST_PORT_EMBED` (e.g. `:18001`), avoiding `/readyz` 503 when embed listens on a non-default port.

```bash
make run
```

Manual equivalent (only if you do not use `.env`):

```bash
export PORT=18080
export MCP_GATEWAY_CONFIG=deployments/gateway.real.yaml
export AUTH_MODE=jwt
export JWT_PUBLIC_KEY_FILE=/tmp/mcp-lab-jwt.pub.pem
export JWT_ISS=https://lab.local
export JWT_AUD=mcp-gateway
export ROUTER_MODE=on
export QDRANT_URL=http://127.0.0.1:6333
export EMBED_URL=http://127.0.0.1:18001   # must match HOST_PORT_EMBED in .env / docker-compose
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318

make run
```

Before recording, run the rehearsal gate:

```bash
make docker-up
make demo-lab-preflight    # deps + fixture + JWT (gateway stopped)
make run                   # other terminal
make demo-lab-verify       # catalog + JWT + LangGraph
```

Verify:

```bash
curl -sf http://127.0.0.1:18080/readyz
curl -sf http://127.0.0.1:6333/healthz
curl -sf http://127.0.0.1:18001/healthz   # or :8001 when HOST_PORT_EMBED=8001
curl -sf http://127.0.0.1:4318/
```

Confirm log line shows `"addr":":18080"`.

---

## Issue JWT for smoke

```bash
eval "$(bash scripts/lab_jwt_keys.sh env)"
# JWT_ADMIN (3 tools), JWT_ADMIN_FULL (mcp_tools ["*"], full catalog), JWT_RESTRICTED (deny demo)
```

Manual equivalent:

```bash
export JWT_ADMIN="$(go run ./tools/gen-jwt \
  -key /tmp/mcp-lab-jwt.key -iss https://lab.local -aud mcp-gateway \
  -sub lab-admin -mcp-tools 'prom__read_text_file,k8s__echo,gh__create_entities')"
export JWT_RESTRICTED="$(go run ./tools/gen-jwt \
  -key /tmp/mcp-lab-jwt.key -iss https://lab.local -aud mcp-gateway \
  -sub lab-restricted -mcp-tools prom__read_text_file)"
```

---

## E2E smoke (three silos)

```bash
GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_ADMIN" \
  SMOKE_EXPECT_TOOL=prom__read_text_file \
  SMOKE_EXPECT_TEXT=fixture-ok \
  SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}' \
  bash scripts/smoke_e2e.sh

GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_ADMIN" \
  SMOKE_EXPECT_TOOL=k8s__echo SMOKE_EXPECT_TEXT=ok \
  bash scripts/smoke_e2e.sh

GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_ADMIN" \
  SMOKE_EXPECT_TOOL=gh__create_entities \
  SMOKE_TOOL_ARGS='{"entities":[{"name":"lab-smoke","entityType":"note","observations":["smoke"]}]}' \
  bash scripts/smoke_e2e.sh
```

Each run should print `SMOKE OK`.

---

## JWT deny

```bash
GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_RESTRICTED" \
  SMOKE_EXPECT_TOOL=prom__list_directory \
  SMOKE_EXPECT_RPC_ERROR=-32003 \
  SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab"}' \
  bash scripts/smoke_e2e.sh
```

Expect JSON-RPC **-32003** (`tool "prom__list_directory" not allowed for this principal`).

---

## Load for Prometheus (JWT)

**LangGraph agent integration run only** — the multibackend benchmark (2026-05-30) uses 60× parallel + 20× sequential smoke (below) and Prometheus **mean** phase latency. The LangGraph agent integration run adds JWT `loadtest` with `-token` ([calibration-results.md](calibration-results.md), [integration-checklist.md](integration-checklist.md#langgraph-agent-integration-run)).

`scripts/loadtest` sends `Authorization: Bearer` when you pass `-token` or set `LOADTEST_JWT`. Use **one worker** under JWT because concurrent `tools/list` fan-out can collide on upstream JSON-RPC ids (see [known limitations](../errors.md#known-limitations-multiplexing)).

```bash
go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode direct -workers 1 -duration 30s \
  -token "$JWT_ADMIN" \
  -tool prom__read_text_file \
  -args '{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}'
```

Alternative (smoke loop, any worker count per process):

```bash
for i in $(seq 1 60); do
  GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_ADMIN" \
    SMOKE_EXPECT_TOOL=prom__read_text_file \
    SMOKE_EXPECT_TEXT=fixture-ok \
    SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}' \
    bash scripts/smoke_e2e.sh &
done
wait
for i in $(seq 1 20); do
  GATEWAY_URL=http://127.0.0.1:18080 SMOKE_JWT="$JWT_ADMIN" \
    SMOKE_EXPECT_TOOL=prom__read_text_file \
    SMOKE_EXPECT_TEXT=fixture-ok \
    SMOKE_TOOL_ARGS='{"path":"/private/tmp/mcp-gateway-lab/readme.txt"}' \
    bash scripts/smoke_e2e.sh
done
```

Then query internal **mean** latency per phase (see [calibration-results.md](calibration-results.md)). Do **not** use histogram p95 when sub-ms samples land in the first 5 s bucket (artefact ~4750 ms).

---

## Router recall regression (optional, same session)

```bash
QDRANT_URL=http://127.0.0.1:6333 EMBED_URL=http://127.0.0.1:8001 \
 go test -tags=integration -race -count=1 \
 ./internal/routertest -run TestRouterEvalVectorRecallMiniLM -v
```

Expected: recall@1 = recall@3 = 1.000 (26/26).

---

## Record results

Copy measured values into [calibration-results.md](calibration-results.md) → **Multibackend benchmark (2026-05-30)** or **LangGraph agent integration run (2026-06-08)**.

---

## Related

- [scenario-jwt-allowlist.md](scenario-jwt-allowlist.md) — JWT mechanics
- [calibration-run.md](calibration-run.md) — calibration (mocks, `AUTH_MODE=none`)
- [integration-checklist.md](integration-checklist.md) — SRE mock (mocks) vs multibackend benchmark (this doc)
- [scripts/loadtest/README.md](../../scripts/loadtest/README.md) — JWT loadtest flags
