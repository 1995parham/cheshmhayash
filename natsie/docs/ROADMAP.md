# Roadmap

`natsie` ships in slices, each one operationally useful on its own. Order is by dependency, not priority.

## v0.1 — Consumer scan (this commit)

- [x] `consumer scan` against a single context, TSV output, classify active/stale/abandoned.
- [x] Per-stream filter (`--stream`).
- [x] Pending and idle thresholds (`--min-pending`, `--min-idle`).
- [ ] JSON output (`--format json`).
- [ ] Cross-cluster peer check (`--peer-context`).

## v0.2 — Consumer apply

- [ ] `consumer scan --emit-manifest <file>` writes a delete-manifest with a checksum and a per-row reason code.
- [ ] `consumer apply <manifest> [--dry-run]` deletes the consumers in the manifest after re-verifying their state. Re-verification is non-negotiable: if a consumer has been active since the manifest was written, it gets skipped.
- [ ] Audit log appended on every apply.

## v0.3 — Fuzzy peer matching

- [ ] Resolve "the service that owned consumer X" by `filter_subject` + recent activity, across all configured contexts.
- [ ] Detect rename migrations (consumer A stale, consumer A' active, same subject) and surface them in the report.

## v0.4 — Stream + peer reports

- [ ] `stream report` with retention, replication, per-pod placement skew.
- [ ] `peer check` for ghost peers and phantom Raft groups.

## v1.0 — Bot mode

- [ ] `bot serve` with periodic scans, optional chat sinks (Slack, Mattermost, generic webhook), and a slash-command approval flow.
- [ ] Service-owner mapping (stream prefix → channel/team) loaded from config.
- [ ] OpenTelemetry traces + Prometheus metrics for scan duration, candidate counts, approval latency.
