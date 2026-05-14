import { useEffect, useMemo, useState } from "react";
import { api, ApiError } from "../api";
import type { PingReply } from "../types";
import { bytes, num, shortId } from "../fmt";
import { ServerDetail } from "./ServerDetail";

interface Props {
  cluster: string;
  refreshKey: number;
}

export function ServersView({ cluster, refreshKey }: Props) {
  const [replies, setReplies] = useState<PingReply[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setErr(null);
    api
      .ping(cluster)
      .then((r) => setReplies(r))
      .catch((e: ApiError) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [cluster, refreshKey]);

  const totals = useMemo(() => {
    return replies.reduce(
      (acc, r) => {
        const s = r.statsz ?? {};
        acc.servers++;
        acc.connections += s.connections ?? 0;
        acc.subs += s.num_subscriptions ?? s.subscriptions ?? 0;
        acc.in_msgs += s.in_msgs ?? 0;
        acc.out_msgs += s.out_msgs ?? 0;
        acc.in_bytes += s.in_bytes ?? 0;
        acc.out_bytes += s.out_bytes ?? 0;
        return acc;
      },
      { servers: 0, connections: 0, subs: 0, in_msgs: 0, out_msgs: 0, in_bytes: 0, out_bytes: 0 },
    );
  }, [replies]);

  const clusterName = replies[0]?.server?.cluster ?? "—";

  return (
    <section>
      <div className="summary">
        <Stat label="Cluster" value={clusterName} />
        <Stat label="Servers" value={num(totals.servers)} />
        <Stat label="Connections" value={num(totals.connections)} />
        <Stat label="Subscriptions" value={num(totals.subs)} />
        <Stat label="In msgs" value={num(totals.in_msgs)} />
        <Stat label="Out msgs" value={num(totals.out_msgs)} />
        <Stat label="In bytes" value={bytes(totals.in_bytes)} />
        <Stat label="Out bytes" value={bytes(totals.out_bytes)} />
      </div>

      {loading ? (
        <div className="spinner">loading servers…</div>
      ) : err ? (
        <div className="empty">discovery failed: {err}</div>
      ) : replies.length === 0 ? (
        <div className="empty">no replies — credentials may lack system-account access</div>
      ) : (
        <div className="grid">
          {replies.map((r) => (
            <ServerCard
              key={r.server.id}
              reply={r}
              selected={selectedId === r.server.id}
              onClick={() => setSelectedId(r.server.id)}
            />
          ))}
        </div>
      )}

      {selectedId ? (
        <ServerDetail
          cluster={cluster}
          server={replies.find((r) => r.server.id === selectedId)!}
          onClose={() => setSelectedId(null)}
        />
      ) : null}
    </section>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="stat">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
    </div>
  );
}

function ServerCard({
  reply,
  selected,
  onClick,
}: {
  reply: PingReply;
  selected: boolean;
  onClick: () => void;
}) {
  const s = reply.server;
  const z = reply.statsz ?? {};
  const meta = z.jetstream?.meta;
  const isLeader = meta?.leader && meta.leader === s.name;
  return (
    <article
      className={`card${selected ? " selected" : ""}`}
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
    >
      <div className="row">
        <div className="head">
          <div className="name" title={s.name}>
            <span className="dot good"></span>
            {s.name || "—"}
          </div>
          <div className="id" title={s.id}>
            {shortId(s.id)}
          </div>
        </div>
        <div className="ver">v{s.ver || "?"}</div>
      </div>
      <div className="meta">
        <div>
          cluster <b>{s.cluster || "—"}</b>
        </div>
        <div>
          jetstream <b>{s.jetstream ? "yes" : "no"}</b>
        </div>
        <div>
          connections <b>{num(z.connections)}</b>
        </div>
        <div>
          subs <b>{num(z.num_subscriptions ?? z.subscriptions)}</b>
        </div>
        <div>
          cpu <b>{z.cpu != null ? `${z.cpu}%` : "—"}</b>
        </div>
        <div>
          mem <b>{bytes(z.mem)}</b>
        </div>
        {meta ? (
          <div>
            meta <b>{isLeader ? "leader" : meta.leader ? `→ ${meta.leader}` : "—"}</b>
          </div>
        ) : null}
        {meta ? (
          <div>
            cluster size <b>{num(meta.cluster_size)}</b>
          </div>
        ) : null}
      </div>
    </article>
  );
}
