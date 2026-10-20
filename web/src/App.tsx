import { useState } from "react";
import { getToken, clearToken } from "./api";
import { Login } from "./Login";
import { scopesOf } from "./oidc";
import { Persons } from "./Persons";

export function App({ signInError }: { signInError: string }) {
  const [authed, setAuthed] = useState<boolean>(!!getToken());
  const scopes = authed ? scopesOf(getToken() ?? "") : [];

  return (
    <div className="app">
      <header className="topbar">
        <h1>SIS Admin</h1>
        {authed && (
          <div className="row">
            {/* What the token allows; SIS enforces it on every call. */}
            <span className="muted">
              {scopes.includes("sis.write") ? "read/write" : scopes.includes("sis.read") ? "read only" : "no access"}
            </span>
            <button
              className="ghost"
              onClick={() => {
                clearToken();
                setAuthed(false);
              }}
            >
              Log out
            </button>
          </div>
        )}
      </header>
      <main>
        {authed ? (
          <Persons
            onUnauthorized={() => {
              // A delayed 401 from an earlier session can reach here after a
              // new sign-in stored its token (possibly from a Persons
              // instance that has since unmounted). Only drop to the login
              // page when there really is no token any more.
              if (getToken() === null) setAuthed(false);
            }}
          />
        ) : (
          <Login error={signInError} />
        )}
      </main>
    </div>
  );
}
