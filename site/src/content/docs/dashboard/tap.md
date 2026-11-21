---
title: Tap
description: The Tap tab streams the live messages of a NATS subject, decoded with protobuf schemas, with rate and time limits.
---

The Tap tab shows the messages of a subject as they arrive, decoded with the same chain as [`sub`](/cheshmhayash/cli/pub-sub/): protobuf by the schemas, then JSON, text, a schemaless protobuf dump, or base64. It uses core NATS and creates no JetStream consumer, so it is safe on production streams.

[Open the Tap tab in the demo](/cheshmhayash/demo/#tap). The demo has no live cluster, so it plays made-up sample messages on a few `orders`, `payments` and `courier` subjects.

## Using the tab

1. Type a subject. Wildcards work: `orders.>` or `payments.*.created`.
2. Optionally type a protobuf type (`shop:shop.v1.OrderCreated`) to force it for every message.
3. Click **Start**. Messages appear at the top, with subject, size, format and the decoded body.
4. Use **Pause** to stop adding messages, **Clear** to empty the list, and the filter box to search what is shown.

The status line counts messages received, shown, skipped (over the rate limit) and dropped.

## Turn it on

The tap shows message content, so it is off by default, and every tap is logged:

```toml
[tap]
enabled = true
allow = ["orders.>", "payments.*.created"]  # a tap must stay inside one of these
max_rate = 20                # messages per second per browser; the rest are counted as skipped
max_duration_seconds = 600   # a tap closes after this long
max_taps = 10                # concurrent taps per dashboard process
role = "admin"               # with auth on: "admin" (default) or "readonly"
```

The connection's account decides what a tap can see. A cluster entry that uses the system account sees only `$SYS` subjects. So add a `[[nats]]` entry with an application user for the subjects you want to tap. In the Helm chart, the same settings are under `tap.*`.

If a tap is refused (outside `allow`, too many taps, wrong role), the panel shows the reason.
