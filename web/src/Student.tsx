import { useEffect, useRef, useState } from "react";
import { type Person, deletePerson, getPerson, NotFoundError, UnauthorizedError } from "./api";
import { enrollmentDef, resultDef } from "./entities";
import { PersonForm } from "./PersonForm";
import { Resource } from "./Resource";
import { go, href, studentsParams } from "./route";

// Student is one person's page: their profile and, below it, their
// enrollments and results, each editable in place. Records added here are
// always this person's (personId is fixed).
export function Student({
  id,
  backQuery,
  backOffset,
  canWrite,
  onUnauthorized,
}: {
  id: string;
  // backQuery and backOffset are the search page the user came from, for the
  // Back link.
  backQuery: string;
  backOffset: number;
  canWrite: boolean;
  onUnauthorized: () => void;
}) {
  const [person, setPerson] = useState<Person | null>(null);
  const [missing, setMissing] = useState(false);
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState(false);
  const loadGen = useRef(0);

  function fail(e: unknown) {
    if (e instanceof UnauthorizedError) onUnauthorized();
    else setError((e as Error).message);
  }

  async function load() {
    const gen = ++loadGen.current;
    try {
      const p = await getPerson(id);
      if (gen !== loadGen.current) return;
      setPerson(p);
      setMissing(false);
    } catch (e) {
      if (gen !== loadGen.current) return;
      if (e instanceof NotFoundError) setMissing(true);
      else fail(e);
    }
  }

  useEffect(() => {
    setPerson(null);
    setEditing(false);
    setError("");
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  async function remove() {
    if (!person) return;
    const name = `${person.givenName} ${person.familyName}`;
    if (!window.confirm(`Delete ${name}, with all their enrollments and results? This cannot be undone.`)) return;
    setDeleting(true);
    setError("");
    try {
      await deletePerson(id);
      go("students", studentsParams(backQuery, 0));
    } catch (e) {
      fail(e);
      setDeleting(false);
    }
  }

  const back = (
    <a className="back" href={href("students", studentsParams(backQuery, backOffset))}>
      ← {backQuery ? `Results for “${backQuery}”` : "All people"}
    </a>
  );

  if (missing) {
    return (
      <div className="stack">
        {back}
        <section className="card">
          <h2>Not found</h2>
          <p className="muted">This person does not exist, or was deleted.</p>
        </section>
      </div>
    );
  }
  if (!person) {
    return (
      <div className="stack">
        {back}
        {error ? (
          <p className="error" role="alert">
            {error}
          </p>
        ) : (
          <p className="muted">Loading…</p>
        )}
      </div>
    );
  }

  // The fixed personId keeps every record created here attached to this
  // person; the person picker is hidden.
  const mine = { personId: id };
  const enc = encodeURIComponent(id);

  return (
    <div className="stack">
      {back}
      <section className="card">
        <div className="row spread">
          <div>
            <h2 className="title">
              {person.givenName} {person.middleName} {person.familyName}
            </h2>
            <p className="muted mono">
              {person.sourcedId} · {(person.roles ?? []).join(", ") || "no roles"} · {person.status}
            </p>
          </div>
          {canWrite && !editing && (
            <div className="row">
              <button className="ghost" onClick={() => setEditing(true)}>
                Edit
              </button>
              <button className="danger" onClick={remove} disabled={deleting}>
                {deleting ? "Deleting…" : "Delete"}
              </button>
            </div>
          )}
        </div>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {editing ? (
          <PersonForm
            id={id}
            initial={person}
            onSaved={(p) => {
              setPerson(p);
              setEditing(false);
            }}
            onCancel={() => setEditing(false)}
            onUnauthorized={onUnauthorized}
          />
        ) : (
          <Profile person={person} />
        )}
      </section>

      <section className="card">
        <Resource
          key={`e-${id}`}
          def={enrollmentDef}
          listPath={`persons/${enc}/enrollments`}
          fixed={mine}
          compact
          canWrite={canWrite}
          onUnauthorized={onUnauthorized}
        />
      </section>

      <section className="card">
        <Resource
          key={`r-${id}`}
          def={resultDef}
          listPath={`persons/${enc}/results`}
          fixed={mine}
          compact
          canWrite={canWrite}
          onUnauthorized={onUnauthorized}
        />
      </section>
    </div>
  );
}

function Profile({ person }: { person: Person }) {
  const pairs = (label: string, list?: { type: string; value: string }[]) =>
    list && list.length > 0 ? (
      <>
        <dt>{label}</dt>
        <dd>
          {list.map((c, i) => (
            <div key={i}>
              <span className="mono">{c.value}</span> {c.type && <span className="muted">({c.type})</span>}
            </div>
          ))}
        </dd>
      </>
    ) : null;
  return (
    <dl className="profile">
      {person.dateOfBirth && (
        <>
          <dt>Date of birth</dt>
          <dd>{person.dateOfBirth}</dd>
        </>
      )}
      {pairs("Identifiers", person.identifiers)}
      {pairs("Emails", person.emails)}
      {pairs("Phones", person.phones)}
      {person.expiresAt && (
        <>
          <dt>Expires</dt>
          <dd>{new Date(person.expiresAt).toLocaleString()} (loaded with a time limit)</dd>
        </>
      )}
      <dt>Id</dt>
      <dd className="mono">{person.id}</dd>
    </dl>
  );
}
