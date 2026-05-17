# ADR 0001: Transport and embedding architecture

## Status

Accepted

## Context

The MCP gateway sits between AI hosts and multiple MCP backends. It must push JSON-RPC results to hosts efficiently, route `tools/call` when names do not match the aggregated catalog exactly, and run in environments where reliability, cost, and data governance matter as much as raw model quality.

## Decision 1: Server-Sent Events (SSE) instead of WebSockets

We expose host ingress as `GET /mcp/sse` (server → client stream) plus `POST /mcp/rpc` (client → server commands).

### Rationale

- **Operational simplicity:** SSE is HTTP/1.1-friendly, passes through most corporate proxies and load balancers with the same semantics as normal GET requests, and does not require WebSocket upgrade negotiation or sticky-session hacks for basic L7 routing.
- **Latency:** For *server-pushed* JSON-RPC responses, SSE avoids polling and matches the MCP “long-lived notification channel” model with a single directional stream plus idempotent POSTs.
- **SRE / failure modes:** On disconnect, the browser or client reconnects; session state is keyed by `Mcp-Session-Id`. Graceful shutdown can cancel the stream context and drain `http.Server.Shutdown` without bespoke WebSocket close frames.
- **Scope fit:** Hosts need server→client events; full-duplex WebSockets add complexity (ping/pong, framing, middleware) without a requirement for bidirectional binary channels on the same socket.

### Consequences

- POST returns `202 Accepted` for requests with an `id`; results appear on SSE. Clients must implement SSE parsing and backoff on reconnect.

## Decision 2: Local ONNX embeddings instead of OpenAI (or other hosted embedding APIs)

Embeddings are produced by a sidecar (e.g. all-MiniLM-L6-v2) behind `POST /embed`, consumed by the gateway’s semantic router.

### Rationale

- **Privacy and compliance:** Tool names, descriptions, and argument *keys* (never secret values in our design) stay inside the deployment boundary, important for regulated tenants and for reproducible, air-gapped deployments.
- **Cost and predictability:** No per-token API spend or rate-limit coupling during catalog reindex or traffic spikes; capacity is bounded by local CPU/GPU.
- **Latency SLOs:** A colocated embed service avoids WAN RTT variance; combined with Qdrant over the local network (or same host), the router path is suitable for tight P99 budgets.
- **Offline and tests:** CI and air-gapped demos run with fixed models (`HF_HUB_OFFLINE`, local weights) without API keys.

### Consequences

- Operators must ship and monitor the embed sidecar (image, health checks, resource limits). Model upgrades are a deliberate release step, not an external API version change.

## Decision 3: Qdrant for vector search

Qdrant provides filtered cosine search over tool vectors with payload fields for catalog version and backend id, supporting explainable routing and versioned indexes.

### Rationale

- **Performance:** HNSW-style ANN with payload filters matches “many tools, host-selected allow-lists” without post-filtering everything in application memory.
- **SRE:** HTTP API, clear health endpoints, and first-class Docker/Compose fit for integration tests and local stacks.

## References

- `internal/gateway/httpserver`, SSE + RPC transport
- `internal/router`, semantic routing pipeline
- `deployments/docker-compose.yaml`, Qdrant, embed, OTel, Tempo, Prometheus, Grafana
