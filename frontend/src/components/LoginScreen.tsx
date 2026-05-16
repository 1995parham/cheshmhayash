import { LogIn } from "lucide-react";

// LoginScreen is shown when /api/auth/me reports anonymous. Clicking the
// button is a top-level navigation (not fetch) so the IdP redirect can set
// cookies and bounce back to /.
export function LoginScreen() {
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
          <p className="login-sub">
            This dashboard is gated by your organisation&apos;s identity provider.
          </p>
          <a className="login-btn" href={href}>
            <LogIn size={16} /> Continue with OIDC
          </a>
        </div>
      </main>
    </>
  );
}
