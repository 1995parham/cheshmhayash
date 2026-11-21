---
title: Deploy
description: Run cheshmhayash with Docker or the Helm chart, pick its components, and add the MCP server, the tap, the bot and ingress.
---

## The binary and its components

`cheshmhayash serve` runs three components. Pick them with `--components` (or `CHESHMHAYASH_COMPONENTS`); all three run by default:

| Component | What it is |
| --- | --- |
| `api` | The JSON API and the overview cache. |
| `ui` | The React panel, served from `--static-dir` (default `frontend/dist`). |
| `notify` | Sends JetStream advisories to Slack, Mattermost or Matrix webhooks. |

`/healthz` is always served. Without `api`, `/api/*` answers 404. An unknown component name is fatal at start.

```sh
cheshmhayash serve --components api,ui   # no notifier
cheshmhayash serve --components notify   # notifier only
```

Other `serve` flags: `--overview-period` (default `10s`) sets how often the JetStream overview cache refreshes, and `--schemas` adds a schemas file for the tap.

## Docker

The image is `ghcr.io/1995parham/cheshmhayash`, on distroless with a non-root user. It has one binary; the default command is `serve`.

```sh
docker run --rm -p 1378:1378 \
  -v "$PWD/settings.toml:/app/settings.toml:ro" \
  ghcr.io/1995parham/cheshmhayash
```

Releases are signed with cosign (keyless), and the image carries an SBOM and provenance.

## Helm

The chart is `oci://ghcr.io/1995parham/cheshmhayash-chart`. Its version moves with each release; the app version follows the image tag.

```sh
helm install panel oci://ghcr.io/1995parham/cheshmhayash-chart -f my-values.yaml
```

### Clusters

Each entry of `clusters` becomes a `[[nats]]` block. `auth` takes exactly one of three shapes:

```yaml
clusters:
  - name: prod
    url: nats://nats.nats.svc.cluster.local:4222
    requestTimeoutMs: 5000
    discoveryTimeoutMs: 1500
    auth:
      # 1. the chart writes a Secret from these values
      # userPassword: { user: admin, password: changeme }

      # 2. env vars from a Secret you own (recommended)
      existingSecret:
        name: nats-sys-creds
        userKey: user
        passwordKey: password

      # 3. a .creds file mounted read-only (for nkeys and JWTs)
      # credsFileSecret: { name: nats-sys-creds, key: sys.creds }
```

### Values you will likely set

| Value | Default | Meaning |
| --- | --- | --- |
| `replicaCount` | `2` | Dashboard replicas. |
| `components` | `[api, ui, notify]` | Passed as `--components`. |
| `notify` | `[]` | Webhook entries: `provider` (`slack`, `mattermost`, `matrix`), and `url` or `urlSecret`. |
| `schemas.configMap` / `schemas.image` | | Mount a schemas folder from a ConfigMap or an image at `schemas.mountPath` (`/schemas`). |
| `tap.enabled` | `false` | The [live tap](/cheshmhayash/dashboard/tap/), with `allow`, `maxRate`, `maxDurationSeconds`, `maxTaps` and `role`. |
| `auth.*` | off | [Authentication](/cheshmhayash/guides/auth/). |
| `mcp.enabled` | `false` | Add the MCP HTTP server as its own Deployment and Service. |
| `mcp.write` | `false` | Register the destructive MCP tools. |
| `mcp.allowSampling` | `false` | Register `subject_sample`. |
| `bot.enabled` | `false` | Run the [operations bot](/cheshmhayash/guides/bot/). |
| `ingress.*` / `httpProxy.*` | off | A plain Ingress, or a Contour HTTPProxy. Do not turn on both for one host name. |
| `networkPolicy.enabled` | `false` | Allow ingress from chosen selectors, and egress to NATS and DNS only. |
| `autoscaling.enabled` | `false` | An HPA for the dashboard. |
| `extraEnv`, `extraEnvFrom` | `[]` | Extra environment, for example `LOG_LEVEL`. |

The chart rolls the Deployment when the rendered `settings.toml` changes (`restartOnConfigChange`), runs as non-root with a read-only root filesystem, and adds a PodDisruptionBudget.

### MCP over HTTP

```yaml
mcp:
  enabled: true
  port: 8080
  write: false
  allowSampling: false
auth:
  mcpKeys:
    - name: ci-bot
      existingSecret: { name: cheshmhayash-mcp, key: ci-bot }
```

Route `/mcp` and `/.well-known/oauth-protected-resource` to the MCP Service, and the rest to the dashboard. The MCP server's SSE channel sends a keep-alive every 30 seconds, so set the proxy idle timeout above that.

## Notifications

The `notify` component listens to `$JS.EVENT.ADVISORY.>` and to the system-account bridge of the same events on every cluster. It posts stream and consumer create, delete and update, leader elections and lost quorum as chat messages:

```toml
[[notify]]
provider = "slack"          # slack | mattermost | matrix
url = "https://hooks.slack.com/services/T000/B000/XXX"
channel = "#nats-ops"       # optional
username = "cheshmhayash"   # optional
```

Matrix needs a bridge that takes Slack-style webhooks, such as matrix-hookshot. Delivery is best effort: a permission error is logged, not fatal.
