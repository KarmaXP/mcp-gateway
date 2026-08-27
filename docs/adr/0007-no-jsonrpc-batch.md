# ADR 0007: JSON-RPC batches are refused

## Status

Accepted.

## Context

JSON-RPC 2.0 allows a client to send an array of requests and receive an array of responses. The MCP
specification removed batching in its 2025-06-18 revision, and no MCP host this gateway has been
tested against sends one.

Supporting batches through a multiplexer is not free: each element may target a different upstream,
partial failure has to be expressed per element, and the ordering guarantees interact with the
per-session SSE stream that carries the responses.

## Decision

The gateway refuses a batch. `rpc.ParseRequest` returns `ErrNotObject` for any body that is not a
JSON object, and the request is rejected before it reaches the multiplexer.

## Consequences

- A host that sends a batch gets `rpc: body must be a JSON object`, which says the body was the
  wrong shape but does not say that batching specifically is unsupported. A caller has to infer it.
  A dedicated `ErrBatchUnsupported` mapped to `-32600` would say so directly; that is a change to a
  host-visible error string and is not made here.
- Nothing in the gateway needs per-element correlation, so one JSON-RPC id maps to one response for
  the life of a request. Golden rule R3 stays a single-valued statement.
- Adding batching later is a protocol change, not an implementation detail: it needs an ADR that
  supersedes this one.

## References

- [internal/rpc/rpc.go](../../internal/rpc/rpc.go), `ErrNotObject` and the parse guard
- [docs/errors.md](../errors.md), the JSON-RPC error codes the gateway returns
