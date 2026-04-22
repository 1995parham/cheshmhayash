# cheshmhayash

![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/1995parham/cheshmhayash/ci.yaml?label=ci&logo=github&style=flat-square&branch=main)

## Introduction

`cheshmhayash` is a NATS administration dashboard and HTTP gateway. It talks
to your clusters **over the NATS protocol itself** — the same system-account
channels (`$SYS.REQ.*`) and JetStream API (`$JS.API.*`) that
[`natscli`](https://github.com/nats-io/natscli) uses — and exposes them as
plain HTTP + JSON for browsers, scripts, and internal tooling.

```
  o
 -|-   --- cheshmhayash --- NATS (cluster)
 /\
```

Because every endpoint is served through a single authenticated NATS
connection, you do **not** need the server's HTTP monitoring port
(`:8222`) open; only the client port (`:4222`, or wherever) is required.

## Features

### Read endpoints

Responses are forwarded verbatim from the NATS server — the JSON payload is
the same one you would get from the HTTP monitoring interface.

- Cluster-wide server discovery (`$SYS.REQ.SERVER.PING`)
- Targeted per-server endpoints: `VARZ`, `CONNZ`, `ROUTEZ`, `GATEWAYZ`,
  `LEAFZ`, `SUBSZ`, `JSZ`, `ACCOUNTZ`, `HEALTHZ`, `STATSZ`
- Account-scoped endpoints: `CONNZ`, `LEAFZ`, `SUBSZ`, `JSZ`, `INFO`
- JetStream: list streams, stream info, list consumers, consumer info

### Actions

- Server: config reload, lame-duck mode, kick a connection by CID
- JetStream: purge stream, delete stream, delete consumer

Destructive JetStream actions require `?confirm=true`; without it the
server responds with `428 Precondition Required`.

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
| `GET` | `/api/jsm/clusters/{c}/streams?offset=N` | paginated |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}` | stream info |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/purge?confirm=true` | purge all messages |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}?confirm=true` | delete stream |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers?offset=N` | list consumers |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}` | consumer info |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}?confirm=true` | delete consumer |
| `GET` | `/healthz` | liveness / readiness probe |

## Configuration

Settings are loaded from `config/default.toml`, overlaid with an optional
`settings.toml`, and finally with environment variables prefixed
`CHESHMHAYASH__` (double-underscore separates nested keys).

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
# user = "sys"
# password = "changeme"
# request_timeout_ms = 2000     # single-reply request timeout
# discovery_timeout_ms = 500    # window for multi-reply collection
```

### System-account requirement

`$SYS.REQ.*` subjects are only routed when the connecting client is bound
to the server's system account. Without system-account credentials, admin
endpoints will time out. JetStream endpoints (`$JS.API.*`) work against
whichever account the credentials grant you access to.

## Running

### Local

```sh
cargo run --release
```

Point your NATS server at the configured URL and hit
`http://127.0.0.1:1378/api/admin/clusters/local/servers` to see discovery
replies.

### Docker

```sh
docker build -t cheshmhayash .
docker run --rm -p 1378:1378 \
    -v "$PWD/settings.toml:/app/settings.toml:ro" \
    cheshmhayash
```

### Kubernetes (Helm)

The chart lives in `chart/cheshmhayash/`. Clusters are declared via
`values.yaml`:

```yaml
config:
  servers:
    - name: prod
      url: nats://nats.nats.svc.cluster.local:4222
      credsFile: /etc/cheshmhayash/sys.creds
```

## Development

```sh
cargo fmt --all
cargo clippy --all-targets -- -D warnings
cargo build --release
```

A `docker-compose.yml` is included for spinning up a local NATS server
with monitoring enabled, so the dashboard has something to talk to.

## License

MIT
