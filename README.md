# cheshmhayash 👀

<p align="center">
  <img src="./banner.png" alt="cheshmhayash" width="640" />
</p>

<p align="center">
  <a href="https://github.com/1995parham/cheshmhayash/actions/workflows/ci.yaml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/1995parham/cheshmhayash/ci.yaml?label=ci&logo=github&style=for-the-badge&branch=main" /></a>
  <img alt="Go" src="https://img.shields.io/github/go-mod/go-version/1995parham/cheshmhayash?style=for-the-badge&logo=go" />
  <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue?style=for-the-badge" />
</p>

## Introduction

`cheshmhayash` is a NATS administration dashboard and HTTP gateway. It talks
to your clusters **over the NATS protocol itself** — the same system-account
channels (`$SYS.REQ.*`) and JetStream API (`$JS.API.*`) that
[`natscli`](https://github.com/nats-io/natscli) uses — and exposes them as
plain HTTP + JSON for browsers, scripts, and internal tooling.

Because every endpoint is served through a single authenticated NATS
connection, you do **not** need the server's HTTP monitoring port (`:8222`)
open; only the client port (`:4222`, or wherever) is required.

## Screenshots

<table>
  <tr>
    <td width="33%">
      <a href="./docs/screenshots/01-servers.png"><img src="./docs/screenshots/01-servers.png" alt="Servers view" /></a>
      <p align="center"><sub><b>Servers</b> — per-server stats card grid with JS leader badge.</sub></p>
    </td>
    <td width="33%">
      <a href="./docs/screenshots/02-jetstream.png"><img src="./docs/screenshots/02-jetstream.png" alt="JetStream view" /></a>
      <p align="center"><sub><b>JetStream</b> — cluster-wide overview, accounts table, drill-in streams. Live SSE indicator top-right.</sub></p>
    </td>
    <td width="33%">
      <a href="./docs/screenshots/03-topology.png"><img src="./docs/screenshots/03-topology.png" alt="Topology view" /></a>
      <p align="center"><sub><b>Topology</b> — raft-group polygons connecting the servers (meta in amber, streams in green, consumers in blue).</sub></p>
    </td>
  </tr>
</table>

## Features

### Read endpoints

Responses are forwarded verbatim from the NATS server — the JSON payload is
the same one you would get from the HTTP monitoring interface.

- Cluster-wide server discovery (`$SYS.REQ.SERVER.PING`)
- Targeted per-server endpoints: `VARZ`, `CONNZ`, `ROUTEZ`, `GATEWAYZ`,
  `LEAFZ`, `SUBSZ`, `JSZ`, `ACCOUNTZ`, `HEALTHZ`, `STATSZ`
- Account-scoped endpoints: `CONNZ`, `LEAFZ`, `SUBSZ`, `JSZ`, `INFO`
- Cluster-wide JetStream overview built from
  `$SYS.REQ.SERVER.PING.JSZ` — every account, every stream, every
  consumer, in a single round-trip
- JetStream: list streams, stream info, list consumers, consumer info

### Actions

- Server: config reload, lame-duck mode, kick a connection by CID
- JetStream: **edit** stream config (CodeMirror JSON editor with syntax
  highlighting), **step down** meta/stream/consumer raft leader, purge,
  delete, delete consumer

Destructive JetStream actions require `?confirm=true`; without it the
server responds with `428 Precondition Required`.

### Real-time + LLM integration

- Per-cluster JSZ overview is refreshed in the background and pushed to
  the SPA over SSE — the panel updates in real time, with a small
  `live` / `polling` / `disconnected` indicator next to each view.
- **MCP server** baked into the same binary. Speaks the Model Context
  Protocol over both stdio (`cheshmhayash -mcp`) and HTTP Streamable
  (`POST /mcp`) so LLM agents (Claude Desktop, Cursor, etc.) can
  inspect and operate the cluster as tools. Read-only by default;
  destructive verbs gate on `CHESHMHAYASH_MCP_WRITE=1`.
- **Webhook notifications** to Slack / Mattermost / Matrix (via hookshot
  bridge): subscribes to `$JS.EVENT.ADVISORY.>` and surfaces stream /
  consumer create / delete / update / leader-elected / quorum-lost as
  chat messages. Config under `[[notify]]`.

## HTTP API

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/admin/clusters` | configured cluster names |
| `GET` | `/api/admin/clusters/{c}/servers` | PING discovery |
| `GET` | `/api/admin/clusters/{c}/servers/{endpoint}` | PING + endpoint (e.g. `VARZ`) |
| `GET` | `/api/admin/clusters/{c}/servers/{id}/{endpoint}` | targeted server query |
| `GET` | `/api/admin/clusters/{c}/accounts/{account}/{endpoint}` | account-scoped |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/reload` | reload config |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/lame-duck` | graceful drain |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/kick` | body: `{"cid": N}` |
| `GET` | `/api/jsm/clusters/{c}/overview` | cluster-wide JetStream (sys-account) · pass `?live=true` to bypass cache |
| `GET` | `/api/jsm/clusters/{c}/overview/stream` | SSE — pushes a fresh overview on every cache refresh |
| `GET` | `/api/jsm/clusters/{c}/streams?offset=N` | paginated |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}` | stream info |
| `PUT` | `/api/jsm/clusters/{c}/streams/{s}` | full `StreamConfig` update |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/purge?confirm=true` | purge all messages |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}?confirm=true` | delete stream |
| `POST` | `/api/jsm/clusters/{c}/actions/meta-stepdown?confirm=true` | force meta raft re-election |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/actions/stepdown?confirm=true` | force stream raft re-election |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers?offset=N` | list consumers |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}` | consumer info |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}?confirm=true` | delete consumer |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}/actions/stepdown?confirm=true` | force consumer raft re-election |
| `POST` | `/mcp` | MCP Streamable HTTP — JSON-RPC 2.0 in body |
| `GET` | `/mcp` | MCP SSE channel (server→client notifications) |
| `GET` | `/healthz` | liveness / readiness probe |

