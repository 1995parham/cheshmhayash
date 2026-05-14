import { useState } from "react";
import { api } from "../api";

const ENDPOINTS = ["INFO", "CONNZ", "LEAFZ", "SUBSZ", "JSZ"] as const;

interface Props {
  cluster: string;
}

export function AccountsView({ cluster }: Props) {
  const [account, setAccount] = useState("");
  const [ep, setEp] = useState<(typeof ENDPOINTS)[number]>("INFO");
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<unknown>(null);
  const [err, setErr] = useState<string | null>(null);

  async function go() {
    if (!account.trim()) {
      setErr("enter an account name");
      return;
    }
    setLoading(true);
    setErr(null);
    setData(null);
    try {
      const r = await api.account(cluster, account.trim(), ep);
      setData(r);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <section>
      <div className="account-bar">
        <label htmlFor="account">account</label>
        <input
          id="account"
          placeholder="e.g. $G"
          spellCheck={false}
          value={account}
          onChange={(e) => setAccount(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && go()}
        />
        <select value={ep} onChange={(e) => setEp(e.target.value as (typeof ENDPOINTS)[number])}>
          {ENDPOINTS.map((e) => (
            <option key={e}>{e}</option>
          ))}
        </select>
        <button onClick={go}>Query</button>
      </div>
      {loading ? (
        <div className="spinner">loading…</div>
      ) : err ? (
        <div className="empty">request failed: {err}</div>
      ) : data ? (
        <pre className="raw">{JSON.stringify(data, null, 2)}</pre>
      ) : (
        <div className="muted">
          Pick an account and endpoint. <code>$G</code> is the default user account; <code>$SYS</code> is the
          system account.
        </div>
      )}
    </section>
  );
}
