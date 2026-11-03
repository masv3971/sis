import { useEffect, useRef, useState, type FormEvent } from "react";
import { type Entity, listEntities, createEntity, updateEntity, deleteEntity, UnauthorizedError } from "./api";
import { type Field, type ResourceDef } from "./entities";
import { Cell, FieldInput, useRefs } from "./fields";

// PAGE_SIZE is the page size requested. The server may clamp it to its
// configured max_limit, so navigation uses the size it reports back.
const PAGE_SIZE = 50;

// Resource is the list + form of one generic /api/v1 entity type, driven by
// its schema in entities.ts. It guards against stale loads and double
// submits like Persons did.
//
// By default it shows the whole collection. A student page embeds it with
// listPath (e.g. persons/<id>/enrollments) and fixed ({personId: <id>}):
// the list is then that person's records, and every write carries the fixed
// values, whose fields the form hides.
export function Resource({
  def,
  canWrite,
  onUnauthorized,
  listPath,
  fixed,
  compact,
  onChanged,
}: {
  def: ResourceDef;
  canWrite: boolean;
  onUnauthorized: () => void;
  listPath?: string;
  fixed?: Entity;
  // compact: a section of a larger page (smaller headings, no grid).
  compact?: boolean;
  // onChanged runs after every successful write.
  onChanged?: () => void;
}) {
  const fixedKeys = Object.keys(fixed ?? {});
  const fields = def.fields.filter((f) => !fixedKeys.includes(f.key));
  const columns = def.columns.filter((k) => !fixedKeys.includes(k));
  const empty: Entity = { ...def.empty, ...fixed };

  const [items, setItems] = useState<Entity[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [form, setForm] = useState<Entity>(empty);
  const [editingId, setEditingId] = useState<string | null>(null);
  // showForm: compact sections open the form on demand.
  const [showForm, setShowForm] = useState(!compact);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // loadGen numbers list loads; only the latest may commit its result.
  const loadGen = useRef(0);
  // submittingRef blocks a second submit synchronously (the state flag only
  // disables the button after the next render).
  const submittingRef = useRef(false);
  // editingRef mirrors editingId for async handlers (remove).
  const editingRef = useRef<string | null>(null);

  function fail(e: unknown) {
    if (e instanceof UnauthorizedError) onUnauthorized();
    else setError((e as Error).message);
  }

  const refTargets = [...new Set(fields.flatMap((f) => (f.ref ? [f.ref] : [])))];
  const [refs, reloadRefs] = useRefs(refTargets, fail);

  // chosen is the selected oneOf field: the one a record already has set,
  // else the first.
  const oneOf = def.oneOf;
  const chosenOf = (e: Entity) => oneOf?.keys.find((k) => e[k] !== undefined && e[k] !== "") ?? oneOf?.keys[0];
  const [chosen, setChosen] = useState(() => chosenOf(empty));

  function setEditing(id: string | null, next: Entity) {
    editingRef.current = id;
    setEditingId(id);
    setForm(next);
    setChosen(chosenOf(next));
  }

  // choose switches the oneOf choice, clearing the other choices and the
  // fields that only belong with them, so the record can never hold both.
  function choose(key: string) {
    const next = { ...form };
    for (const k of oneOf?.keys ?? []) if (k !== key) delete next[k];
    for (const f of fields) if (f.onlyWith && f.onlyWith !== key) delete next[f.key];
    setChosen(key);
    setForm(next);
  }

  const hidden = (f: Field) =>
    (oneOf?.keys.includes(f.key) && f.key !== chosen) || (f.onlyWith !== undefined && f.onlyWith !== chosen);

  function load(at: number): Promise<void> {
    return loadPage(at, ++loadGen.current);
  }

  async function loadPage(at: number, gen: number): Promise<void> {
    try {
      const page = await listEntities(listPath ?? def.path, PAGE_SIZE, at);
      if (gen !== loadGen.current) return;
      const size = page.limit > 0 ? page.limit : PAGE_SIZE;
      // Deleting the last row of the last page leaves it empty; step back.
      if (page.items.length === 0 && at > 0 && page.total > 0) {
        return loadPage(Math.max(0, at - size), gen);
      }
      setItems(page.items);
      setTotal(page.total);
      setOffset(at);
      setPageSize(size);
    } catch (e) {
      if (gen === loadGen.current) fail(e);
    }
  }

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function afterWrite() {
    // A collection may refer to itself (organization parent), so its
    // pickers reload too.
    await Promise.all([load(offset), refTargets.includes(def.path as never) ? reloadRefs() : undefined]);
    onChanged?.();
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    // One write at a time: a second Create without a sourcedId would make
    // the server generate a second, different record.
    if (submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    setError("");
    try {
      // Fixed values win over the draft: the record must stay this
      // person's even if the stored one said otherwise.
      const body = { ...form, ...fixed };
      if (editingId) await updateEntity(def.path, editingId, body);
      else await createEntity(def.path, body);
      setEditing(null, empty);
      if (compact) setShowForm(false);
      await afterWrite();
    } catch (e) {
      fail(e);
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  }

  // edit starts from a full copy of the record: PUT replaces it, so fields
  // the form does not show must be sent back unchanged.
  function edit(e: Entity) {
    if (submittingRef.current) return;
    setEditing(e.id ?? null, structuredClone(e));
    setShowForm(true);
    setError("");
  }

  function cancel() {
    setEditing(null, empty);
    if (compact) setShowForm(false);
    setError("");
  }

  async function remove(e: Entity) {
    const id = e.id!;
    if (!window.confirm(`Delete this ${def.singular}?`)) return;
    try {
      await deleteEntity(def.path, id);
      if (editingRef.current === id) setEditing(null, empty);
      await afterWrite();
    } catch (err) {
      fail(err);
    }
  }

  function setField(key: string, value: unknown) {
    const next = { ...form };
    if (value === undefined) delete next[key];
    else next[key] = value;
    setForm(next);
  }

  const first = total === 0 ? 0 : offset + 1;
  const last = offset + items.length;
  const labelOf = (key: string) => def.fields.find((f) => f.key === key)?.label ?? key;
  const Heading = compact ? "h3" : "h2";

  const formCard = canWrite && showForm && (
    <section className={compact ? "subcard" : "card"}>
      <Heading>{editingId ? `Edit ${def.singular}` : `New ${def.singular}`}</Heading>
      {def.note && <p className="muted note">{def.note}</p>}
      <form onSubmit={submit}>
        <fieldset className="plain" disabled={submitting}>
          {fields.map((f) => {
            if (hidden(f)) return null;
            const input = (
              <FieldInput
                key={f.key}
                // The chosen oneOf field is the record's subject: required.
                field={f.key === chosen && oneOf?.keys.includes(f.key) ? { ...f, required: true } : f}
                value={form[f.key]}
                refs={refs}
                onChange={(v) => setField(f.key, v)}
              />
            );
            if (f.key !== chosen || !oneOf?.keys.includes(f.key)) return input;
            return (
              <div key={f.key}>
                <fieldset className="group">
                  <legend>{oneOf.label}</legend>
                  <div className="row wrap">
                    {oneOf.keys.map((k) => (
                      <label key={k} className="check">
                        <input type="radio" name={`${def.path}-oneof`} checked={chosen === k} onChange={() => choose(k)} />
                        {labelOf(k)}
                      </label>
                    ))}
                  </div>
                </fieldset>
                {input}
              </div>
            );
          })}
        </fieldset>
        {/* Write errors belong next to the form that caused them. */}
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="row">
          <button type="submit" disabled={submitting}>
            {editingId ? "Save" : "Create"}
          </button>
          {(editingId || compact) && (
            <button type="button" className="ghost" onClick={cancel} disabled={submitting}>
              Cancel
            </button>
          )}
        </div>
      </form>
    </section>
  );

  const listCard = (
    <section className={compact ? "" : "card"}>
      <div className="row spread">
        <Heading>
          {def.title} ({total})
        </Heading>
        {compact && canWrite && !showForm && (
          <button type="button" className="ghost" onClick={() => setShowForm(true)}>
            Add {def.singular}
          </button>
        )}
      </div>
      {error && !formCard && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {columns.map((k) => (
                <th key={k}>{labelOf(k)}</th>
              ))}
              {canWrite && <th />}
            </tr>
          </thead>
          <tbody>
            {items.map((e) => (
              <tr key={e.id}>
                {columns.map((k) => (
                  <Cell key={k} k={k} e={e} field={def.fields.find((f) => f.key === k)} refs={refs} />
                ))}
                {canWrite && (
                  <td className="row">
                    <button className="ghost" onClick={() => edit(e)} disabled={submitting}>
                      Edit
                    </button>
                    <button className="danger" onClick={() => remove(e)}>
                      Delete
                    </button>
                  </td>
                )}
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={columns.length + 1} className="muted">
                  No {def.title.toLowerCase()} yet.
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
  );

  if (compact) {
    return (
      <div className="section">
        {listCard}
        {formCard}
      </div>
    );
  }
  return (
    <div className={canWrite ? "grid" : ""}>
      {formCard}
      {listCard}
    </div>
  );
}
