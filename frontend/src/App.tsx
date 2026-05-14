import { useCallback, useEffect, useState } from "react";
import clsx from "clsx";
import { RefreshCw, Server, Users, Database, Network } from "lucide-react";
import { api, ApiError } from "./api";
import { ServersView } from "./components/ServersView";
import { AccountsView } from "./components/AccountsView";
import { JetStreamView } from "./components/JetStreamView";
import { TopologyView } from "./components/TopologyView";
import { ToastProvider, useToast } from "./state/toast";
import { ConfirmProvider } from "./components/ConfirmDialog";
import { Footer } from "./components/Footer";

type Tab = "servers" | "accounts" | "jetstream" | "topology";

export function App() {
  return (
    <ToastProvider>
      <ConfirmProvider>
        <Shell />
      </ConfirmProvider>
    </ToastProvider>
  );
}

function Shell() {
  const [clusters, setClusters] = useState<string[]>([]);
  const [cluster, setCluster] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("servers");
  const [refreshKey, setRefreshKey] = useState(0);
  const [bootErr, setBootErr] = useState<string | null>(null);
  const toast = useToast();

  useEffect(() => {
    api
      .clusters()
      .then((names) => {
        setClusters(names);
        setCluster(names[0] ?? null);
      })
      .catch((e: ApiError) => setBootErr(e.message));
  }, []);

  const refresh = useCallback(() => setRefreshKey((n) => n + 1), []);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement | null;
      if (t?.matches("input,select,textarea")) return;
      if (e.key === "r" && !e.metaKey && !e.ctrlKey) refresh();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [refresh]);

  return (
    <>
      <section className="hero" aria-label="cheshmhayash">
        <div className="hero-overlay">
          <h1 className="hero-title">cheshmhayash</h1>
          <p className="hero-sub">NATS administration panel</p>
        </div>
      </section>

      <header className="topbar">
        <nav className="tabs">
          <button
            className={clsx(tab === "servers" && "active")}
            onClick={() => setTab("servers")}
          >
            <Server size={14} /> Servers
          </button>
          <button
            className={clsx(tab === "accounts" && "active")}
            onClick={() => setTab("accounts")}
          >
            <Users size={14} /> Accounts
          </button>
          <button
            className={clsx(tab === "jetstream" && "active")}
            onClick={() => setTab("jetstream")}
          >
            <Database size={14} /> JetStream
          </button>
          <button
            className={clsx(tab === "topology" && "active")}
            onClick={() => setTab("topology")}
          >
            <Network size={14} /> Topology
          </button>
        </nav>
        <div className="cluster-picker">
          <label htmlFor="cluster">cluster</label>
          <select
            id="cluster"
            value={cluster ?? ""}
            onChange={(e) => setCluster(e.target.value)}
            disabled={clusters.length === 0}
          >
            {clusters.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
          <button
            className="icon-btn"
            title="Refresh (R)"
            onClick={() => {
              refresh();
              toast.push("refreshed");
            }}
          >
            <RefreshCw size={14} />
          </button>
        </div>
      </header>

      <main>
        {bootErr ? (
          <div className="empty">unable to load clusters: {bootErr}</div>
        ) : !cluster ? (
          <div className="empty">no clusters configured</div>
        ) : tab === "servers" ? (
          <ServersView cluster={cluster} refreshKey={refreshKey} />
        ) : tab === "accounts" ? (
          <AccountsView cluster={cluster} />
        ) : tab === "jetstream" ? (
          <JetStreamView cluster={cluster} refreshKey={refreshKey} />
        ) : (
          <TopologyView cluster={cluster} refreshKey={refreshKey} />
        )}
      </main>
      <Footer />
    </>
  );
}
