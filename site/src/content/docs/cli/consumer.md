---
title: consumer
description: Find stale JetStream consumers across clusters and remove them safely with a reviewed manifest.
---

The `consumer` commands answer a question that comes up after each upgrade or service migration: which consumers are still here that probably should not be, and what is the safest way to remove them?

Cleanup works like a code review: **scan → a manifest a person edits → apply**. Apply checks each consumer against the live cluster again just before it deletes it. Auto-delete exists, but it is off by default.

These commands connect through NATS contexts (`--context prod-a`). See [two ways to connect](/cheshmhayash/cli/#two-ways-to-connect).

## How a consumer is classified

| Status | Meaning |
| --- | --- |
| `ACTIVE` | Push-bound, has pull waiters, or acked within `--min-idle`. |
| `STALE` | Has a backlog and no ack for longer than `--min-idle`. A consumer that was never acked is stale only when it is older than `--min-idle`. |
| `OK` | Exists and is healthy, but does not meet the report thresholds (for example, no backlog). |
| `ABSENT` | Only in the peer column: the consumer does not exist on the peer cluster. |
| `ERR` | Could not be queried. |

Two more columns help you decide:

- **renamed_to**: a stale consumer has an active sibling on the same stream with the same filter subject. It is likely the old name of a rename.
- **managed**: something else owns the consumer. See [protected consumers](#protected-consumers).

## consumer scan

```text
cheshmhayash consumer scan --context NAME [flags]
```

| Flag | Meaning |
| --- | --- |
| `--context` | The NATS context to scan. Required. |
| `--peer-context` | A sibling cluster to check each consumer on, for cross-region pairs. Overrides the config. |
| `--stream` | Scan only this stream. |
| `--format` | `tsv` (default), `json` or `pretty`. |
| `--min-pending` | Report only consumers with more pending messages than this. |
| `--min-idle` | Report only consumers idle longer than this. |
| `--emit-manifest PATH` | Also write a YAML cleanup manifest of the stale rows. |
| `--force` | Overwrite an existing manifest at that path. |
| `--delete` | Delete the stale consumers now, with no manifest. |
| `--dry-run` | With `--delete`, show what would be deleted and delete nothing. |

```sh
# Scan one cluster
cheshmhayash consumer scan --context prod-a

# Check each consumer on the sibling cluster too
cheshmhayash consumer scan --context prod-a --peer-context prod-b

# Only consumers idle for more than a day with more than 10k pending
cheshmhayash consumer scan --context prod-a --min-idle 24h --min-pending 10000

# JSON for other tools
cheshmhayash consumer scan --context prod-a --format json
```

## The scan → edit → apply flow

```sh
# 1. Scan and write a manifest of the stale rows.
cheshmhayash consumer scan --context prod-a --peer-context prod-b \
  --min-pending 10000 --min-idle 24h --emit-manifest cleanup.yaml

# 2. Read cleanup.yaml. Remove rows you want to keep, or set `skip: true`
#    on a row to keep it as a record. Add `reason:` lines.

# 3. See what apply would do. It re-checks each consumer.
cheshmhayash consumer apply cleanup.yaml --dry-run

# 4. Apply.
cheshmhayash consumer apply cleanup.yaml
```

## consumer apply

```text
cheshmhayash consumer apply MANIFEST [--dry-run]
```

`apply` reads a manifest (or `-` for standard input) and queries each consumer again just before it deletes it. A consumer that became active after the scan is kept: one that is push-bound, has pull waiters, or acked after the manifest's `generated_at`. This re-check is what makes it safe to run cleanup unattended later.

`--dry-run` re-checks and logs each decision, but deletes nothing.

## Delete without a manifest

When you are the reviewer and you already look at the scan, the manifest adds nothing:

```sh
cheshmhayash consumer scan --context prod-a --min-idle 24h --delete --dry-run   # preview
cheshmhayash consumer scan --context prod-a --min-idle 24h --delete             # delete
```

`--delete` skips only the approval step. Each consumer is still re-checked, protected consumers are still refused, and the result is still shown per row.

## consumer owner

```text
cheshmhayash consumer owner --subject FILTER [--context NAME ...] [--format tsv|json|pretty]
```

`owner` searches every configured context (or only the `--context` ones) for consumers that filter the subject. It shows them active first. Use it when a consumer is stale here and you want to know where its work moved: to another cluster, or to a new name. Pass the filter subject exactly as `consumer scan` shows it.

```sh
cheshmhayash consumer owner --subject 'orders.created.>'
```

## Protected consumers

A consumer that a NACK `Consumer` resource, Terraform or a GitOps pipeline declares is not cheshmhayash's to delete. If you delete it, the reconciler creates it again, or the cluster drifts from its declared state. So cheshmhayash refuses.

Protected consumers still show in the scan, with a `managed` reason. They never go into a manifest, and `apply` vetoes them too, so a hand-edited manifest or an approval click cannot delete one.

Mark them in the ops config:

```yaml
# ~/.config/cheshmhayash/ops.yaml
protect:
  # Consumers that carry any of these metadata keys are protected.
  # `cheshmhayash.io/managed` works with no config at all.
  metadata_keys:
    - app.kubernetes.io/managed-by
  # Or match by name, for consumers whose metadata you cannot set.
  patterns:
    - { stream: orders, consumer: "svc-*" }
    - { consumer: "*-gitops" }
```

NACK copies `spec.metadata` from the resource to the consumer, so one line marks it:

```yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Consumer
metadata:
  name: orders-worker
spec:
  streamName: orders
  durableName: orders-worker
  metadata:
    cheshmhayash.io/managed: "true"
```

A value of `false`, `no`, `0` or `off` removes the protection for one consumer.
