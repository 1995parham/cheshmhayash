import { LogIn, ShieldAlert } from "lucide-react";
import type { AuthMode } from "../hooks/useAuth";

// LoginScreen is shown when /api/auth/me reports anonymous.
//
// In oidc mode the button is a top-level navigation (not fetch) so the IdP
// redirect can set cookies and bounce back to /. In jwt mode there is nothing
// to click — an upstream gateway is responsible for the token — so we show a
// hint to check the proxy session instead of a dead login link.
export function LoginScreen({ mode }: { mode?: AuthMode }) {
  const returnTo = window.location.pathname + window.location.search;
  const href = `/api/auth/login?return_to=${encodeURIComponent(returnTo || "/")}`;
  return (
    <>
      <section className="hero" aria-label="cheshmhayash">
        <div className="hero-overlay">
          <h1 className="hero-title">cheshmhayash</h1>
          <p className="hero-sub">NATS administration panel</p>
        </div>
      </section>
      <main>
        <div className="login-card">
          <h2>sign in</h2>
          {mode === "jwt" ? (
            <>
              <p className="login-sub">
                Access is handled by your gateway. We couldn&apos;t read a valid token on this
                request — sign in to the proxy (or confirm your account is allowed) and reload.
              </p>
              <button className="login-btn" type="button" onClick={() => window.location.reload()}>
                <ShieldAlert size={16} /> Reload
              </button>
            </>
          ) : (
            <>
              <p className="login-sub">
                This dashboard is gated by your organisation&apos;s identity provider.
              </p>
              <a className="login-btn" href={href}>
                <LogIn size={16} /> Continue with OIDC
              </a>
            </>
          )}
        </div>
      </main>
    </>
  );
}
