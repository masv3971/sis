import { useEffect, useRef, useState, type FormEvent } from "react";
import { type Person, searchPersons, UnauthorizedError } from "./api";
import { PersonForm, emptyPerson } from "./PersonForm";
import { go, href } from "./route";

// PAGE_SIZE is the page size requested. The server may clamp it to its
// configured max_limit, so navigation uses the size it reports back.
const PAGE_SIZE = 25;

// Students is the start page: find a person by name, student number, email
// or id, then open them. The query lives in the URL (#/students?q=…), so
// Back from a student returns to the same results.
export function Students({
  query,
  canWrite,
  onUnauthorized,
}: {
  query: string;
  canWrite: boolean;
  onUnauthorized: () => void;
}) {
  const [draft, setDraft] = useState(query);
  const [persons, setPersons] = useState<Person[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  // loadGen numbers searches; only the latest may commit its result, so a
  // slow earlier search cannot overwrite a newer one.
  const loadGen = useRef(0);

  async function load(at: number) {
    const gen = ++loadGen.current;
    setLoading(true);
    setError("");
    try {
      const page = await searchPersons(query, PAGE_SIZE, at);
      if (gen !== loadGen.current) return;
      setPersons(page.persons);
      setTotal(page.total);
      setOffset(at);
      setPageSize(page.limit > 0 ? page.limit : PAGE_SIZE);
    } catch (e) {
      if (gen !== loadGen.current) return;
      if (e instanceof UnauthorizedError) onUnauthorized();
      else setError((e as Error).message);
    } finally {
      if (gen === loadGen.current) setLoading(false);
    }
  }

  // A new query (typed here, or Back/Forward) starts at the first page.
  useEffect(() => {
    setDraft(query);
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  function submit(e: FormEvent) {
    e.preventDefault();
    const q = draft.trim();
    if (q === query) void load(0);
    else go("students", q ? { q } : {});
  }

  const first = total === 0 ? 0 : offset + 1;
  const last = offset + persons.length;
  // The query rides along, so the student page's Back link returns here.
  const personParams = (id: string): Record<string, string> => (query ? { id, q: query } : { id });

  return (
    <div className="stack">
      <section className="card">
        <form className="row search" onSubmit={submit} role="search">
          <input
            type="search"
            aria-label="Search people"
            placeholder="Name, student number, email or id"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            autoFocus
          />
          <button type="submit">Search</button>
          {canWrite && !creating && (
            <button type="button" className="ghost" onClick={() => setCreating(true)}>
              New person
            </button>
          )}
        </form>
        <p className="muted hint">Every word must match. Leave empty to list everyone.</p>
      </section>

      {creating && (
        <section className="card">
          <h2>New person</h2>
          <PersonForm
            id={null}
            initial={emptyPerson}
            onSaved={(p) => go("person", { id: p.id! })}
            onCancel={() => setCreating(false)}
            onUnauthorized={onUnauthorized}
          />
        </section>
      )}

      <section className="card">
        <h2>
          {query ? `Matching “${query}”` : "People"} ({total})
        </h2>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="table-wrap">
          <table className="clickable">
            <thead>
              <tr>
                <th>Name</th>
                <th>Identifiers</th>
                <th>Email</th>
                <th>Roles</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody aria-busy={loading}>
              {persons.map((p) => (
                <tr key={p.id} onClick={() => go("person", personParams(p.id!))}>
                  <td>
                    {/* A real link, so keyboard and middle-click work. */}
                    <a href={href("person", personParams(p.id!))} onClick={(e) => e.stopPropagation()}>
                      {p.givenName} {p.familyName}
                    </a>
                  </td>
                  <td className="mono">{(p.identifiers ?? []).map((i) => i.value).join(", ") || p.sourcedId}</td>
                  <td>{p.emails?.[0]?.value}</td>
                  <td>{(p.roles ?? []).join(", ")}</td>
                  <td>{p.status}</td>
                </tr>
              ))}
              {persons.length === 0 && !loading && (
                <tr>
                  <td colSpan={5} className="muted">
                    {query ? "Nobody matches." : "No people yet."}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {total > pageSize && (
          <div className="row pager">
            <button className="ghost" disabled={offset === 0} onClick={() => load(Math.max(0, offset - pageSize))}>
              Previous
            </button>
            <span className="muted">
              {first}–{last} of {total}
            </span>
            <button className="ghost" disabled={last >= total} onClick={() => load(offset + pageSize)}>
              Next
            </button>
          </div>
        )}
      </section>
    </div>
  );
}
