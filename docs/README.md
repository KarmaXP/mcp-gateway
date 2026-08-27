# Documentation index

Start with the [repository README](../README.md), then use this map.

## Getting started

| Goal | Document |
|------|----------|
| Run the gateway locally in one command | [README — Run it in 30 seconds](../README.md#run-it-in-30-seconds) |
| Understand local ports and example configs | [local-ports.md](local-ports.md) |
| Register upstream MCP servers | [ADDING_UPSTREAMS.md](ADDING_UPSTREAMS.md) |
| Contribute code (conventions, `make fmt`, definition of done) | [CONTRIBUTING.md](../CONTRIBUTING.md) |
| Connect an IDE, script, or agent | [CONNECTING_AGENTS.md](CONNECTING_AGENTS.md) |

## Reference

| Goal | Document |
|------|----------|
| Environment variables and YAML options | [configuration.md](configuration.md) |
| JSON-RPC and HTTP error codes | [errors.md](errors.md) |
| MCP methods (tools, resources, prompts) | [mcp-capabilities.md](mcp-capabilities.md) |
| HTTP API contract (OpenAPI) | [artifacts/openapi/openapi.yaml](artifacts/openapi/openapi.yaml) (keep aligned with [errors.md](errors.md) and [`internal/gateway/errcodes`](../internal/gateway/errcodes/codes.go)) |

## Architecture

| Goal | Document |
|------|----------|
| Overview and where to read next | [architecture/README.md](architecture/README.md) |
| Full technical specification | [architecture/mcp_gateway.plan.md](architecture/mcp_gateway.plan.md) |
| Design decisions (ADRs) | [adr/](adr/) |

## Operations

| Goal | Document |
|------|----------|
| Day-2 operations, observability, CI | [DEVELOPER.md](DEVELOPER.md) |
| Docker Compose and deployment notes | [deployment.md](deployment.md) |
| Router / latency measurement | [evaluation/calibration-run.md](evaluation/calibration-run.md) |

## Walkthroughs and test scenarios

| Scenario | Document |
|----------|----------|
| Index of evaluation guides | [evaluation/README.md](evaluation/README.md) |
| Pre-agent integration (one session) | [evaluation/integration-checklist.md](evaluation/integration-checklist.md) |
| Real backends + JWT (multibackend benchmark) | [evaluation/scenario-real-upstreams-jwt.md](evaluation/scenario-real-upstreams-jwt.md) |
| Recorded calibration numbers | [evaluation/calibration-results.md](evaluation/calibration-results.md) |
| Multi-backend SRE routing | [evaluation/scenario-sre-multiupstream.md](evaluation/scenario-sre-multiupstream.md) |
| JWT tool allow-list | [evaluation/scenario-jwt-allowlist.md](evaluation/scenario-jwt-allowlist.md) |
| Backend unavailable | [evaluation/scenario-upstream-down.md](evaluation/scenario-upstream-down.md) |

## Tools in the repo

| Tool | Location |
|------|----------|
| Minimal MCP host client | [scripts/mcp_host_demo/README.md](../scripts/mcp_host_demo/README.md) |
| MCP smoke (curl) | `make demo`, `scripts/smoke_test.sh`, `scripts/smoke_e2e.sh` |
| Load testing | [scripts/loadtest/README.md](../scripts/loadtest/README.md) |
| Regenerate router eval catalog JSON | `make gen-router-eval-catalog` |
