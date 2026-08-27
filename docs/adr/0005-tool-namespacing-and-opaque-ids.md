# ADR 0005: `__` namespaces tool names, and opaque values hide behind `gw0:`

## Status

Accepted.

## Context

The gateway merges the catalogs of several MCP servers into one. Two tools called `search` on two
upstreams must reach the host as two distinguishable names, and a `tools/call` for either must be
routable back to the server that owns it without a lookup table that can go stale.

The same problem arrives again for values the gateway does not own: a resource URI is an arbitrary
string chosen by the upstream, and it may legitimately contain whatever separator the gateway picks.

## Decision

**Tool names are namespaced as `prefix__native`**, with `__` as the separator and the prefix
configured per upstream. The prefix must be unique across the catalog and must not itself contain
the separator. On forward, the gateway strips the prefix and sends the native name, so the upstream
never sees a name it did not publish.

**A native tool name containing `__` is refused, not escaped.** `namespace.Join` returns
`ErrNativeContainsSep`, and the merge drops that tool from the catalog with a warning.

**Values the gateway does not own are escaped instead of refused.** `namespace.JoinOpaque` encodes
the native segment as base64url behind the marker `gw0:` whenever it contains the separator, or
whenever it already starts with `gw0:`. `SplitOpaque` recovers the original. Resource URIs and
prompt names travel this way.

## Consequences

- **A tool whose native name contains `__` is invisible through this gateway.** It is dropped from
  the merged catalog and the only trace is a `slog.Warn`; the host is never told the tool exists.
  This is the asymmetry to be aware of: resources get an escape, tools do not. Fixing it means
  routing tools through the opaque encoding too, which changes every namespaced tool name a host
  has already seen.
- The prefix is part of the host-visible contract. Renaming an upstream's prefix renames every one
  of its tools from the host's point of view.
- `gw0:` is a wire contract. A value already stored by a host round-trips only while the marker and
  the base64url alphabet stay as they are.
- Prefix uniqueness is validated at load, so a duplicate is a startup error rather than a routing
  ambiguity discovered on the first call.

## References

- [internal/gateway/namespace/namespace.go](../../internal/gateway/namespace/namespace.go), join, split and prefix validation
- [internal/gateway/namespace/opaque.go](../../internal/gateway/namespace/opaque.go), the `gw0:` encoding
- [docs/ADDING_UPSTREAMS.md](../ADDING_UPSTREAMS.md), choosing a prefix
