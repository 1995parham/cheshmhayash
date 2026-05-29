import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api";
import { bytes, iso, num } from "../fmt";
import { useCanWrite } from "../state/access";
import { useToast } from "../state/toast";
import type { ConnzReply, HealthzReply, PingReply, VarzReply } from "../types";
import { useConfirm } from "./ConfirmDialog";

const SUBTABS = [
  "overview",
  "VARZ",
  "CONNZ",
  "SUBSZ",
  "JSZ",
  "ROUTEZ",
  "GATEWAYZ",
  "LEAFZ",
  "HEALTHZ",
  "ACCOUNTZ",
] as const;
type Subtab = (typeof SUBTABS)[number];

interface Props {
  cluster: string;
  server: PingReply;
  onClose: () => void;
}

export function ServerDetail({ cluster, server, onClose }: Props) {
  const [tab, setTab] = useState<Subtab>("overview");
  const canWrite = useCanWrite();
  const id = server.server.id;

  return (
    <section className="detail">
      <header className="detail-header">
        <div>
          <div className="title">{server.server.name}</div>
          <div className="sub">{id}</div>
        </div>
        <div className="spacer"></div>
        {canWrite ? <ServerActions cluster={cluster} server={server} /> : null}
        <button className="close-btn" onClick={onClose} aria-label="close">
          <X size={18} />
        </button>
      </header>
      <div className="subtabs">
        {SUBTABS.map((t) => (
          <button key={t} className={t === tab ? "active" : ""} onClick={() => setTab(t)}>
            {t}
          </button>
        ))}
      </div>
      <div className="detail-body">
        {tab === "overview" ? (
          <Overview reply={server} />
        ) : (
          <Endpoint cluster={cluster} id={id} ep={tab} />
        )}
      </div>
    </section>
  );
}

function ServerActions({ cluster, server }: { cluster: string; server: PingReply }) {
  const toast = useToast();
  const confirm = useConfirm();
  const id = server.server.id;
  const name = server.server.name;

  async function reload() {
    if (!(await confirm.ask("Reload config", `Reload server ${name}?`))) return;
    try {
      await api.reload(cluster, id);
      toast.push("reload accepted", "ok");
    } catch (e) {
      toast.push(`reload failed: ${(e as Error).message}`, "error");
    }
  }
  async function lameDuck() {
    if (!(await confirm.ask("Lame-duck mode", `Drain clients from ${name}?`))) return;
    try {
      await api.lameDuck(cluster, id);
      toast.push("lame-duck accepted", "ok");
    } catch (e) {
      toast.push(`lame-duck failed: ${(e as Error).message}`, "error");
    }
  }
  async function kick() {
    const v = window.prompt("CID to kick:");
    if (!v) return;
    const cid = Number(v);
    if (!Number.isFinite(cid) || cid <= 0) {
      toast.push("invalid CID", "error");
      return;
    }
    if (!(await confirm.ask("Kick connection", `Disconnect CID ${cid} on ${name}?`))) return;
    try {
      await api.kick(cluster, id, cid);
      toast.push("kick accepted", "ok");
    } catch (e) {
      toast.push(`kick failed: ${(e as Error).message}`, "error");
    }
  }

  return (
    <div className="detail-actions">
      <button onClick={reload}>Reload config</button>
      <button className="danger" onClick={lameDuck}>
        Lame duck
      </button>
      <button className="danger" onClick={kick}>
        Kick CID…
      </button>
    </div>
  );
}

function Overview({ reply }: { reply: PingReply }) {
  const s = reply.server;
  const z = reply.statsz ?? {};
  const meta = z.jetstream?.meta;
  return (
    <div className="kv">
      <KV k="name" v={s.name} />
      <KV k="id" v={s.id} />
      <KV k="version" v={s.ver} />
      <KV k="cluster" v={s.cluster} />
      <KV k="host" v={s.host} />
      <KV k="jetstream" v={s.jetstream ? "yes" : "no"} />
      <KV k="time" v={iso(s.time)} />
      <KV k="connections" v={num(z.connections)} />
      <KV k="total connections" v={num(z.total_connections)} />
      <KV k="subscriptions" v={num(z.num_subscriptions ?? z.subscriptions)} />
      <KV k="in msgs" v={num(z.in_msgs)} />
      <KV k="out msgs" v={num(z.out_msgs)} />
      <KV k="in bytes" v={bytes(z.in_bytes)} />
      <KV k="out bytes" v={bytes(z.out_bytes)} />
      <KV k="slow consumers" v={num(z.slow_consumers)} />
      <KV k="cpu" v={z.cpu != null ? `${z.cpu}%` : "—"} />
      <KV k="mem" v={bytes(z.mem)} />
      <KV k="cores" v={num(z.cores)} />
      <KV k="gomaxprocs" v={num(z.gomaxprocs)} />
      <KV k="active accounts" v={num(z.active_accounts)} />
      <KV k="active servers" v={num(z.active_servers)} />
      {meta ? <KV k="meta leader" v={meta.leader} /> : null}
      {meta ? <KV k="meta cluster size" v={num(meta.cluster_size)} /> : null}
      {meta ? <KV k="meta pending" v={num(meta.pending)} /> : null}
    </div>
  );
}