## Configuration

Settings are loaded from `config/default.toml`, overlaid with an optional
`settings.toml`, and finally with environment variables prefixed
`CHESHMHAYASH__` (double-underscore separates nested keys; list elements
are indexed, e.g. `CHESHMHAYASH__NATS__0__USER`).

```toml
[server]
host = "0.0.0.0"
port = 1378

# Each [[nats]] block describes a cluster to administer. The `url` points
# at the client port. Administrative endpoints ($SYS.REQ.*) require the
# connection to be authenticated against the system account.
[[nats]]
name = "local"
url = "nats://127.0.0.1:4222"
# creds_file = "./sys.creds"
# user = "admin"
# password = "changeme"
# request_timeout_ms = 2000     # single-reply request timeout
# discovery_timeout_ms = 500    # window for multi-reply collection

# Webhook notifications — one entry per chat destination. `provider` is
# one of slack | mattermost | matrix (Matrix expects a Slack-compatible
# bridge such as matrix-hookshot or maubot/webhook).
# [[notify]]
# provider = "slack"
# url = "https://hooks.slack.com/services/T000/B000/XXX"
# channel = "#nats-events"        # optional
# username = "cheshmhayash"        # optional
```

`CHESHMHAYASH_OVERVIEW_PERIOD` (Go duration, default `10s`) controls how
often the background JSZ cache refreshes. `CHESHMHAYASH_MCP_WRITE=1`
exposes the destructive MCP tools.

### System-account requirement

`$SYS.REQ.*` subjects are only routed when the connecting client is bound
to the server's system account. Without system-account credentials, admin
endpoints time out and the cluster-wide JetStream overview comes back
empty. The per-account `$JS.API.*` endpoints (list streams, stream info,
update/purge/delete, consumers) run against whichever account the
credentials grant; they will return `JetStream not enabled` (err_code
10039) on a connection bound to `$SYS`.

## Tech stack

- **Backend** — Go 1.26, stdlib `net/http` (1.22+ pattern syntax),
  `log/slog`, [`nats.go`](https://github.com/nats-io/nats.go) v1.52,
  `BurntSushi/toml` for config.
- **Frontend** — React 19 + TypeScript 6 on Vite 8. JSON editor uses
  CodeMirror 6 (`@uiw/react-codemirror`, `@codemirror/lang-json`,
  `@codemirror/theme-one-dark`).
- **Runtime image** — `gcr.io/distroless/static-debian12:nonroot` (~10 MB).

## Running

### Local

```sh
# Backend
go run .                              # serves API + built SPA on :1378

# Frontend (dev — separate terminal)
cd frontend && npm install
npm run dev                           # HMR on :5173, /api proxied to :1378
```

For a single-binary production build:

```sh
cd frontend && npm install && npm run build
go build -o ./bin/cheshmhayash .
./bin/cheshmhayash                    # http://127.0.0.1:1378
```

### Docker

```sh
docker build -t cheshmhayash .
docker run --rm -p 1378:1378 \
    -v "$PWD/settings.toml:/app/settings.toml:ro" \
    cheshmhayash
```

### Kubernetes (Helm)

The chart lives in `chart/cheshmhayash-chart/` and is published as an OCI
artifact to GHCR on every tagged release.

```sh
# install from GHCR
helm install panel \
  oci://ghcr.io/1995parham/cheshmhayash-chart \
  --version 1.0.0 \
  -f my-values.yaml

# or from a local checkout
helm install panel chart/cheshmhayash-chart -f my-values.yaml
```

Clusters and authentication mode are declared in `values.yaml`. Each entry
becomes a `[[nats]]` block. `auth` accepts exactly one of three modes —
`userPassword` (chart-managed Secret), `existingSecret` (env vars from an
external Secret), or `credsFileSecret` (a `.creds` file mounted read-only):

```yaml
clusters:
  - name: prod
    url: nats://nats.nats.svc.cluster.local:4222
    auth:
      existingSecret:
        name: nats-prod-creds
        userKey: user
        passwordKey: password
```

## Development

```sh
# backend
go fmt ./...
go vet ./...
go test -race ./...
golangci-lint run

# frontend
cd frontend
npm run typecheck
npm run lint
npm run build
```

A `docker-compose.yml` is included for spinning up a local NATS server
with monitoring enabled, so the dashboard has something to talk to.

## License

Free and open source **forever**, just like
[NATS](https://nats.io). Released under the
[MIT License](LICENSE) — see the file for details.

---

<p align="center">
  Built with ❤️ by <a href="https://github.com/1995parham">@1995parham</a> ·
  <a href="https://github.com/1995parham/cheshmhayash">1995parham/cheshmhayash</a>
</p>
