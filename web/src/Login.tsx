import { useEffect, useState, type FormEvent } from "react";
import { authConfig, InvalidCredentialsError, login } from "./api";

export function Login({ onLogin }: { onLogin: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [oidc, setOidc] = useState(false);

  useEffect(() => {
    authConfig().then((c) => setOidc(c.oidc));
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    // The server requires both fields; say so up front rather than round-trip
    // to a 400. login() still maps a 400 to the same message as a backstop.
    if (!username || !password) {
      setError("Enter both a username and a password.");
      return;
    }
    setError("");
    setBusy(true);
    try {
      await login(username, password);
      onLogin();
    } catch (err) {
      setError(err instanceof InvalidCredentialsError ? "Invalid username or password" : (err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card login" onSubmit={submit}>
      <h2>Sign in</h2>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
      </label>
      <label>
        Password
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
      </label>
      {error && <p className="error">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? "Signing in…" : "Sign in"}
      </button>
      {oidc && (
        <button type="button" className="ghost" onClick={() => (window.location.href = "/auth/oidc/login")}>
          Sign in with SSO
        </button>
      )}
    </form>
  );
}
