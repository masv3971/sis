import { useEffect, useState, type FormEvent } from "react";
import {
  type Person,
  listPersons,
  createPerson,
  updatePerson,
  deletePerson,
  UnauthorizedError,
} from "./api";

const emptyForm: Person = { givenName: "", familyName: "", status: "active" };

export function Persons({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [persons, setPersons] = useState<Person[]>([]);
  const [form, setForm] = useState<Person>(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState("");

  async function refresh() {
    try {
      setPersons(await listPersons());
    } catch (e) {
      if (e instanceof UnauthorizedError) onUnauthorized();
      else setError((e as Error).message);
    }
  }

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      if (editingId) await updatePerson(editingId, form);
      else await createPerson(form);
      setForm(emptyForm);
      setEditingId(null);
      await refresh();
    } catch (e) {
      if (e instanceof UnauthorizedError) onUnauthorized();
      else setError((e as Error).message);
    }
  }

  function edit(p: Person) {
    setEditingId(p.id ?? null);
    setForm({
      givenName: p.givenName,
      familyName: p.familyName,
      middleName: p.middleName,
      status: p.status,
      sourcedId: p.sourcedId,
      roles: p.roles,
    });
  }

  function cancel() {
    setEditingId(null);
    setForm(emptyForm);
    setError("");
  }

  async function remove(id: string) {
    try {
      await deletePerson(id);
      await refresh();
    } catch (e) {
      if (e instanceof UnauthorizedError) onUnauthorized();
      else setError((e as Error).message);
    }
  }

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
            <button type="submit">{editingId ? "Save" : "Create"}</button>
            {editingId && (
              <button type="button" className="ghost" onClick={cancel}>
                Cancel
              </button>
            )}
          </div>
        </form>
      </section>

      <section className="card">
        <h2>People ({persons.length})</h2>
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
      </section>
    </div>
  );
}
