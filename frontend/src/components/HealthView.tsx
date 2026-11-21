import clsx from "clsx";
import { ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { type ApiError, api } from "../api";
import { duration, num } from "../fmt";
import { useOverviewStream } from "../hooks/useOverviewStream";
import type {
  HealthFinding,
  HealthKind,
  HealthPeer,
  HealthReport,
  HealthServerLoad,
  HealthSeverity,
} from "../types";
import { StreamDetail } from "./StreamDetail";

interface Props {
  cluster: string;
  refreshKey: number;
}

// How often the view asks for a new report. The server reads the cached
// overview, so a short period costs no NATS traffic.
const POLL_MS = 30_000;

const IDLE_CHOICES = ["1h", "6h", "24h", "72h", "168h"];

const KIND_LABEL: Record<HealthKind, string> = {
  stale_consumer: "stale consumer",
  ack_pending_full: "ack pending full",
  no_leader: "no leader",
  replica_offline: "replica offline",
  replica_lagging: "replica lagging",
  unlimited_stream: "no limits",
  near_limit: "near limit",
  under_replicated: "one replica",
  ghost_peer: "ghost peer",
  lagging_peer: "lagging peer",
};

const KIND_HELP: Record<HealthKind, string> = {
  stale_consumer:
    "Messages wait and nobody acked for longer than min idle. Delete it with `consumer scan` + `consumer apply` if it is abandoned.",
  ack_pending_full:
    "Unacknowledged messages reached max_ack_pending, so the server delivers nothing more until the client acks.",
  no_leader: "The Raft group has no leader. Reads and writes stop until a leader is elected.",
  replica_offline: "A replica is offline. The group keeps quorum while most replicas run.",
  replica_lagging: "A replica is online but not caught up with the leader.",
  unlimited_stream:
    "Limits retention with no max_age, max_bytes or max_msgs: the stream grows until the disk is full.",
  near_limit:
    "The stream is above 90% of max_bytes or max_msgs. With discard new, publishes fail at the limit.",
  under_replicated: "One replica: the stream is unavailable while its server is down.",
  ghost_peer:
    "Offline in every Raft group that names it and leads none: a removed server that was never peer-removed.",
  lagging_peer: "Named by Raft groups but current in none of them.",
};

const SEVERITIES: HealthSeverity[] = ["critical", "warning", "info"];

export function HealthView({ cluster, refreshKey }: Props) {
  const { overview } = useOverviewStream(cluster, refreshKey);
  const [report, setReport] = useState<HealthReport | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [minIdle, setMinIdle] = useState("24h");
  const [minPending, setMinPending] = useState(1);
  const [severities, setSeverities] = useState<Set<HealthSeverity>>(
    () => new Set<HealthSeverity>(["critical", "warning"]),
  );
  const [kind, setKind] = useState<HealthKind | null>(null);
  const [query, setQuery] = useState("");
  const [openStream, setOpenStream] = useState<{ account: string; stream: string } | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    api
      .health(cluster, minIdle, minPending)
      .then((r) => {
        setReport(r);
        setErr(null);
      })
      .catch((e: ApiError) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [cluster, minIdle, minPending]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: refreshKey is the manual refresh trigger.
  useEffect(() => {
    load();
    const t = window.setInterval(load, POLL_MS);
    return () => window.clearInterval(t);
  }, [load, refreshKey]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: cluster is an intentional reset trigger.
  useEffect(() => {
    setReport(null);
    setKind(null);
    setOpenStream(null);
  }, [cluster]);

  const kindCounts = useMemo(() => {
    const m = new Map<HealthKind, number>();
    for (const f of report?.findings ?? []) {
      if (severities.has(f.severity)) m.set(f.kind, (m.get(f.kind) ?? 0) + 1);
    }
    return [...m.entries()].sort((a, b) => b[1] - a[1]);
  }, [report, severities]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (report?.findings ?? []).filter(
      (f) =>
        severities.has(f.severity) &&
        (kind === null || f.kind === kind) &&
        (q === "" ||
          [f.account, f.stream, f.consumer, f.peer, f.message]
            .filter(Boolean)
            .some((v) => v?.toLowerCase().includes(q))),
    );
  }, [report, severities, kind, query]);

  if (!report) {
    if (err) return <div className="empty">health check failed: {err}</div>;
    return <div className="spinner">checking JetStream health…</div>;
  }

  const toggleSeverity = (s: HealthSeverity) => {
    setSeverities((cur) => {
      const next = new Set(cur);
      if (next.has(s)) next.delete(s);
      else next.add(s);
      return next;
    });
    setKind(null);
  };

  const s = report.summary;
  const healthy = s.critical === 0 && s.warning === 0;

  return (
    <section className="health">
      <div className="row-toolbar">
        <span className="muted">
          read {num(s.streams)} streams and {num(s.consumers)} consumers · overview of{" "}
          {new Date(report.updated).toLocaleTimeString()}
          {loading ? " · checking…" : ""}
          {err ? ` · last check failed: ${err}` : ""}
        </span>
        <div className="health-thresholds">
          <label>
            stale after
            <select value={minIdle} onChange={(e) => setMinIdle(e.target.value)}>
              {IDLE_CHOICES.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </label>
          <label>
            with at least
            <input
              type="number"
              min={0}
              value={minPending}
              onChange={(e) => setMinPending(Math.max(0, Number(e.target.value) || 0))}
            />
            pending
          </label>
        </div>
      </div>

      <div className="summary">
        {SEVERITIES.map((sev) => (
          <button
            key={sev}
            type="button"
            className={clsx("stat", "health-stat", sev, severities.has(sev) && "on")}
            onClick={() => toggleSeverity(sev)}
            title={severities.has(sev) ? `hide ${sev}` : `show ${sev}`}
          >
            <div className="label">{sev}</div>
            <div className="value">{num(s[sev])}</div>
          </button>
        ))}
      </div>

      {healthy ? (
        <div className="health-ok">
          <ShieldCheck size={16} /> No critical or warning findings on {cluster}.
        </div>
      ) : null}

      <div className="health-filters">
        <button
          type="button"
          className={clsx("chip", kind === null && "on")}
          onClick={() => setKind(null)}
        >
          all
        </button>
        {kindCounts.map(([k, n]) => (
          <button
            key={k}
            type="button"
            className={clsx("chip", kind === k && "on")}
            onClick={() => setKind(kind === k ? null : k)}
            title={KIND_HELP[k]}
          >
            {KIND_LABEL[k]} <b>{n}</b>
          </button>
        ))}
        <input
          className="health-search"
          placeholder="filter by name or text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>

      <FindingsTable
        findings={visible}
        onOpenStream={(account, stream) => setOpenStream({ account, stream })}
      />

      <div className="health-grid">
        <PeersTable peers={report.peers ?? []} />
        <PlacementTable placement={report.placement ?? []} />
      </div>

      {openStream && overview ? (
        <StreamDetail
          cluster={cluster}
          overview={overview}
          account={openStream.account}
          streamName={openStream.stream}
          onClose={() => setOpenStream(null)}
          onUpdated={() => {
            setOpenStream(null);
            load();
          }}
        />
      ) : null}
    </section>
  );
}

function FindingsTable({
  findings,
  onOpenStream,
}: {
  findings: HealthFinding[];
  onOpenStream: (account: string, stream: string) => void;
}) {
  if (findings.length === 0) {
    return <div className="empty">no findings match the filters</div>;
  }
  return (
    <table className="t health-findings">
      <thead>
        <tr>
          <th>severity</th>
          <th>problem</th>
          <th>account</th>
          <th>asset</th>
          <th>detail</th>
        </tr>
      </thead>
      <tbody>
        {findings.map((f) => (
          <tr key={`${f.kind}/${f.account}/${f.stream}/${f.consumer}/${f.peer}`}>
            <td>
              <span className={clsx("sev", f.severity)}>{f.severity}</span>
            </td>
            <td title={KIND_HELP[f.kind]}>{KIND_LABEL[f.kind]}</td>
            <td className="mono">{f.account ?? ""}</td>
            <td className="mono">
              {f.stream && f.account ? (
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => onOpenStream(f.account ?? "", f.stream ?? "")}
                  title="open the stream"
                >
                  {f.stream}
                </button>
              ) : null}
              {f.consumer ? <span> / {f.consumer}</span> : null}
              {f.peer ? <span>{f.peer}</span> : null}
            </td>
            <td>
              {f.message}
              {f.idle && f.kind === "stale_consumer" ? (
                <span className="muted"> (idle {duration(f.idle)})</span>
              ) : null}
              {f.managed ? (
                <span className="meta-badge" title={f.managed}>
                  managed
                </span>
              ) : null}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function PeersTable({ peers }: { peers: HealthPeer[] }) {
  return (
    <div>
      <h4>Raft peers</h4>
      {peers.length === 0 ? (
        <div className="empty">no replicated streams or consumers</div>
      ) : (
        <table className="t">
          <thead>
            <tr>
              <th>server</th>
              <th>health</th>
              <th>groups</th>
              <th>leads</th>
              <th>current</th>
              <th>offline</th>
            </tr>
          </thead>
          <tbody>
            {peers.map((p) => (
              <tr key={p.peer} title={p.sample?.join(", ")}>
                <td className="mono">{p.peer}</td>
                <td>
                  <span
                    className={clsx(
                      "dot",
                      p.health === "OK" ? "good" : p.health === "GHOST" ? "bad" : "warn",
                    )}
                  />{" "}
                  {p.health.toLowerCase()}
                </td>
                <td className="num">{num(p.groups)}</td>
                <td className="num">{num(p.leads)}</td>
                <td className="num">{num(p.current)}</td>
                <td className="num">{num(p.offline)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function PlacementTable({ placement }: { placement: HealthServerLoad[] }) {
  const maxReplicas = Math.max(1, ...placement.map((p) => p.replicas));
  return (
    <div>
      <h4>Stream placement</h4>
      {placement.length === 0 ? (
        <div className="empty">no clustered streams</div>
      ) : (
        <table className="t">
          <thead>
            <tr>
              <th>server</th>
              <th>leads</th>
              <th>replicas</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {placement.map((p) => (
              <tr key={p.server}>
                <td className="mono">{p.server}</td>
                <td className="num">{num(p.leads)}</td>
                <td className="num">{num(p.replicas)}</td>
                <td className="health-bar-cell">
                  <div
                    className="health-bar"
                    style={{ width: `${(100 * p.replicas) / maxReplicas}%` }}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
