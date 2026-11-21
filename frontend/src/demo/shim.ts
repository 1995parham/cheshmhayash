// Demo mode: the SPA runs with no backend. This module replaces fetch and
// EventSource for /api/* with answers from a recorded, renamed snapshot
// (frontend/demo/data, copied next to the build), so the real components
// render real-looking data. Writes are refused, the overview numbers move a
// little on each tick, and the live tap plays synthetic messages.
//
// It is imported only when the bundle is built with `--mode demo`.

import type { HealthFinding, HealthReport, TapMessage } from "../types";

const DATA = `${import.meta.env.BASE_URL}data`;
const TICK_MS = 5_000;
const READ_ONLY = "This is a read-only demo: changes are turned off.";

const realFetch = window.fetch.bind(window);
const RealEventSource = window.EventSource;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const files = new Map<string, Promise<unknown>>();

// load reads one recorded reply; the file path is the API path with the
// query folded in by the recorder.
function load(segments: string[]): Promise<unknown> {
  const url = `${DATA}/${segments.map(encodeURIComponent).join("/")}.json`;
  let p = files.get(url);
  if (!p) {
    p = realFetch(url).then((r) => (r.ok ? r.json() : undefined));
    files.set(url, p);
  }
  return p;
}

// ---- moving numbers -------------------------------------------------------

const started = Date.now();

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

type Json = Record<string, unknown>;

function bump(obj: unknown, key: string, by: number) {
  const o = obj as Json | undefined;
  if (o && typeof o[key] === "number") o[key] = (o[key] as number) + by;
}

// jitter adds a few messages to busy streams for every tick since the page
// opened, and moves their consumers along, so the overview looks alive.
function jitter(replies: unknown): unknown {
  const ticks = Math.floor((Date.now() - started) / TICK_MS);
  const out = structuredClone(replies) as { data?: { account_details?: Json[] } }[];
  for (const rep of out) {
    for (const acct of rep.data?.account_details ?? []) {
      for (const st of (acct.stream_detail as Json[] | undefined) ?? []) {
        const state = st.state as Json | undefined;
        if (!state?.messages) continue;
        const rate = hash(String(st.name)) % 9;
        const add = rate * ticks;
        if (!add) continue;
        const size = Math.max(64, Math.round(Number(state.bytes) / Number(state.messages)));
        bump(state, "messages", add);
        bump(state, "bytes", add * size);
        bump(state, "last_seq", add);
        for (const c of (st.consumer_detail as Json[] | undefined) ?? []) {
          bump(c.delivered, "stream_seq", add);
          bump(c.delivered, "consumer_seq", add);
          bump(c.ack_floor, "stream_seq", add);
          bump(c.ack_floor, "consumer_seq", add);
        }
      }
    }
  }
  return out;
}

// ---- health ---------------------------------------------------------------

async function health(cluster: string, q: URLSearchParams): Promise<Response> {
  const idle = q.get("min_idle") || "24h";
  const minPending = Number(q.get("min_pending") || "1");
  const rep = (await load(["api", "jsm", "clusters", cluster, `health@${idle}`])) as
    | (HealthReport & { min_pending?: number })
    | undefined;
  if (!rep) return json({ message: `no snapshot for min_idle=${idle}` }, 404);
  const findings = rep.findings.filter(
    (f: HealthFinding) => f.kind !== "stale_consumer" || (f.pending ?? 0) >= minPending,
  );
  const count = (s: string) => findings.filter((f) => f.severity === s).length;
  return json({
    ...rep,
    findings,
    min_pending: minPending,
    updated: new Date().toISOString(),
    summary: {
      ...rep.summary,
      critical: count("critical"),
      warning: count("warning"),
      info: count("info"),
    },
  });
}

// ---- live tap -------------------------------------------------------------

const TAP_SUBJECTS: { subject: string; type?: string; make: (n: number) => unknown }[] = [
  {
    subject: "orders.created",
    type: "shop.v1.OrderCreated",
    make: (n) => ({
      orderId: `ord_${100200 + n}`,
      customerId: `cus_${(n * 37) % 9000}`,
      items: (n % 4) + 1,
      total: { currency: "EUR", units: 12 + (n % 80) },
    }),
  },
  {
    subject: "orders.packed",
    type: "shop.v1.OrderPacked",
    make: (n) => ({
      orderId: `ord_${100180 + n}`,
      warehouse: ["north", "south", "east"][n % 3],
      packedBy: `picker-${n % 12}`,
    }),
  },
  {
    subject: "courier.status",
    make: (n) => ({
      courier: `cr_${n % 40}`,
      status: ["idle", "en_route", "delivering"][n % 3],
      battery: 30 + (n % 70),
    }),
  },
  {
    subject: "payments.captured",
    type: "pay.v1.PaymentCaptured",
    make: (n) => ({
      paymentId: `pay_${7700 + n}`,
      orderId: `ord_${100170 + n}`,
      method: n % 2 ? "card" : "wallet",
    }),
  },
  { subject: "audit.log", make: (n) => `user u${n % 9} opened order ord_${100100 + n}` },
];

