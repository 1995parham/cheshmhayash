# CLAUDE.md

Project notes for Claude / Claude Code. Keep this short and current;
prefer deleting stale lines over qualifying them.

## What this is

`cheshmhayash` is a NATS administration dashboard. The backend speaks
NATS on `$SYS.REQ.*` and `$JS.API.*` — the same channels `natscli` uses
— and exposes the results as plain HTTP + JSON. The frontend is a React
panel that talks to that JSON API. The same binary also exposes an
**MCP server** over stdio and HTTP for LLM tool-use, and a **webhook
notifier** that fans JetStream advisory events out to Slack/Mattermost/
Matrix. There is **no** HTTP monitoring port (`:8222`) requirement; only
the NATS client port (`:4222`).

## Layout

```
.
├── main.go                        # entrypoint — HTTP server, or `-mcp` stdio MCP
├── internal/
│   ├── config/                    # koanf loader: struct defaults → settings.toml → env
│   ├── natsx/                     # NATS client + admin/jsm subjects + overview cache
│   ├── handler/                   # http.ServeMux routes (Go 1.22+ pattern syntax)
│   ├── auth/                      # OIDC + HMAC-signed cookie sessions + MCP bearer
│   ├── mcp/                       # JSON-RPC 2.0 MCP server: stdio + Streamable HTTP
│   └── notify/                    # JS advisory → slack/mattermost/matrix webhooks
├── frontend/                      # React 19 + TS 6 + Vite 8 SPA
│   ├── src/
│   │   ├── App.tsx                # shell + tabs + hero
│   │   ├── api.ts                 # typed fetch client + overview aggregator
│   │   ├── hooks/useOverviewStream.ts  # EventSource → overview snapshots (poll fallback)
│   │   ├── components/            # ServersView / JetStreamView / TopologyView / …
│   │   └── state/                 # toast + confirm contexts
│   ├── public/banner.png          # served as /banner.png (also the hero bg)
│   └── dist/                      # build output — served by Go in prod
├── settings.toml                  # operator override (gitignored)
├── Dockerfile                     # multi-stage: node → go → distroless:nonroot
├── chart/cheshmhayash-chart/      # Helm chart → ghcr.io/1995parham/cheshmhayash-chart on tags
├── docs/screenshots/              # README hero strip
└── .github/workflows/             # ci.yaml (lint+test+helm-lint) + release.yaml (image+chart+cosign)
```

## Build & run

Local dev — the Vite dev server proxies `/api` + `/healthz` to the Go
process on `:1378`:

```sh
go run .                              # backend on :1378
cd frontend && npm run dev            # UI on :5173 with HMR
```

Production (single binary serves API + built SPA from `frontend/dist`):

```sh
cd frontend && npm run build
go build -o ./bin/cheshmhayash .
./bin/cheshmhayash                    # http://127.0.0.1:1378
```

Docker — `docker build -t cheshmhayash .` and `docker run -p 1378:1378
-v "$PWD/settings.toml:/app/settings.toml:ro" cheshmhayash`.

MCP mode (stdio) — same binary with `-mcp`:

```sh
./bin/cheshmhayash -mcp                            # read-only tool set
CHESHMHAYASH_MCP_WRITE=1 ./bin/cheshmhayash -mcp   # enables purge/delete/kick/reload/stepdown
```

It reuses `settings.toml`; logs go to stderr because stdout is the
JSON-RPC channel.

## Configuration

Settings load in order:

1. Go struct defaults (`internal/config/default.go`)
2. `settings.toml` (optional, gitignored)
3. `CHESHMHAYASH__*` env vars

Env keys use `__` for nesting; list elements are indexed
(`CHESHMHAYASH__NATS__0__USER=admin`). The env layer is overlaid
manually after koanf Unmarshal so `[]NATS` slices merge per-index
instead of getting clobbered by koanf's numeric-keyed-map merge.

