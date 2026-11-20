import type {
  AggregatedAccount,
  AggregatedOverview,
  AggregatedStream,
  JszData,
  PingReply,
  TapConfig,
  TapMessage,
  TapStats,
} from "./types";

export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, body: unknown) {
    const msg =
      body && typeof body === "object" && "message" in body && typeof body.message === "string"
        ? body.message
        : typeof body === "string"
          ? body
          : `HTTP ${status}`;
    super(msg);
    this.status = status;
    this.body = body;
  }
}

async function readBody(r: Response): Promise<unknown> {
  try {
    return await r.json();
  } catch {
    return await r.text();
  }
}

async function http<T>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  const r = await fetch(url, init);
  if (!r.ok) throw new ApiError(r.status, await readBody(r));
  return (await r.json()) as T;
}

const enc = encodeURIComponent;

// ---- Admin --------------------------------------------------------------
export const api = {
  version: () => http<{ version: string }>("GET", "/api/version"),
  clusters: () => http<string[]>("GET", "/api/admin/clusters"),
  ping: (c: string) => http<PingReply[]>("GET", `/api/admin/clusters/${enc(c)}/servers`),
  pingEndpoint: <T = unknown>(c: string, ep: string) =>
    http<T[]>("GET", `/api/admin/clusters/${enc(c)}/servers/${ep}`),
  server: <T = unknown>(c: string, id: string, ep: string) =>
    http<T>("GET", `/api/admin/clusters/${enc(c)}/servers/${enc(id)}/${ep}`),
  account: <T = unknown>(c: string, acct: string, ep: string) =>
    http<T[]>("GET", `/api/admin/clusters/${enc(c)}/accounts/${enc(acct)}/${ep}`),
  reload: (c: string, id: string) =>
    http<unknown>("POST", `/api/admin/clusters/${enc(c)}/servers/${enc(id)}/actions/reload`),
  lameDuck: (c: string, id: string) =>
    http<unknown>("POST", `/api/admin/clusters/${enc(c)}/servers/${enc(id)}/actions/lame-duck`),
  kick: (c: string, id: string, cid: number) =>
    http<unknown>("POST", `/api/admin/clusters/${enc(c)}/servers/${enc(id)}/actions/kick`, { cid }),

  // ---- JSM -------------------------------------------------------------
  jsOverview: (c: string) =>
    http<{ server: { name: string }; data: JszData }[]>(
      "GET",
      `/api/jsm/clusters/${enc(c)}/overview`,
    ),
  jsOverviewLive: (c: string) =>
    http<{ server: { name: string }; data: JszData }[]>(
      "GET",
      `/api/jsm/clusters/${enc(c)}/overview?live=true`,
    ),
  jsOverviewStreamURL: (c: string) => `/api/jsm/clusters/${enc(c)}/overview/stream`,
  streams: (c: string, offset = 0) =>
    http<unknown>("GET", `/api/jsm/clusters/${enc(c)}/streams?offset=${offset}`),
  stream: (c: string, s: string) =>
    http<unknown>("GET", `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}`),
  updateStream: (c: string, s: string, config: unknown) =>
    http<unknown>("PUT", `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}`, config),
  purgeStream: (c: string, s: string) =>
    http<unknown>("POST", `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/purge?confirm=true`),
  deleteStream: (c: string, s: string) =>
    http<unknown>("DELETE", `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}?confirm=true`),
  consumers: (c: string, s: string, offset = 0) =>
    http<unknown>(
      "GET",
      `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/consumers?offset=${offset}`,
    ),
  consumer: (c: string, s: string, n: string) =>
    http<unknown>("GET", `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/consumers/${enc(n)}`),
  deleteConsumer: (c: string, s: string, n: string) =>
    http<unknown>(
      "DELETE",
      `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/consumers/${enc(n)}?confirm=true`,
    ),
  metaStepdown: (c: string) =>
    http<unknown>("POST", `/api/jsm/clusters/${enc(c)}/actions/meta-stepdown?confirm=true`),
  streamStepdown: (c: string, s: string) =>
    http<unknown>(
      "POST",
      `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/actions/stepdown?confirm=true`,
    ),
  consumerStepdown: (c: string, s: string, n: string) =>
    http<unknown>(
      "POST",
      `/api/jsm/clusters/${enc(c)}/streams/${enc(s)}/consumers/${enc(n)}/actions/stepdown?confirm=true`,
    ),
};

