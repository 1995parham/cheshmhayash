# natsie

A Swiss-army knife for NATS operations: report on, diagnose, and (with explicit human approval) clean up consumers, streams, and cluster state across one or many JetStream clusters.

`natsie` is built for the ops engineer who has dozens of NATS contexts, recurring cluster events, and consumers that quietly outlive the services that created them. It is **never autonomous** — every destructive action requires an explicit manifest + apply step. Detection and reporting run unattended; deletion does not.

## Why another NATS tool

The ecosystem has `nats` (the official CLI), `nats-top`, and `nats-surveyor` — all focused on inspection, benchmarking, or live metrics. None of them answer the operational question that comes up after every cluster upgrade or service migration:

> Which consumers, streams, or peers are still here that *probably shouldn't be*, and what's the safest way to remove them?

`natsie` is the answer to that question. It treats cleanup like a code review: scan → propose → human approves → apply, with an audit trail.

## Status

Early. The first working subcommand is `natsie consumer scan` — the rest of the surface area is planned (see [ROADMAP](docs/ROADMAP.md)).

## Subcommands (current and planned)

| Command | Status | Purpose |
| --- | --- | --- |
| `consumer scan` | **working** | Enumerate consumers across one or more contexts; classify as active / stale / abandoned with cross-cluster peer awareness; emit TSV/JSON. |
| `consumer apply` | planned | Apply a delete-manifest produced by `scan`, with confirmation. |
| `stream report` | planned | Per-stream size, retention, replication, and ownership signals. |
| `peer check` | planned | Detect ghost peers / phantom Raft groups from past shrinks. |
| `bot serve` | planned | Long-running daemon: periodic scans, chat notifications (Slack/Mattermost/webhook), `/approve` slash command flow. |

## Design pillars

1. **Never auto-deletes.** Destructive actions always require an explicit `apply <manifest>` step, and the manifest is human-readable.
2. **Cross-cluster aware.** Many production deployments run NATS in pairs or N-way groups; "consumer X is stale here, but active on the peer" is a first-class signal.
3. **Fuzzy peer matching.** Consumer name conventions drift — region suffixes, environment tags, service renames. The peer check should match by `filter_subject` and recent activity, not just identical names.
4. **No vendor lock-in.** Connection (NATS contexts), rules, notification sinks, and approval flows are all pluggable. Snapp-shaped opinions live in private config, not the binary.

## Install

```bash
go install github.com/1995parham/natsie/cmd/natsie@latest
```

Or from source, with [just](https://just.systems):

```bash
git clone https://github.com/1995parham/natsie && cd natsie
just build
```

## Quick start

```bash
# Scan one cluster, emit TSV to stdout
natsie consumer scan --context snapp-js-main-teh1

# Scan with cross-cluster peer awareness
natsie consumer scan --context snapp-js-main-teh1 --peer-context snapp-js-main-teh2

# Only report consumers idle > 24h with > 10k pending
natsie consumer scan --context snapp-js-main-teh1 \
  --min-idle 24h --min-pending 10000

# Emit JSON for piping to other tools
natsie consumer scan --context snapp-js-main-teh1 --format json
```

`natsie` reads from the same `~/.config/nats/context/*.json` files that `nats context` uses — no separate credential handling.

## Project layout

```
.
├── cmd/natsie/         # binary entrypoint (main.go)
├── internal/
│   ├── cmd/            # urfave/cli v3 command tree
│   │   └── consumer/   # consumer subcommands (scan, ...)
│   ├── infra/          # infrastructure adapters
│   │   ├── config/     # koanf-based config loader
│   │   └── natsctx/    # ~/.config/nats/context reader + dialer
│   └── scanner/        # stream/consumer classification
├── .github/workflows/  # lint, test, build, codeql
├── docs/ROADMAP.md
├── justfile            # just recipes (build, test, lint, tidy, update)
└── .golangci.yml       # linter config
```

## License

Apache-2.0. See [LICENSE](LICENSE).
