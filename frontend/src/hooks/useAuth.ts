import { useCallback, useEffect, useState } from "react";

export type Role = "admin" | "readonly";

export interface AuthIdentity {
  authenticated: boolean;
  sub?: string;
  email?: string;
  name?: string;
  given_name?: string;
  family_name?: string;
  groups?: string[];
  role?: Role;
}

// canWrite decides whether mutating controls should be shown. Auth-disabled
// deployments ("disabled" status) keep full access, matching the
// backward-compatible server default. When authenticated, only the admin
// role may write; a missing role (older server) is treated as admin so a
// dashboard never silently loses its controls after a partial upgrade.
export function canWrite(status: AuthStatus): boolean {
  if (status.state === "authenticated") {
    return status.identity.role !== "readonly";
  }
  return status.state === "disabled";
}

// displayName picks the best label to show in the UI:
// "Given Family" if both are present, else `name`, else `email`, else `sub`.
export function displayName(id: AuthIdentity): string {
  const gn = (id.given_name ?? "").trim();
  const fn = (id.family_name ?? "").trim();
  if (gn && fn) return `${gn} ${fn}`;
  if (gn) return gn;
  if (fn) return fn;
  return id.name ?? id.email ?? id.sub ?? "";
}

export type AuthStatus =
  | { state: "loading" }
  | { state: "disabled" }
  | { state: "anonymous" }
  | { state: "authenticated"; identity: AuthIdentity };

// useAuth probes /api/auth/me on mount.
//
// - 200 → authenticated, identity in body
// - 401 → auth on, not signed in
// - 404 → auth disabled (no route registered)
// - anything else → treat as disabled so the panel stays usable
export function useAuth(): {
  status: AuthStatus;
  refresh: () => void;
  logout: () => Promise<void>;
} {
  const [status, setStatus] = useState<AuthStatus>({ state: "loading" });

  const refresh = useCallback(() => {
    setStatus({ state: "loading" });
    fetch("/api/auth/me", { credentials: "same-origin" })
      .then(async (r) => {
        if (r.status === 404) {
          setStatus({ state: "disabled" });
          return;
        }
        if (r.status === 401) {
          setStatus({ state: "anonymous" });
          return;
        }
        if (!r.ok) {
          setStatus({ state: "disabled" });
          return;
        }
        const body = (await r.json()) as AuthIdentity;
        if (body.authenticated) {
          setStatus({ state: "authenticated", identity: body });
        } else {
          setStatus({ state: "anonymous" });
        }
      })
      .catch(() => setStatus({ state: "disabled" }));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    await fetch("/api/auth/logout", { method: "POST", credentials: "same-origin" });
    setStatus({ state: "anonymous" });
  }, []);

  return { status, refresh, logout };
}
