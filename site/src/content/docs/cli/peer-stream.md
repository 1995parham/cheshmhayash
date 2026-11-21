---
title: peer and stream
description: Find ghost JetStream peers and report stream retention, replication and placement.
---

## peer check

```text
cheshmhayash peer check --context NAME [--format tsv|json|pretty] [--ghosts-only]
```

`peer check` walks every stream and consumer Raft group and adds up each server's standing:

| Health | Meaning |
| --- | --- |
| `OK` | Current in the groups that name it. |
| `LAGGING` | Named by groups but current in none of them. |
| `GHOST` | Offline in every group that names it, and leads none. |

A ghost is almost always a server that was removed from the cluster without a `peer-remove`. The groups still count it as a replica, so they have less room for failure than you think.

```sh
cheshmhayash peer check --context prod-a
cheshmhayash peer check --context prod-a --ghosts-only
```

## stream report

```text
cheshmhayash stream report --context NAME [--stream NAME] [--format tsv|json|pretty]
```

`stream report` shows each stream's retention and limits, replicas, size, consumer count and replica placement. The pretty format adds a summary of placement skew per server, so you can see a server that holds too many replicas.

```sh
cheshmhayash stream report --context prod-a --format pretty
cheshmhayash stream report --context prod-a --stream orders
```

The same rules run in the dashboard's [Health tab](/cheshmhayash/dashboard/health/), and as scheduled reports in the [bot](/cheshmhayash/guides/bot/).
