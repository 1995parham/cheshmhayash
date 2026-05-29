import type { StreamStatus as Status } from "../hooks/useOverviewStream";

interface Props {
  status: Status;
  lastUpdate: Date | null;
  lastError: string | null;
}

// Small inline indicator used by JetStream + Topology views to show the
// SSE connection health for the cluster overview.
//
//   live      — receiving server-sent updates
//   stale     — connected once, fell back to polling
//   connecting— waiting for the first frame or reconnecting
//   error     — last attempt failed (will auto-reconnect)
//   closed    — server closed the stream; we're polling
export function StreamStatus({ status, lastUpdate, lastError }: Props) {
  const dot =
    status === "live" ? "good" : status === "stale" || status === "connecting" ? "warn" : "bad";
  const label =
    status === "live"
      ? "live"
      : status === "stale"
        ? "polling"
        : status === "connecting"
          ? "connecting"
          : status === "closed"
            ? "disconnected"
            : "error";

  const age = lastUpdate ? `${ageString(lastUpdate)} ago` : "—";
  const tooltip = lastError ? `${label} · ${lastError}` : `${label} · last update ${age}`;

  return (
    <span className="stream-status" title={tooltip}>
      <span className={`dot ${dot}`}></span>
      <span className="stream-status-label">{label}</span>
      <span className="stream-status-age muted">· {age}</span>
    </span>
  );
}

function ageString(t: Date): string {
  const seconds = Math.floor((Date.now() - t.getTime()) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return `${h}h`;
}
