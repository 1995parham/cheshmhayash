---
title: schema
description: Inspect protobuf schema sources, message names, fields and subject rules, and check them in CI.
---

`cheshmhayash schema` shows what the loaded schemas contain and how each subject decodes. It reads the `[schemas]` section of `settings.toml` and every `--schemas` file. The [schemas guide](/cheshmhayash/guides/schemas/) explains the file format.

| Command | What it does |
| --- | --- |
| `schema list [-v]` | Lists the sources, with files, skipped files and message counts. `-v` explains each skipped file. |
| `schema messages [FILTER] [--source NAME]` | Lists message names, optionally only those that contain `FILTER`. |
| `schema show MESSAGE` | Shows the fields of a message. |
| `schema resolve SUBJECT...` | Shows which message each subject decodes as. |
| `schema check` | Fails when a source skipped files or a rule names a missing message. Use it in CI. |

## Examples

```text
$ cheshmhayash schema list --schemas schemas.toml
SOURCE  TYPE       FILES  SKIPPED  MESSAGES  PATH
shop    proto_dir  2      0        3         proto

1 subject rules
```

```text
$ cheshmhayash schema messages --schemas schemas.toml
shop:google.protobuf.Timestamp
shop:shop.v1.Item
shop:shop.v1.OrderCreated
```

```text
$ cheshmhayash schema show --schemas schemas.toml shop:shop.v1.OrderCreated
message shop:shop.v1.OrderCreated  (shop/v1/orders.proto)
  1  string                     order_id
  2  string                     customer_id
  3  repeated shop.v1.Item      items
  4  int64                      total_cents
  5  google.protobuf.Timestamp  created_at
```

```text
$ cheshmhayash schema resolve --schemas schemas.toml shop.orders.created shop.orders.paid
SUBJECT              RULE                 MESSAGE                    STATUS
shop.orders.created  shop.orders.created  shop:shop.v1.OrderCreated  ok
shop.orders.paid     -                    -                          no rule: decoded as JSON, text, protowire or base64
```

```text
$ cheshmhayash schema check --schemas schemas.toml
ok: 1 sources, 1 subject rules
```

`schema check` exits non-zero on a problem, so a schemas repository can run it on each merge request.
