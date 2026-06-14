# ADR 0003: RAR shape, JWT ∩ RAR merge, and policy fail mode

## Status

Accepted

## Context

The gateway must combine **OAuth 2.0 Rich Authorization Requests (RAR)**-style `authorization_details` with optional JWT claims (`mcp_tools`, `mcp_tool_groups`) while keeping tool naming and authorization deterministic, auditable, and safe under partial or malformed inputs.

## Decision 1: Canonical RAR entry shape for MCP tools

`authorization_details` entries that authorize MCP tools use:

- **`type`:** exactly `"mcp_tool"` (other types are ignored for tool expansion).
- **`tool_name`:** optional string; when set, it is an **exact** namespaced tool id (e.g. `k8s__get_logs`).
- **`tool_pattern`:** optional string; when set, it is a **glob** interpreted with Go `filepath.Match` semantics (e.g. `prom__*`).

**`tool_name` and `tool_pattern` are mutually exclusive** for a given entry: both set yields a parse error.

Implementation: `internal/policy/rar.go` (`expandAuthorizationDetails`, `MatchTool`).

## Decision 2: Merging JWT tool claims with RAR

`internal/policy/engine.go` (`EffectiveAllowList`) builds a JWT-derived list from:

- Claim **`mcp_tools`** (namespaced tool ids), plus
- Claim **`mcp_tool_groups`** expanded through configured **`policy.tool_groups`** in gateway YAML.

RAR contributes the list from **`authorization_details`** as above.

Merge rules:

| JWT list (after group expansion) | RAR list | Effective allow list |
|----------------------------------|----------|----------------------|
| Non-empty            | Non-empty | **Intersection:** only JWT tools that match **any** RAR entry (exact or glob). |
| Empty              | Non-empty | **RAR only.** |
| Non-empty            | Empty   | **JWT only** (tools + expanded groups). |
| Empty              | Empty   | **No restriction** (empty effective list → full merged catalog; same as pre-policy behavior for callers without tool claims). |

Unknown **`mcp_tool_groups`** entries are an error from `EffectiveAllowList` (fail closed unless degradation is enabled; see Decision 3).

### Scope note (A13.2): JWT/RAR allow-list applies to `tools/*` only

The effective allow-list derived from `mcp_tools`, `mcp_tool_groups`, and `authorization_details` is enforced on **`tools/list`** and **`tools/call`** only. It does **not** currently filter or deny **`resources/*`** or **`prompts/*`** gateway methods; those remain pass-through after AuthN in the current scope (see ADR 0004).

## Decision 3: Failure mode for policy evaluation

- **Default:** **Fail closed.** If `authorization_details` cannot be parsed or conflicts with invariants (e.g. mutually exclusive fields), `EffectiveAllowList` returns an error and the JWT middleware rejects the request (**401**), unless **`policy.allow_on_eval_failure`** is set **true** in config.
- **Degraded mode (opt-in):** When **`allow_on_eval_failure`** is **true**, a bad `authorization_details` payload is logged and **ignored**; the session falls back to the **JWT-only** allow list (from `mcp_tools` / `mcp_tool_groups` only). RAR is not applied in that fallback path for that request.

Implementation: `internal/policy/engine.go` (`allowOnEvalFail` / `AllowOnEvalFailure`), `internal/auth/middleware.go` (401 on `EffectiveAllowList` error).

## Decision 4: Audit emission (`AuditSink`) and hot policy reload (`SIGHUP`)

- **Audit:** Allow/deny audit lines are emitted through a pluggable **`policy.AuditSink`** (`Emit(ctx, AuditRecord)`). The default **`SlogAuditSink`** preserves prior behavior: structured **`slog`** (with `mcp_security_audit`, hashed subject prefix, no tokens/args) plus **`telemetry.RecordPolicyDecision`**. **`policy.SetAuditSink`** swaps the sink process-wide (tests, future Kafka/syslog). **`LogAudit`** only builds an **`AuditRecord`** and delegates to the active sink.
- **Reload:** The active **`policy.Engine`** is held in a **`policy.Holder`** (`Load` / `Store` with **`atomic.Pointer`**). **`SIGHUP`** triggers **`config.Load()`** and **`holder.Store(policy.NewEngine(cfg.Policy))`**. JWT middleware and multiplex read the engine via **`holder.Load()`** on each use, so **new `POST /mcp/rpc`** requests see the updated policy as soon as the swap completes. An existing **SSE** connection does not by itself block policy updates: every RPC still passes through HTTP middleware. Clients that **cache** `tools/list` or tool metadata **in-process** may observe stale catalog or assumptions until they refresh or reconnect.

## Decision 5 (A7.4 addendum): JWKS unavailability remains fail-closed

- JWKS fetch/lookup/verification failures continue to return **401** in JWT mode (fail closed).
- The gateway does **not** provide `auth.allow_jwks_unavailable` / `AUTH_ALLOW_JWKS_UNAVAILABLE` bypass semantics.
- The only explicit insecure bypass for local development remains `AUTH_MODE=none`, which disables JWT validation by configuration rather than silently degrading an otherwise JWT-protected deployment.

## Consequences

- Operators can cite this ADR for security review: RAR shape, merge semantics, and fail-closed default are explicit and implemented in `internal/policy` + auth middleware.
- Dashboards should rely on **low-cardinality** policy metrics (`outcome`, `reason` enums) rather than tool names or subjects in metric labels (see `internal/telemetry` and `internal/defaults/metrics.go`).

## References

- `docs/architecture/mcp_gateway.plan.md`, security layer and policy
- `internal/policy/`, engine, RAR expansion, `AuditSink`, `Holder`, audit logging
- `internal/auth/middleware.go`, JWT + effective allow list per request
- `internal/auth/claims.go`, `mcp_tools`, `mcp_tool_groups`, `authorization_details` input