```toml
[server]
host = "127.0.0.1"
port = 1378

[[nats]]
name = "local"
url = "nats://127.0.0.1:4222"
user = "admin"
password = "•••"
request_timeout_ms   = 5000
discovery_timeout_ms = 1500

# Optional — chat webhooks (Slack/Mattermost/Matrix-via-hookshot)
# [[notify]]
# provider = "slack"
# url = "https://hooks.slack.com/services/…"
# channel = "#nats-events"

# Optional — OIDC dashboard auth + MCP bearer keys (see settings.toml
# for the full template). Off by default; turning [auth].enabled = true
# requires oidc.issuer/client_id/redirect_url, a session.secret (≥16 chars),
# and at least one entry under access.allowed_{emails,domains,groups}.
# Optionally set [auth.access.admin].allowed_{emails,domains,groups} to
# split write access from read-only — everyone who signs in but isn't on
# the admin list is read-only.
```

Env knobs:

- `CHESHMHAYASH_MCP_WRITE=1` — register destructive MCP tools
- `CHESHMHAYASH_OVERVIEW_PERIOD=10s` — JSZ cache refresh interval
- `LOG_LEVEL=info|debug|warn|error`
- `CHESHMHAYASH__AUTH__ENABLED=true` — turn OIDC on (see settings.toml
  for the rest of the keys; slices are comma-separated, MCP keys use
  the `…__MCP_KEYS__0__VALUE` indexed form)
- `CHESHMHAYASH__AUTH__MODE=jwt` — switch auth from the cookie login flow
  (`oidc`, default) to per-request bearer-JWT validation: cheshmhayash runs
  no login, just verifies the `Authorization: Bearer` access token a
  "builtin oauth" gateway forwards (against `auth.oidc.issuer`) and reads
  its claims for the same allowlists. Only issuer + an allowlist required;
  optional `…__AUTH__JWT__AUDIENCES=a,b` restricts accepted `aud` values
- `CHESHMHAYASH__AUTH__MCP_OAUTH__ENABLED=true` +
  `…__MCP_OAUTH__RESOURCE=https://host/mcp` — accept same-issuer OIDC
  access tokens at `/mcp` (requires `AUTH__ENABLED=true`)

`$SYS.REQ.*` only flows when the connection is bound to the system
account. Without sys creds, server-discovery endpoints time out and the
cluster-wide JS overview comes back empty. The cluster-wide JS overview
uses `$SYS.REQ.SERVER.PING.JSZ` so it gets per-account detail across the
whole cluster.

## HTTP API

| Method      | Path | Notes |
| ---         | --- | --- |
| `GET`       | `/healthz` | liveness/readiness |
| `GET`       | `/api/admin/clusters` | configured cluster names |
| `GET`       | `/api/admin/clusters/{c}/servers` | `$SYS.REQ.SERVER.PING` |
| `GET`       | `/api/admin/clusters/{c}/servers/{endpoint}` | PING for one endpoint (VARZ/CONNZ/…) |
| `GET`       | `/api/admin/clusters/{c}/servers/{id}/{endpoint}` | targeted server query |
| `GET`       | `/api/admin/clusters/{c}/accounts/{account}/{endpoint}` | account-scoped |
| `POST`      | `…/servers/{id}/actions/reload` \| `…/lame-duck` \| `…/kick` | server actions |
| `GET`       | `/api/jsm/clusters/{c}/overview` | cached overview · `?live=true` bypasses |
| `GET`       | `/api/jsm/clusters/{c}/overview/stream` | SSE — frame per cache tick + 20s heartbeats |
| `GET\|PUT\|DELETE` | `/api/jsm/clusters/{c}/streams/{s}` | info / update / delete |
| `POST`      | `…/streams/{s}/purge?confirm=true` | drop all messages |
| `POST`      | `/api/jsm/clusters/{c}/actions/meta-stepdown?confirm=true` | force meta raft re-election |
| `POST`      | `…/streams/{s}/actions/stepdown?confirm=true` | force stream raft re-election |
| `POST`      | `…/streams/{s}/consumers/{con}/actions/stepdown?confirm=true` | force consumer raft re-election |
| `GET\|DELETE` | `…/streams/{s}/consumers/{con}` | info / delete |
| `POST\|GET` | `/mcp` | MCP Streamable HTTP transport (POST = JSON-RPC; GET = SSE keep-alive) |
| `GET`       | `/.well-known/oauth-protected-resource[/mcp]` | RFC 9728 metadata — points MCP clients at the IdP (when `auth.mcp_oauth` on) |
| `GET`       | `/api/auth/login` | redirect to OIDC IdP (auth on) |
| `GET`       | `/api/auth/callback` | OIDC redirect target |
| `POST`      | `/api/auth/logout` | clear session cookie |
| `GET`       | `/api/auth/me` | identity probe — 200 / 401 / 404 (when off) |

