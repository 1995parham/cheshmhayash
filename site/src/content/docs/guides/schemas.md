---
title: Protobuf schemas
description: Describe your protobuf schemas and subject rules so cheshmhayash can decode and encode NATS payloads, with a full worked example.
---

cheshmhayash decodes protobuf payloads in `sub`, the dashboard's Tap tab and the MCP tool `subject_sample`, and it encodes JSON to protobuf in `pub`. It loads the schemas at run time, from files you point it at. Nothing is compiled into the binary, so private `.proto` files stay private.

## Where schemas are described

You describe schemas in one of two places, and you can use both:

- The `[schemas]` section of `settings.toml`.
- A standalone schemas file, passed with `--schemas` (repeat it for more) or `CHESHMHAYASH_SCHEMAS`. A private repository can ship one next to its descriptor sets.

When several files are loaded, their sources and rules are joined in order. A relative path resolves against the folder of the file it is in.

## Sources

A source is one named set of protobuf files. Its name prefixes each message: `shop:shop.v1.OrderCreated` is the message `shop.v1.OrderCreated` of the source `shop`. Each source keeps its own registry, so two repositories that both declare `package orders;` can load side by side.

| Type | `path` is | When to use it |
| --- | --- | --- |
| `descriptor_set` | A file made by `protoc --include_imports --descriptor_set_out=...` or `buf build -o ...`. | CI builds the file, and you ship one artifact. |
| `proto_dir` | An import root of `.proto` files. All of them are compiled at start. | You have the `.proto` files and no build step. |

A `proto_dir` source can add more import roots with `import_paths`, for example a copy of `googleapis`. The well-known types (`google/protobuf/*.proto`) are built in. A file that does not compile is skipped, not fatal: `schema list -v` says why, and `schema check` fails on it.

```toml
[[sources]]
name = "billing"
type = "descriptor_set"
path = "billing.binpb"           # buf build -o billing.binpb

[[sources]]
name = "shop"
type = "proto_dir"
path = "../shop-proto/proto"
import_paths = ["third_party"]
```

## Subject rules

A rule maps the subjects that match a NATS pattern to a message. Wildcards work (`*` for one token, `>` for the rest), and the first matching rule wins, so put specific rules before general ones:

```toml
[[subjects]]
match = "shop.orders.created"
message = "shop:shop.v1.OrderCreated"

[[subjects]]
match = "billing.invoices.>"
message = "billing:billing.v1.Invoice"
```

## How a payload's type is chosen

For each message, cheshmhayash takes the first of:

1. The `--type` flag (or the type box in the Tap tab).
2. The `Proto-Type` message header. Change the header name with `type_header = "..."`, or turn the lookup off with `type_header = "-"`.
3. The first subject rule that matches.

So a publisher can name its type in a header, and no rule is needed.

## The decoder chain

Decoders form a chain. The first one that claims a payload renders it:

| Order | Format | When |
| --- | --- | --- |
| 1 | `protobuf` | The payload has a type and the schema fits. |
| 2 | `json` | The payload is valid JSON. |
| 3 | `text` | The payload is readable UTF-8 text. |
| 4 | `protowire` | The whole payload parses as protobuf. Fields are keyed by number, because there is no schema. |
| 5 | `binary` | Anything else, shown as base64. |

A payload is never lost. When a schema does not fit, the next decoder shows the payload with a note. Fields that the schema does not know are reported as schema drift.

## Worked example

This example goes from a `.proto` file to decoded output. Start a NATS server on your laptop with `nats-server`.

### 1. The proto file

`proto/shop/v1/orders.proto`:

```protobuf
syntax = "proto3";

package shop.v1;

import "google/protobuf/timestamp.proto";

message OrderCreated {
  string order_id = 1;
  string customer_id = 2;
  repeated Item items = 3;
  int64 total_cents = 4;
  google.protobuf.Timestamp created_at = 5;
}

message Item {
  string sku = 1;
  int32 quantity = 2;
}
```

### 2. The schemas file

`schemas.toml`, next to the `proto` folder:

```toml
[[sources]]
name = "shop"
type = "proto_dir"
path = "proto"

[[subjects]]
match = "shop.orders.created"
message = "shop:shop.v1.OrderCreated"
```

### 3. Check it

```text
$ cheshmhayash schema check --schemas schemas.toml
ok: 1 sources, 1 subject rules

$ cheshmhayash schema resolve --schemas schemas.toml shop.orders.created
SUBJECT              RULE                 MESSAGE                    STATUS
shop.orders.created  shop.orders.created  shop:shop.v1.OrderCreated  ok
```

### 4. Subscribe

In one terminal:

```sh
cheshmhayash sub --schemas schemas.toml -s nats://127.0.0.1:4222 'shop.>'
```

### 5. Publish JSON, send protobuf

In another terminal. `pub` finds the type from the rule and encodes the JSON to 39 bytes of protobuf:

```text
$ cheshmhayash pub --schemas schemas.toml -s nats://127.0.0.1:4222 shop.orders.created \
    '{"order_id":"o-1001","customer_id":"c-42","items":[{"sku":"mug-blue","quantity":2}],"total_cents":2400,"created_at":"2026-11-21T10:15:00Z"}'
published 1 × 39 B to shop.orders.created as shop.v1.OrderCreated (from subject shop.orders.created) at cli
```

### 6. Read it decoded

The subscriber prints:

```text
17:57:32.327  shop.orders.created  shop:shop.v1.OrderCreated  39 B
{
  "order_id": "o-1001",
  "customer_id": "c-42",
  "items": [{ "sku": "mug-blue", "quantity": 2 }],
  "total_cents": "2400",
  "created_at": "2026-11-21T10:15:00Z"
}
```

`total_cents` is a string because the protobuf JSON mapping writes 64-bit integers as strings.

### 7. Use it everywhere

The same `schemas.toml` works in the other places:

- **Dashboard tap**: `cheshmhayash serve --schemas schemas.toml`, or `schemas.*` in the Helm chart.
- **MCP**: `cheshmhayash mcp --allow-sampling --schemas schemas.toml`.
- **CI**: run `cheshmhayash schema check --schemas schemas.toml` in the schemas repository.
