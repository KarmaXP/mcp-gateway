# Deployment

Run the gateway **on the host** (recommended for development) or **in Docker** (optional). Dependencies for the semantic router (Qdrant, embed sidecar) usually run in Compose while you iterate with `make run`.

Compose file: [`deployments/docker-compose.yaml`](../deployments/docker-compose.yaml).

---

## Local development (typical)

```bash
make bootstrap     # .env from .env.example
make docker-up     # Qdrant, embed, OTel, Tempo, Prometheus, Grafana
make demo-upstreams   # optional: alpha/beta mocks
MCP_GATEWAY_CONFIG=deployments/gateway.example.yaml make run
```

Gateway listens on **`PORT`** (default **8080**). See [local-ports.md](local-ports.md).

---

## Docker Compose profiles

| Make target | Compose profile | What starts |
|-------------|-----------------|-------------|
| `make docker-up` | (default services) | Qdrant, embed, otel-collector, Tempo, Prometheus, Grafana |
| `make docker-up-full` | `gateway` | Above + gateway container on `:8080` |
| `make docker-up-demo` | `demo` | Default stack + mock alpha/beta on **3101/3102** |
| `make docker-up-sre` | `sre` | Default stack + k8s/prom/gh mocks on **3201–3203** |

Stop everything: `make docker-down`.

---

## Published ports (defaults)

Override with `HOST_PORT_*` in `.env` (see [`.env.example`](../.env.example) — Docker Compose host ports).

| Port | Service |
|------|---------|
| 8080 | Gateway (profile `gateway` or host `make run`) |
| 6333 | Qdrant HTTP |
| 8001 | Embedding sidecar |
| 4318 | OTLP HTTP (collector) |
| 9090 | Prometheus |
| 3000 | Grafana (`admin` / `admin` in dev compose) |

---

## Environment wiring (router on)

When the gateway runs **on the host** and dependencies are in Docker:

```bash
export QDRANT_URL=http://127.0.0.1:6333
export EMBED_URL=http://127.0.0.1:8001
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export ROUTER_MODE=on
export MCP_GATEWAY_CONFIG=deployments/gateway.sre.example.yaml
make run
```

`/readyz` checks Qdrant and embed when the router is active. Upstream MCP servers are **not** probed at readiness (handled per request).

### Multiupstream benchmark — real stdio upstreams + JWT

For end-to-end validation with official MCP servers over stdio (not HTTP mocks), JWT, and OTLP:

```bash
make docker-up   # no make sre-up
# see docs/evaluation/scenario-real-upstreams-jwt.md for JWT keys and exports
export MCP_GATEWAY_CONFIG=deployments/gateway.real.yaml
export AUTH_MODE=jwt
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export PORT=18080
make run
```

Recorded numbers: [calibration-results.md](evaluation/calibration-results.md).

---

## Production notes

- **TLS:** Terminate TLS at a reverse proxy or ingress; the gateway speaks plain HTTP in the default image.
- **Secrets:** Use `auth_token_env`, JWKS URLs, and external secret stores, never commit `.env` or tokens in YAML.
- **Auth:** Set `AUTH_MODE=jwt` with your IdP keys; avoid `AUTH_MODE=none` outside isolated labs.
- **Observability:** Point `OTEL_EXPORTER_OTLP_ENDPOINT` at your collector; import [Grafana dashboard JSON](artifacts/grafana/mcp-gateway-observability.json).
- **Scaling:** One gateway process handles many SSE sessions; scale horizontally behind a load balancer only if sessions are sticky to the instance that owns each SSE connection (or use a single replica per shard).
- **Resources:** Size the embed sidecar for catalog reindex load; Qdrant needs persistent volume for production catalogs.

---

## Health checks

| Path | Use |
|------|-----|
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness (includes Qdrant + embed when router on) |

---

## Related docs

- [configuration.md](configuration.md)
- [DEVELOPER.md](DEVELOPER.md): metrics, traces, CI
- [ADDING_UPSTREAMS.md](ADDING_UPSTREAMS.md): register production upstreams
