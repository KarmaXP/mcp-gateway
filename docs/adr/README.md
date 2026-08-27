# Architecture decision records

One decision per record, in the present tense, numbered in the order they were taken.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-architecture-decisions.md) | SSE over WebSockets, local ONNX embeddings, Qdrant for vector search | Accepted |
| [0002](0002-filter-list-mode.md) | `filter_list` returns an intent-filtered catalog | Accepted |
| [0003](0003-security-rar-jwt-merge-failmode.md) | RAR shape, JWT merge, and fail-closed policy evaluation | Accepted |
| [0004](0004-gateway-scope.md) | Which MCP methods the gateway aggregates, forwards or refuses | Accepted |
| [0005](0005-tool-namespacing-and-opaque-ids.md) | `__` namespaces tool names; opaque values are base64 behind `gw0:` | Accepted |
| [0006](0006-one-term-upstream.md) | `upstream` is the only internal term, and five external names are frozen | Accepted |
| [0007](0007-no-jsonrpc-batch.md) | JSON-RPC batches are refused | Accepted |
| [0008](0008-trace-topology.md) | `mcp.host.request` is a child of the HTTP span, and there is exactly one | Accepted |

**0001 and 0003 each bundle several decisions under one Status**, which means none of them can be
superseded on its own. New records take one decision each; those two are left as written rather
than renumbered, because the links to them are already published.
