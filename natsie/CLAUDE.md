# CLAUDE.md

Project notes for Claude / Claude Code. Keep this short and current; prefer
deleting stale lines over qualifying them.

## What this is

`natsie` is a NATS operations tool: scan one or more JetStream clusters for
consumers/streams/peers that probably shouldn't be there, emit a hand-editable
YAML manifest, and apply deletions only after explicit human approval. The
same binary also runs as a long-lived bot (`bot serve`) with scheduled scans,
chat notifications, signed approval URLs, and a JSONL audit log.

Core invariant: **auto-delete is opt-in, never the default**. Detection and
reporting run unattended. Deletion defaults to a `scan → edit → apply` flow;
it happens automatically only where someone turned it on — by passing
`consumer scan --delete` or setting `auto_delete: true` on a bot schedule. Whichever path is taken, `apply` re-verifies every consumer
immediately before deleting it (anything that became active between scan and
apply is preserved) and refuses outright to delete a consumer owned by an
external controller.

## Layout

```
.
├── cmd/natsie/                     # binary entrypoint (main.go only)
├── internal/
│   ├── cmd/                        # urfave/cli v3 command tree
│   │   ├── root.go                 # top-level app
│   │   ├── consumer/               # `consumer scan` / `consumer apply` / `consumer owner`
│   │   └── bot/                    # `bot serve`
│   ├── infra/
│   │   ├── config/                 # koanf loader (struct defaults → yaml → env)
│   │   └── natsctx/                # ~/.config/nats/context reader + dialer
│   ├── scanner/                    # consumer classification, rename detection, stream report, peer/ghost check
│   ├── manifest/                   # YAML manifest read/write + schema
│   ├── cleanup/                    # delete via $JS.API.CONSUMER.DELETE + re-verify
│   ├── protect/                    # veto: consumers owned by NACK/Terraform/GitOps
│   ├── audit/                      # JSONL audit log append
│   ├── chatops/                    # slash commands, approval tokens, render
│   ├── owners/                     # stream-prefix → owner/channel routing (bot serve)
│   └── version/                    # build info (carlmjohnson/versioninfo)
├── chart/                          # Helm chart for `bot serve`
├── docker-compose.yml              # local NATS sidecar for dev
├── Dockerfile                      # multi-stage; distroless runtime
├── justfile                        # build / test / lint / tidy / dev
└── .golangci.yml                   # default: all + small disable list
```

## Build & run

```sh
just build                 # → ./natsie
just test                  # go test -race -covermode=atomic
just lint                  # golangci-lint run
just tidy                  # go mod tidy
just dev up                # local NATS via docker compose (port 4222)
just dev down              # stop + remove volumes
just docker                # docker build -t ghcr.io/1995parham/natsie:dev .
```

Or directly:

```sh
go build -o natsie ./cmd/natsie
go test -race ./...
```

## Subcommands

| Command | Status | Purpose |
| --- | --- | --- |
| `consumer scan` | working | Enumerate consumers; classify active/stale/abandoned; cross-cluster peer aware. Emits TSV, JSON, or a YAML cleanup manifest. `--delete` deletes the stale rows directly (re-verified, protection still applies); `--dry-run` previews. |
| `consumer apply` | working | Apply a manifest produced by `scan`. Re-verifies each consumer; preserves anything active since `generated_at`. Supports `--dry-run` and `-` (manifest from stdin). |
| `consumer owner` | working | Resolve which cluster/consumer owns a `filter_subject` across all configured contexts (active-first). Exact-match on the normalized filter subject. |
| `peer check` | working | Aggregate every stream/consumer Raft group's membership; flag GHOST peers (offline in every group, leading none). Streams + consumers; phantom orphaned Raft groups (needs system-account access) not yet covered. |
| `bot serve` | working | Long-running daemon: cron-scheduled scans, chat sinks, HTTP listener, slash-command handler, signed approval URLs, JSONL audit log. |
| `stream report` | working | Per-stream retention/limits, replication, size, consumer count, replica placement; pretty mode adds per-server placement-skew. |

