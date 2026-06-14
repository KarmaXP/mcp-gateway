# MCP Gateway

An open-source **[Model Context Protocol (MCP)](https://modelcontextprotocol.io/)** gateway in **Go**: one HTTP endpoint for your AI host, many MCP backends behind it, with optional semantic routing, auth, and observability.

New to MCP? It is the shared wire format between an assistant (or agent) and the **tools** you expose (clusters, metrics, tickets, internal APIs, …). This project adds a **gateway** when you want a single connection, merged catalogs, and centralized policy.

---

## ✨ What it does

- **Multiplexes** several MCP servers behind one host-facing URL (SSE + JSON-RPC).
- **Namespaces** tools as `prefix__tool_name` so catalogs stay unambiguous.
- **Routes** natural-language intent to the right tool when the router is enabled (vector search + rules).
- **Secures** ingress with JWT, allow-lists, JSON Schema checks, and audit hooks.
- **Observes** traffic via OpenTelemetry, Prometheus, Grafana, and Tempo (Docker stack).

Built for platform, SRE, and security-minded teams standardizing on MCP.

---

## 🚀 Quick start

**Requirements:** Go 1.26+ ([`go.mod`](go.mod)). Docker is optional.

```bash
git clone https://github.com/KarmaXP/mcp-gateway.git
cd mcp-gateway
make demo     # one mock upstream + gateway + MCP handshake + tools/call (no Docker)
```

`make demo` prints the gateway URL and how to stop it (`make stop`).

Optional first-time env file:

```bash
make bootstrap   # copies .env.example → .env if missing
```

---

## 🧪 Try more locally

| Command | What you get |
|---------|----------------|
| `make run` | Gateway on **8080** by default (override `PORT` in `.env`; see [local-ports.md](docs/local-ports.md)) |
| `make demo-full` | Two mock backends + `alpha__echo` through the gateway |
| `make sre-smoke` | Three SRE-style tools (`k8s__`, `prom__`, `gh__`) via mock upstreams |
| `make verify-e2e` | Full automated check: unit tests + demo + multi-backend + SRE smoke |
| `make docker-up` | Qdrant, embedding sidecar, OTel, Prometheus, Grafana |
| `make sre-up` | Docker deps + SRE mocks (for semantic router with `router.mode: on`) |

Ports and mock layouts: **[Local ports reference](docs/local-ports.md)**. Operator details: **[Developer guide](docs/DEVELOPER.md)** (`make help` lists all targets).

**Full stack with Docker:**

```bash
make bootstrap
make docker-up
make run
```

---

## 📚 Documentation

**Full index:** **[docs/README.md](docs/README.md)**

| I want to… | Read |
|------------|------|
| Configure env vars and YAML | **[Configuration reference](docs/configuration.md)** |
| Understand errors and status codes | **[Error reference](docs/errors.md)** |
| See supported MCP methods | **[MCP capabilities](docs/mcp-capabilities.md)** |
| Register backends (HTTP or stdio) | **[Adding backends](docs/ADDING_BACKENDS.md)** |
| Connect an IDE, script, or agent | **[Connecting agents](docs/CONNECTING_AGENTS.md)** |
| Deploy with Docker / production notes | **[Deployment](docs/deployment.md)** |
| Operate (metrics, CI, observability) | **[Developer guide](docs/DEVELOPER.md)** |
| Architecture overview | **[Architecture](docs/architecture/README.md)** |
| HTTP contract (OpenAPI) | **[openapi.yaml](docs/artifacts/openapi/openapi.yaml)** |
| Measure router quality and latency | **[Calibration runbook](docs/evaluation/calibration-run.md)** · **[Recorded results](docs/evaluation/calibration-results.md)** |
| Real MCP backends + JWT (integrated lab) | **[Real backends scenario](docs/evaluation/scenario-real-backends-jwt.md)** |
| Multi-backend SRE walkthrough | **[SRE scenario](docs/evaluation/scenario-sre-multibackend.md)** |

---

## 🗺️ Roadmap

| Status | Item |
|--------|------|
| ✅ **Available** | Host transport (SSE + JSON-RPC), multi-backend merge, JWT + policy, semantic router, compose observability stack, local mocks and smoke targets |
| ✅ **Available** | Example configs: single-backend demo, alpha/beta, SRE three-backend layout |
| 📖 **Documented pattern** | LangGraph (or any MCP host) calling the gateway, see [Connecting agents](docs/CONNECTING_AGENTS.md); you implement the agent in your own repo |
| 🔧 **You provide** | Real MCP backends (Kubernetes, Prometheus, GitHub, …) and production deployment |
| 📊 **Recorded** | Lab results in [calibration-results.md](docs/evaluation/calibration-results.md) (baseline 2026-05-18, integrated run 2026-05-30, full lab 2026-06-08) |
| 📊 **Optional** | Re-run calibration when tuning; see [calibration runbook](docs/evaluation/calibration-run.md) |

---

## 🛠️ Development

Contributors and maintainers:

```bash
make ci         # lint + vet + race tests (matches GitHub Actions)
make test-integration  # JWT + router integration (needs Qdrant/embed when running full stack)
```

See **[docs/DEVELOPER.md](docs/DEVELOPER.md)** for auth setup, OpenAPI, and integration-test prerequisites.

---

## 📄 License

See [LICENSE](LICENSE).
