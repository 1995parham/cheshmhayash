---
title: Dashboard overview
description: A tour of the cheshmhayash dashboard, its tabs, the live updates and the read-only and admin roles.
---

`cheshmhayash serve` runs a React panel and the JSON API behind it. Pick a cluster at the top, then a tab. The tab is kept in the URL hash, so `#health` links straight to the Health tab and survives a reload.

:::tip[Try it]
The [live demo](/cheshmhayash/demo/) runs the real dashboard in your browser. It is a read-only snapshot of a cluster with renamed data. Buttons that change something show a message and do nothing.
:::

## Tabs

| Tab | What it shows | Demo |
| --- | --- | --- |
| [Servers](/cheshmhayash/dashboard/servers/) | Every server of the cluster, with traffic totals and the raw monitoring endpoints. | [open](/cheshmhayash/demo/#servers) |
| [Accounts](/cheshmhayash/dashboard/accounts/) | Account-scoped endpoints for one account. | [open](/cheshmhayash/demo/#accounts) |
| [JetStream](/cheshmhayash/dashboard/jetstream/) | Accounts, streams and consumers of the whole cluster, updated live. | [open](/cheshmhayash/demo/#jetstream) |
| [Health](/cheshmhayash/dashboard/health/) | What misbehaves, most severe first. | [open](/cheshmhayash/demo/#health) |
| [Topology](/cheshmhayash/dashboard/topology/) | Raft groups across the servers: who leads, who follows, who is behind. | [open](/cheshmhayash/demo/#topology) |
| [Tap](/cheshmhayash/dashboard/tap/) | Live, decoded messages of a subject. Off by default. | [open](/cheshmhayash/demo/#tap) |

## Live updates

The server keeps one JetStream overview per cluster in a cache. It asks every server for its JSZ report (`$SYS.REQ.SERVER.PING.JSZ`) every 10 seconds, and pushes each new snapshot to the browser over Server-Sent Events. A small pill shows the state: `live`, `polling` (the SSE stream failed and the panel polls), `connecting` or `disconnected`.

Change the period with `serve --overview-period` or `CHESHMHAYASH_OVERVIEW_PERIOD`. The Health tab reads the same cache, so it adds no NATS traffic.

## Roles

With [authentication](/cheshmhayash/guides/auth/) off, everyone has full access. With it on, each signed-in user is either:

- **admin**: read and write. The user matches the `[auth.access.admin]` allowlist, or that list is empty.
- **readonly**: read only. The panel hides every button that changes something, and the API refuses every `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` with 403.

Destructive calls also need `?confirm=true`. Without it the API answers `428 Precondition Required`. The panel asks you to confirm and then adds it.
