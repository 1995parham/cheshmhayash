import { Crown, Eraser, Pencil, Trash2, X } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { bytes, duration, iso, num } from "../fmt";
import { useCanWrite } from "../state/access";
import { useToast } from "../state/toast";
import type { AggregatedOverview, AggregatedStream } from "../types";
import { useConfirm } from "./ConfirmDialog";
import { StreamEditor } from "./StreamEditor";

interface Props {
  cluster: string;
  overview: AggregatedOverview;
  account: string;
  streamName: string;
  onClose: () => void;
  onUpdated: () => void;
}

export function StreamDetail({
  cluster,
  overview,
  account,
  streamName,
  onClose,
  onUpdated,
}: Props) {
  const acct = overview.accountList.find((a) => a.name === account);
  const s: AggregatedStream | undefined = acct?.streams.find((x) => x.name === streamName);
  const confirm = useConfirm();
  const toast = useToast();
  const canWrite = useCanWrite();
  const [editing, setEditing] = useState(false);

  if (!s) return null;

  const cfg = s.config ?? { name: streamName };
  const st = s.state ?? {};
  const cl = s.cluster ?? {};

  async function purge() {
    if (!(await confirm.ask("Purge stream", `Delete every message in ${streamName}?`))) return;
    try {
      await api.purgeStream(cluster, streamName);
      toast.push(`${streamName} purged`, "ok");
      onUpdated();
    } catch (e) {
      toast.push(`purge failed: ${(e as Error).message}`, "error");
    }
  }
  async function del() {
    if (!(await confirm.ask("Delete stream", `Permanently delete ${streamName}?`))) return;
    try {
      await api.deleteStream(cluster, streamName);
      toast.push(`${streamName} deleted`, "ok");
      onUpdated();
    } catch (e) {
      toast.push(`delete failed: ${(e as Error).message}`, "error");
    }
  }
  async function stepdown() {
    if (
      !(await confirm.ask(
        "Step down stream leader",
        `Force ${streamName}'s raft leader (${cl.leader ?? "?"}) to step down? A new leader will be elected from the replicas.`,
        "primary",
      ))
    )
      return;
    try {
      await api.streamStepdown(cluster, streamName);
      toast.push(`${streamName}: leader step-down requested`, "ok");
      onUpdated();
    } catch (e) {
      toast.push(`step-down failed: ${(e as Error).message}`, "error");
    }
  }
  async function consumerStepdown(consumerName: string, leader: string | undefined) {
    if (
      !(await confirm.ask(
        "Step down consumer leader",
        `Force ${streamName}/${consumerName}'s raft leader (${leader ?? "?"}) to step down?`,
        "primary",
      ))
    )
      return;
    try {
      await api.consumerStepdown(cluster, streamName, consumerName);
      toast.push(`${consumerName}: leader step-down requested`, "ok");
      onUpdated();
    } catch (e) {
      toast.push(`step-down failed: ${(e as Error).message}`, "error");
    }
  }

  return (
    <>
      <section className="detail">
        <header className="detail-header">
          <div>
            <div className="title">{streamName}</div>
            <div className="sub">
              account: <span className="mono">{account}</span> · leader:{" "}
              <span className="mono">{cl.leader ?? "—"}</span>
            </div>
          </div>
          <div className="spacer"></div>
          {canWrite ? (
            <div className="detail-actions">
              <button onClick={() => setEditing(true)}>
                <Pencil size={14} />
                Edit
              </button>
              <button onClick={stepdown} title="Force raft leader re-election">
                <Crown size={14} />
                Step down
              </button>
              <button className="danger" onClick={purge}>
                <Eraser size={14} />
                Purge
              </button>
              <button className="danger" onClick={del}>
                <Trash2 size={14} />
                Delete
              </button>
            </div>
          ) : null}
          <button className="close-btn" onClick={onClose} aria-label="close">
            <X size={18} />
          </button>
        </header>

        <div className="detail-body">
          <div className="kv">
            <KV k="name" v={cfg.name} />
            <KV k="subjects" v={(cfg.subjects ?? []).join(", ")} />
            <KV k="storage" v={cfg.storage} />
            <KV k="retention" v={cfg.retention} />
            <KV k="discard" v={cfg.discard} />
            <KV k="replicas" v={num(cfg.num_replicas)} />
            <KV
              k="max msgs"
              v={cfg.max_msgs != null && cfg.max_msgs >= 0 ? num(cfg.max_msgs) : "unlimited"}
            />
            <KV
              k="max bytes"
              v={cfg.max_bytes != null && cfg.max_bytes >= 0 ? bytes(cfg.max_bytes) : "unlimited"}
            />
            <KV k="max age" v={cfg.max_age ? duration(cfg.max_age) : "unlimited"} />
            <KV k="messages" v={num(st.messages)} />
            <KV k="bytes" v={bytes(st.bytes)} />
            <KV k="first seq" v={num(st.first_seq)} />
            <KV k="last seq" v={num(st.last_seq)} />
            <KV k="first ts" v={iso(st.first_ts)} />
            <KV k="last ts" v={iso(st.last_ts)} />
            <KV k="raft group" v={cl.raft_group} />
            <KV
              k="replica peers"
              v={(cl.replicas ?? []).map((r) => `${r.name}${r.current ? "✓" : "✗"}`).join(", ")}
            />
          </div>

          {s.consumers.length > 0 ? (
            <>
              <h4 style={{ marginTop: 18 }}>Consumers ({s.consumers.length})</h4>
              <table className="t">
                <thead>
                  <tr>
                    <th>name</th>
                    <th>type</th>
                    <th>filter</th>
                    <th>delivered</th>
                    <th>ack pend</th>
                    <th>redeliv</th>
                    <th>pending</th>
                    <th>leader</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {s.consumers.map((c) => {
                    const cc = c.config ?? {};
                    const filter = cc.filter_subject ?? (cc.filter_subjects ?? []).join(", ");
                    const name = cc.durable_name ?? cc.name ?? c.name ?? "";
                    return (
                      <tr key={name}>
                        <td>{name || "—"}</td>
                        <td>{cc.deliver_subject ? "push" : "pull"}</td>
                        <td className="mono">{filter}</td>
                        <td className="num">{num(c.delivered?.consumer_seq)}</td>
                        <td className="num">{num(c.num_ack_pending)}</td>
                        <td className="num">{num(c.num_redelivered)}</td>
                        <td className="num">{num(c.num_pending)}</td>
                        <td className="mono">{c.cluster?.leader ?? ""}</td>
                        <td>
                          {canWrite ? (
                            <button
                              className="link-btn"
                              title="Force consumer raft re-election"
                              onClick={() => consumerStepdown(name, c.cluster?.leader)}
                            >
                              step down
                            </button>
                          ) : null}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </>
          ) : (
            <div className="muted" style={{ marginTop: 14 }}>
              no consumers
            </div>
          )}
        </div>
      </section>

      {editing ? (
        <StreamEditor
          cluster={cluster}
          stream={streamName}
          initialConfig={cfg}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            onUpdated();
          }}
        />
      ) : null}
    </>
  );
}

function KV({ k, v }: { k: string; v: unknown }) {
  return (
    <>
      <div className="k">{k}</div>
      <div className="v">{v == null || v === "" ? "—" : String(v)}</div>
    </>
  );
}
