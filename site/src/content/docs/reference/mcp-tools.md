---
title: MCP tools
description: Every tool of the cheshmhayash MCP server, with its arguments and the flag that registers it.
---

Every tool takes `cluster`: a cluster name from `settings.toml`. Call `cluster_list` to see them.

## Read tools

These are always registered.

| Tool | Arguments | What it does |
| --- | --- | --- |
| `cluster_list` | | Lists the configured cluster names. |
| `server_ping` | `cluster` | Sends `$SYS.REQ.SERVER.PING` and returns every server's reply. Use it to list the servers. |
| `server_ping_endpoint` | `cluster`, `endpoint` | Sends `$SYS.REQ.SERVER.PING.<endpoint>` to every server. `VARZ` for config and uptime, `CONNZ` for clients, `JSZ` for JetStream, `HEALTHZ` for liveness. |
| `server_endpoint` | `cluster`, `server_id`, `endpoint` | Asks one server (`$SYS.REQ.SERVER.<id>.<endpoint>`). |
| `account_endpoint` | `cluster`, `account`, `endpoint` | Sends `$SYS.REQ.ACCOUNT.<account>.<endpoint>`. Needs a system-account connection. |
| `jsm_overview` | `cluster` | The cluster-wide JetStream overview: one JSZ reply per server, with accounts, streams, consumers, config and Raft. Large on a busy cluster. |
| `jetstream_health` | `cluster`, `min_idle`, `min_pending` | The findings of the [Health tab](/cheshmhayash/dashboard/health/), most severe first. `min_idle` is a Go duration (default `24h`); `min_pending` defaults to 1. |
| `stream_list` | `cluster`, `offset` | Lists streams in the account of the cluster's credentials, a page at a time. |
| `stream_info` | `cluster`, `stream` | The full stream state: replicas, consumers and storage. |
| `consumer_list` | `cluster`, `stream`, `offset` | Lists the consumers of a stream, a page at a time. |
| `consumer_info` | `cluster`, `stream`, `consumer` | A consumer's state: ack pending, redeliveries and more. |

## Write tools

Registered only with `mcp --write` (`CHESHMHAYASH_MCP_WRITE=1`). Most of them also need `confirm: true` in the call.

| Tool | Arguments | What it does |
| --- | --- | --- |
| `server_reload` | `cluster`, `server_id` | Reloads one server's configuration. |
| `server_lame_duck` | `cluster`, `server_id`, `confirm` | Puts a server in lame-duck mode. Connections move to peers, and new ones are refused. |
| `server_kick` | `cluster`, `server_id`, `cid`, `confirm` | Disconnects one client by its connection ID from `CONNZ`. |
| `stream_update` | `cluster`, `stream`, `config` | Sends a full `StreamConfig`. NATS refuses partial updates, and some fields (storage, retention) cannot change without a new stream. |
| `stream_purge` | `cluster`, `stream`, `confirm` | Removes every message. The config and the consumers stay. |
| `stream_delete` | `cluster`, `stream`, `confirm` | Deletes a stream. This cannot be undone. |
| `consumer_delete` | `cluster`, `stream`, `consumer`, `confirm` | Deletes a consumer. |
| `meta_stepdown` | `cluster`, `confirm` | Forces the JetStream meta leader to step down. Useful before you drain its node. |
| `stream_stepdown` | `cluster`, `stream`, `confirm` | Forces a stream's Raft leader to step down. Useful only on replicated streams. |
| `consumer_stepdown` | `cluster`, `stream`, `consumer`, `confirm` | Forces a consumer's Raft leader to step down. |

## Sampling tool

Registered only with `mcp --allow-sampling` (`CHESHMHAYASH_MCP_SAMPLE=1`).

| Tool | Arguments | What it does |
| --- | --- | --- |
| `subject_sample` | `cluster`, `subject`, `count`, `timeout_seconds`, `type` | Subscribes on core NATS for a short time and returns up to `count` messages (default 5, at most 50), decoded with the [schemas](/cheshmhayash/guides/schemas/). It waits at most `timeout_seconds` (default 5, at most 30). Wildcards work. It creates no JetStream consumer. |

The connection's account decides which messages are visible. A system-account connection sees only `$SYS` subjects.
