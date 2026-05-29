import { Crown } from "lucide-react";
import { useMemo, useState } from "react";

// A clean SVG visualization of raft-group placement across servers.
// Servers sit around a circle; each raft group is drawn as a translucent
// polygon connecting its replica servers, with the leader vertex marked.
// No d3 dependency — just hand-computed SVG paths and trig.

export interface GraphRaftGroup {
  id: string;
  kind: "meta" | "stream" | "consumer";
  leader?: string;
  members: string[]; // server names
  label: string;
}

interface Props {
  servers: string[];
  groups: GraphRaftGroup[];
  metaLeader?: string;
}

const COLORS = {
  meta: { stroke: "#f3b760", fill: "rgba(243,183,96,0.22)" },
  stream: { stroke: "#7cdcb6", fill: "rgba(124,220,182,0.10)" },
  consumer: { stroke: "#6ea8fe", fill: "rgba(110,168,254,0.07)" },
} as const;

const SIZE = 640;
const RADIUS = 220;
const NODE_R = 38;

export function TopologyGraph({ servers, groups, metaLeader }: Props) {
  const [show, setShow] = useState({
    meta: true,
    stream: true,
    consumer: false,
  });
  const [hoverServer, setHoverServer] = useState<string | null>(null);
  const [hoverGroup, setHoverGroup] = useState<string | null>(null);

  const layout = useMemo(() => layoutServers(servers), [servers]);

  const visibleGroups = useMemo(
    () => groups.filter((g) => show[g.kind] && g.members.length > 0),
    [groups, show],
  );

  // Highlight any group containing the hovered server (or the hovered group).
  function isHighlighted(g: GraphRaftGroup): boolean {
    if (hoverGroup) return hoverGroup === g.id;
    if (hoverServer) return g.members.includes(hoverServer);
    return false;
  }

  const counts = useMemo(() => {
    const c: Record<GraphRaftGroup["kind"], number> = {
      meta: 0,
      stream: 0,
      consumer: 0,
    };
    for (const g of groups) c[g.kind]++;
    return c;
  }, [groups]);

  return (
    <div className="topo-graph">
      <div className="topo-graph-toolbar">
        <Toggle
          label={`meta (${counts.meta})`}
          color={COLORS.meta.stroke}
          on={show.meta}
          onClick={() => setShow((s) => ({ ...s, meta: !s.meta }))}
        />
        <Toggle
          label={`streams (${counts.stream})`}
          color={COLORS.stream.stroke}
          on={show.stream}
          onClick={() => setShow((s) => ({ ...s, stream: !s.stream }))}
        />
        <Toggle
          label={`consumers (${counts.consumer})`}
          color={COLORS.consumer.stroke}
          on={show.consumer}
          onClick={() => setShow((s) => ({ ...s, consumer: !s.consumer }))}
        />
        <span className="muted topo-graph-hint">
          hover a node to highlight its raft groups · hover an edge to spotlight one group
        </span>
      </div>

      <div className="topo-graph-svg-wrap">
        <svg
          className="topo-graph-svg"
          viewBox={`0 0 ${SIZE} ${SIZE}`}
          xmlns="http://www.w3.org/2000/svg"
        >
          <defs>
            <radialGradient id="ring-bg" cx="50%" cy="50%" r="50%">
              <stop offset="0%" stopColor="rgba(110,168,254,0.05)" />
              <stop offset="80%" stopColor="rgba(11,15,23,0)" />
            </radialGradient>
            <filter id="glow">
              <feGaussianBlur stdDeviation="3" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS + 30} fill="url(#ring-bg)" />
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            stroke="rgba(110,168,254,0.08)"
            strokeDasharray="2 6"
          />

          {/* Raft-group polygons. Draw deselected first so hovered ones
              land on top. */}
          {visibleGroups.map((g) => {
            const hi = isHighlighted(g);
            const dim = (hoverServer || hoverGroup) && !hi;
            return (
              <RaftPolygon
                key={g.id}
                group={g}
                positions={layout}
                highlighted={hi}
                dimmed={!!dim}
                onHover={(on) => setHoverGroup(on ? g.id : null)}
              />
            );
          })}

          {/* Server nodes drawn last so they sit on top. */}
          {servers.map((s) => {
            const pos = layout.get(s);
            if (!pos) return null;
            const active = hoverServer === s;
            const ledCount = visibleGroups.filter((g) => g.leader === s).length;
            const memberCount = visibleGroups.filter((g) => g.members.includes(s)).length;
            return (
              <ServerNode
                key={s}
                name={s}
                x={pos.x}
                y={pos.y}
                active={active}
                isMetaLeader={metaLeader === s}
                ledCount={ledCount}
                memberCount={memberCount}
                onHover={(on) => setHoverServer(on ? s : null)}
              />
            );
          })}
        </svg>
      </div>

      {hoverServer ? (
        <div className="topo-graph-tip">
          <span className="mono">{hoverServer}</span> ·{" "}
          {visibleGroups.filter((g) => g.leader === hoverServer).length} leaders ·{" "}
          {visibleGroups.filter((g) => g.members.includes(hoverServer)).length} memberships
        </div>
      ) : hoverGroup ? (
        <HoverGroupTip group={visibleGroups.find((g) => g.id === hoverGroup)} />
      ) : (
        <div className="topo-graph-tip muted">
          showing {visibleGroups.length} raft group
          {visibleGroups.length === 1 ? "" : "s"}
        </div>
      )}
    </div>
  );
}

