## 1. Context, why this PR exists

<!-- What is wrong, missing or surprising today, and where a reader can see it for themselves.
     Cover the ones that apply:

       · The trigger: a bug hit in practice, a finding while reading the code, an operator report.
       · Today's behaviour and why it is a problem — the consequence, not "it isn't ideal".
         "An unauthenticated request never reaches the rate limiter" beats "the ordering is odd".
       · Similar cases. This repo has carried the same defect in two transports at once, and the
         same id-comparison bug in three places. If you found siblings, say whether this PR fixes
         them too, or why it deliberately leaves them alone.
       · The issue, and the ADR in docs/adr/ that governs the area if there is one. -->

## 2. What this PR changes

<!-- Describe the observable behaviour, not the code edit. "The host now receives one payload per
     JSON-RPC id instead of two" tells a reviewer what to check; "fixes the return value of
     runMiddlewares" does not.

     Say what you deliberately left out of scope, so a reviewer does not read an omission as an
     oversight. If an external contract changed (YAML key, env var, span or metric name, JSON
     field), name it here — those are the ones that break deployments silently. -->

## 3. How it was verified

<!-- What you actually ran and what you saw — paste the part that matters, not the whole log.

     Be specific about manual checks: which target, which request, what came back. "make demo,
     then tools/call on alpha__echo, got the result through the gateway" is verifiable; "tested
     locally" is not.

     Say what you could NOT verify and why — no Docker, no Qdrant, no real upstream. A known gap
     is fine; a silent one is not. -->

```bash
go vet ./... && go test -race ./... && make lint
```

## Checklist

- [ ] One logical change — no rename or reformat mixed with a behaviour change
- [ ] `make fmt` (**not** bare `gofmt` — see [CONTRIBUTING.md](https://github.com/KarmaXP/mcp-gateway/blob/main/CONTRIBUTING.md))
- [ ] The commands above are green, and section 3 says what else was run
- [ ] Tests updated. For a bug fix, a regression test that **fails without this change**
- [ ] No new literal outside `internal/defaults`, no wire string outside `internal/gateway/mcpwire`
- [ ] Nothing logs or traces tokens, JWT payloads or tool arguments

## If applicable

- [ ] Documented decision changed → ADR in `docs/adr/` updated here
- [ ] Something renamed → `docs/` grepped for the old name
- [ ] Routes or error codes changed → `docs/artifacts/openapi/openapi.yaml` and `docs/errors.md`
- [ ] Config keys or env vars added → `docs/configuration.md` and `.env.example`
