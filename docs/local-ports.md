# Local ports and mock upstreams

Fixed defaults for local development and smoke scripts. Change only together with `Makefile`, deployment YAML, and `docs/DEVELOPER.md`.

| Key | Value | Use |
|-----|-------|-----|
| Dev gateway port | **8080** (override in `.env`, e.g. `18080`) | `PORT` / `GATEWAY_PORT`; `make run` and `make stop` both source `.env` |
| Auto-smoke gateway port | **18081** | `SMOKE_AUTO_START_GATEWAY=1` in `scripts/smoke_test.sh` (avoids clashing with `make run` on 8080) |
| Smoke upstream | **127.0.0.1:31400**, prefix `smoke`, tool `echo` → `smoke__echo` | `gateway.demo.yaml`, `scripts/smoke_upstream` |
| Alpha / beta upstreams | **3101** / **3102**, prefixes `alpha` / `beta` | `gateway.example.yaml` after `make demo-backends` |
| SRE upstreams | **3201** / **3202** / **3203**, prefixes `k8s` / `prom` / `gh` | `gateway.sre.example.yaml` after `make sre-backends` |
| Default config file | `deployments/gateway.demo.yaml` | `make demo`, `make run` |

SRE smoke tools (namespaced): `k8s__get_pod_logs`, `prom__query_instant`, `gh__list_prs`.
