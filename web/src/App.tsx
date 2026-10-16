import { useState } from "react";
import { getToken, clearToken } from "./api";
import { Login } from "./Login";
import { Persons } from "./Persons";

export function App() {
  const [authed, setAuthed] = useState<boolean>(!!getToken());

  return (
    <div className="app">
      <header className="topbar">
        <h1>SIS Admin</h1>
        {authed && (
          <button
            className="ghost"
            onClick={() => {
              clearToken();
              setAuthed(false);
            }}
          >
            Log out
          </button>
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
          <Login onLogin={() => setAuthed(true)} />
        )}
      </main>
    </div>
  );
}
