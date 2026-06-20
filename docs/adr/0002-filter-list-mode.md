# ADR 0002: `filter_list` mode semantics

## Status

Accepted, **`filter_list` is implemented** behind `router.mode: filter_list` / `ROUTER_MODE=filter_list` (see `docs/DEVELOPER.md`, `internal/router/semantic_router_filter_list.go`, `internal/gateway/multiplex/tools_list.go`).

## Context

The architecture plan describes two router-adjacent `tools/list` behaviors:

- **`assist_list`:** the host receives the full aggregated catalog; semantic routing applies on `tools/call` (and optionally vector-assisted list filtering is off).
- **`filter_list`:** `tools/list` returns a subset of the merged catalog filtered by similarity to request-scoped intent (`X-MCP-Intent` via `hostctx`), subject to JWT/RAR allow-lists and vector index filters (see architecture plan, semantic router).

## Decision

The gateway supports `filter_list` with explicit semantics:

- **Empty `X-MCP-Intent`:** `tools/list` returns the full merged catalog after allow-list policy only (same as `assist_list` for that request). No last-known intent from prior RPCs.
- **Degraded vector/embed/catalog mismatch:** full list is returned and a warning is logged (hosts keep a usable catalog).

## Consequences

- Operators enable intent-filtered lists only when `ROUTER_MODE=filter_list` (requires Qdrant + embed like other semantic modes).
- Session intent remains request-scoped per `POST /mcp/rpc` header, consistent with `tools/call` routing.

## References

- `docs/architecture/mcp_gateway.plan.md`, `tools/list` modes, internal latency histogram
- `docs/evaluation/calibration-run.md`, live embedding + Qdrant calibration template
- `internal/gateway/multiplex`, merged `tools/list`
- `internal/router`, semantic routing and `FilterToolsForList`
