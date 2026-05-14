import { useEffect, useMemo, useState } from "react";
import { Crown, CircleDot, Minus, Filter } from "lucide-react";
import { aggregateOverview, api } from "../api";
import type { AggregatedConsumer, AggregatedOverview, AggregatedStream } from "../types";
import { num } from "../fmt";
import { TopologyGraph, type GraphRaftGroup } from "./TopologyGraph";
import { useConfirm } from "./ConfirmDialog";
import { useToast } from "../state/toast";

interface Props {
  cluster: string;
  refreshKey: number;
}

// What each cell in the heatmap means.
type Role = "leader" | "follower" | "stale" | "absent";

interface RaftRow {
  group: string;       // raft_group identifier (if known)
  label: string;       // human label rendered in the first column
  account?: string;    // for grouping/filter
  stream?: string;     // for grouping/filter
  leader?: string;     // server name of leader
  members: Map<string, { current: boolean }>;  // member server → freshness
}

interface ServerStats {
  name: string;
  metaLeader: boolean;
  metaFollower: boolean;
  streamLeader: number;
  streamFollower: number;
  consumerLeader: number;
  consumerFollower: number;
}

export function TopologyView({ cluster, refreshKey }: Props) {
  const [overview, setOverview] = useState<AggregatedOverview | null>(null);
  const [servers, setServers] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [accountFilter, setAccountFilter] = useState<string>("");
  const [showConsumers, setShowConsumers] = useState(false);

  useEffect(() => {
    setLoading(true);
    setErr(null);
    api
      .jsOverview(cluster)
      .then((replies) => {
        const agg = aggregateOverview(replies);
        setOverview(agg);
        // server order = ordered by server name from the JS overview replies
        setServers(replies.map((r) => r.server.name).sort());
      })
      .catch((e: Error) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [cluster, refreshKey]);

  const topology = useMemo(() => buildTopology(overview, servers), [overview, servers]);

  if (loading) return <div className="spinner">building topology…</div>;
  if (err) return <div className="empty">overview failed: {err}</div>;
  if (!overview || !topology) return null;

  const filteredStreams = accountFilter
    ? topology.streamRows.filter((r) => r.account === accountFilter)
    : topology.streamRows;
  const filteredConsumers = accountFilter
    ? topology.consumerRows.filter((r) => r.account === accountFilter)
    : topology.consumerRows;

  const accounts = overview.accountList.map((a) => a.name);

  // Flatten the matrix rows into the graph's GraphRaftGroup shape.
  const graphGroups: GraphRaftGroup[] = [
    {
      id: "meta",
      kind: "meta",
      leader: topology.metaRow.leader,
      members: Array.from(topology.metaRow.members.keys()),
      label: "_meta_",
    },
    ...filteredStreams.map<GraphRaftGroup>((r) => ({
      id: `s/${r.label}`,
      kind: "stream",
      leader: r.leader,
      members: Array.from(r.members.keys()),
      label: r.label,
    })),
    ...filteredConsumers.map<GraphRaftGroup>((r) => ({
      id: `c/${r.label}`,
      kind: "consumer",
      leader: r.leader,
      members: Array.from(r.members.keys()),
      label: r.label,
    })),
  ];

  return (
    <section>
      <div className="row-toolbar">
        <span className="muted">
          {topology.servers.length} servers · meta cluster size {overview.meta?.cluster_size ?? "?"} ·{" "}
          {filteredStreams.length} stream raft groups · {filteredConsumers.length} consumer raft groups
        </span>
        {accounts.length > 1 ? (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <Filter size={12} className="muted" />
            <select value={accountFilter} onChange={(e) => setAccountFilter(e.target.value)}>
              <option value="">all accounts</option>
              {accounts.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </span>
        ) : null}
      </div>

      <TopologyGraph
        servers={topology.servers}
        groups={graphGroups}
        metaLeader={overview.meta?.leader}
      />

      <DistributionChart stats={topology.serverStats} servers={topology.servers} />

      <h4 style={{ margin: "20px 0 6px", display: "flex", alignItems: "center", gap: 8 }}>
        Meta raft
        <MetaStepdownButton cluster={cluster} leader={overview.meta?.leader} onDone={() => setOverview(null)} />
      </h4>
      <p className="muted" style={{ marginTop: 0 }}>
        One Raft group per cluster, owned by <code>$SYS</code>. Manages stream/consumer
        placement decisions. Leader: <b className="mono">{overview.meta?.leader ?? "—"}</b>.
      </p>
      <Heatmap servers={topology.servers} rows={[topology.metaRow]} />

      <h4 style={{ margin: "20px 0 6px" }}>
        Stream raft groups <span className="muted">({filteredStreams.length})</span>
      </h4>
      <p className="muted" style={{ marginTop: 0 }}>
        One Raft group per replicated stream. Leader receives writes; followers track.
      </p>
      <Heatmap servers={topology.servers} rows={filteredStreams} />

      <h4 style={{ margin: "20px 0 6px" }}>
        Consumer raft groups <span className="muted">({filteredConsumers.length})</span>
      </h4>
      <p className="muted" style={{ marginTop: 0 }}>
        Replicated consumers run their own Raft group co-located with their parent
        stream's peers.{" "}
        {filteredConsumers.length > 30 && !showConsumers ? (
          <button className="link-btn" onClick={() => setShowConsumers(true)}>
            show all {filteredConsumers.length}
          </button>
        ) : null}
      </p>
      <Heatmap
        servers={topology.servers}
        rows={showConsumers ? filteredConsumers : filteredConsumers.slice(0, 30)}
      />
      {!showConsumers && filteredConsumers.length > 30 ? (
        <div className="muted" style={{ textAlign: "center", padding: "10px 0" }}>
          showing first 30 of {filteredConsumers.length} —{" "}
          <button className="link-btn" onClick={() => setShowConsumers(true)}>
            show all
          </button>
        </div>
      ) : null}
    </section>
  );
}

// ---------- topology construction ---------------------------------------

function buildTopology(
  overview: AggregatedOverview | null,
  serverOrder: string[],
): {
  servers: string[];
  metaRow: RaftRow;
  streamRows: RaftRow[];
  consumerRows: RaftRow[];
  serverStats: ServerStats[];
} | null {
  if (!overview) return null;

  // Anchor the server list. Use the order from the overview replies but
  // fall back to gathering names from stream/consumer cluster info if none
  // were provided (defensive).
  const fromMembers = new Set<string>();
  for (const a of overview.accountList) {
    for (const s of a.streams) {
      for (const r of s.cluster?.replicas ?? []) fromMembers.add(r.name);
      if (s.cluster?.leader) fromMembers.add(s.cluster.leader);
      for (const c of s.consumers) {
        if (c.cluster?.leader) fromMembers.add(c.cluster.leader);
        for (const r of c.cluster?.replicas ?? []) fromMembers.add(r.name);
      }
    }
  }
  const allServers =
    serverOrder.length > 0
      ? Array.from(new Set([...serverOrder, ...fromMembers])).sort()
      : [...fromMembers].sort();

  // Meta row — every server is a peer; the meta leader is known.
  const metaLeader = overview.meta?.leader;
  const metaMembers = new Map<string, { current: boolean }>();
  for (const s of allServers) metaMembers.set(s, { current: true });
  const metaRow: RaftRow = {
    group: "_meta_",
    label: "meta (_meta_)",
    leader: metaLeader,
    members: metaMembers,
  };

  // Stream + consumer rows.
  const streamRows: RaftRow[] = [];
  const consumerRows: RaftRow[] = [];
  for (const a of overview.accountList) {
    for (const s of a.streams) {
      const sMembers = membersOf(s);
      streamRows.push({
        group: s.cluster?.raft_group ?? s.name,
        label: `${a.name}/${s.name}`,
        account: a.name,
        stream: s.name,
        leader: s.cluster?.leader,
        members: sMembers,
      });
      for (const c of s.consumers) {
        consumerRows.push({
          group: c.name ?? "?",
          label: `${a.name}/${s.name} → ${shorten(c.config?.durable_name ?? c.config?.name ?? c.name ?? "")}`,
          account: a.name,
          stream: s.name,
          leader: c.cluster?.leader,
          // Consumers inherit the stream's replica set when their own
          // is not enumerated (server-side optimization).
          members:
            (c.cluster?.replicas ?? []).length > 0
              ? new Map(
                  (c.cluster?.replicas ?? []).map((r) => [r.name, { current: r.current }] as const),
                )
              : sMembers,
        });
      }
    }
  }

  // Sort: stream rows alphabetically by account then stream.
  streamRows.sort((a, b) => (a.label < b.label ? -1 : a.label > b.label ? 1 : 0));
  consumerRows.sort((a, b) => (a.label < b.label ? -1 : a.label > b.label ? 1 : 0));

  // Per-server statistics for the distribution chart.
  const serverStats: ServerStats[] = allServers.map((name) => ({
    name,
    metaLeader: metaLeader === name,
    metaFollower: metaLeader !== name,
    streamLeader: 0,
    streamFollower: 0,
    consumerLeader: 0,
    consumerFollower: 0,
  }));
  const statByName = new Map(serverStats.map((s) => [s.name, s]));
  for (const r of streamRows) {
    for (const member of r.members.keys()) {
      const st = statByName.get(member);
      if (!st) continue;
      if (member === r.leader) st.streamLeader++;
      else st.streamFollower++;
    }
  }
  for (const r of consumerRows) {
    for (const member of r.members.keys()) {
      const st = statByName.get(member);
      if (!st) continue;
      if (member === r.leader) st.consumerLeader++;
      else st.consumerFollower++;
    }
  }

  return { servers: allServers, metaRow, streamRows, consumerRows, serverStats };
}

function membersOf(s: AggregatedStream): Map<string, { current: boolean }> {
  const out = new Map<string, { current: boolean }>();
  for (const r of s.cluster?.replicas ?? []) out.set(r.name, { current: r.current });
  if (s.cluster?.leader && !out.has(s.cluster.leader)) {
    // Leader is sometimes implicit (not echoed in replicas[]). Add it as
    // current.
    out.set(s.cluster.leader, { current: true });
  }
  return out;
}

function roleOf(row: RaftRow, server: string): Role {
  const m = row.members.get(server);
  if (!m) return "absent";
  if (row.leader === server) return "leader";
  return m.current ? "follower" : "stale";
}

function shorten(s: string, n = 36): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}

// Suppress lint warning until consumed elsewhere — we use it via the API
// indirectly through aggregated state.
void ({} as AggregatedConsumer);

// ---------- distribution chart ------------------------------------------

function DistributionChart({ stats, servers }: { stats: ServerStats[]; servers: string[] }) {
  void servers;
  const max = Math.max(
    1,
    ...stats.map((s) => s.streamLeader + s.streamFollower + s.consumerLeader + s.consumerFollower),
  );
  return (
    <div className="topology-dist">
      <div className="topology-dist-head">
        <span className="muted">Raft-group membership per server (L = leader, F = follower)</span>
        <Legend />
      </div>
      <div className="topology-dist-rows">
        {stats.map((s) => {
          const total = s.streamLeader + s.streamFollower + s.consumerLeader + s.consumerFollower;
          const pct = (n: number) => (total === 0 ? 0 : (n / max) * 100);
          return (
            <div className="topology-dist-row" key={s.name}>
              <div className="topology-dist-label">
                <div className="topology-dist-server">
                  <span className="mono">{s.name}</span>
                  {s.metaLeader ? (
                    <span title="meta leader" className="meta-badge">
                      <Crown size={11} /> meta
                    </span>
                  ) : null}
                </div>
                <div className="topology-dist-counts">
                  <span title="stream-raft leaders">
                    <CircleDot size={10} className="role-leader-text" /> {num(s.streamLeader)}
                  </span>
                  <span title="stream-raft followers">
                    <CircleDot size={10} className="role-follower-text" /> {num(s.streamFollower)}
                  </span>
                  <span title="consumer-raft leaders">
                    <Crown size={10} className="role-leader-text" /> {num(s.consumerLeader)}
                  </span>
                  <span title="consumer-raft followers">
                    <Minus size={10} className="role-follower-text" /> {num(s.consumerFollower)}
                  </span>
                </div>
              </div>
              <div className="topology-dist-bar">
                <span
                  className="seg seg-stream-leader"
                  style={{ width: `${pct(s.streamLeader)}%` }}
                  title={`${s.streamLeader} stream leaders`}
                />
                <span
                  className="seg seg-stream-follower"
                  style={{ width: `${pct(s.streamFollower)}%` }}
                  title={`${s.streamFollower} stream followers`}
                />
                <span
                  className="seg seg-consumer-leader"
                  style={{ width: `${pct(s.consumerLeader)}%` }}
                  title={`${s.consumerLeader} consumer leaders`}
                />
                <span
                  className="seg seg-consumer-follower"
                  style={{ width: `${pct(s.consumerFollower)}%` }}
                  title={`${s.consumerFollower} consumer followers`}
                />
                <span className="topology-dist-total">{num(total)}</span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Legend() {
  return (
    <div className="topology-legend">
      <span>
        <i className="swatch seg-stream-leader"></i> stream L
      </span>
      <span>
        <i className="swatch seg-stream-follower"></i> stream F
      </span>
      <span>
        <i className="swatch seg-consumer-leader"></i> consumer L
      </span>
      <span>
        <i className="swatch seg-consumer-follower"></i> consumer F
      </span>
    </div>
  );
}

// ---------- heatmap -----------------------------------------------------

function Heatmap({ servers, rows }: { servers: string[]; rows: RaftRow[] }) {
  if (rows.length === 0) return <div className="muted">no raft groups</div>;
  return (
    <div className="topology-matrix-wrap">
      <table className="topology-matrix">
        <thead>
          <tr>
            <th className="raft-label">raft group</th>
            {servers.map((s) => (
              <th key={s} className="server-col" title={s}>
                <span className="mono">{shortServer(s)}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.group + r.label}>
              <td className="raft-label" title={r.label}>
                <span className="mono">{r.label}</span>
              </td>
              {servers.map((s) => (
                <td key={s} className={`role role-${roleOf(r, s)}`}>
                  <RoleGlyph role={roleOf(r, s)} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RoleGlyph({ role }: { role: Role }) {
  switch (role) {
    case "leader":
      return <span title="leader">L</span>;
    case "follower":
      return <span title="follower">F</span>;
    case "stale":
      return <span title="stale follower">·</span>;
    default:
      return <span></span>;
  }
}

function shortServer(s: string): string {
  // js-nats-production-3 → "prod-3" if matches the common Snapp pattern;
  // otherwise return last 12 chars.
  const m = s.match(/^(.*?)-(\d+)$/);
  if (m) return m[2] ? `…-${m[2]}` : s;
  return s.length > 14 ? `…${s.slice(-12)}` : s;
}

// MetaStepdownButton — small inline action next to the Meta raft heading.
function MetaStepdownButton({
  cluster,
  leader,
  onDone,
}: {
  cluster: string;
  leader: string | undefined;
  onDone: () => void;
}) {
  const confirm = useConfirm();
  const toast = useToast();
  async function go() {
    if (!(await confirm.ask(
      "Step down meta leader",
      `Force ${leader ?? "the current meta leader"} on ${cluster} to step down? Triggers a re-election.`,
      "primary",
    ))) return;
    try {
      await api.metaStepdown(cluster);
      toast.push(`meta leader step-down requested on ${cluster}`, "ok");
      onDone();
    } catch (e) {
      toast.push(`step-down failed: ${(e as Error).message}`, "error");
    }
  }
  return (
    <button
      className="link-btn"
      onClick={go}
      title="Force meta-cluster raft re-election"
      style={{ fontSize: 12 }}
    >
      ↻ step down
    </button>
  );
}