function HoverGroupTip({ group }: { group?: GraphRaftGroup }) {
  if (!group) return null;
  return (
    <div className="topo-graph-tip">
      <span className="mono">{group.label}</span> · <span className="muted">{group.kind} raft</span>{" "}
      · leader <span className="mono">{group.leader ?? "—"}</span> ·{" "}
      <span className="muted">replicas {group.members.join(", ")}</span>
    </div>
  );
}

function Toggle({
  label,
  color,
  on,
  onClick,
}: {
  label: string;
  color: string;
  on: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`topo-toggle${on ? " on" : ""}`}
      onClick={onClick}
      style={{ borderColor: on ? color : "var(--border)" }}
    >
      <span className="topo-toggle-swatch" style={{ background: color, opacity: on ? 1 : 0.3 }} />
      {label}
    </button>
  );
}

function ServerNode({
  name,
  x,
  y,
  active,
  isMetaLeader,
  ledCount,
  memberCount,
  onHover,
}: {
  name: string;
  x: number;
  y: number;
  active: boolean;
  isMetaLeader: boolean;
  ledCount: number;
  memberCount: number;
  onHover: (on: boolean) => void;
}) {
  const short = shortName(name);
  return (
    <g
      transform={`translate(${x},${y})`}
      onMouseEnter={() => onHover(true)}
      onMouseLeave={() => onHover(false)}
      style={{ cursor: "pointer" }}
    >
      <circle
        r={NODE_R + 8}
        fill={active ? "rgba(110,168,254,0.18)" : "transparent"}
        stroke={active ? "rgba(110,168,254,0.6)" : "transparent"}
      />
      <circle
        r={NODE_R}
        fill="#131826"
        stroke={isMetaLeader ? "#f3b760" : "#6ea8fe"}
        strokeWidth={isMetaLeader ? 2.5 : 1.5}
        filter={active ? "url(#glow)" : undefined}
      />
      {isMetaLeader ? (
        <g transform={`translate(0,${-NODE_R - 14})`}>
          <rect
            x={-18}
            y={-9}
            width={36}
            height={18}
            rx={4}
            fill="rgba(243,183,96,0.2)"
            stroke="#f3b760"
          />
          <text
            y={4}
            textAnchor="middle"
            fontSize={10}
            fontWeight={600}
            fill="#f3b760"
            fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
          >
            META
          </text>
        </g>
      ) : null}
      <text
        textAnchor="middle"
        y={-4}
        fontSize={11}
        fill="#e6e9f0"
        fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
      >
        {short}
      </text>
      <text
        textAnchor="middle"
        y={11}
        fontSize={10}
        fill="#7cdcb6"
        fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
      >
        L {ledCount}
      </text>
      <text
        textAnchor="middle"
        y={24}
        fontSize={9}
        fill="#8b94a8"
        fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
      >
        of {memberCount}
      </text>
    </g>
  );
}

