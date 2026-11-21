---
title: Introduction
description: What cheshmhayash is, what it needs from your NATS cluster, and how to run it with Go, Docker or Helm.
---

cheshmhayash is a NATS administration tool in one binary. Each part is a subcommand:

| Subcommand | What it does |
| --- | --- |
| `serve` | The HTTP dashboard: the JSON API, the React panel and the webhook notifier. |
| `mcp` | The MCP server for LLM agents, over stdio or Streamable HTTP. |
| `sub`, `pub`, `schema` | Protobuf-aware client commands, with schemas loaded at run time. |
| `consumer`, `peer`, `stream` | JetStream operations: find and safely clean up stale consumers, ghost peers and risky streams. |
| `bot serve` | The operations bot: scheduled scans, chat messages and signed approvals. |

The name is Persian for "the eyes".

## What it needs

cheshmhayash connects to the NATS **client port** (`:4222` by default). It does not need the HTTP monitoring port (`:8222`).

The dashboard and the MCP server read cluster-wide data from `$SYS.REQ.*` subjects. The server routes these subjects only to a connection that is bound to the **system account**. So give the cluster entry system-account credentials: a user and password, or a `.creds` file.

Without system-account credentials, server discovery times out and the JetStream overview is empty. The per-account `$JS.API.*` calls (stream info, update, purge, consumers) run in the account that the credentials belong to. On a `$SYS` connection they answer `JetStream not enabled` (error code 10039).

## Run it from source

You need Go 1.27 and Node.js.

```sh
git clone https://github.com/1995parham/cheshmhayash
cd cheshmhayash

cd frontend && npm install && npm run build && cd ..
go build -o ./bin/cheshmhayash ./cmd/cheshmhayash
```

Write a `settings.toml` next to the binary:

```toml
[server]
host = "127.0.0.1"
port = 1378

[[nats]]
name = "local"
url = "nats://127.0.0.1:4222"
user = "admin"
password = "changeme"
```

Then start the dashboard and open `http://127.0.0.1:1378`:

```sh
./bin/cheshmhayash serve
```

For frontend work, run the Vite dev server. It proxies `/api` to the Go process on `:1378`:

```sh
go run ./cmd/cheshmhayash serve       # terminal 1
cd frontend && npm run dev            # terminal 2, UI on :5173
```

## Run it with Docker

The image holds the one binary. Its entrypoint is `/app/cheshmhayash` and its default command is `serve`.

```sh
docker run --rm -p 1378:1378 \
  -v "$PWD/settings.toml:/app/settings.toml:ro" \
  ghcr.io/1995parham/cheshmhayash

# the MCP server from the same image
docker run --rm -p 8080:8080 \
  -v "$PWD/settings.toml:/app/settings.toml:ro" \
  ghcr.io/1995parham/cheshmhayash mcp --http --addr :8080
```

The repository also has a `docker-compose.yml` that starts a local NATS server, so the dashboard has something to talk to.

## Run it on Kubernetes

The Helm chart is published to GHCR on each release:

```sh
helm install panel oci://ghcr.io/1995parham/cheshmhayash-chart -f my-values.yaml
```

A minimal `my-values.yaml`:

```yaml
clusters:
  - name: prod
    url: nats://nats.nats.svc.cluster.local:4222
    auth:
      existingSecret:
        name: nats-sys-creds
        userKey: user
        passwordKey: password
```

The [deploy guide](/cheshmhayash/guides/deploy/) covers the MCP workload, the bot, the tap, ingress and auth.

## Next steps

- Click through the [live demo](/cheshmhayash/demo/) to see the dashboard with sample data.
- Read the [dashboard tour](/cheshmhayash/dashboard/).
- Decode your messages with [protobuf schemas](/cheshmhayash/guides/schemas/).
- Connect Claude through the [MCP server](/cheshmhayash/guides/mcp/).
