import { useEffect, useRef, useState } from "react";
import { aggregateOverview, api, ApiError } from "../api";
import type { AggregatedOverview, JszData } from "../types";

type RawOverviewReply = { server: { name: string }; data: JszData };

export type StreamStatus = "connecting" | "live" | "stale" | "error" | "closed";

interface State {
  overview: AggregatedOverview | null;
  status: StreamStatus;
  lastError: string | null;
  lastUpdate: Date | null;
}

const initial: State = {
  overview: null,
  status: "connecting",
  lastError: null,
  lastUpdate: null,
};

// useOverviewStream subscribes to /api/jsm/clusters/{c}/overview/stream
// via EventSource. EventSource auto-reconnects on transport errors, so
// our error path is just "remember it and update the indicator". When
// the stream isn't available (legacy backend) we fall back to a single
// fetch + a 15s polling loop so the UI still works.
export function useOverviewStream(cluster: string, refreshKey: number): State {
  const [state, setState] = useState<State>(initial);
  const fallbackTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    setState(initial);

    let cancelled = false;
    let es: EventSource | null = null;

    function setStatus(status: StreamStatus, lastError: string | null = null) {
      setState((s) => ({ ...s, status, lastError: lastError ?? s.lastError }));
    }

    function ingest(replies: RawOverviewReply[]) {
      if (cancelled) return;
      try {
        const agg = aggregateOverview(replies);
        setState({ overview: agg, status: "live", lastError: null, lastUpdate: new Date() });
      } catch (e) {
        setStatus("error", (e as Error).message);
      }
    }

    function startPolling() {
      // Backend doesn't have the streaming endpoint (or it 5xx'd).
      // One-shot fetch then poll at refreshKey-ish cadence.
      const tick = () => {
        api
          .jsOverview(cluster)
          .then(ingest)
          .catch((e: ApiError) => setStatus("error", e.message));
      };
      tick();
      fallbackTimer.current = window.setInterval(tick, 15_000);
      setStatus("stale"); // we have data but we're not streaming
    }

    function startStream() {
      try {
        es = new EventSource(api.jsOverviewStreamURL(cluster));
      } catch (e) {
        setStatus("error", (e as Error).message);
        startPolling();
        return;
      }

      es.onopen = () => {
        if (!cancelled) setStatus("connecting");
      };
      es.onmessage = (e) => {
        try {
          const replies = JSON.parse(e.data) as RawOverviewReply[];
          ingest(replies);
        } catch (err) {
          setStatus("error", (err as Error).message);
        }
      };
      es.addEventListener("error", () => {
        if (cancelled || !es) return;
        // EventSource readyState: 0=connecting, 1=open, 2=closed
        if (es.readyState === EventSource.CLOSED) {
          setStatus("closed", "stream closed by server");
          // Drop to polling so the UI doesn't go stale.
          if (!fallbackTimer.current) startPolling();
        } else {
          setStatus("connecting", "reconnecting");
        }
      });
    }

    startStream();

    return () => {
      cancelled = true;
      if (es) es.close();
      if (fallbackTimer.current) {
        window.clearInterval(fallbackTimer.current);
        fallbackTimer.current = undefined;
      }
    };
  }, [cluster, refreshKey]);

  return state;
}
