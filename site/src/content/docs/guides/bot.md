---
title: Operations bot
description: Run JetStream scans on a schedule, post the results to chat, and approve a consumer cleanup with a signed link.
---

`cheshmhayash bot serve` is a long-running daemon. It runs the [operations commands](/cheshmhayash/cli/consumer/) on a schedule, posts the results to chat, and lets a person approve a cleanup with one click. It writes every action to an audit log.

The bot does what a person does with `consumer scan` and `consumer apply`, with the same safety rules:

- **Auto-delete is opt-in.** A scan proposes a manifest. Nothing is deleted until a person approves it, unless a schedule has `auto_delete: true`.
- **Every deletion re-checks the consumer** against the live cluster just before it deletes it.
- **Protected consumers are never deleted**, whatever the manifest says.

## Configuration

The bot reads the ops config (`--ops-config`, default `~/.config/cheshmhayash/ops.yaml`) and NATS contexts from `~/.config/nats/context/`.

```yaml
bot:
  schedules:
    # kind defaults to consumer-stale: the only kind that makes a manifest
    # and an approve link. The other kinds are reports.
    - name: daily
      cron: "0 3 * * *"
      context: prod-a
      peer_context: prod-b
      min_pending: 10000
      min_idle: 24h
    - name: ghost-peers
      kind: peer-check           # peers offline in every Raft group
      cron: "*/30 * * * *"
      context: prod-a
    - name: unbounded-streams
      kind: stream-unlimited     # streams with no retention limit
      cron: "0 6 * * *"
      context: prod-a
    - name: replication
      kind: stream-report        # streams with fewer than 2 replicas
      cron: "0 7 * * *"
      context: prod-a
    - name: nightly-sweep
      cron: "0 4 * * *"
      context: prod-a
      min_pending: 10000
      min_idle: 168h
      auto_delete: true          # delete, do not propose; still re-checks and protects

  notify:
    - mattermost://chat.example.com/hooks/abc-xyz?channel=nats-ops
    # - slack://hooks.slack.com/services/T.../B.../...
    # - webhook://hooks.example.com/n8n/abc   # JSON: {title, body, manifest_id, link}
    # - stdout://

  store: file:///var/lib/cheshmhayash/manifests
  audit_log: /var/lib/cheshmhayash/audit.jsonl

  http:
    listen: ":8080"
    base_url: https://nats-bot.example.com   # the public URL in chat links

  signing_key: change-me-to-32-random-bytes

  # Optional: send the part of each manifest that a team owns to that team.
  # Owners get visibility only; the approve link goes to the global notify list.
  owners:
    - name: orders
      streams: [orders, orders-dlq]
      consumer_prefix: [orders-]
      notify:
        - mattermost://chat.example.com/hooks/def-uvw?channel=orders-oncall

  # Optional: listen in a Mattermost channel (see "Chat transports").
  mattermost:
    enabled: false
    server: https://chat.example.com
    token_file: /etc/cheshmhayash/mattermost-token
    team: platform
    channel: nats-ops
    trigger: "!cheshmhayash"
```

Environment variables that start with `CHESHMHAYASH_OPS_`, with `__` between sections, override the file. For example: `CHESHMHAYASH_OPS_DEFAULTS__MIN_PENDING=10000`.

## Schedule kinds

| Kind | What it does |
| --- | --- |
| `consumer-stale` (default) | Scans for stale consumers. Stores a manifest and posts a signed approve link. |
| `peer-check` | Reports ghost peers. |
| `stream-unlimited` | Reports streams with no retention limit. |
| `stream-report` | Reports streams with fewer than two replicas, with a placement-skew summary. |

The report kinds never delete anything: cheshmhayash never deletes a stream or removes a peer.

## The approval flow

1. A `consumer-stale` schedule runs, or someone types `scan <ctx>` in chat.
2. The bot stores the manifest and posts a summary with a link: `https://<base_url>/approve/<id>?token=<hmac>`.
3. A person opens the link. A `GET` shows a plain-text preview of what would be deleted.
4. The person confirms. A `POST` re-checks every consumer against the live cluster, applies the protect rules, deletes the rest, and returns a summary.

The token is an HMAC-SHA256 of the manifest ID with `signing_key`. A leaked link can approve only the one manifest it was made for.

## Chat commands

Both chat transports share one set of commands:

```text
list                      list stored manifest IDs
show <id>                 preview a manifest
clusters                  list the NATS contexts this bot can reach
streams [ctx]             list streams (one context, or all)
stream <ctx> <name>       one stream in detail
last <ctx> <stream> [sub] metadata of the last message on a subject
consumers <ctx> <stream>  all consumers of a stream, unfiltered
usage [ctx]               total footprint and the largest streams
cluster <ctx>             connected server, peers and account
scan <ctx> [stream]       scan now; replies with a signed approve link
help                      this list
```

## Chat transports

Pick one by whether your chat server can reach the bot.

**Push (slash command).** The chat server sends a `POST` to `/slash` on the bot, so it needs a route to the bot. Set up a `/cheshmhayash` slash command in Mattermost or Slack with `signing_key` as its token:

```text
/cheshmhayash list
/cheshmhayash scan prod-a
```

**Pull (WebSocket).** The bot opens an outbound WebSocket to Mattermost with a bot-account token. It watches one channel for messages that start with `trigger` and answers over REST. Use it when the chat server cannot reach the bot:

```text
!cheshmhayash list
!cheshmhayash scan prod-a
```

The bot account must be a member of the team and of the channel. If Mattermost goes down, the listener retries with backoff, and the scheduler keeps running.

## HTTP endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | `{"status":"ok"}` for probes. |
| `GET` | `/metrics` | Prometheus metrics: scan duration and candidates, apply outcomes, approval latency, build info. |
| `GET` | `/manifest/{id}` | The stored manifest as YAML. |
| `POST` | `/slash` | The slash-command handler. Needs the token. |
| `GET` | `/approve/{id}?token=...` | A preview of what would be deleted. |
| `POST` | `/approve/{id}?token=...` | Re-checks and applies. Returns a JSON summary. |

## Audit log

Each scan, preview and apply is one JSON line in `audit_log`. The bot opens the file in append mode and holds no lock, so you can rotate it with logrotate.

## On Kubernetes

Set `bot.enabled: true` in the Helm chart. It adds a single-replica Deployment with the `Recreate` strategy, a PVC for the manifest store and audit log, a Service, and optionally a ServiceMonitor, Ingress or HTTPProxy, and NetworkPolicy. NATS contexts come from `bot.natsContexts` (inline JSON, an existing Secret, or rendered from env at start).

:::caution
Do not run more than one replica. The scheduler, the store and the audit log assume one writer.
:::
