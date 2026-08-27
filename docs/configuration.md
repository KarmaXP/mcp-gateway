# Configuration reference

The gateway loads **`MCP_GATEWAY_CONFIG`** (YAML) and optional **`MCP_GATEWAY_BACKENDS`** (JSON). Environment variables override or supplement YAML after load.

Copy [`.env.example`](../.env.example) with `make bootstrap`. Example YAML files live under [`deployments/`](../deployments/).

---

## Example configs

| File | Backends | Router | Use when |
|------|----------|--------|----------|
| [`gateway.demo.yaml`](../deployments/gateway.demo.yaml) | 1× smoke mock `:31400` | `off` | First run (`make demo`, default `make run`) |
| [`gateway.example.yaml`](../deployments/gateway.example.yaml) | alpha `:3101`, beta `:3102` | `off` (tunable) | Multi-backend lab (`make demo-upstreams`, `make demo-full`) |
| [`gateway.sre.example.yaml`](../deployments/gateway.sre.example.yaml) | k8s/prom/gh mocks `:3201–3203` | `on` | SRE walkthrough (`make sre-up`, `make sre-smoke`) |
| [`gateway.real.yaml`](../deployments/gateway.real.yaml) | stdio MCP (`npx` everything, filesystem, memory) | `on` | Real backends + JWT lab ([scenario-real-upstreams-jwt.md](evaluation/scenario-real-upstreams-jwt.md)) |

Ports: [local-ports.md](local-ports.md).

---

## Core

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_GATEWAY_CONFIG` | `gateway.demo.yaml` (via `make run`) | Path to main YAML |
| `MCP_GATEWAY_BACKENDS` | n/a | Optional JSON array merged after YAML |
| `PORT` | `8080` | HTTP listen port |
| `GATEWAY_PORT` | `8080` | Fallback listen port if `PORT` is unset; `make stop` also reads both from `.env` |

---

## Semantic router

Requires **`QDRANT_URL`** when `router.mode` / `ROUTER_MODE` is `on`, `assist_list`, or `filter_list`.

| Variable / YAML `router:` | Default | Description |
|-----------------------------|---------|-------------|
| `ROUTER_MODE` / `mode` | `off` | `off` \| `on` \| `assist_list` \| `filter_list` |
| `QDRANT_URL` | n/a | Qdrant HTTP API (e.g. `http://127.0.0.1:6333`) |
| `QDRANT_COLLECTION` / `qdrant.collection` | `mcp_tool_catalog` | Vector collection name |
| `EMBED_URL` / `embed.url` | `http://127.0.0.1:8001` | Embedding sidecar `POST /embed` |
| `ROUTER_TOP_K` / `top_k` | `8` | Vector search top-K |
| `ROUTER_SCORE_MIN` / `score_min` | `0.35` | Minimum similarity threshold |
| `ROUTER_HYBRID_ALPHA` / `hybrid_alpha` | `0.2` | BM25 rerank weight in hybrid score |
| `ROUTER_ALLOW_AUTO_RENAME` / `allow_auto_rename` | `false` | When `true`, router may change the tool name on `tools/call` before upstream; JWT/RAR AuthZ runs on the **resolved** name. When `false`, a call for a disallowed name returns **-32003** before router rename errors (**-32004**). |
| `ROUTER_VECTOR_DIM` / `vector_dim` | `384` | Embedding dimension (must match model) |
| `embed_timeout`, `query_timeout` | see defaults package | Per-phase timeouts in YAML |

**Modes:**

| Mode | `tools/list` | `tools/call` |
|------|--------------|--------------|
| `off` | Full merged catalog | Exact namespaced names only |
| `on` / `assist_list` | Full catalog | Router may resolve ambiguous names |
| `filter_list` | Subset when `X-MCP-Intent` is set | Same as `on` |

