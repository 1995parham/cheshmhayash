---
title: Configuration
description: Every settings.toml section of cheshmhayash, how settings load, and the environment variables.
---

## How settings load

Settings load in three layers. A later layer wins:

1. The defaults built into the binary.
2. `settings.toml` (optional; another file with `--config`).
3. Environment variables that start with `CHESHMHAYASH__`.

An environment key uses `__` between levels. A list entry has an index, for example `CHESHMHAYASH__NATS__0__USER=admin`, and the entries merge one by one with the file. A list value is comma-separated.

The operations commands (`consumer`, `peer`, `stream`, `bot`) use a separate YAML file. See the [bot guide](/cheshmhayash/guides/bot/#configuration).

## settings.toml

```toml
[server]
host = "0.0.0.0"        # default
port = 1378             # default

# One block per cluster. At least one is required.
[[nats]]
name = "prod"
url = "nats://nats-1.example.com:4222,nats://nats-2.example.com:4222"
# Use one: a creds file, or a user and password of the system account.
# creds_file = "./sys.creds"
user = "admin"
password = "changeme"
# request_timeout_ms = 5000     # one-reply requests
# discovery_timeout_ms = 1500   # how long to collect replies of a fan-out

# A second entry with an application user, for the tap.
# [[nats]]
# name = "prod-apps"
# url = "nats://nats-1.example.com:4222"
# user = "tap"
# password = "..."

[[notify]]
provider = "slack"      # slack | mattermost | matrix
url = "https://hooks.slack.com/services/T000/B000/XXX"
channel = "#nats-ops"
username = "cheshmhayash"

[tap]
enabled = false         # default
allow = ["orders.>"]
max_rate = 20
max_duration_seconds = 600
max_taps = 10
role = "admin"

[schemas]
type_header = "Proto-Type"
[[schemas.sources]]
name = "shop"
type = "proto_dir"
path = "proto"
[[schemas.subjects]]
match = "shop.orders.created"
message = "shop:shop.v1.OrderCreated"

[auth]
enabled = false         # default
mode = "oidc"           # oidc | jwt
```

The sections in detail:

| Section | Page |
| --- | --- |
| `[server]`, `[[nats]]`, `[[notify]]` | [Deploy](/cheshmhayash/guides/deploy/) |
| `[tap]` | [Tap](/cheshmhayash/dashboard/tap/#turn-it-on) |
| `[schemas]` | [Protobuf schemas](/cheshmhayash/guides/schemas/) |
| `[auth]` | [Authentication](/cheshmhayash/guides/auth/) and [MCP](/cheshmhayash/guides/mcp/#authentication) |

## Environment variables

| Variable | Meaning |
| --- | --- |
| `CHESHMHAYASH__...` | Any settings key, as above. |
| `CHESHMHAYASH_CONFIG` | The settings file (`--config`). |
| `CHESHMHAYASH_OPS_CONFIG` | The ops YAML file (`--ops-config`). |
| `CHESHMHAYASH_OPS_<SECTION>__<KEY>` | Any key of the ops YAML file. |
| `CHESHMHAYASH_COMPONENTS` | `serve --components`, for example `api,ui`. |
| `CHESHMHAYASH_OVERVIEW_PERIOD` | `serve --overview-period`, a Go duration. Default `10s`. |
| `CHESHMHAYASH_SCHEMAS` | Extra schemas files. |
| `CHESHMHAYASH_CLUSTER` | `--cluster` of the client commands. |
| `CHESHMHAYASH_MCP_WRITE=1` | `mcp --write`. |
| `CHESHMHAYASH_MCP_SAMPLE=1` | `mcp --allow-sampling`. |
| `NATS_URL`, `NATS_USER`, `NATS_PASSWORD`, `NATS_CREDS` | The connection flags of the client commands. |
| `LOG_LEVEL` | `debug`, `info`, `warn` or `error`. An unknown value is fatal. |
| `NO_COLOR` | Plain output from `sub` and `pub`. |
