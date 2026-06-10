import clsx from "clsx";
import { Database, LogOut, Network, RefreshCw, Server, Users } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { type ApiError, api } from "./api";
import { AccountsView } from "./components/AccountsView";
import { ConfirmProvider } from "./components/ConfirmDialog";
import { Footer } from "./components/Footer";
import { JetStreamView } from "./components/JetStreamView";
import { LoginScreen } from "./components/LoginScreen";
import { ServersView } from "./components/ServersView";
import { TopologyView } from "./components/TopologyView";
import { canWrite, displayName, useAuth } from "./hooks/useAuth";
import { CanWriteProvider } from "./state/access";
import { ToastProvider, useToast } from "./state/toast";

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
  const { status, logout } = useAuth();
  const [clusters, setClusters] = useState<string[]>([]);
  const [cluster, setCluster] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("servers");
  const [refreshKey, setRefreshKey] = useState(0);
  const [bootErr, setBootErr] = useState<string | null>(null);
  const toast = useToast();

  // Hold cluster discovery until we know the user is allowed in. /api/admin
  // returns 401 when auth is on and the cookie is missing — fetching too
  // early just produces a misleading bootErr.
  const canFetch = status.state === "disabled" || status.state === "authenticated";

  useEffect(() => {
    if (!canFetch) return;
    api
      .clusters()
      .then((names) => {
        setClusters(names);
        setCluster(names[0] ?? null);
      })
      .catch((e: ApiError) => setBootErr(e.message));
  }, [canFetch]);

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

  if (status.state === "loading") {
    return null;
  }
  if (status.state === "anonymous") {
    return <LoginScreen mode={status.mode} />;
  }

  const writable = canWrite(status);
  const readOnly = status.state === "authenticated" && status.identity.role === "readonly";
  // In jwt mode the gateway owns the session — there's no server-side logout
  // to call, so hide the sign-out control.
  const canSignOut = status.state === "authenticated" && status.identity.mode !== "jwt";

  return (
    <CanWriteProvider value={writable}>
      <section className="hero" aria-label="cheshmhayash">
        <div className="hero-overlay">
          <h1 className="hero-title">cheshmhayash</h1>
          <p className="hero-sub">NATS administration panel</p>
        </div>
      </section>

      <header className="topbar">
        <nav className="tabs">
          <button className={clsx(tab === "servers" && "active")} onClick={() => setTab("servers")}>
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
          {status.state === "authenticated" ? (
            <div className="user-menu">
              {readOnly ? (
                <span
                  className="role-badge"
                  title="You have read-only access; write actions are hidden."
                >
                  read-only
                </span>
              ) : null}
              <span className="who" title={status.identity.email ?? status.identity.sub ?? ""}>
                {displayName(status.identity)}
              </span>
              {canSignOut ? (
                <button
                  className="icon-btn"
                  title="Sign out"
                  onClick={() => {
                    void logout().then(() => {
                      window.location.href = "/";
                    });
                  }}
                >
                  <LogOut size={14} />
                </button>
              ) : null}
            </div>
          ) : null}
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
    </CanWriteProvider>
  );
}
