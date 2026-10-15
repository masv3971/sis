import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  type Person,
  listPersons,
  createPerson,
  updatePerson,
  deletePerson,
  UnauthorizedError,
} from "./api";

// PAGE_SIZE is the page size requested. The server may clamp it to its
// configured max_limit, so navigation uses the size it reports back.
const PAGE_SIZE = 50;

const emptyForm: Person = { givenName: "", familyName: "", status: "active" };

export function Persons({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [persons, setPersons] = useState<Person[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [form, setForm] = useState<Person>(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // loadGen numbers load() calls; only the latest may commit its result.
  // A slow earlier GET (initial load, or Previous/Next clicked twice) would
  // otherwise overwrite a newer page when it finally resolves.
  const loadGen = useRef(0);
  // submittingRef blocks a second submit synchronously; the state flag
  // above only disables the button after the next render, which is too late
  // for a fast double-click or a repeated Enter.
  const submittingRef = useRef(false);

  function fail(e: unknown) {
    if (e instanceof UnauthorizedError) onUnauthorized();
    else setError((e as Error).message);
  }

  function load(at: number): Promise<void> {
    return loadPage(at, ++loadGen.current);
  }

  // loadPage fetches the page at offset `at` and commits it only if no newer
  // load has started meanwhile. The step-back retry keeps the same
  // generation, so it is superseded by a newer load just like the original.
  async function loadPage(at: number, gen: number): Promise<void> {
    try {
      const page = await listPersons(PAGE_SIZE, at);
      if (gen !== loadGen.current) return;
      const size = page.limit > 0 ? page.limit : PAGE_SIZE;
      // Deleting the last row of the last page leaves it empty; step back.
      if (page.persons.length === 0 && at > 0 && page.total > 0) {
        return loadPage(Math.max(0, at - size), gen);
      }
      setPersons(page.persons);
      setTotal(page.total);
      setOffset(at);
      setPageSize(size);
    } catch (e) {
      if (gen === loadGen.current) fail(e);
    }
  }

  useEffect(() => {
    load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    // One write at a time: a second Create without a sourcedId would make
    // the server generate a second, different person.
    if (submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    setError("");
    try {
      if (editingId) await updatePerson(editingId, form);
      else await createPerson(form);
      setForm(emptyForm);
      setEditingId(null);
      await load(offset);
    } catch (e) {
      fail(e);
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  }

  // edit starts from a full copy of the person: PUT replaces the whole
  // record, so any field the form does not show (identifiers, emails,
  // phones, dateOfBirth, ...) must be sent back unchanged.
  function edit(p: Person) {
    setEditingId(p.id ?? null);
    setForm(structuredClone(p));
  }

  function cancel() {
    setEditingId(null);
    setForm(emptyForm);
    setError("");
  }

  async function remove(id: string) {
    try {
      await deletePerson(id);
      await load(offset);
    } catch (e) {
      fail(e);
    }
  }

  const first = total === 0 ? 0 : offset + 1;
  const last = offset + persons.length;

  return (
    <div className="grid">
      <section className="card">
        <h2>{editingId ? "Edit person" : "New person"}</h2>
        <form onSubmit={submit}>
          <label>
            Given name
            <input
              value={form.givenName}
              onChange={(e) => setForm({ ...form, givenName: e.target.value })}
              required
            />
          </label>
          <label>
            Family name
            <input
              value={form.familyName}
              onChange={(e) => setForm({ ...form, familyName: e.target.value })}
              required
            />
          </label>
          <label>
            Status
            <select value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value })}>
              <option value="active">active</option>
              <option value="inactive">inactive</option>
              <option value="tobedeleted">tobedeleted</option>
            </select>
          </label>
          {error && <p className="error">{error}</p>}
          <div className="row">
            <button type="submit" disabled={submitting}>
              {editingId ? "Save" : "Create"}
            </button>
            {editingId && (
              <button type="button" className="ghost" onClick={cancel} disabled={submitting}>
                Cancel
              </button>
            )}
          </div>
        </form>
      </section>

      <section className="card">
        <h2>People ({total})</h2>
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>sourcedId</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {persons.map((p) => (
              <tr key={p.id}>
                <td>
                  {p.givenName} {p.familyName}
                </td>
                <td>{p.status}</td>
                <td className="mono">{p.sourcedId}</td>
                <td className="row">
                  <button className="ghost" onClick={() => edit(p)}>
                    Edit
                  </button>
                  <button className="danger" onClick={() => remove(p.id!)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {persons.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  No people yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
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