function matches(pattern: string, subject: string): boolean {
  const p = pattern.split(".");
  const s = subject.split(".");
  for (let i = 0; i < p.length; i++) {
    if (p[i] === ">") return s.length > i;
    if (i >= s.length || (p[i] !== "*" && p[i] !== s[i])) return false;
  }
  return p.length === s.length;
}

function tap(q: URLSearchParams, signal: AbortSignal | null | undefined): Response {
  const subject = q.get("subject") || ">";
  const pool = TAP_SUBJECTS.filter((t) => matches(subject, t.subject));
  const enc = new TextEncoder();
  let timer = 0;
  let stats = 0;
  const body = new ReadableStream<Uint8Array>({
    start(ctl) {
      let seq = 0;
      let sent = 0;
      let received = 0;
      const frame = (event: string, data: unknown) =>
        ctl.enqueue(enc.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`));
      const stop = (reason: string) => {
        window.clearInterval(timer);
        window.clearInterval(stats);
        try {
          frame("end", { reason });
          ctl.close();
        } catch {
          // the reader is already gone
        }
      };
      timer = window.setInterval(() => {
        received++;
        if (pool.length === 0) return;
        const t = pool[seq % pool.length];
        const payload = t.make(seq);
        const text = typeof payload === "string" ? payload : JSON.stringify(payload);
        const msg: TapMessage = {
          seq: ++seq,
          time: new Date().toISOString(),
          subject: t.subject,
          size: text.length,
          format: t.type ? "protobuf" : typeof payload === "string" ? "text" : "json",
          type: t.type,
          payload,
        };
        sent++;
        frame("message", msg);
        if (seq >= 300) stop("demo limit of 300 messages");
      }, 400);
      stats = window.setInterval(
        () => frame("stats", { received, sent, skipped: 0, dropped: 0 }),
        1000,
      );
      signal?.addEventListener("abort", () => stop("stopped"));
    },
    cancel() {
      window.clearInterval(timer);
      window.clearInterval(stats);
    },
  });
  return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
}

// ---- router ---------------------------------------------------------------

async function route(url: URL, method: string, signal?: AbortSignal | null): Promise<Response> {
  const segs = url.pathname
    .slice(url.pathname.indexOf("/api/") + 1)
    .split("/")
    .map(decodeURIComponent);
  const path = segs.join("/");

  if (path.startsWith("api/auth/")) return json({ message: "auth is off in the demo" }, 404);
  if (method !== "GET") return json({ message: READ_ONLY }, 403);

  if (path === "api/tap/config") {
    return json({ allow: [">"], max_rate: 50, max_duration_seconds: 120, can_tap: true });
  }
  if (segs[1] === "tap" && segs[4] === "stream") return tap(url.searchParams, signal);

  if (segs[1] === "jsm" && segs[4] === "health") return health(segs[3], url.searchParams);

  const data = await load(segs);
  if (data === undefined) {
    return json({ message: "This view is not part of the demo snapshot." }, 404);
  }
  if (segs[1] === "jsm" && segs[4] === "overview") return json(jitter(data));
  return json(data);
}

window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  const req = input instanceof Request ? input : null;
  const url = new URL(req ? req.url : String(input), window.location.href);
  if (!url.pathname.includes("/api/")) return realFetch(input, init);
  const method = (init?.method || req?.method || "GET").toUpperCase();
  return route(url, method, init?.signal ?? req?.signal);
};

// ---- overview stream ------------------------------------------------------

class DemoEventSource extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readyState = 0;
  readonly url: string;
  readonly withCredentials = false;
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  private timer = 0;

  constructor(url: string | URL) {
    super();
    this.url = String(url);
    const u = new URL(this.url, window.location.href);
    window.setTimeout(() => {
      this.readyState = 1;
      this.emit(new Event("open"));
      const send = () =>
        route(new URL(u.pathname.replace(/\/stream$/, ""), u), "GET")
          .then((r) => r.text())
          .then(
            (data) => this.readyState === 1 && this.emit(new MessageEvent("message", { data })),
          );
      void send();
      this.timer = window.setInterval(send, TICK_MS);
    }, 50);
  }

  private emit(e: Event) {
    this.dispatchEvent(e);
    const h =
      e.type === "open" ? this.onopen : e.type === "message" ? this.onmessage : this.onerror;
    h?.call(this, e as MessageEvent);
  }

  close() {
    this.readyState = 2;
    window.clearInterval(this.timer);
  }
}

window.EventSource = ((url: string | URL, init?: EventSourceInit) =>
  String(url).includes("/overview/stream")
    ? new DemoEventSource(url)
    : new RealEventSource(url, init)) as unknown as typeof EventSource;
Object.assign(window.EventSource, { CONNECTING: 0, OPEN: 1, CLOSED: 2 });

// ---- ribbon ---------------------------------------------------------------

const ribbon = document.createElement("div");
ribbon.className = "demo-ribbon";
ribbon.innerHTML =
  'Live demo: a read-only snapshot of a production cluster with renamed data. <a href="../">Read the docs</a>';
// ?clean hides the ribbon for screenshots.
if (!new URLSearchParams(window.location.search).has("clean")) document.body.prepend(ribbon);
