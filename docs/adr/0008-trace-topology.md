# ADR 0008: `mcp.host.request` is a child of the HTTP server span, and there is exactly one

## Status

Accepted. Supersedes the "root span" half of the project's own O1 rule, which was written before
`otelhttp` was wired in.

## Context

The project rule O1 said two things: that every JSON-RPC request produces one `mcp.host.request`
span, and that this span is the root. The first is enforced. The second stopped being true when
`otelhttp.NewHandler` became the outermost middleware, because it opens an HTTP server span and
everything below it is a child.

Restoring the rule would mean removing that span or detaching `mcp.host.request` from it. Both
were considered, and the second is not available: **a root span has no parent by definition**, so
forcing one would discard the `traceparent` an MCP host sends and cut the caller's trace at the
gateway. Trace propagation is already a closed decision in `docs/DEVELOPER.md`, so the rule as
written contradicted it.

## Decision

**The HTTP server span is the entry point.** `otelhttp.NewHandler` names it `METHOD /path`, carries
the OTel HTTP semantic conventions, and continues the caller's trace when one arrives.

**`mcp.host.request` is its child**, and there is exactly one per JSON-RPC request. It is opened by
the JWT middleware rather than by the RPC handler, so that a request rejected before the handler
runs — a bad token, a spent rate-limit bucket — still produces one domain span carrying the error
instead of none. The handler recognises an already-open span through a context marker and reuses it
rather than starting a second.

Everything the gateway does hangs below `mcp.host.request`: `mcp.security.authn` and
`mcp.security.authz`, `mcp.router.semantic`, `mcp.validate.json_schema`, the `mcp.multiplex.*`
family, and one `mcp.backend.call` per upstream reached.

## Consequences

- A trace read in any APM starts at the transport and nests the domain work inside, which is the
  shape an operator expects and the shape endpoint-latency panels are built from.
- Two spans per request instead of one. That is the price of the HTTP semantic conventions, paid
  once per request and not per upstream.
- The "one span" half of the rule is load-bearing and easy to break: any new middleware that opens
  `mcp.host.request` without checking the marker produces two, and the second would silently
  become the one the handler ends. Middleware order is fixed in
  [internal/gateway/orchestrator](../../internal/gateway/orchestrator/orchestrator.go) for that reason.
- `mcp.backend.call` keeps its `backend` spelling. See [ADR 0006](0006-one-term-upstream.md).

## References

- [internal/gateway/orchestrator/orchestrator.go](../../internal/gateway/orchestrator/orchestrator.go), middleware order
- [internal/auth/middleware.go](../../internal/auth/middleware.go), where the span opens
- [internal/gateway/httpserver/server.go](../../internal/gateway/httpserver/server.go), where it is reused rather than reopened
- [internal/telemetry/spans.go](../../internal/telemetry/spans.go), the span names
