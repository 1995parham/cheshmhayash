import clsx from "clsx";
import { Pause, Play, Square, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ApiError, openTap, tapConfig } from "../api";
import type { TapConfig, TapMessage, TapStats } from "../types";

// The newest messages kept on screen; older ones scroll out.
const KEEP = 500;

interface Props {
  cluster: string;
}

type Phase = "idle" | "live" | "ended" | "error";

// TapView streams decoded messages of a subject from /api/tap. The server
// decodes payloads (protobuf by the configured schemas, else JSON, text,
// a schemaless protobuf dump or base64) and enforces the allow list and
// the rate and time limits; this view only shows what arrives.
export function TapView({ cluster }: Props) {
  const [cfg, setCfg] = useState<TapConfig | null>(null);
  const [cfgErr, setCfgErr] = useState<string | null>(null);
  const [subject, setSubject] = useState("");
  const [type, setType] = useState("");
  const [filter, setFilter] = useState("");
  const [messages, setMessages] = useState<TapMessage[]>([]);
  const [stats, setStats] = useState<TapStats | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [note, setNote] = useState<string | null>(null);
  const [paused, setPaused] = useState(false);
  const pausedRef = useRef(false);
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    tapConfig()
      .then((c) => {
        setCfg(c);
        setSubject((s) => s || (c.allow[0] ?? ""));
      })
      .catch((e: ApiError) =>
        setCfgErr(e.status === 404 ? "off" : e.message || `HTTP ${e.status}`),
      );
  }, []);

  // A cluster change or leaving the tab stops a running tap.
  // biome-ignore lint/correctness/useExhaustiveDependencies: cluster is the intentional trigger.
  useEffect(() => () => abort.current?.abort(), [cluster]);

  function stop() {
    abort.current?.abort();
    abort.current = null;
    setPhase((p) => (p === "live" ? "ended" : p));
  }

  async function start() {
    if (!subject.trim()) {
      setNote("enter a subject, for example orders.>");
      return;
    }
    stop();
    const ctl = new AbortController();
    abort.current = ctl;
    setMessages([]);
    setStats(null);
    setNote(null);
    setPhase("live");
    try {
      await openTap(cluster, subject.trim(), type.trim(), ctl.signal, {
        onMessage: (m) => {
          if (pausedRef.current) return;
          setMessages((prev) => [m, ...prev].slice(0, KEEP));
        },
        onStats: setStats,
        onEnd: (reason) =>
          setNote(
            reason === "max_duration" ? "the tap reached its time limit" : `ended: ${reason}`,
          ),
      });
      if (!ctl.signal.aborted) setPhase("ended");
    } catch (e) {
      if (ctl.signal.aborted) return;
      setPhase("error");
      setNote(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  function togglePause() {
    pausedRef.current = !pausedRef.current;
    setPaused(pausedRef.current);
  }

  if (cfgErr === "off") {
    return (
      <div className="empty">
        The live tap is off. Set <code>[tap] enabled = true</code> in settings.toml (or{" "}
        <code>tap.enabled</code> in the Helm chart) to turn it on.
      </div>
    );
  }
  if (cfgErr) return <div className="empty">tap unavailable: {cfgErr}</div>;
  if (!cfg) return <div className="spinner">loading…</div>;
  if (!cfg.can_tap) {
    return <div className="empty">The live tap needs the admin role.</div>;
  }

  const needle = filter.trim().toLowerCase();
  const shown = needle
    ? messages.filter(
        (m) =>
          m.subject.toLowerCase().includes(needle) ||
          JSON.stringify(m.payload).toLowerCase().includes(needle),
      )
    : messages;

  return (
    <section className="tap">
      <div className="account-bar">
        <label htmlFor="tap-subject">subject</label>
        <input
          id="tap-subject"
          placeholder="orders.>"
          spellCheck={false}
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void start()}
          list="tap-allow"
        />
        <datalist id="tap-allow">
          {cfg.allow.map((a) => (
            <option key={a} value={a} />
          ))}
        </datalist>
        <input
          aria-label="protobuf type"
          placeholder="type (optional), e.g. backend:ride.Accept"
          spellCheck={false}
          value={type}
          onChange={(e) => setType(e.target.value)}
        />
        {phase === "live" ? (
          <button onClick={stop} title="Stop the tap">
            <Square size={13} /> Stop
          </button>
        ) : (
          <button onClick={() => void start()} title="Start the tap">
            <Play size={13} /> Start
          </button>
        )}
        <button onClick={togglePause} disabled={phase !== "live"} title="Stop adding messages">
          <Pause size={13} /> {paused ? "Resume" : "Pause"}
        </button>
        <button onClick={() => setMessages([])} title="Clear">
          <Trash2 size={13} />
        </button>
        <input
          aria-label="filter"
          placeholder="filter shown messages"
          spellCheck={false}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      <div className="tap-status muted">
        <span className={clsx("dot", phase === "live" && "live")} /> {phase}
        {stats ? (
          <>
            {" · "}received {stats.received} · shown {stats.sent}
            {stats.skipped ? ` · skipped ${stats.skipped} (over ${cfg.max_rate}/s)` : ""}
            {stats.dropped ? ` · dropped ${stats.dropped}` : ""}
          </>
        ) : null}
        {" · "}limit {cfg.max_rate}/s, {Math.round(cfg.max_duration_seconds / 60)} min
        {cfg.allow.length ? ` · allowed: ${cfg.allow.join(", ")}` : ""}
        {note ? <div className="tap-note">{note}</div> : null}
      </div>

      {shown.length === 0 ? (
        <div className="muted">
          {phase === "live" ? "waiting for messages…" : "Start a tap to see decoded messages."}
        </div>
      ) : (
        <ul className="tap-list">
          {shown.map((m) => (
            <li key={m.seq}>
              <details>
                <summary>
                  <span className="mono muted">{new Date(m.time).toLocaleTimeString()}</span>
                  <span className="mono">{m.subject}</span>
                  <span className={clsx("meta-badge", `tap-${m.format}`)}>
                    {m.type ?? m.format}
                  </span>
                  <span className="muted">{m.size} B</span>
                  {m.notes?.length ? <span className="tap-warn">⚠ {m.notes.length}</span> : null}
                </summary>
                {m.notes?.map((n) => (
                  <div key={n} className="tap-note">
                    {n}
                  </div>
                ))}
                {m.headers ? <pre className="raw">{JSON.stringify(m.headers, null, 2)}</pre> : null}
                <pre className="raw">{JSON.stringify(m.payload, null, 2)}</pre>
              </details>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
