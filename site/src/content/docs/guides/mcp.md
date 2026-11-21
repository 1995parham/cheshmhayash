---
title: MCP server
description: Connect Claude Code, Claude Desktop or any MCP client to your NATS clusters with the cheshmhayash MCP server.
---

`cheshmhayash mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server. It gives an LLM agent tools to inspect NATS clusters: list servers, read monitoring endpoints, read the JetStream overview, and find what misbehaves. It reads the same `settings.toml` as the dashboard.

It is **read-only by default**. Tools that change something, and the tool that reads message payloads, are opt-in. The full list is in the [MCP tools reference](/cheshmhayash/reference/mcp-tools/).

## Transports

| Mode | Command | Use it for |
| --- | --- | --- |
| stdio | `cheshmhayash mcp` | A local client that starts the process, such as Claude Code or Claude Desktop. |
| Streamable HTTP | `cheshmhayash mcp --http` | A shared server that many clients reach over the network. |

In stdio mode, standard output is the JSON-RPC channel, so logs always go to standard error.

In HTTP mode, the server serves `POST /mcp` (JSON-RPC), `GET /mcp` (an SSE channel for server notifications), `/healthz`, and the OAuth metadata when it is on. It listens on `server.host:server.port` from the settings, or on `--addr`:

```sh
cheshmhayash mcp --http --addr :8080
```

## Flags

| Flag | Env | Meaning |
| --- | --- | --- |
| `--http` | | Serve Streamable HTTP instead of stdio. |
| `--addr` | | The listen address for `--http`. The default comes from the settings. |
| `--write` | `CHESHMHAYASH_MCP_WRITE=1` | Also register the destructive tools: purge, delete, kick, reload, lame duck, step down and stream update. |
| `--allow-sampling` | `CHESHMHAYASH_MCP_SAMPLE=1` | Register `subject_sample`, which sends decoded message payloads to the model. |
| `--schemas` | `CHESHMHAYASH_SCHEMAS` | An extra schemas file for `subject_sample`. Repeat it for more. |

:::caution
`--allow-sampling` sends message content to the model. Turn it on only when the subjects hold nothing that must not leave your network. `--write` lets the model delete streams and consumers. Each destructive tool also needs `confirm: true` in the call.
:::

## Claude Code

Add a local server that runs over stdio:

```sh
claude mcp add cheshmhayash -- \
  /usr/local/bin/cheshmhayash --config /path/to/settings.toml mcp
```

Add a shared HTTP server with a bearer key:

```sh
claude mcp add --transport http cheshmhayash https://nats-mcp.example.com/mcp \
  --header "Authorization: Bearer $CHESHMHAYASH_MCP_KEY"
```

When the server has [OAuth](#authentication) on, leave out the header. Claude Code finds the identity provider from the server's metadata and opens a browser to sign in.

Then ask, for example: "Which consumers on the prod cluster are stale?" or "Is any JetStream peer a ghost?".

## Claude Desktop

Add the server to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "cheshmhayash": {
      "command": "/usr/local/bin/cheshmhayash",
      "args": ["--config", "/path/to/settings.toml", "mcp"]
    }
  }
}
```

To let the model read payloads, add `"--allow-sampling"` after `"mcp"`, and a `"--schemas"` file to decode protobuf.

## Docker

```sh
docker run --rm -i \
  -v "$PWD/settings.toml:/app/settings.toml:ro" \
  ghcr.io/1995parham/cheshmhayash mcp
```

Use `-i` for stdio. For HTTP, publish a port and add `--http --addr :8080`.

## Authentication

stdio mode has no authentication: the client that starts the process owns it. In HTTP mode, `/mcp` accepts three kinds of credentials, checked in this order:

1. **Static keys.** List them under `auth.mcp_keys`. A client sends `Authorization: Bearer <key>`. The comparison is constant-time. Keys work even when dashboard auth is off.
2. **OAuth access tokens** (`auth.mcp_oauth`). `/mcp` accepts access-token JWTs from the same OIDC issuer as the dashboard. It checks the signature, issuer, expiry and audience, then the same allowlist. It serves the RFC 9728 metadata at `/.well-known/oauth-protected-resource`, so clients find the identity provider by themselves.
3. **Unverified JWTs** (`auth.mcp_jwt`). Only for a trusted gateway that has already checked the token. cheshmhayash reads the claims without any verification.

```toml
[auth]
enabled = true

[[auth.mcp_keys]]
name = "ci-bot"
value = "a-long-random-string"

[auth.mcp_oauth]
enabled = true
resource = "https://nats-mcp.example.com/mcp"   # also the required token audience
# skip_audience_check = true                     # only for an IdP that cannot set the audience
```

The identity provider must put the `resource` URL in the token's `aud` claim. With Keycloak, add an audience mapper to the client. The [authentication guide](/cheshmhayash/guides/auth/) covers the allowlists.

Write tools are set when the server starts (`--write`), not per user.