// Fold the per-server JSZ replies into a single per-account / per-stream
// / per-consumer view. Each stream and consumer is counted once via its
// leader's report (the authoritative one for state).
export function aggregateOverview(
  replies: { server: { name: string }; data: JszData }[],
): AggregatedOverview {
  const accountMap = new Map<string, AggregatedAccount>();
  const streamMap = new Map<string, AggregatedStream>(); // key: `${account}/${stream}`
  let cluster: string | undefined;
  let meta = replies[0]?.data?.meta_cluster;

  for (const r of replies) {
    const sname = r.server?.name;
    const data = r.data ?? {};
    cluster ??= meta?.name;
    meta ??= data.meta_cluster;
    for (const a of data.account_details ?? []) {
      let acct = accountMap.get(a.name);
      if (!acct) {
        acct = {
          name: a.name,
          id: a.id,
          api: a.api,
          streams: [],
          totals: { streams: 0, consumers: 0, messages: 0, bytes: 0 },
        };
        accountMap.set(a.name, acct);
      }
      for (const s of a.stream_detail ?? []) {
        const key = `${a.name}/${s.name}`;
        const isLeader = s.cluster?.leader && s.cluster.leader === sname;
        let entry = streamMap.get(key);
        if (!entry) {
          entry = {
            name: s.name,
            account: a.name,
            created: s.created,
            config: s.config,
            state: s.state,
            cluster: s.cluster,
            consumers: [],
          };
          streamMap.set(key, entry);
          acct.streams.push(entry);
        }
        if (isLeader) {
          entry.state = s.state;
          entry.cluster = s.cluster;
          entry.config = s.config;
          acct.totals.streams++;
          acct.totals.messages += s.state?.messages ?? 0;
          acct.totals.bytes += s.state?.bytes ?? 0;
          entry.consumers = [];
          for (const c of s.consumer_detail ?? []) {
            const cleader = c.cluster?.leader;
            if (!cleader || cleader === sname) {
              entry.consumers.push({
                name: c.name,
                stream: s.name,
                account: a.name,
                config: c.config,
                created: c.created,
                cluster: c.cluster,
                num_pending: c.num_pending,
                num_ack_pending: c.num_ack_pending,
                num_redelivered: c.num_redelivered,
                num_waiting: c.num_waiting,
                delivered: c.delivered,
              });
              acct.totals.consumers++;
            }
          }
        }
      }
    }
  }

  const accountList = [...accountMap.values()].sort((a, b) => b.totals.streams - a.totals.streams);
  return {
    cluster: cluster ?? replies[0]?.data?.meta_cluster?.name,
    meta,
    accountList,
    totalAccounts: accountList.length,
    totalStreams: accountList.reduce((n, a) => n + a.totals.streams, 0),
    totalConsumers: accountList.reduce((n, a) => n + a.totals.consumers, 0),
    totalMessages: accountList.reduce((n, a) => n + a.totals.messages, 0),
    totalBytes: accountList.reduce((n, a) => n + a.totals.bytes, 0),
  };
}

// ---- Live tap -------------------------------------------------------------

export const tapConfig = () => http<TapConfig>("GET", "/api/tap/config");

export interface TapHandlers {
  onMessage: (m: TapMessage) => void;
  onStats: (s: TapStats) => void;
  onEnd: (reason: string) => void;
}

// openTap streams /api/tap/clusters/{c}/stream with fetch rather than
// EventSource: fetch exposes the status and error body of a refused tap,
// and does not reconnect by itself after the server ends the stream.
// It resolves when the stream ends; abort the signal to stop it.
export async function openTap(
  cluster: string,
  subject: string,
  type: string,
  signal: AbortSignal,
  h: TapHandlers,
): Promise<void> {
  const q = new URLSearchParams({ subject });
  if (type) q.set("type", type);
  const r = await fetch(`/api/tap/clusters/${enc(cluster)}/stream?${q}`, { signal });
  if (!r.ok || !r.body) throw new ApiError(r.status, await readBody(r));

  const reader = r.body.pipeThrough(new TextDecoderStream()).getReader();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    buf += value;
    let cut = buf.indexOf("\n\n");
    while (cut >= 0) {
      dispatch(buf.slice(0, cut), h);
      buf = buf.slice(cut + 2);
      cut = buf.indexOf("\n\n");
    }
  }
}

function dispatch(frame: string, h: TapHandlers) {
  let event = "message";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event: ")) event = line.slice(7);
    else if (line.startsWith("data: ")) data += line.slice(6);
  }
  if (!data) return; // heartbeat comment
  const v: unknown = JSON.parse(data);
  if (event === "message") h.onMessage(v as TapMessage);
  else if (event === "stats") h.onStats(v as TapStats);
  else if (event === "end") h.onEnd((v as { reason?: string }).reason ?? "end");
}
