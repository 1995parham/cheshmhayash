# Operations commands

The operations commands of cheshmhayash (`consumer`, `peer`, `stream` and `bot`) report on, diagnose, and (with explicit human approval) clean up consumers, streams, and cluster state across one or many JetStream clusters.

cheshmhayash is built for the ops engineer who has dozens of NATS contexts, recurring cluster events, and consumers that quietly outlive the services that created them. **Auto-delete is supported and off by default.** Out of the box, cleanup goes through a manifest a human reviews. Turn on `--delete` or `auto_delete: true` and cheshmhayash will remove stale consumers on its own — with the same re-verification against live state microseconds before each deletion, and the same refusal to touch consumers owned by an external controller.

## Why another NATS tool

The ecosystem has `nats` (the official CLI), `nats-top`, and `nats-surveyor` — all focused on inspection, benchmarking, or live metrics. None of them answer the operational question that comes up after every cluster upgrade or service migration:

> Which consumers, streams, or peers are still here that *probably shouldn't be*, and what's the safest way to remove them?

cheshmhayash is the answer to that question. It treats cleanup like a code review: scan → propose → human approves → apply, with an audit trail.

## Subcommands

| Command | Status | Purpose |
| --- | --- | --- |
| `consumer scan` | **working** | Enumerate consumers across one or more contexts; classify as active / stale / abandoned with cross-cluster peer awareness; emit TSV/JSON, optionally a YAML cleanup manifest. `--delete` removes the stale rows directly, skipping the manifest round-trip. |
| `consumer apply` | **working** | Apply a delete-manifest produced by `scan`, re-verifying each consumer first. Supports `--dry-run` and `-` (read manifest from stdin). |
| `consumer owner` | **working** | Resolve which cluster/consumer currently owns a `filter_subject` across all configured contexts (active-first), to find where a stale consumer's work moved. TSV/JSON/pretty. |
| `peer check` | **working** | Walk every stream/consumer Raft group and aggregate each server's standing; flag GHOST peers (offline in every group, leading none) from a `peer-remove` that never happened. TSV/JSON/pretty, `--ghosts-only`. |
| `stream report` | **working** | Per-stream retention/limits, replication factor, size, consumer count, and replica placement; pretty mode adds a per-server placement-skew summary. TSV/JSON/pretty, `--stream` filter. |
| `bot serve` | **working** | Long-running daemon: scheduled scans, chat notifications (Slack / Mattermost / stdout), HTTP listener with manifest viewer, slash-command handler, signed approval URLs, JSONL audit log. |

## Design pillars

1. **Auto-delete is opt-in.** The default flow proposes; it does not act. Deletion happens when you approve a manifest, pass `consumer scan --delete`, or set `auto_delete: true` on a schedule. Every one of those paths re-verifies immediately before deleting, so a consumer that woke up since the scan is preserved — including on the unattended path.
2. **Cross-cluster aware.** Many production deployments run NATS in pairs or N-way groups; "consumer X is stale here, but active on the peer" is a first-class signal.
3. **Rename- and move-aware.** Consumer name conventions drift — region suffixes, environment tags, service renames. `scan` flags a stale consumer as a likely rename (`renamed_to`) when another consumer on the same stream filters the same `filter_subject` and is still active; `consumer owner` chases the same `filter_subject` across every configured context to find where the work moved, so a migration isn't mistaken for an abandoned consumer.
4. **Respects other owners.** A consumer declared by a Kubernetes operator (NACK), Terraform, or a GitOps pipeline is never cheshmhayash's to delete. Deleting one out of band either fights the reconciler forever or silently drifts the cluster from its declared state, so cheshmhayash refuses — see [Working alongside NACK](#working-alongside-nack-and-other-declarative-owners).
5. **No vendor lock-in.** Connection (NATS contexts), rules, notification sinks, and approval flows are all pluggable. Operator-specific opinions live in config, not the binary.

## Configuration file

