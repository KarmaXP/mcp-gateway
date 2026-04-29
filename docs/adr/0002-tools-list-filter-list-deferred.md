# ADR 0002: Defer `filter_list` tools/list mode (Tier 2)

## Status

Accepted

## Context

The architecture plan describes two router-adjacent `tools/list` behaviors:

- **`assist_list` (implemented):** the host receives the full aggregated catalog; semantic routing applies on `tools/call` only.
- **`filter_list` (Tier 2):** `tools/list` would return a subset of tools filtered by session intent (similarity, policy, or session state).

Tier 1 hardening prioritizes **multiplexer correctness**, **MCP spec parity** (e.g. `ping`, transport), **request-scoped intent** (`X-MCP-Intent` via `hostctx`) for `tools/call`, and **observable router layers**. `filter_list` requires additional contracts: stable session intent before list, catalog consistency when the filtered view diverges from the vector index, and host UX when the list changes mid-session.

## Decision

**Defer `filter_list` to Phase 3 / future work.** The gateway continues to ship **`assist_list`** as the supported mode for Phase 1–2 snapshots; `filter_list` remains a documented roadmap item in the main plan, not a blocking implementation for the TFM final snapshot.

## Consequences

- Operators and thesis reviewers should assume **full `tools/list`** unless a later phase adds `filter_list` behind an explicit config flag.
- Session intent (`X-MCP-Intent` and related mechanisms) is still valuable for **`tools/call`** semantic routing without implying a filtered list API.

## References

- `docs/architecture/mcp_gateway.plan.md` — `tools/list` modes and `IntentText`
- `internal/gateway/multiplex` — merged `tools/list`
- `internal/router` — semantic routing on `tools/call`
