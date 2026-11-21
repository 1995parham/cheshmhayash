---
title: Accounts
description: The Accounts tab queries the account-scoped NATS monitoring endpoints for one account.
---

The Accounts tab asks every server about one account. Type the account name (for example `APP` or a public key), pick an endpoint and run it:

| Endpoint | What it returns |
| --- | --- |
| `INFO` | The account's details and limits. |
| `CONNZ` | The account's connections. |
| `LEAFZ` | The account's leaf node connections. |
| `SUBSZ` | The account's subscriptions. |
| `JSZ` | The account's JetStream usage. |

The request is `$SYS.REQ.ACCOUNT.<account>.<endpoint>`, so the cluster entry needs system-account credentials. The reply of each server is shown as JSON.

[Open the Accounts tab in the demo](/cheshmhayash/demo/#accounts).
