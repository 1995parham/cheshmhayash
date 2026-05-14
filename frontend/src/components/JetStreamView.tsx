import { useEffect, useMemo, useState } from "react";
import type { AggregatedAccount, AggregatedStream } from "../types";
import { bytes, num } from "../fmt";
import { StreamDetail } from "./StreamDetail";
import { useOverviewStream } from "../hooks/useOverviewStream";
import { StreamStatus } from "./StreamStatus";

interface Props {
  cluster: string;
  refreshKey: number;
}

export function JetStreamView({ cluster, refreshKey }: Props) {
  const { overview, status, lastError, lastUpdate } = useOverviewStream(cluster, refreshKey);
  const [focusedAccount, setFocusedAccount] = useState<string | null>(null);
  const [openStream, setOpenStream] = useState<{ account: string; stream: string } | null>(null);

  // Auto-focus the single-account case the first time the overview lands.
  useEffect(() => {
    if (overview && overview.accountList.length === 1 && focusedAccount === null) {
      setFocusedAccount(overview.accountList[0]!.name);
    }
  }, [overview, focusedAccount]);

  // Close any open stream detail when the cluster changes.
  useEffect(() => {
    setOpenStream(null);
    setFocusedAccount(null);
  }, [cluster]);

  if (!overview) {
    if (status === "error" || status === "closed") {
      return <div className="empty">overview failed: {lastError ?? status}</div>;
    }
    return <div className="spinner">loading cluster JetStream overview…</div>;
  }

  const focused = focusedAccount
    ? overview.accountList.find((a) => a.name === focusedAccount)
    : null;

  return (
    <section>
      <div className="row-toolbar">
        <span className="muted">
          {overview.totalAccounts} accounts · {overview.totalStreams} streams ·{" "}
          {overview.totalConsumers} consumers · {bytes(overview.totalBytes)}
        </span>
        <StreamStatus status={status} lastUpdate={lastUpdate} lastError={lastError} />
      </div>

      <div className="summary">
        <Stat label="Cluster" value={overview.cluster ?? "—"} />
        <Stat label="Meta leader" value={overview.meta?.leader ?? "—"} />
        <Stat label="Cluster size" value={num(overview.meta?.cluster_size)} />
        <Stat label="Accounts" value={num(overview.totalAccounts)} />
        <Stat label="Streams" value={num(overview.totalStreams)} />
        <Stat label="Consumers" value={num(overview.totalConsumers)} />
        <Stat label="Messages" value={num(overview.totalMessages)} />
        <Stat label="Bytes" value={bytes(overview.totalBytes)} />
      </div>

      <h4 style={{ margin: "0 0 8px" }}>Accounts</h4>
      <AccountsTable
        accounts={overview.accountList}
        focused={focusedAccount}
        onPick={(n) => setFocusedAccount(n)}
      />

      {focused ? (
        <StreamsTable
          account={focused}
          onPick={(stream) => setOpenStream({ account: focused.name, stream })}
        />
      ) : null}

      {openStream && overview ? (
        <StreamDetail
          cluster={cluster}
          overview={overview}
          account={openStream.account}
          streamName={openStream.stream}
          onClose={() => setOpenStream(null)}
          onUpdated={() => setOpenStream(null)}
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

function AccountsTable({
  accounts,
  focused,
  onPick,
}: {
  accounts: AggregatedAccount[];
  focused: string | null;
  onPick: (name: string) => void;
}) {
  return (
    <table className="t">
      <thead>
        <tr>
          <th>account</th>
          <th>streams</th>
          <th>consumers</th>
          <th>messages</th>
          <th>bytes</th>
          <th>API calls</th>
          <th>API errors</th>
        </tr>
      </thead>
      <tbody>
        {accounts.map((a) => (
          <tr
            key={a.name}
            className={focused === a.name ? "selected" : ""}
            onClick={() => onPick(a.name)}
            style={{ cursor: "pointer" }}
          >
            <td className="mono">{a.name}</td>
            <td className="num">{num(a.totals.streams)}</td>
            <td className="num">{num(a.totals.consumers)}</td>
            <td className="num">{num(a.totals.messages)}</td>
            <td className="num">{bytes(a.totals.bytes)}</td>
            <td className="num">{num(a.api?.total)}</td>
            <td className="num">{num(a.api?.errors)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function StreamsTable({
  account,
  onPick,
}: {
  account: AggregatedAccount;
  onPick: (stream: string) => void;
}) {
  const sorted = useMemo(
    () => [...account.streams].sort((a, b) => (b.state?.messages ?? 0) - (a.state?.messages ?? 0)),
    [account],
  );
  return (
    <div style={{ marginTop: 18 }}>
      <h4 style={{ margin: "0 0 8px" }}>
        Streams in <span className="mono">{account.name}</span> ({sorted.length})
      </h4>
      <table className="t">
        <thead>
          <tr>
            <th>name</th>
            <th>subjects</th>
            <th>msgs</th>
            <th>bytes</th>
            <th>consumers</th>
            <th>storage</th>
            <th>replicas</th>
            <th>leader</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((s) => (
            <StreamRow key={s.name} s={s} onClick={() => onPick(s.name)} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function StreamRow({ s, onClick }: { s: AggregatedStream; onClick: () => void }) {
  const cfg = s.config ?? { name: s.name };
  const st = s.state ?? {};
  const subjects = cfg.subjects ?? [];
  const display = subjects.slice(0, 3).join(", ") + (subjects.length > 3 ? "…" : "");
  return (
    <tr onClick={onClick} style={{ cursor: "pointer" }}>
      <td>{s.name}</td>
      <td className="mono" title={subjects.join(", ")}>
        {display}
      </td>
      <td className="num">{num(st.messages)}</td>
      <td className="num">{bytes(st.bytes)}</td>
      <td className="num">{s.consumers.length}</td>
      <td>{cfg.storage ?? ""}</td>
      <td className="num">{num(cfg.num_replicas)}</td>
      <td className="mono">{s.cluster?.leader ?? ""}</td>
    </tr>
  );
}
