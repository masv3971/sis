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
          <Persons onUnauthorized={() => setAuthed(false)} />
        ) : (
          <Login onLogin={() => setAuthed(true)} />
        )}
      </main>
    </div>
  );
}
