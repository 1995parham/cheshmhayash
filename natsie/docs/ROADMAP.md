# Roadmap

`natsie` ships in slices, each one operationally useful on its own. Order is by dependency, not priority.

## v0.1 — Consumer scan

- [x] `consumer scan` against a single context, TSV output, classify active/stale/abandoned.
- [x] Per-stream filter (`--stream`).
- [x] Pending and idle thresholds (`--min-pending`, `--min-idle`).
- [x] JSON output (`--format json`).
- [x] Cross-cluster peer check (`--peer-context`).

## v0.2 — Consumer apply

- [x] `consumer scan --emit-manifest <file>` writes a hand-editable YAML manifest of stale rows.
- [x] `consumer apply <manifest> [--dry-run]` re-verifies and deletes. Consumers active since manifest `generated_at` are preserved.
- [x] Deletion via the raw `$JS.API.CONSUMER.DELETE` subject so leading-dash names work out of the box.
- [x] Audit log appended on every apply (shipped as part of v1.0 audit; available to CLI via shared `internal/cleanup`).
- [ ] `--manifest-stdin` to apply a manifest read from stdin (for chat-bot integration).

## v0.3 — Fuzzy peer matching

- [ ] Resolve "the service that owned consumer X" by `filter_subject` + recent activity, across all configured contexts.
- [ ] Detect rename migrations (consumer A stale, consumer A' active, same subject) and surface them in the report.

## v0.4 — Stream + peer reports

- [ ] `stream report` with retention, replication, per-pod placement skew.
- [ ] `peer check` for ghost peers and phantom Raft groups.

## v1.0 — Bot mode

- [x] `bot serve` with periodic scans, optional chat sinks (Slack, Mattermost, stdout), and a slash-command + signed approval URL flow.
- [x] HTTP listener for `/health`, `/manifest/{id}`, `/slash`, `/approve/{id}` (preview + apply).
- [x] JSONL audit log of scans, approval previews, and approval applies.
- [ ] Service-owner mapping (stream prefix → channel/team) loaded from config.
- [ ] OpenTelemetry traces + Prometheus metrics for scan duration, candidate counts, approval latency.
- [ ] `bot serve` clustered mode (currently single-writer due to schedule + file store assumptions).
- [ ] Generic `webhook://` notifier (Slack & Mattermost are already covered; everything else goes through one shape).