function RaftPolygon({
  group,
  positions,
  highlighted,
  dimmed,
  onHover,
}: {
  group: GraphRaftGroup;
  positions: Map<string, { x: number; y: number }>;
  highlighted: boolean;
  dimmed: boolean;
  onHover: (on: boolean) => void;
}) {
  const pts = group.members
    .map((m) => positions.get(m))
    .filter((p): p is { x: number; y: number } => !!p);
  if (pts.length === 0) return null;

  const colors = COLORS[group.kind];
  const baseOpacity = group.kind === "consumer" ? 0.55 : 0.9;
  const opacity = highlighted ? 1 : dimmed ? 0.07 : baseOpacity;
  const leaderPos = group.leader ? positions.get(group.leader) : undefined;

  // Single-server raft (R1): draw a small ring around the server.
  if (pts.length === 1) {
    const p = pts[0]!;
    return (
      <g
        onMouseEnter={() => onHover(true)}
        onMouseLeave={() => onHover(false)}
        style={{ cursor: "pointer", opacity }}
      >
        <circle
          cx={p.x}
          cy={p.y}
          r={NODE_R + 6}
          fill="none"
          stroke={colors.stroke}
          strokeWidth={highlighted ? 2 : 1}
        />
      </g>
    );
  }

  // Edge for R2, polygon for R3+.
  if (pts.length === 2) {
    const [a, b] = pts as [(typeof pts)[0], (typeof pts)[0]];
    return (
      <g
        onMouseEnter={() => onHover(true)}
        onMouseLeave={() => onHover(false)}
        style={{ cursor: "pointer", opacity }}
      >
        <line
          x1={a.x}
          y1={a.y}
          x2={b.x}
          y2={b.y}
          stroke={colors.stroke}
          strokeWidth={highlighted ? 2 : 1}
          strokeOpacity={highlighted ? 1 : 0.6}
        />
      </g>
    );
  }

  const path = `${pts.map((p, i) => `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`).join(" ")} Z`;
  return (
    <g
      onMouseEnter={() => onHover(true)}
      onMouseLeave={() => onHover(false)}
      style={{ cursor: "pointer", opacity }}
    >
      <path
        d={path}
        fill={colors.fill}
        stroke={colors.stroke}
        strokeWidth={highlighted ? 2 : 1}
        strokeOpacity={highlighted ? 1 : 0.5}
        strokeLinejoin="round"
      />
      {highlighted && leaderPos ? (
        <circle
          cx={leaderPos.x}
          cy={leaderPos.y}
          r={NODE_R + 4}
          fill="none"
          stroke={colors.stroke}
          strokeWidth={2}
        />
      ) : null}
    </g>
  );
}

// ---------- helpers -----------------------------------------------------

function layoutServers(servers: string[]): Map<string, { x: number; y: number }> {
  const m = new Map<string, { x: number; y: number }>();
  const cx = SIZE / 2;
  const cy = SIZE / 2;
  const n = servers.length;
  if (n === 0) return m;
  // Start at the top (−π/2) and go clockwise.
  for (let i = 0; i < n; i++) {
    const theta = -Math.PI / 2 + (i * 2 * Math.PI) / n;
    m.set(servers[i]!, {
      x: cx + RADIUS * Math.cos(theta),
      y: cy + RADIUS * Math.sin(theta),
    });
  }
  return m;
}

function shortName(s: string): string {
  // js-nats-production-3 → "js-…-3"
  const m = s.match(/^(.*?)-(\d+)$/);
  if (m?.[1] && m[2]) {
    const head = m[1].split("-")[0] ?? m[1];
    return `${head}-${m[2]}`;
  }
  return s.length > 12 ? `${s.slice(0, 6)}…${s.slice(-3)}` : s;
}

// Re-export Crown so it doesn't get tree-shaken away if used only here later.
export { Crown };
