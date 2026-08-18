# Contributing

Everything here is specific to this repository. General engineering opinions are deliberately
absent — bring your own.

## The one surprising rule: `make fmt`, not `gofmt`

This repo keeps **one space before `=`** in `const`/`var` blocks instead of gofmt's column
alignment, normalised by `make fmt` (gofmt plus `scripts/normalize-go-eq-spacing.sh`).

**`gofmt -l .` reports many files on a clean checkout. That is expected, not a defect.** To check
formatting, run `bash scripts/check-go-eq-spacing.sh` — exit 0 means the tree is correct. The
`gofmt` linter is disabled in `.golangci.yml` because it would always fail on this style;
`make lint` enforces the real rule, and CI runs `make lint` so the two cannot diverge.

If you disagree with the convention, open an issue before sending a PR that reformats the tree.

## Definition of done

```bash
go vet ./... && go test -race ./... && make lint
```

`make ci` reproduces CI's `lint-and-unit` job exactly — the job that gates every PR. `make help` is
the target index.

Default `go test ./...` must never require Docker, the network or Qdrant. Tests that do carry the
`integration` build tag and run separately:

```bash
make test-integration
```

That target supplies `QDRANT_URL`, `EMBED_URL` and `OTEL_EXPORTER_OTLP_ENDPOINT`, defaulting to the
local compose stack; the JWT policy tests run without any of them and the rest skip when their
service is unreachable. Invoking `go test -tags=integration` by hand works too, but you own those
variables.

To bring the stack up, `make bootstrap` **then** `make docker-up` — compose is invoked with
`--env-file .env`, and `bootstrap` is what creates `.env` from `.env.example`. On a fresh clone
`docker-up` alone fails with `couldn't find env file`.

## Layout

| Path | Contents |
|---|---|
| `cmd/gateway/` | Entry point: load config, wire dependencies, run |
| `internal/gateway/` | Multiplexer, session, HTTP/SSE server, namespacing, wire constants |
| `internal/router/` | Semantic router: embeddings, vector store, BM25, deterministic rules |
| `internal/auth/`, `policy/`, `validate/` | JWT, allow-lists and RAR, argument limits |
| `internal/backend/` | Upstream transports (HTTP+SSE, stdio) and test doubles |
| `internal/telemetry/` | OpenTelemetry setup, spans, metrics |
| `internal/defaults/` | Every cross-package tunable — new timeouts, limits and sizes go here |
| `internal/gateway/mcpwire/` | MCP and HTTP protocol strings — never duplicate a wire literal |
| `docs/` | Operator documentation, ADRs, OpenAPI contract |

## Vocabulary, and four contracts you must not rename

An MCP server the gateway proxies to is an **upstream**, in Go identifiers, log keys and messages.

Four external names keep the older `backend` spelling because they are observable outside the
process: renaming one silently breaks deployed configuration, a host, or someone's saved query. They
are frozen — rename the Go identifier around them instead.

| Contract | Where | Breaks if renamed |
|---|---|---|
| YAML key `backends:` | [`internal/config/config.go:21`](internal/config/config.go:21), every `deployments/*.yaml` | deployed config files |
| Env var `MCP_GATEWAY_BACKENDS` | [`internal/config/config.go`](internal/config/config.go) | deployment environment |
| OTel span `mcp.backend.call`, attribute `mcp.backend.id` | [`spans.go:16`](internal/telemetry/spans.go:16), [`attrs.go:17`](internal/telemetry/attrs.go:17) | exported traces — any Tempo query or dashboard built on them |
| JSON `backend_id`, `serverInfo.extras.backends` | [`multiplexer.go:431`](internal/gateway/multiplex/multiplexer.go:431) | responses hosts parse |

## What lands easily, and what does not

**Easily:** a bug fix with a regression test that fails without it · a documentation fix · a test
for something untested · replacing a literal with a constant in `internal/defaults`.

**Open an issue first:** changing a decision recorded in [`docs/adr/`](docs/adr/) · renames across
packages · new dependencies · reformatting.

**Likely rejected:** making the gateway inspect, summarise or rewrite tool results. It is a broker,
not an LLM — see [ADR 0004](docs/adr/0004-gateway-scope.md). Also a PR that mixes a rename with a
behaviour change: the interesting half stops being reviewable.

## Sending the PR

One logical change. Describe the **observable** behaviour: "the host receives two payloads for one
JSON-RPC id" tells a reviewer what to verify.

The [PR template](.github/PULL_REQUEST_TEMPLATE.md) asks for three things, in this order — why the
change exists (including whether the same problem appears elsewhere in the repo), what observably
changes, and how you verified it, manual checks included. Then the checklists. Filling in section 3
with what you actually ran is the part reviewers lean on most: CI proves the suite passes, not that
you exercised the path you changed.

Dependency updates are automated in [`.github/dependabot.yml`](.github/dependabot.yml). It carries
one entry per Dockerfile directory, so **adding a Dockerfile means adding an entry** — otherwise its
base image is silently never updated.

Found a security problem? Do not open an issue or PR — see [`SECURITY.md`](SECURITY.md).
Operating the gateway day to day: [`docs/DEVELOPER.md`](docs/DEVELOPER.md).