Destructive verbs require `?confirm=true`; without it the server returns
`428 Precondition Required`.

## Subsystems

- **Overview cache** (`internal/natsx/cache.go`) — one goroutine per
  cluster issues `$SYS.REQ.SERVER.PING.JSZ` every
  `CHESHMHAYASH_OVERVIEW_PERIOD`, caches the marshalled snapshot, and
  fans it out to SSE subscribers on buffered channels (slow consumers
  drop frames rather than block the refresher).
- **MCP** (`internal/mcp/`) — `Server.Serve` is the JSON-RPC core,
  shared between `RunStdio` and `ServeHTTP`. Tools live in `tools.go`;
  destructive ones gate on `write := os.Getenv("CHESHMHAYASH_MCP_WRITE") == "1"`.
- **Notify** (`internal/notify/`) — subscribes to
  `$JS.EVENT.ADVISORY.>` and the `$SYS.ACCOUNT.*.JETSTREAM.EVENT.ADVISORY.>`
  bridge on every cluster, classifies events in `classify.go`, sends to
  Slack/Mattermost/Matrix as `{"text": "…"}` via `webhook.go`. Best-
  effort — permission denials are logged, not fatal.
- **Auth** (`internal/auth/`) — OIDC login flow (`/api/auth/{login,
  callback,logout,me}`) with PKCE + state + nonce, allowlist gate on
  email / domain / group claims, and HMAC-SHA256-signed cookie sessions
  (no DB). Two roles: `authorize()` resolves each session to `admin`
  (read + write) or `readonly` (GET only). `admin` ⇔ identity matches the
  `[auth.access.admin]` allowlist; everyone else who passes the sign-in
  allowlist is `readonly`. When `[auth.access.admin]` is empty, every
  signed-in user is `admin` (pre-role default). The middleware enforces
  it by HTTP method — any `POST/PUT/PATCH/DELETE` under `/api/` needs the
  `admin` role (403 otherwise), so write gating is automatic for new
  routes. The role rides in `/api/auth/me` (`"role"`); the SPA hides
  destructive controls for `readonly`. **Auth mode** (`auth.mode`) selects
  how that identity is obtained: `oidc` (default) runs the login flow above;
  `jwt` (`bearer.go`) skips login entirely and validates an
  `Authorization: Bearer` access-token JWT on every request — the same
  verify → `sessionFromIDToken` → `authorize()` path as MCP OAuth, reusing
  `auth.oidc.issuer` for JWKS — for deployments fronted by a "builtin oauth"
  gateway. In jwt mode only `/api/auth/me` is registered (it resolves from
  the token and reports `"mode":"jwt"` so the SPA drops the login/logout
  affordances); there are no `/login`, `/callback`, `/logout`, or cookies.
  `MCPMiddleware` gates `/mcp`: it
  accepts a static bearer key from `auth.mcp_keys` (constant-time) and,
  when `auth.mcp_oauth.enabled`, also an OIDC **access-token JWT** from the
  same issuer as the UI — validated by `mcpVerifier` + an audience check
  (`mcp_oauth.go`), then the same `authorize()` allowlist. With OIDC-MCP on
  the server advertises OAuth 2.0 Protected Resource Metadata (RFC 9728) at
  `/.well-known/oauth-protected-resource[/mcp]` and sends
  `WWW-Authenticate: Bearer resource_metadata=…` on 401 so spec-compliant
  MCP clients self-discover the IdP; token audience is validated (RFC 8707)
  against `auth.mcp_oauth.resource` to block confused-deputy replay, so
  Keycloak must mint access tokens whose `aud` includes it. MCP **write**
  tools stay gated by `CHESHMHAYASH_MCP_WRITE` (startup env), not by
  identity — a follow-up. stdio MCP stays open. Auth is fully off when
  `auth.enabled = false` (the default).

