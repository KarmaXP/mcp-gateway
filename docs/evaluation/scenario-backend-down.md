# Backend unavailable walkthrough

Reproducible evaluation scenario for **one backend down** during `tools/list` and `tools/call`, with `aggregation.report_partial_failures: true`.

This validates:
- Host-visible partial failure metadata on aggregated list methods.
- Host-visible error behavior on direct `tools/call` to the down backend.
- Trace and metric signals for incident correlation in Tempo/Prometheus.

## References in tests

- `internal/gateway/multiplex/multiplex_partial_failures_test.go`
  - `TestToolsListReportsPartialFailuresWhenEnabled`
  - `TestToolsListReportsTimeoutPartialFailure`
- Related transport failure behavior for `tools/call`:
  - `internal/gateway/multiplex/multiplex_edge_test.go`
  - `TestToolsCallBackendTransportError`

## Preconditions

- Gateway running with at least 2 configured backends (example: `alpha`, `beta`).
- `aggregation.report_partial_failures: true` (or `AGGREGATION_REPORT_PARTIAL_FAILURES=true`).
- `aggregation.strict_list: false` (default fail-open behavior for this scenario).
- Tempo and Prometheus receiving gateway telemetry.
- Known tool on each backend (example namespaced tools `alpha__echo`, `beta__ping`).

## Fault injection

Bring one backend down (example: `beta`) before calling `tools/list` and `tools/call`:
- Stop the backend process/container, or
- Block network connectivity from gateway to that backend.

## Step 1: tools/list while one backend is down

Send `tools/list` from the host:

```json
{"jsonrpc":"2.0","id":101,"method":"tools/list"}
```

Expected host JSON-RPC result:

```json
{
  "jsonrpc": "2.0",
  "id": 101,
  "result": {
    "tools": [
      {"name": "alpha__echo"}
    ],
    "extras": {
      "partial_failures": [
        {
          "backend_id": "beta",
          "reason": "transport_error"
        }
      ]
    }
  }
}
```

Notes:
- `result.tools` excludes tools from the down backend.
- `result.extras.partial_failures` exists only when `aggregation.report_partial_failures` is enabled.
- `reason` is one of stable codes: `transport_error`, `jsonrpc_error`, `timeout`, `omitted`.
- If you force timeout instead of hard-down, expect `reason: "timeout"` (covered by `TestToolsListReportsTimeoutPartialFailure`).

## Step 2: tools/call to tool on down backend

Send `tools/call` targeting the down backend's namespaced tool:

```json
{
  "jsonrpc":"2.0",
  "id":102,
  "method":"tools/call",
  "params":{"name":"beta__ping","arguments":{}}
}
```

Expected host JSON-RPC error:

```json
{
  "jsonrpc": "2.0",
  "id": 102,
  "error": {
    "code": -32000,
    "message": "backend call failed"
  }
}
```

Notes:
- `tools/call` is not an aggregated list response, so no `extras.partial_failures` is expected here.
- Error code `-32000` maps to gateway internal failure (`GatewayInternal`).

## Telemetry checks

### Tempo (traces)

For the same test window/session, confirm:
- `mcp.multiplex.tools_list` span exists for `tools/list` and completes (fail-open aggregate behavior).
- `mcp.backend.call` span exists for failing `tools/call` with:
  - `mcp.method="tools/call"`
  - `mcp.backend.id="beta"`
  - span status `ERROR` (backend transport failure path).
- Optional parent span correlation: `mcp.host.request` enclosing request handling.

### Prometheus (metrics)

Check gateway internal phase histogram for both methods:
- `mcp.gateway.internal.duration_seconds` (or translated name such as `mcp_gateway_internal_duration_seconds_bucket`)
- labels:
  - `method="tools/list"` and `phase="mux"`
  - `method="tools/call"` and `phase="mux"`

Suggested sanity queries (adapt metric naming to your pipeline):

```promql
sum by (method, phase) (rate(mcp_gateway_internal_duration_seconds_count{method=~"tools/list|tools/call"}[5m]))
```

```promql
histogram_quantile(0.95, sum by (le, method, phase) (rate(mcp_gateway_internal_duration_seconds_bucket{method=~"tools/list|tools/call"}[5m])))
```

Expected signal interpretation:
- `tools/list` still emits normal internal-duration samples while host result carries `extras.partial_failures`.
- `tools/call` to the down backend emits internal-duration samples and a failed backend span in traces.

## Pass criteria

- Host sees `extras.partial_failures` on `tools/list` with correct `backend_id` and stable `reason`.
- Host receives `-32000 backend call failed` on `tools/call` to the down backend.
- Tempo shows failed backend span for `tools/call`, correlated with the test request.
- Prometheus shows internal-duration activity for both methods during the scenario window.
