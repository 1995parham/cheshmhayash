# CLAUDE.md

Project notes for Claude / Claude Code. Keep this short and current; prefer
deleting stale lines over qualifying them.

## What this is

`cheshmhayash` is a NATS administration dashboard. The backend speaks NATS
on `$SYS.REQ.*` and `$JS.API.*` — the same channels `natscli` uses — and
exposes the results as plain HTTP + JSON. The frontend is a React panel
that talks to that JSON API. There is **no** HTTP monitoring port
(`:8222`) requirement; only the NATS client port (`:4222`) is needed.

## Layout

```
.
├── main.go                          # entrypoint
├── internal/
│   ├── config/                      # TOML + env-var loader
│   ├── natsx/                       # NATS client + admin/jsm subjects
│   └── handler/                     # http.ServeMux routes (Go 1.22+ syntax)
├── frontend/                        # React + TS + Vite SPA
│   ├── src/
│   │   ├── App.tsx                  # shell + tabs + hero
│   │   ├── api.ts                   # typed fetch client + overview aggregator
│   │   ├── components/              # ServersView, JetStreamView, StreamEditor…
│   │   └── state/                   # toast context
│   ├── public/banner.png            # served as /banner.png (also the hero bg)
│   └── dist/                        # build output — served by Go in prod
├── config/default.toml              # ships in image
├── settings.toml                    # operator override (gitignored)
├── Dockerfile                       # multi-stage: node → go → distroless
├── chart/                           # Helm chart for k8s deploys
└── .github/workflows/ci.yaml        # backend lint+test, frontend build, docker
```

## Build & run

Local dev — the Vite dev server proxies `/api` + `/healthz` to the Go
process on `:1378`:

```sh
# backend
go run .                              # serves API on :1378

# frontend (separate terminal)
cd frontend && npm run dev            # serves UI on :5173 with HMR
```

Production (single binary serves API and built SPA from `frontend/dist`):

```sh
cd frontend && npm run build
go build -o ./bin/cheshmhayash .
./bin/cheshmhayash                    # http://127.0.0.1:1378
```

Docker — `docker build -t cheshmhayash .` and `docker run -p 1378:1378
-v "$PWD/settings.toml:/app/settings.toml:ro" cheshmhayash`.

## Configuration

Settings load in order — `config/default.toml`, then `settings.toml` if
present, then `CHESHMHAYASH__*` env vars. Nested keys use `__` as the
separator; lists are indexed (`CHESHMHAYASH__NATS__0__USER=admin`).

A working `settings.toml` for a local NATS with system creds:

```toml
[server]
host = "127.0.0.1"
port = 1378

[[nats]]
name = "local"
url = "nats://127.0.0.1:4222"
user = "admin"            # or use creds_file = "./sys.creds"
password = "•••"
request_timeout_ms = 5000
discovery_timeout_ms = 1500
```

`$SYS.REQ.*` subjects only flow when the connection is bound to the
system account. Without sys creds, server-discovery endpoints time out
and the JetStream overview returns empty. `$JS.API.*` requests run
against whichever account the credentials grant; the cluster-wide
JetStream overview uses `$SYS.REQ.SERVER.PING.JSZ` so it gets per-account
detail across the whole cluster.

## HTTP API

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/healthz` | liveness/readiness |
| `GET` | `/api/admin/clusters` | configured cluster names |
| `GET` | `/api/admin/clusters/{c}/servers` | `$SYS.REQ.SERVER.PING` |
| `GET` | `/api/admin/clusters/{c}/servers/{endpoint}` | PING for one endpoint (e.g. `VARZ`) |
| `GET` | `/api/admin/clusters/{c}/servers/{id}/{endpoint}` | targeted server query |
| `GET` | `/api/admin/clusters/{c}/accounts/{account}/{endpoint}` | account-scoped |
| `POST` | `…/servers/{id}/actions/reload` | config reload |
| `POST` | `…/servers/{id}/actions/lame-duck` | graceful drain |
| `POST` | `…/servers/{id}/actions/kick` | body: `{"cid": N}` |
| `GET` | `/api/jsm/clusters/{c}/overview` | cluster-wide JS overview (sys account) |
| `GET` | `/api/jsm/clusters/{c}/streams?offset=N` | paginated, account-scoped |
| `GET\|PUT\|DELETE` | `/api/jsm/clusters/{c}/streams/{s}` | info / update / delete |
| `POST` | `…/streams/{s}/purge?confirm=true` | drop all messages |
| `GET\|DELETE` | `…/streams/{s}/consumers/{con}` | info / delete |

Destructive JetStream verbs require `?confirm=true`; without it the
server returns `428 Precondition Required`.

## Tech / versions

- Go `1.26.x`, stdlib `net/http` (1.22+ pattern syntax), `log/slog`
- `github.com/nats-io/nats.go v1.52.x`
- `github.com/BurntSushi/toml v1.6.x`
- React 19, TypeScript 6, Vite 8, CodeMirror 6 (`@uiw/react-codemirror`,
  `@codemirror/lang-json`, `@codemirror/theme-one-dark`)
- Runtime image: `gcr.io/distroless/static-debian12:nonroot`

## Conventions

- Backend handlers pass the NATS reply through as `json.RawMessage` —
  don't unmarshal and re-marshal, the server already produced valid JSON
  and re-encoding loses field ordering / numbers' representation.
- Frontend treats responses as opaque except for the few fields it
  renders. Add to `frontend/src/types.ts` only when a new component
  needs the shape.
- New SYS or JS subjects: add a thin method to `internal/natsx/`, expose
  via `internal/handler/`, then call from `frontend/src/api.ts`.
- All cluster-wide aggregation (per-account / per-stream / per-consumer)
  happens client-side in `frontend/src/api.ts#aggregateOverview` from
  the single `/overview` payload — keep the server stateless.

## Frontend dev tips

- `npm run typecheck` runs `tsc -b --noEmit` over both project
  references. Faster than `npm run build` for type errors.
- The CodeMirror bundle is the biggest part of `index-*.js` (~640 kB).
  Code-split if you ever add a route that doesn't need the editor.
- Lucide icons are tree-shakeable — import named (`import { X } from
  "lucide-react"`), never `import *`.