## Tech / versions

- Go 1.26.x, stdlib `net/http` (1.22+ pattern syntax), `log/slog`
- `github.com/nats-io/nats.go v1.52.x`
- `github.com/knadh/koanf/v2` for config (struct defaults → toml → env)
- React 19, TypeScript 6, Vite 8, CodeMirror 6 (`@uiw/react-codemirror`,
  `@codemirror/lang-json`, `@codemirror/theme-one-dark`)
- Biome 2.x is the frontend lint + formatter (`frontend/biome.jsonc`,
  replaces ESLint/Prettier); `tsc -b` still owns type checking
- Runtime image: `gcr.io/distroless/static-debian12:nonroot`
- Chart published to `oci://ghcr.io/1995parham/cheshmhayash-chart`,
  image to `ghcr.io/1995parham/cheshmhayash`. Release workflow signs
  with cosign keyless; buildx already attaches SBOM + provenance.

## Conventions

- **Lint on every change** before staging:
  `golangci-lint run` (the single source of truth for Go — it also
  reports gofmt/goimports formatting and runs `go vet`; config in
  `.golangci.yml` enables ~all linters minus documented dogma), `go test
  ./...`, and in `frontend/`: `npm run ci` (Biome lint + format),
  `npm run typecheck` (`tsc -b`), `npx vite build`. Zero warnings, zero
  issues — the CI workflow runs the same set. `golangci-lint fmt` and
  `npm run lint:fix` auto-fix formatting on each side.
- Backend handlers pass NATS replies through as `json.RawMessage` —
  don't unmarshal + re-marshal, it loses field ordering and number
  precision.
- Frontend treats responses as opaque except for the fields it renders.
  Add to `frontend/src/types.ts` only when a new component needs the
  shape.
- New SYS or JS subject: add a thin method to `internal/natsx/`, expose
  via `internal/handler/`, register a tool in `internal/mcp/tools.go`,
  then call from `frontend/src/api.ts`.
- Cluster-wide aggregation (per-account / per-stream / per-consumer)
  happens client-side in `frontend/src/api.ts#aggregateOverview` from
  the single `/overview` payload — keep the server stateless.
- Chart version + appVersion bump together on every release; the
  release workflow rewrites Chart.yaml `appVersion` to match the
  pushed tag.

## Frontend dev tips

- `npm run typecheck` runs `tsc -b --noEmit` over both project
  references. Faster than `npm run build` for type errors.
- The CodeMirror bundle is the biggest part of `index-*.js` (~640 kB).
  Code-split if you ever add a route that doesn't need the editor.
- Lucide icons are tree-shakeable — import named (`import { X } from
  "lucide-react"`), never `import *`. Brand glyphs (GitHub) aren't in
  lucide; inline a small SVG (see `Footer.tsx`).
- `useOverviewStream(cluster, refreshKey)` returns
  `{overview, status, lastUpdate, lastError}`; the `<StreamStatus>` pill
  renders `live` / `polling` / `connecting` / `disconnected`.
