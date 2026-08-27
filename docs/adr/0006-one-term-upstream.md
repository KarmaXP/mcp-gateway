# ADR 0006: `upstream` is the only internal term, and five external names are frozen

## Status

Accepted.

## Context

An MCP server the gateway proxies to was called both `backend` and `upstream`, 51 times against 275,
often inside the same file. `tools_call.go` called `resolveBackendForTool` and passed the result to
`invokeUpstreamToolsCall`, one line apart. Two words for one concept is the first thing a reader
notices, and it costs a reviewer real time deciding whether the two mean different things.

Renaming everything is not available: several of those names are read by software outside this
repository, and changing them breaks deployments quietly rather than loudly.

## Decision

**`upstream` is the term**, in every Go identifier, log message, document and diagram. `backend`
does not appear in new code.

**Five external contracts keep the old spelling**, because something outside the repo reads them:

| Contract | Why it cannot move |
|---|---|
| YAML key `backends:` | Every deployed configuration file |
| Env var `MCP_GATEWAY_BACKENDS` | Every deployment that sets it |
| OTel `mcp.backend.call` and `mcp.backend.id` | Any dashboard or alert already querying them. No dashboard in this repo does, so the risk is outside it |
| JSON field `backend_id` | Host-side parsers of `partial_failures` |
| Qdrant payload key `backend` | Every point already indexed |

The last is the one to be most careful with. Renaming a metric spoils a graph; renaming a persisted
payload key makes the router unable to read an index that already exists.

## Consequences

- A reader meets `backend` in exactly five places and each one is a boundary with the outside world,
  so the inconsistency now carries information instead of noise.
- Changing any of the five is a breaking change that needs its own release note and a deprecation
  window with both names accepted. It is not a cleanup.
- The Go field and the wire name differ on purpose: `Upstreams []UpstreamDefinition` carries
  `yaml:"backends"`, and `PartialFailure.UpstreamID` carries `json:"backend_id"`.

## References

- [internal/config/config.go](../../internal/config/config.go), the YAML key and the env override
- [internal/telemetry/spans.go](../../internal/telemetry/spans.go) and [attrs.go](../../internal/telemetry/attrs.go), the OTel names
- [internal/router/store/qdrant.go](../../internal/router/store/qdrant.go), the persisted payload key