Exact tool names skip vector search. See [Connecting agents — Semantic routing](CONNECTING_AGENTS.md#semantic-routing).

**`filter_list` degradation:** if the catalog is stale, embed/query fails, or no tool clears `score_min`, the router returns the full merged catalog in memory; the gateway still applies JWT/RAR filtering on the response (`tools/list` never bypasses AuthZ).

**SSE catalog notifications:** When `aggregation.forward_tools_list_changed` / `AGGREGATION_FORWARD_TOOLS_LIST_CHANGED` is enabled, upstream `notifications/tools/list_changed` (and resource/prompt variants) are delivered on a **best-effort** broadcast queue (bounded workers + queue). Under heavy load some sessions may miss a hint; hosts should refresh via `tools/list` when needed. Metrics: `mcp.gateway.session.broadcast_tasks_dropped`, `mcp.gateway.session.notifications_dropped`.

---

## Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `AUTH_MODE` | `none` | `none` or `jwt` |
| `JWT_PUBLIC_KEY_PEM` | n/a | RS256 public key (PEM) for static verification |
| `JWT_PUBLIC_KEY_FILE` | n/a | Path to PEM file (preferred in CI/scripts; overrides inline PEM when set) |
| `JWT_JWKS_URL` | n/a | JWKS endpoint (requires `kid` on tokens). Must be `https`, except on a loopback host |
| `JWT_JWKS_CACHE_TTL` | `5m` | JWKS cache duration. A value of zero or less falls back to the default rather than refetching per request |
| `JWT_ISS` | n/a | Expected `iss`. Required when `AUTH_MODE=jwt` |
| `JWT_AUD` | n/a | Expected `aud`. Required when `AUTH_MODE=jwt` |

Signing keys must be RSA with a modulus of at least 2048 bits, whether they come from a PEM or from JWKS. `exp`, `nbf` and `iat` are checked with 60 seconds of leeway, so normal IdP clock drift does not produce a 401.

JWT claims used by the gateway:

- **`mcp_tools`**, array of allowed namespaced tool ids, or `["*"]` for the whole catalog
- **`authorization_details`**, RAR-style entries (see [ADR 0003](adr/0003-security-rar-jwt-merge-failmode.md))
- **`mcp_tool_groups`**, expanded via YAML `policy.tool_groups`

A token carrying none of the three grants **no tools at all** (ADR 0003 Decision 6). Each group key in the JWT must exist in `policy.tool_groups`. **Unknown group names fail policy evaluation** (fail-closed → **401** on `tools/list` and `tools/call`). This matches [ADR 0003](adr/0003-security-rar-jwt-merge-failmode.md). Opt-in **`allow_on_eval_failure`** applies to malformed **`authorization_details`** (RAR) only, not to unknown group keys, and it degrades the principal to **deny-all**, never to their wider JWT list.

JWKS or signature failure → **401** (fail-closed). There is no bypass when JWKS is down.

---

## Policy block (`policy:` in YAML)

| Field / env | Description |
|-------------|-------------|
| `version` / `POLICY_VERSION` | Audit/policy version string |
| `elevated_tools` | Tools that require compiled JSON Schema (elevated-tools policy) |
| `tool_groups` | Named groups for JWT `mcp_tool_groups` |
| `allow_on_eval_failure` / `POLICY_ALLOW_ON_EVAL_FAILURE` | If `true`, a malformed RAR degrades the principal to deny-all instead of returning 401. Covers RAR parsing only |
| `harden_schemas` / `POLICY_HARDEN_SCHEMAS` | **Default true.** Completes every tool schema that enumerates properties with `additionalProperties: false`; a schema that enumerates none is left open. Set false to accept undeclared arguments |
| `max_argument_*` | Size/depth/key limits before schema validation |
| `audit_sink` / `POLICY_AUDIT_*` | `slog` (default) or `syslog` |
| _(env only)_ `POLICY_AUDIT_SUBJECT_PEPPER` | Secret that keys the subject pseudonym in audit records. Keep it stable for a deployment, or pseudonyms stop correlating across restarts. Absent means unkeyed |

---

## Aggregation block (`aggregation:` in YAML)

| Field / env | Description |
|-------------|-------------|
| `strict_initialize` / `AGGREGATION_STRICT_INITIALIZE` | Fail if any upstream `initialize` fails |
| `strict_list` / `AGGREGATION_STRICT_LIST` | Fail if any upstream list RPC fails |
| `report_partial_failures` / `AGGREGATION_REPORT_PARTIAL_FAILURES` | Include partial failure metadata in merge |
| `forward_tools_list_changed` / `AGGREGATION_FORWARD_TOOLS_LIST_CHANGED` | Handle upstream tools catalog change notifications |
| `max_in_flight` / `AGGREGATION_MAX_IN_FLIGHT` | Global cap on concurrent upstream RPCs (0 = disabled) |
| `init_timeout`, `list_timeout`, `call_timeout` | Per-operation timeouts |
| `list_cache_ttl` | TTL for merged tools list cache |

---

## Rate limiting (`rate_limit:` / env)

| Field / env | Default | Description |
|-------------|---------|-------------|
| `enabled` / `RATE_LIMIT_ENABLED` | `false` | Enable token bucket on MCP routes |
| `rps` / `RATE_LIMIT_RPS` | `100` | Sustained rate per subject |
| `burst` / `RATE_LIMIT_BURST` | `200` | Burst size |

Health paths are excluded. Subject = JWT `sub` when present, else client IP.

**Always on, independent of `enabled`:** a per-IP budget of 10 failed authentications, refilling at 1 per second, bounds the signature verification and JWKS traffic one client can force. Only a failure spends it, so a valid token never touches it; exceeding it returns **429** instead of 401. Live SSE sessions are capped at 1024, and a `GET /mcp/sse` beyond that returns **503**.

---

## Gateway ingress (`gateway:` in YAML)

| Field / env | Description |
|-------------|-------------|
| `allowed_origins` / `GATEWAY_ALLOWED_ORIGINS` | Comma-separated Origin allow-list for SSE and POST (empty = no check) |

---

## Backends (`backends:`)

Each entry:

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Stable id (logs, traces) |
| `prefix` | yes | Namespace prefix for tools |
| `url` | one of `url` / `command` | HTTP+SSE MCP upstream |
| `command` | one of `url` / `command` | Stdio MCP (argv array) |
| `env` | no | Extra env vars for stdio child |
| `max_concurrency` | no | Per-upstream concurrency cap |
| `auth_token` / `auth_token_env` | no | Bearer token to upstream |

Guide: [ADDING_UPSTREAMS.md](ADDING_UPSTREAMS.md).

---

## OpenTelemetry

| Variable | Description |
|----------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint (e.g. `http://127.0.0.1:4318`); unset = no export |
| `OTEL_SERVICE_NAME` | Service name resource attribute |

---

## Reload vs restart

| Change | Action |
|--------|--------|
| `policy:` (most fields) | **`SIGHUP`** reloads policy engine |
| `backends:`, `router:`, `aggregation:`, rate limit, origins, audit sink | **Restart** process |
| Environment variables | **Restart** |

---

## Related docs

- [errors.md](errors.md)
- [mcp-capabilities.md](mcp-capabilities.md)
- [deployment.md](deployment.md)
- [DEVELOPER.md](DEVELOPER.md): observability and CI
