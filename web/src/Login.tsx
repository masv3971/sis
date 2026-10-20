import { useEffect, useState } from "react";
import { authConfig, signIn, type AuthConfig } from "./oidc";

// Login offers sign-in at the configured token issuer. SIS never sees a
// password: the issuer authenticates the user and decides the scopes.
export function Login({ error: initialError }: { error: string }) {
  const [cfg, setCfg] = useState<AuthConfig | null>(null);
  const [error, setError] = useState(initialError);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    authConfig().then(setCfg);
  }, []);

  async function start() {
    if (!cfg) return;
    setError("");
    setBusy(true);
    try {
      await signIn(cfg); // navigates away on success
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }

  return (
    <div className="card login">
      <h2>Sign in</h2>
      {cfg && !cfg.enabled && <p className="muted">Sign-in is not configured for this SIS.</p>}
      {/* role="alert" is an assertive live region: screen readers announce
          the message when it appears. */}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <button type="button" onClick={start} disabled={!cfg?.enabled || busy}>
        {busy ? "Redirecting…" : "Sign in"}
      </button>
    </div>
  );
}
