# Router trace capture (OTLP/Tempo)

Optional procedure for **new** calibration runs when you want Tempo trace figures alongside Prometheus numbers. Canonical lab sessions are in [calibration-results.md](calibration-results.md): integrated lab run (2026-05-30) marks Tempo host decomposition **Not measured**; full lab session (2026-06-08) records one representative trace as **Measured** via Grafana datasource proxy.

## 1) Spans and attributes to capture

### Stable span names (`internal/telemetry/spans.go`)

- `mcp.host.request` (root request span)
- `mcp.router.semantic` (semantic routing decision span)
- `mcp.backend.call` (selected backend invocation after routing)

### Stable OTEL attribute keys (`internal/telemetry/attrs.go`)

- `mcp.method` (set on host/router/backend spans)
- `mcp.session.id` (root host span)
- `mcp.jsonrpc.id` (root host span, when request has id)
- `mcp.tool.name` (set after routing on authz/schema/backend spans)
- `mcp.backend.id` (backend call span)
- `mcp.agent.tokens_used` (root host span when header present)

### Router decision fields (`internal/router/types.go`)

`RoutingDecision` is the canonical decision payload:

- `Outcome`
- `FallbackLayer`
- `ToolNameNamespaced` (selected tool)
- `Confidence` (winner score)
- `Candidates[]` (`Name`, `Score`, `Source`) = top-K list
- `LatencyMS`

These are field names (not `internal/telemetry` constants). Use the same casing in figure labels/captions.

## 2) Tempo query example

In Grafana Tempo Explore (TraceQL), query semantic router spans for `tools/call`:

```traceql
{ name = "mcp.router.semantic" && span.mcp.method = "tools/call" }
```

Optional narrowing for one host session:

```traceql
{ name = "mcp.host.request" && span.mcp.method = "tools/call" && span.mcp.session.id = "<session-id>" }
```

## 3) Capture procedure (re-runs)

1. Run the calibration workload from [calibration-run.md](calibration-run.md) (semantic mode, not direct mode).
2. Open a matching trace in Tempo using the query above.
3. In the selected trace, include at least:
   - `mcp.host.request`
   - `mcp.router.semantic`
   - `mcp.backend.call`
4. Record router decision details for the figure:
   - selected tool (`ToolNameNamespaced` and/or `mcp.tool.name`)
   - top score (`Confidence`)
   - top-K candidates (`Candidates[]` names + scores)
   - routing outcome/layer (`Outcome`, `FallbackLayer`)
5. If your Tempo span view does not expose all decision fields, correlate by `trace_id` with gateway structured logs (`router decision`) for top-K and scores; keep the same trace id in the figure notes.
6. Optional: capture a Grafana screenshot during the run window and store it with the same run id/timestamp as your calibration notes.

## 4) Primary latency evidence

For evaluation review, prefer Prometheus **mean** internal phase latency from [calibration-results.md](calibration-results.md) when sub-ms samples make histogram p95 unreliable. Tempo traces supplement qualitative decomposition when captured; they are not required when marked **Not measured** with reason.
