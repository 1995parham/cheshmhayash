// Display formatters. Keep them pure so they're easy to test and use
// inside render functions.

export function num(n: number | null | undefined): string {
  if (n == null) return "—";
  return n.toLocaleString();
}

export function bytes(n: number | null | undefined): string {
  if (n == null) return "—";
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];
  let v = n / 1024;
  for (const u of units) {
    if (v < 1024) return `${v < 10 ? v.toFixed(2) : v < 100 ? v.toFixed(1) : Math.round(v)} ${u}`;
    v /= 1024;
  }
  return `${v.toExponential(1)} EiB`;
}

export function duration(ns: number | null | undefined): string {
  if (ns == null) return "—";
  const s = ns / 1e9;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${Math.floor(s % 60)}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

export function iso(s: string | null | undefined): string {
  if (!s) return "—";
  try {
    return new Date(s).toLocaleString();
  } catch {
    return s;
  }
}

export function shortId(id: string | null | undefined): string {
  if (!id) return "";
  if (id.length <= 16) return id;
  return `${id.slice(0, 8)}…${id.slice(-6)}`;
}
