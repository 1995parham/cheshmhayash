# natsie

[![CI](https://img.shields.io/github/actions/workflow/status/1995parham/natsie/test.yaml?branch=main&style=for-the-badge&logo=github&label=ci)](https://github.com/1995parham/natsie/actions/workflows/test.yaml)
[![Release](https://img.shields.io/github/v/release/1995parham/natsie?style=for-the-badge&logo=github&color=blue)](https://github.com/1995parham/natsie/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/1995parham/natsie?style=for-the-badge)](https://goreportcard.com/report/github.com/1995parham/natsie)
[![License: GPL v3](https://img.shields.io/badge/license-GPL%20v3-blue?style=for-the-badge)](./LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/1995parham/natsie?style=for-the-badge&logo=go)](https://go.dev)
[![Codecov](https://img.shields.io/codecov/c/github/1995parham/natsie?style=for-the-badge&logo=codecov)](https://codecov.io/gh/1995parham/natsie)

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
| `consumer scan` | **working** | Enumerate consumers across one or more contexts; classify as active / stale / abandoned with cross-cluster peer awareness; emit TSV/JSON, optionally a YAML cleanup manifest. |
| `consumer apply` | **working** | Apply a delete-manifest produced by `scan`, re-verifying each consumer first. Supports `--dry-run`. |
| `stream report` | planned | Per-stream size, retention, replication, and ownership signals. |
| `peer check` | planned | Detect ghost peers / phantom Raft groups from past shrinks. |
| `bot serve` | **working** | Long-running daemon: scheduled scans, chat notifications (Slack / Mattermost / stdout), HTTP listener with manifest viewer, slash-command handler, signed approval URLs, JSONL audit log. |

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

## The scan → edit → apply workflow

Deletion is gated on a hand-editable manifest. The flow:

```bash
# 1. Scan and emit a cleanup manifest of stale rows.
natsie consumer scan --context snapp-js-main-teh1 \
  --peer-context snapp-js-main-teh2 \
  --min-pending 10000 --min-idle 24h \
  --emit-manifest cleanup.yaml

# 2. Hand-review cleanup.yaml. Delete rows you don't want touched, or set
#    `skip: true` on rows you want to keep in the manifest as a record but
#    excluded from apply. Add `reason:` lines so future you remembers why.

# 3. Dry-run to confirm what apply would do (re-verifies each consumer).
natsie consumer apply cleanup.yaml --dry-run

# 4. Apply for real.
natsie consumer apply cleanup.yaml
```

Apply re-queries every consumer immediately before deleting it. A consumer
that has become active in the window between scan and apply — push-bound,
has pull waiters, or has acked since the manifest's `generated_at` — is
preserved. The window is the safety property that lets the bot operate
unattended later; deleting from a stale snapshot is the failure mode that
makes other cleanup tools dangerous.

## Running as a bot

`natsie bot serve` is the long-lived daemon mode. It runs scheduled scans,
posts chat messages with a signed approval URL, exposes a small HTTP
listener for the manifest viewer / slash commands / approval clicks, and
appends every action to a JSONL audit log.

### Configuration

```yaml
# ~/.config/natsie/config.yaml (or /etc/natsie/config.yaml in production)

bot:
  schedules:
    - name: daily
      cron: "0 3 * * *"
      context: snapp-js-main-teh1
      peer_context: snapp-js-main-teh2
      min_pending: 10000
      min_idle: 24h
    - name: hodhod-hourly
      cron: "0 * * * *"
      context: snapp-js-hodhod
      min_pending: 5000
      min_idle: 6h

  notify:
    - mattermost://chat.example.com/hooks/abc-xyz?channel=nats-cleanup
    # - slack://hooks.slack.com/services/T.../B.../...
    # - webhook://hooks.example.com/n8n/abc   # structured JSON: {title, body, manifest_id, link}
    # - stdout://   # for local testing

  store: file:///var/lib/natsie/manifests
  audit_log: /var/lib/natsie/audit.jsonl

  http:
    listen: ":8080"
    base_url: https://natsie.example.com   # public URL used in chat links

  signing_key: change-me-to-32-random-bytes
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
| `GET`  | `/health` | JSON `{"status":"ok"}` for load-balancer probes. |
| `GET`  | `/manifest/{id}` | Returns the stored manifest as `application/yaml`. |
| `POST` | `/slash` | Slash-command handler (`list`, `show <id>`, `help`). Token-protected. |
| `GET`  | `/approve/{id}?token=...` | Renders a plain-text preview of what would be deleted. |
| `POST` | `/approve/{id}?token=...` | Re-verifies + applies. Returns JSON summary. |

### Slash commands

Configure `/natsie` in Mattermost or Slack to POST to `https://<your-host>/slash`
with the configured token. From chat:

```
/natsie list          → list stored manifest IDs
/natsie show m-...    → preview a manifest
/natsie help          → usage
```

### Audit log

Every scan run, approval preview, and apply attempt is appended as one
JSON object per line to `bot.audit_log`. Rotate it with logrotate or
similar; the bot opens the file in append mode and does not hold an
exclusive lock.

### Kubernetes

A minimal Deployment + Service + ConfigMap, with the config mounted
into `/etc/natsie/config.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: natsie }
spec:
  replicas: 1                          # do not scale > 1; schedules + store assume single-writer
  selector: { matchLabels: { app: natsie } }
  template:
    metadata: { labels: { app: natsie } }
    spec:
      containers:
        - name: natsie
          image: ghcr.io/1995parham/natsie:latest
          args: ["bot", "serve", "--config", "/etc/natsie/config.yaml"]
          ports: [{ containerPort: 8080 }]
          volumeMounts:
            - { name: config,    mountPath: /etc/natsie }
            - { name: state,     mountPath: /var/lib/natsie }
            - { name: nats-ctx,  mountPath: /root/.config/nats }
          livenessProbe:
            httpGet: { path: /health, port: 8080 }
      volumes:
        - { name: config,   configMap: { name: natsie-config } }
        - { name: state,    persistentVolumeClaim: { claimName: natsie-state } }
        - { name: nats-ctx, secret:    { secretName: natsie-nats-contexts } }
```

The `natsie-nats-contexts` Secret should contain the same `*.json` files
the official `nats context` CLI writes — one per cluster, with the
credentials inline. The bot does not run as a NATS account by itself; it
borrows whatever the operator already trusts.

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

GPL-3.0-only. See [LICENSE](LICENSE).