These commands read the same `~/.config/nats/context/*.json` files that `nats context` writes, and a YAML file of their own: scan defaults, context topology (`peer`), protection rules and the bot. The global `--ops-config` flag (or `CHESHMHAYASH_OPS_CONFIG`) names it; the default is `~/.config/cheshmhayash/ops.yaml`, and `~/.config/natsie/config.yaml` is read when it is the only one (these commands used to be the natsie tool). Environment variables prefixed `CHESHMHAYASH_OPS_`, with `__` between sections, override it, for example `CHESHMHAYASH_OPS_DEFAULTS__MIN_PENDING=10000`.

## Quick start

```bash
# Scan one cluster, emit TSV to stdout
cheshmhayash consumer scan --context prod-a

# Scan with cross-cluster peer awareness
cheshmhayash consumer scan --context prod-a --peer-context prod-b

# Only report consumers idle > 24h with > 10k pending
cheshmhayash consumer scan --context prod-a \
  --min-idle 24h --min-pending 10000

# Emit JSON for piping to other tools
cheshmhayash consumer scan --context prod-a --format json

# Where did this filter subject's work move to? (searches every context)
cheshmhayash consumer owner "rides.trip.>"

# Ghost peers: offline in every Raft group and leading none
cheshmhayash peer check --context prod-a --ghosts-only

# Per-stream retention, replication, size, and replica placement
cheshmhayash stream report --context prod-a --format pretty
```

cheshmhayash reads from the same `~/.config/nats/context/*.json` files that `nats context` uses — no separate credential handling.

## The scan → edit → apply workflow

Deletion is gated on a hand-editable manifest. The flow:

```bash
# 1. Scan and emit a cleanup manifest of stale rows.
cheshmhayash consumer scan --context prod-a \
  --peer-context prod-b \
  --min-pending 10000 --min-idle 24h \
  --emit-manifest cleanup.yaml

# 2. Hand-review cleanup.yaml. Delete rows you don't want touched, or set
#    `skip: true` on rows you want to keep in the manifest as a record but
#    excluded from apply. Add `reason:` lines so future you remembers why.

# 3. Dry-run to confirm what apply would do (re-verifies each consumer).
cheshmhayash consumer apply cleanup.yaml --dry-run

# 4. Apply for real.
cheshmhayash consumer apply cleanup.yaml
```

Apply re-queries every consumer immediately before deleting it. A consumer
that has become active in the window between scan and apply — push-bound,
has pull waiters, or has acked since the manifest's `generated_at` — is
preserved. The window is the safety property that lets the bot operate
unattended later; deleting from a stale snapshot is the failure mode that
makes other cleanup tools dangerous.

## Deleting without the manifest round-trip

The manifest flow exists so a human can review a proposal before it is acted
on. When you are that human and you are already looking at the scan, the
round-trip buys nothing:

```bash
# Preview: re-verifies every candidate, deletes nothing
cheshmhayash consumer scan --context prod-a --min-idle 24h --delete --dry-run

# Delete for real
cheshmhayash consumer scan --context prod-a --min-idle 24h --delete
```

`--delete` gives up the approval gate and nothing else. Each consumer is
still re-queried immediately before deletion and preserved if it has become
active, externally-owned consumers are still refused, and the outcome is
still summarised per row. The same is available unattended — see
`auto_delete` below.

## Working alongside NACK and other declarative owners

