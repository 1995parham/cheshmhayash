---
title: sub and pub
description: Subscribe and publish on NATS with payloads decoded from and encoded to protobuf by schemas loaded at run time.
---

`cheshmhayash sub` and `cheshmhayash pub` work like `nats sub` and `nats pub`, but they decode and encode protobuf. The schemas load at run time, so private `.proto` files never go into the binary. The [schemas guide](/cheshmhayash/guides/schemas/) explains how to describe them.

## sub

```text
cheshmhayash sub [flags] SUBJECT [SUBJECT...]
```

`sub` (alias `subscribe`) uses core NATS only. It never creates a JetStream consumer, so it is safe on production streams.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--type`, `-t` | | Decode every payload as this message (`source:full.Name` or `full.Name`). |
| `--queue`, `-q` | | Join this queue group and share messages with its other members. |
| `--count`, `-n` | `0` | Stop after this many messages. `0` means no limit. |
| `--timeout` | `0` | Stop after this long. `0` means no limit. |
| `--output`, `-o` | `pretty` | `pretty`, `json` (one object per line) or `raw`. |
| `--headers`, `-H` | | Show message headers (pretty output). |
| `--emit-defaults` | | Show protobuf fields that hold their zero value. |
| `--no-color` | | Do not color the pretty output. Also `NO_COLOR`. |

Plus the [connection flags](/cheshmhayash/cli/#two-ways-to-connect).

The pretty output of three messages, one protobuf, one JSON and one text:

```text
$ cheshmhayash sub --schemas schemas.toml -s nats://127.0.0.1:4222 'shop.>'
listening on [shop.>] at cli (nats://127.0.0.1:4222)
17:57:32.327  shop.orders.created  shop:shop.v1.OrderCreated  39 B
{
  "order_id": "o-1001",
  "customer_id": "c-42",
  "items": [{ "sku": "mug-blue", "quantity": 2 }],
  "total_cents": "2400",
  "created_at": "2026-11-21T10:15:00Z"
}
17:57:32.339  shop.orders.paid  json  33 B
{ "order_id": "o-1001", "paid": true }
17:57:32.351  shop.audit  text  20 B
"order o-1001 created"
```

With `-o json`, each message is one JSON line, ready for `jq`:

```sh
cheshmhayash sub --schemas schemas.toml -o json 'shop.orders.>' | jq .payload
```

```json
{"time":"2026-11-21T17:57:34.404303+01:00","subject":"shop.orders.created","size":11,"format":"protobuf","type":"shop:shop.v1.OrderCreated","payload":{"order_id":"o-1002","total_cents":"990"}}
```

## pub

```text
cheshmhayash pub [flags] SUBJECT [PAYLOAD|-]
```

`pub` (alias `publish`) takes a JSON payload. When the subject has a protobuf type, `pub` encodes the JSON to protobuf before it sends it. With `-` or no payload, it reads the payload from standard input.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--type`, `-t` | from the subject rules | Encode the JSON payload as this message. |
| `--raw` | | Send the payload bytes as they are, with no protobuf encoding. |
| `--header`, `-H` | | Add a header as `Key=Value`. Repeat it for more. |
| `--count`, `-n` | `1` | Publish the message this many times. |
| `--request`, `-r` | | Wait for a reply and print it decoded. |
| `--reply-type` | | Decode the reply as this message. |
| `--timeout` | `5s` | How long to wait for the server, and for the reply with `--request`. |
| `--output`, `-o` | `pretty` | The reply output: `pretty`, `json` or `raw`. |
| `--no-color` | | Do not color the pretty output. |

```text
$ cheshmhayash pub --schemas schemas.toml shop.orders.created \
    '{"order_id":"o-1001","customer_id":"c-42","items":[{"sku":"mug-blue","quantity":2}],"total_cents":2400,"created_at":"2026-11-21T10:15:00Z"}'
published 1 × 39 B to shop.orders.created as shop.v1.OrderCreated (from subject shop.orders.created) at cli

$ cheshmhayash pub shop.orders.paid '{"order_id":"o-1001","paid":true}'
published 1 × 33 B to shop.orders.paid as raw bytes (no protobuf type for this subject) at cli
```

## In a cluster

The Helm chart can mount a schemas folder from a ConfigMap or an image (`schemas.*` values). Then you can decode inside the cluster:

```sh
kubectl exec deploy/<release> -- /app/cheshmhayash sub 'orders.>'
```
