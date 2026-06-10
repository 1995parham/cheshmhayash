import { useCallback, useEffect, useState } from "react";

export type Role = "admin" | "readonly";

// AuthMode mirrors the server's auth.mode. "oidc" runs the login flow here;
// "jwt" means an upstream gateway issues the token and there's nothing to sign
// in to (or out of) from the SPA.
export type AuthMode = "oidc" | "jwt";

export interface AuthIdentity {
  authenticated: boolean;
  mode?: AuthMode;
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
  | { state: "anonymous"; mode?: AuthMode }
  | { state: "authenticated"; identity: AuthIdentity };

// readMode best-effort-extracts the auth mode from a JSON /api/auth/me
// response. Returns null when the body isn't JSON or carries no mode (e.g. the
// cookie middleware's plain 401), which the caller treats as "oidc".
async function readMode(r: Response): Promise<AuthMode | null> {
  try {
    const body = (await r.json()) as { mode?: AuthMode };
    return body.mode ?? null;
  } catch {
    return null;
  }
}

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
        // 401 (signed out / no gateway token) and 403 (token valid but not on
        // the allowlist) both render the sign-in screen. The body may carry
        // the auth mode so the screen knows whether a login link applies.
        if (r.status === 401 || r.status === 403) {
          const mode = (await readMode(r)) ?? undefined;
          setStatus({ state: "anonymous", mode });
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
          setStatus({ state: "anonymous", mode: body.mode });
        }
      })
      .catch(() => setStatus({ state: "disabled" }));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    await fetch("/api/auth/logout", {
      method: "POST",
      credentials: "same-origin",
    });
    setStatus({ state: "anonymous" });
  }, []);

  return { status, refresh, logout };
}
