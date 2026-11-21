---
title: HTTP API
description: The routes of the cheshmhayash HTTP API, which passes NATS monitoring and JetStream replies through as JSON.
---

The dashboard is a client of this API, and you can call it from scripts too. Replies from NATS pass through as they are, so the JSON is the same as from the server's HTTP monitoring port.

Destructive calls need `?confirm=true`. Without it the server answers `428 Precondition Required`. With [authentication](/cheshmhayash/guides/auth/) on, every `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` needs the admin role.

## Servers and accounts

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness and readiness. |
| `GET` | `/api/version` | The build version. |
| `GET` | `/api/admin/clusters` | The configured cluster names. |
| `GET` | `/api/admin/clusters/{c}/servers` | `$SYS.REQ.SERVER.PING` discovery. |
| `GET` | `/api/admin/clusters/{c}/servers/{endpoint}` | PING with an endpoint, for example `VARZ`, from every server. |
| `GET` | `/api/admin/clusters/{c}/servers/{id}/{endpoint}` | One server's endpoint. |
| `GET` | `/api/admin/clusters/{c}/accounts/{account}/{endpoint}` | An account-scoped endpoint. |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/reload` | Reload the server's config. |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/lame-duck` | Drain the server. |
| `POST` | `/api/admin/clusters/{c}/servers/{id}/actions/kick` | Body: `{"cid": N}`. |

Server endpoints: `VARZ`, `CONNZ`, `ROUTEZ`, `GATEWAYZ`, `LEAFZ`, `SUBSZ`, `JSZ`, `ACCOUNTZ`, `HEALTHZ`, `STATSZ`. Account endpoints: `CONNZ`, `LEAFZ`, `SUBSZ`, `JSZ`, `INFO`.

## JetStream

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/jsm/clusters/{c}/overview` | The cluster-wide overview from the cache. `?live=true` skips the cache. |
| `GET` | `/api/jsm/clusters/{c}/overview/stream` | Server-Sent Events: one frame per cache refresh, and a heartbeat every 20 seconds. |
| `GET` | `/api/jsm/clusters/{c}/health` | [Health](/cheshmhayash/dashboard/health/) findings. `?min_idle=24h&min_pending=1&live=true`. |
| `GET` | `/api/jsm/clusters/{c}/streams?offset=N` | List streams, a page at a time. |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}` | Stream info. |
| `PUT` | `/api/jsm/clusters/{c}/streams/{s}` | Update with a full `StreamConfig`. |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/purge?confirm=true` | Remove all messages. |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}?confirm=true` | Delete the stream. |
| `POST` | `/api/jsm/clusters/{c}/actions/meta-stepdown?confirm=true` | New meta leader. |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/actions/stepdown?confirm=true` | New stream leader. |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers?offset=N` | List consumers. |
| `GET` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}` | Consumer info. |
| `DELETE` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}?confirm=true` | Delete the consumer. |
| `POST` | `/api/jsm/clusters/{c}/streams/{s}/consumers/{con}/actions/stepdown?confirm=true` | New consumer leader. |

## Tap

Only when `[tap] enabled = true`.

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/tap/config` | The tap limits. |
| `GET` | `/api/tap/clusters/{c}/stream?subject=&type=` | Server-Sent Events: `message`, `stats` and `end`. |

## Auth

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/auth/login` | Redirect to the identity provider (`oidc` mode). |
| `GET` | `/api/auth/callback` | The OIDC redirect target. |
| `POST` | `/api/auth/logout` | Clear the session cookie. |
| `GET` | `/api/auth/me` | The identity and role: 200, 401, or 404 when auth is off. |

## Example

```sh
curl -s http://127.0.0.1:1378/api/jsm/clusters/prod/health?min_idle=6h | jq '.summary'
```
