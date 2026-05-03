# MCP Gateway

Welcome. This repository is an **open-source gateway for the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/)** — the standard way for AI applications and agents to use **tools**, **resources**, and **prompts** from your own systems in a structured, interoperable way.

If you are new to MCP: think of it as a **common language** between an AI host (assistant, IDE, or automation) and the **capabilities** you expose (Kubernetes, metrics, tickets, internal APIs, and more). A gateway sits in the middle when you want **one connection** for the host and **many backends** behind it, with room for policy and observability.

## What you’ll find here

**mcp-gateway** is a production-minded implementation in **Go**: the AI client talks to a **single** HTTP endpoint (streaming + JSON-RPC, as MCP defines for remote use). The gateway **multiplexes** traffic to multiple MCP servers, merges their catalogs, and can optionally **route** natural-language intent to the right tool when names are ambiguous.

It is aimed at **platform engineering**, **SRE**, and **security-conscious** teams who want MCP without a tangle of one-off integrations.

## Who this is for

- **Builders** evaluating or standardizing on MCP across internal tools.
- **Operators** who need **one place** for auth, allow-lists, and telemetry around MCP traffic.
- **Readers** curious how MCP fits a **multi-backend** or **regulated** environment — you do not need to read the code to get value from the docs linked below.

## Documentation map

| I want to… | Start here |
|------------|------------|
| Run, configure, or integrate with this gateway (env vars, auth, OpenAPI, CI, metrics) | **[Developer & operator guide](docs/DEVELOPER.md)** |
| Read the full technical specification and requirements | **[Architecture plan](docs/architecture/mcp_gateway.plan.md)** |
| See the HTTP/API contract (headers, JWT, errors) | **[OpenAPI spec](docs/artifacts/openapi/openapi.yaml)** |
| Understand specific design choices | **[Architecture Decision Records](docs/adr/)** |
| Record live router/latency numbers (embed + vector DB) | **[Calibration runbook](docs/evaluation/calibration-run.md)** |

## Try it locally (minimal)

From this directory, with **Docker** available:

```bash
make docker-up    # optional: Qdrant, embedding sidecar, observability stack
make run          # start the gateway (see .env / gateway.yaml patterns)
```

For Makefile targets, tests, smoke checks, and integration with compose, use **[docs/DEVELOPER.md](docs/DEVELOPER.md)**.

## License

See [LICENSE](LICENSE).