If a consumer is declared by a [NACK](https://github.com/nats-io/nack)
`Consumer` CR, by Terraform, or by any GitOps pipeline, then deleting it out
of band is not a cleanup — it is a fight with the system that owns it:

- with NACK's reconciler (`--control-loop`) enabled, the consumer is
  recreated, your next scan finds it idle again, and cheshmhayash deletes it again;
- with reconciliation off, the delete sticks and the cluster silently
  diverges from the declared state until some unrelated future reconcile
  brings it back — typically during an unrelated deploy, long after anyone
  would connect the two events.

So cheshmhayash refuses. Protected consumers still appear in scan output (with a
`managed` reason), are never written into a manifest, and are vetoed inside
`apply` itself — which means a hand-edited manifest or a signed approval
click cannot delete one either.

Mark them either way round:

```yaml
# ~/.config/cheshmhayash/ops.yaml
protect:
  # Consumers carrying any of these metadata keys are off limits.
  # `cheshmhayash.io/managed` is honoured even with no config at all.
  metadata_keys:
    - app.kubernetes.io/managed-by

  # ...or match by name, for consumers whose metadata you cannot set.
  patterns:
    - { stream: rides, consumer: "svc-*" }
    - { consumer: "*-gitops" }
```

The metadata route is the cheaper one, because NACK passes `spec.metadata`
from the CR straight through to the consumer:

```yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Consumer
metadata:
  name: rides-worker
spec:
  streamName: rides
  durableName: rides-worker
  metadata:
    cheshmhayash.io/managed: "true"     # cheshmhayash will never delete this consumer
```

Setting the value to `false`/`no`/`0`/`off` opts back out, so you can unmark
one consumer without deleting the annotation everywhere.

> cheshmhayash deliberately does **not** talk to the Kubernetes API to discover
> this. Reading `consumers.jetstream.nats.io` would catch every NACK-managed
> consumer with no annotation needed, but it would drag in client-go and RBAC
> and turn cheshmhayash from "a NATS client you point at a context file" into a
> cluster-aware component. The annotation costs one line in the CR.

## Running as a bot

`cheshmhayash bot serve` is the long-lived daemon mode. It runs scheduled scans,
posts chat messages with a signed approval URL, exposes a small HTTP
listener for the manifest viewer / slash commands / approval clicks, and
appends every action to a JSONL audit log.

### Configuration

```yaml
# ~/.config/cheshmhayash/ops.yaml (or /etc/cheshmhayash/config.yaml in production)

bot:
  schedules:
    # kind defaults to consumer-stale — the only kind that produces a
    # manifest and an approve URL. The others are notify-only reports.
    - name: daily
      cron: "0 3 * * *"
      context: prod-a
      peer_context: prod-b
      min_pending: 10000
      min_idle: 24h
    - name: edge-hourly
      cron: "0 * * * *"
      context: edge-1
      min_pending: 5000
      min_idle: 6h
    - name: ghost-peers
      kind: peer-check           # peers offline in every Raft group
      cron: "*/30 * * * *"
      context: prod-a
    - name: unbounded-streams
      kind: stream-unlimited     # streams with no retention limit at all
      cron: "0 6 * * *"
      context: prod-a
    - name: replication
      kind: stream-report        # under-replicated streams
      cron: "0 7 * * *"
      context: prod-a
    - name: nightly-sweep
      cron: "0 4 * * *"
      context: prod-a
      min_pending: 10000
      min_idle: 168h
      # Delete instead of proposing. No manifest is stored and no approve
      # link is sent; the run is announced to notify sinks afterwards and
      # recorded in the audit log. Re-verification and protect rules still
      # apply. Only valid on consumer-stale schedules.
      auto_delete: true

  notify:
    - mattermost://chat.example.com/hooks/abc-xyz?channel=nats-cleanup
    # - slack://hooks.slack.com/services/T.../B.../...
    # - webhook://hooks.example.com/n8n/abc   # structured JSON: {title, body, manifest_id, link}
    # - stdout://   # for local testing

  store: file:///var/lib/cheshmhayash/manifests
  audit_log: /var/lib/cheshmhayash/audit.jsonl

  http:
    listen: ":8080"
    base_url: https://cheshmhayash.example.com   # public URL used in chat links

  signing_key: change-me-to-32-random-bytes

  # Optional: route a subset of each manifest to the team that owns those
  # streams. Owner sinks get visibility only — the approve URL always goes
  # to the global notify list. First matching owner wins.
  owners:
    - name: rides
      streams: [rides, rides-dlq]
      consumer_prefix: [rides-]
      notify:
        - mattermost://chat.example.com/hooks/def-uvw?channel=rides-oncall

  # Optional: pull-mode chat transport (see below). Omit for push-mode only.
  mattermost:
    enabled: false
    server: https://chat.example.com
    token_file: /etc/cheshmhayash/mattermost-token
    team: platform
    channel: nats-cleanup
    trigger: "!cheshmhayash"
```

`signing_key` does two jobs:

- **Slash-command verification token.** Configure the same string in your
  Mattermost or Slack slash-command integration; the bot compares it
  with `crypto/subtle.ConstantTimeCompare`.
- **HMAC-SHA256 key for approval URLs.** Every approval link in chat is
  bound to its manifest ID, so a leaked URL can only approve the one
  manifest it was issued for.

### Endpoints

| Method | Path | Purpose |
| ------ | ---- | ------- |
| `GET`  | `/healthz` | JSON `{"status":"ok"}` for load-balancer probes. |
| `GET`  | `/metrics` | Prometheus metrics: scan duration & candidate counts, apply outcomes, approval latency, build info. |
| `GET`  | `/manifest/{id}` | Returns the stored manifest as `application/yaml`. |
| `POST` | `/slash` | Slash-command handler (see [chat commands](#chat-commands)). Token-protected. |
| `GET`  | `/approve/{id}?token=...` | Renders a plain-text preview of what would be deleted. |
| `POST` | `/approve/{id}?token=...` | Re-verifies + applies. Returns JSON summary. |

### Chat commands

Both chat transports (see below) share one dispatcher, so the vocabulary is
identical whichever one you run:

```
list                      list stored manifest IDs
show <id>                 preview a manifest
clusters                  list NATS contexts this bot can dial
streams [ctx]             list streams (one context, or all of them)
stream <ctx> <name>       single-stream detail
last <ctx> <stream> [sub] metadata for the last message on a subject
consumers <ctx> <stream>  all consumers on a stream, unfiltered
usage [ctx]               aggregate footprint + top streams by bytes
cluster <ctx>             connected server, peers, account
scan <ctx> [stream]       on-demand scan; replies with a signed approve URL
help                      this list
```

### Chat transports: push or pull

Pick based on whether your chat server can reach the bot.

**Push (slash command).** Chat POSTs `/slash` on the bot; needs an ingress
the chat server can reach. Configure `/cheshmhayash` in Mattermost or Slack to
POST to `https://<your-host>/slash` with `signing_key` as the token:

```
/cheshmhayash list
/cheshmhayash scan prod-a
```

**Pull (WebSocket).** The bot opens an outbound WebSocket to Mattermost with
a bot-account token, watches one channel for messages starting with
`trigger`, and replies over REST. No inbound route needed — use this when
cheshmhayash sits behind an ingress your chat server cannot reach. Enable the
`bot.mattermost` block above, then from the channel:

```
!cheshmhayash list
!cheshmhayash scan prod-a
```

The bot account must be a member of the team *and* the channel it should
listen on; being a webhook target is not enough. A Mattermost outage is
recoverable — the listener retries with backoff while the scheduler and
HTTP listener keep running.

### Audit log

Every scan run, approval preview, and apply attempt is appended as one
JSON object per line to `bot.audit_log`. Rotate it with logrotate or
similar; the bot opens the file in append mode and does not hold an
exclusive lock.

### Kubernetes

The cheshmhayash Helm chart runs the bot when `bot.enabled: true`: a single-replica Deployment (`args: [bot, serve, --ops-config, /etc/cheshmhayash/config.yaml]`) with the rendered config, a Secret of NATS contexts (`bot.natsContexts`), the signing key, a state PVC for the manifest store and audit log, a Service, and optionally a ServiceMonitor, Ingress/HTTPProxy and NetworkPolicy. Every value lives under `bot:` in `chart/cheshmhayash-chart/values.yaml`.

The NATS contexts Secret holds the same `*.json` files the official `nats context` CLI writes, one per cluster, with the credentials inline. The bot does not run as a NATS account by itself; it borrows whatever the operator already trusts. Do not scale it beyond one replica: the schedules, the store and the audit log assume a single writer.

## Code layout

The commands live in `internal/cmd/{consumer,peer,stream,bot}` and the rest in `internal/ops/`: `scanner` (classification, renames, peers, stream report), `cleanup` (re-verify and delete: the only deletion path), `protect` (the veto for consumers owned by NACK, Terraform or GitOps), `manifest`, `chatops`, `owners`, `audit`, and `infra/` (config, NATS contexts, the bot's HTTP listener, the Mattermost listener, notify sinks, scheduler, store, metrics).
