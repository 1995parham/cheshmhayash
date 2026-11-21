---
title: Authentication
description: Protect the cheshmhayash dashboard with OIDC sign-in or gateway JWTs, and split users into admin and read-only roles.
---

Authentication is off by default: the dashboard is open to anyone who can reach it. Turn it on with `[auth] enabled = true` and pick a mode.

## Modes

| Mode | How a user is identified | Needs |
| --- | --- | --- |
| `oidc` (default) | cheshmhayash runs the OIDC login flow (PKCE, state and nonce) and sets an HMAC-signed session cookie. There is no database. | `oidc.issuer`, `oidc.client_id`, `oidc.client_secret`, `oidc.redirect_url`, a `session.secret` of 16 characters or more, and an allowlist. |
| `jwt` | A gateway in front of cheshmhayash signs the user in and forwards `Authorization: Bearer <access token>`. cheshmhayash checks the token on each request against the issuer's keys (JWKS). | `oidc.issuer` and an allowlist. `jwt.audiences` optionally limits the `aud` values. |

In `jwt` mode there is no `/login`, `/callback`, `/logout` or cookie. `/api/auth/me` reports `"mode":"jwt"`, and the panel hides the sign-in and sign-out buttons.

## Allowlists and roles

```toml
[auth]
enabled = true
mode = "oidc"

[auth.oidc]
issuer = "https://id.example.com/realms/main"
client_id = "cheshmhayash"
client_secret = "..."
redirect_url = "https://nats.example.com/api/auth/callback"

[auth.session]
secret = "at-least-16-characters"
secure = true                 # send the cookie only over HTTPS

# Who may sign in (at least one list must be set).
[auth.access]
allowed_domains = ["example.com"]
groups_claim = "groups"

# Who may change things. Everyone else who signs in is read-only.
[auth.access.admin]
allowed_groups = ["nats-admins"]
```

| Role | Who | What |
| --- | --- | --- |
| `admin` | Matches `[auth.access.admin]`, or that list is empty. | Read and write. |
| `readonly` | Passes the sign-in allowlist, but not the admin one. | Read only. The panel hides the write buttons, and the API answers 403 to every `POST`, `PUT`, `PATCH` and `DELETE` under `/api/`. |

The write check is by HTTP method, so each new route is covered automatically. The role is in `GET /api/auth/me`.

## The MCP endpoint

The MCP HTTP server has its own credentials: static keys, OAuth access tokens and gateway JWTs. They are in the [MCP guide](/cheshmhayash/guides/mcp/#authentication).

## Environment variables

Every key can come from the environment. Use `CHESHMHAYASH__` with `__` between levels, comma-separated lists, and an index for list entries:

```sh
CHESHMHAYASH__AUTH__ENABLED=true
CHESHMHAYASH__AUTH__MODE=jwt
CHESHMHAYASH__AUTH__OIDC__ISSUER=https://id.example.com/realms/main
CHESHMHAYASH__AUTH__ACCESS__ALLOWED_DOMAINS=example.com
CHESHMHAYASH__AUTH__MCP_KEYS__0__NAME=ci-bot
CHESHMHAYASH__AUTH__MCP_KEYS__0__VALUE=a-long-random-string
```

In the Helm chart, the secret values (`oidc.clientSecret`, `session.secret`, MCP keys) come from a Secret that the chart makes, or from one you name.