## Connection model

`natsie` reads from the same `~/.config/nats/context/*.json` files that
`nats context` uses — no separate credential handling. Inside Kubernetes,
mount those JSON files as a Secret at `/root/.config/nats/context/`. The
binary does not run as its own NATS account; it borrows whatever the
operator already trusts.

## Bot mode

`bot serve` is single-writer by design — do not scale the Deployment beyond
`replicas: 1`. The schedule loop and file store both assume one instance.

Config lives at `~/.config/natsie/config.yaml` (or `/etc/natsie/config.yaml`
in production). Notify sinks are URL-shaped: `mattermost://`, `slack://`,
`webhook://`, `stdout://`.

`signing_key` does two jobs: (1) slash-command verification token compared
with `crypto/subtle.ConstantTimeCompare`, (2) HMAC-SHA256 key for approval
URLs — every link is bound to its manifest ID so a leaked URL can only
approve the one manifest it was issued for.

### HTTP endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | liveness/readiness |
| `GET` | `/metrics` | Prometheus metrics (scan duration/candidates, apply outcomes, approval latency); only registered when metrics are enabled |
| `GET` | `/manifest/{id}` | stored manifest as `application/yaml` |
| `POST` | `/slash` | slash-command handler (`list`, `show <id>`, `help`); token-protected |
| `GET` | `/approve/{id}?token=...` | plain-text preview |
| `POST` | `/approve/{id}?token=...` | re-verify + apply; JSON summary |

### Chat integrations

Mattermost is supported via two paths. Pick based on whether the chat
server can reach the bot:

- **Push (slash command)** — chat POSTs `/slash` on the bot. Needs a public
  or chat-reachable ingress.
- **Pull (WebSocket)** — bot opens an outbound WebSocket to chat with
  `model.NewWebSocketClient4`, filters `posted` events, replies via REST.
  No inbound route needed. Use this when the bot lives behind ingress the
  chat server cannot reach.

Pull-mode requires the bot account to be a member of every team and channel
it should listen on; being a webhook target is not enough.

## Conventions

- **urfave/cli v3** for commands. New subcommands go under `internal/cmd/<group>/`
  and get wired into `internal/cmd/root.go`.
- **koanf** for config: struct defaults → yaml file → env overlay. Env keys
  use the standard koanf env provider convention.
- **NATS deletion** goes through the raw `$JS.API.CONSUMER.DELETE` subject,
  not the jsm.go helper — this lets leading-dash consumer names work.
- **Every deletion path funnels through `cleanup.Apply`.** CLI apply,
  `scan --delete`, bot `auto_delete`, and the signed-approval handler all
  call it, which is why re-verification and the `internal/protect` veto are
  enforced there rather than at scan time — there is no way to edit around
  them.
- **Lint clean before staging.** `just lint && just test`; CI runs the same set.
- **Audit log is append-only.** The bot opens it in append mode and does
  not hold an exclusive lock; rotate with logrotate.
- **No vendor lock-in in the binary.** Connection (NATS contexts), rules,
  notification sinks, and approval flows are pluggable. Operator-specific
  opinions live in config, not source.

## Versions

- Go 1.27.x
- `github.com/nats-io/nats.go` + `github.com/nats-io/jsm.go`
- `github.com/urfave/cli/v3` (command tree)
- `github.com/knadh/koanf/v2` (config)
- `github.com/labstack/echo/v5` (HTTP listener in `bot serve`)
- `github.com/robfig/cron/v3` (schedules)
- `github.com/prometheus/client_golang` (metrics on `/metrics`)
- `github.com/mattermost/mattermost/server/public` (pull-mode WebSocket client)
- Runtime image: distroless
- Chart published from `chart/`; image to `ghcr.io/1995parham/natsie`
