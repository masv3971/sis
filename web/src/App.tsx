import { useState } from "react";
import { getToken, clearToken } from "./api";
import { catalog } from "./entities";
import { Login } from "./Login";
import { scopesOf } from "./oidc";
import { Resource } from "./Resource";
import { href, offsetOf, useRoute } from "./route";
import { Student } from "./Student";
import { Students } from "./Students";
import logo from "./assets/sunet-logo.svg";

// The UI is person-centric: it opens on the student search, and a student's
// enrollments and results are edited on their page. The catalog (the
// organizations, courses, … students refer to) has its own pages.
export function App({ signInError }: { signInError: string }) {
  const [authed, setAuthed] = useState<boolean>(!!getToken());
  const route = useRoute();
  const scopes = authed ? scopesOf(getToken() ?? "") : [];
  // Display only: SIS enforces the scopes on every call. A token without
  // sis.write gets a read-only UI instead of buttons that answer 403.
  const canWrite = scopes.includes("sis.write");

  const onUnauthorized = () => {
    // A delayed 401 from an earlier session can reach here after a new
    // sign-in stored its token (possibly from a page that has since
    // unmounted). Only drop to the login page when there really is no
    // token any more.
    if (getToken() === null) setAuthed(false);
  };

  const def = catalog.find((r) => r.path === route.page);
  const inCatalog = def !== undefined;
  const q = route.params.get("q") ?? "";
  const offset = offsetOf(route.params);

  let page;
  if (!authed) {
    page = <Login error={signInError} />;
  } else if (route.page === "person" && route.params.get("id")) {
    const id = route.params.get("id")!;
    page = <Student key={id} id={id} backQuery={q} backOffset={offset} canWrite={canWrite} onUnauthorized={onUnauthorized} />;
  } else if (def) {
    // key: a new page starts with fresh state (form, paging, loads).
    page = <Resource key={def.path} def={def} canWrite={canWrite} onUnauthorized={onUnauthorized} />;
  } else {
    page = <Students query={q} startOffset={offset} canWrite={canWrite} onUnauthorized={onUnauthorized} />;
  }

  return (
    <div className="app">
      <header className="topbar">
        <h1>
          <a href={href("students")}>
            <img className="logo" src={logo} alt="" />
            SIS Admin
          </a>
        </h1>
        {authed && (
          <div className="row">
            <span className="muted">
              {canWrite ? "read/write" : scopes.includes("sis.read") ? "read only" : "no access"}
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
      {authed && (
        <nav className="tabs" aria-label="Main">
          <a href={href("students")} aria-current={!inCatalog ? "page" : undefined}>
            Students
          </a>
          <span className="tabs-sep muted">Catalog:</span>
          {catalog.map((r) => (
            <a key={r.path} href={href(r.path)} aria-current={r.path === route.page ? "page" : undefined}>
              {r.title}
            </a>
          ))}
        </nav>
      )}
      <main>{page}</main>
    </div>
  );
}
