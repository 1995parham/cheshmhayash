---
title: Command line overview
description: The cheshmhayash command tree, the global flags and how the client and operations commands connect to NATS.
---

Every part of cheshmhayash is a subcommand of one binary:

```text
cheshmhayash serve                 the dashboard (API, SPA, notifier)
cheshmhayash mcp                   the MCP server
cheshmhayash sub | pub             protobuf-aware subscribe and publish
cheshmhayash schema ...            inspect schemas and subject rules
cheshmhayash consumer scan|apply|owner
cheshmhayash peer check
cheshmhayash stream report
cheshmhayash bot serve             the operations bot
```

Run any command with `--help` for its flags.

## Global flags

| Flag | Env | Default | Meaning |
| --- | --- | --- | --- |
| `--log-level` | `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. An unknown value is fatal. |
| `--config`, `-c` | `CHESHMHAYASH_CONFIG` | `settings.toml` | The settings file (TOML) of `serve`, `mcp`, `sub`, `pub` and `schema`. |
| `--ops-config` | `CHESHMHAYASH_OPS_CONFIG` | `~/.config/cheshmhayash/ops.yaml` | The YAML file of the operations commands (`consumer`, `peer`, `stream`, `bot`). |

## Two ways to connect

The commands come in two families, and they connect in different ways.

**Client commands** (`sub`, `pub`, `schema`) use a cluster from `settings.toml`, or a URL on the command line, so they work with no settings file:

| Flag | Env | Meaning |
| --- | --- | --- |
| `--cluster`, `-C` | `CHESHMHAYASH_CLUSTER` | A cluster name from the settings file. Optional when only one is configured. |
| `--server`, `-s` | `NATS_URL` | A NATS URL. It overrides `--cluster`. |
| `--user` | `NATS_USER` | The user name. |
| `--password` | `NATS_PASSWORD` | The password. |
| `--creds` | `NATS_CREDS` | A credentials file. |
| `--schemas` | `CHESHMHAYASH_SCHEMAS` | An extra schemas file. Repeat it for more. |

**Operations commands** (`consumer`, `peer`, `stream`, `bot`) use NATS contexts: the `~/.config/nats/context/*.json` files that `nats context` writes. You pass `--context prod-a`, and the credentials come from that file. They need no separate credential handling.

## The pages here

- [sub and pub](/cheshmhayash/cli/pub-sub/): subscribe and publish with protobuf.
- [schema](/cheshmhayash/cli/schema/): inspect sources, messages and subject rules.
- [consumer](/cheshmhayash/cli/consumer/): find stale consumers and clean them up safely.
- [peer and stream](/cheshmhayash/cli/peer-stream/): ghost peers and stream reports.
- The `serve`, `mcp` and `bot serve` commands are in the [deploy](/cheshmhayash/guides/deploy/), [MCP](/cheshmhayash/guides/mcp/) and [bot](/cheshmhayash/guides/bot/) guides.