function Endpoint({ cluster, id, ep }: { cluster: string; id: string; ep: Subtab }) {
  const [data, setData] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setErr(null);
    api
      .server<unknown>(cluster, id, ep)
      .then((d) => setData(d))
      .catch((e: Error) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [cluster, id, ep]);

  if (loading) return <div className="spinner">loading {ep}…</div>;
  if (err) return <div className="empty">request failed: {err}</div>;

  const d = (data as { data?: unknown }).data ?? data;
  switch (ep) {
    case "VARZ":
      return <VarzView v={d as VarzReply} />;
    case "CONNZ":
      return <ConnzView d={d as ConnzReply} />;
    case "HEALTHZ":
      return <HealthzView d={d as HealthzReply} />;
    default:
      return <pre className="raw">{JSON.stringify(data, null, 2)}</pre>;
  }
}

function KV({ k, v }: { k: string; v: unknown }) {
  return (
    <>
      <div className="k">{k}</div>
      <div className="v">{v == null || v === "" ? "—" : String(v)}</div>
    </>
  );
}

function VarzView({ v }: { v: VarzReply }) {
  return (
    <div className="kv">
      <KV k="name" v={v.server_name} />
      <KV k="id" v={v.server_id} />
      <KV k="version" v={v.version} />
      <KV k="go" v={v.go} />
      <KV k="git" v={v.git_commit} />
      <KV k="uptime" v={v.uptime} />
      <KV k="host" v={`${v.host}:${v.port}`} />
      <KV k="max payload" v={bytes(v.max_payload)} />
      <KV k="max conns" v={num(v.max_connections)} />
      <KV k="connections" v={num(v.connections)} />
      <KV k="total connections" v={num(v.total_connections)} />
      <KV k="routes" v={num(v.routes)} />
      <KV k="leafs" v={num(v.leafnodes)} />
      <KV k="in msgs" v={num(v.in_msgs)} />
      <KV k="out msgs" v={num(v.out_msgs)} />
      <KV k="in bytes" v={bytes(v.in_bytes)} />
      <KV k="out bytes" v={bytes(v.out_bytes)} />
      <KV k="slow consumers" v={num(v.slow_consumers)} />
      <KV k="cpu" v={v.cpu != null ? `${v.cpu}%` : "—"} />
      <KV k="mem" v={bytes(v.mem)} />
      <KV k="config load time" v={iso(v.config_load_time)} />
      <KV k="jetstream" v={v.jetstream?.config ? "enabled" : "no"} />
    </div>
  );
}

function ConnzView({ d }: { d: ConnzReply }) {
  const conns = d.connections ?? [];
  if (conns.length === 0) return <div className="empty">no connections</div>;
  return (
    <>
      <div className="muted" style={{ marginBottom: 8 }}>
        {num(d.num_connections)} of {num(d.total)} (offset {d.offset ?? 0})
      </div>
      <table className="t">
        <thead>
          <tr>
            <th>CID</th>
            <th>name</th>
            <th>account</th>
            <th>ip:port</th>
            <th>subs</th>
            <th>in</th>
            <th>out</th>
            <th>idle</th>
          </tr>
        </thead>
        <tbody>
          {conns.map((c) => (
            <tr key={c.cid}>
              <td className="num">{c.cid}</td>
              <td>{c.name ?? ""}</td>
              <td className="mono">{c.account ?? ""}</td>
              <td className="mono">{`${c.ip ?? ""}:${c.port ?? ""}`}</td>
              <td className="num">{num(c.subscriptions)}</td>
              <td className="num">{num(c.in_msgs)}</td>
              <td className="num">{num(c.out_msgs)}</td>
              <td className="mono">{c.idle ?? ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}

function HealthzView({ d }: { d: HealthzReply }) {
  const ok = d.status === "ok";
  return (
    <div className="kv">
      <KV
        k="status"
        v={
          <span>
            <span className={`dot ${ok ? "good" : "bad"}`}></span> {d.status ?? "—"}
          </span>
        }
      />
      {d.error ? <KV k="error" v={d.error} /> : null}
    </div>
  );
}
