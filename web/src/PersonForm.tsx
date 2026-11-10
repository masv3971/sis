import { useRef, useState, type FormEvent } from "react";
import { type Contact, type Person, createPerson, updatePerson, UnauthorizedError } from "./api";
import { DatePicker, todayISO } from "./DatePicker";

const ROLES = ["student", "staff", "faculty", "guest"];

export const emptyPerson: Person = { givenName: "", familyName: "", status: "active", roles: ["student"] };

// PersonForm creates (id null) or replaces (id set) a person. It edits every
// writable field, including identifiers and contacts, because PUT replaces
// the whole record.
export function PersonForm({
  id,
  initial,
  onSaved,
  onCancel,
  onUnauthorized,
}: {
  id: string | null;
  initial: Person;
  onSaved: (p: Person) => void;
  onCancel?: () => void;
  onUnauthorized: () => void;
}) {
  const [form, setForm] = useState<Person>(() => structuredClone(initial));
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // submittingRef blocks a second submit synchronously: a second Create
  // without a sourcedId would make a second, different person.
  const submittingRef = useRef(false);

  const set = <K extends keyof Person>(key: K, value: Person[K]) => setForm({ ...form, [key]: value });

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    setError("");
    try {
      // Rows left without a value are dropped rather than stored empty.
      const keep = (list?: Contact[]) => list?.filter((c) => c.value.trim() !== "");
      const body: Person = {
        ...form,
        identifiers: keep(form.identifiers),
        emails: keep(form.emails),
        phones: keep(form.phones),
      };
      onSaved(id ? await updatePerson(id, body) : await createPerson(body));
    } catch (err) {
      if (err instanceof UnauthorizedError) onUnauthorized();
      else setError((err as Error).message);
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  }

  const roles = form.roles ?? [];
  // A stored role this form does not know is kept and shown.
  const roleChoices = [...ROLES, ...roles.filter((r) => !ROLES.includes(r))];

  return (
    <form onSubmit={submit}>
      <fieldset className="plain" disabled={submitting}>
        <div className="form-grid">
          <label>
            Given name
            <input value={form.givenName} onChange={(e) => set("givenName", e.target.value)} required />
          </label>
          <label>
            Middle name
            <input value={form.middleName ?? ""} onChange={(e) => set("middleName", e.target.value)} />
          </label>
          <label>
            Family name
            <input value={form.familyName} onChange={(e) => set("familyName", e.target.value)} required />
          </label>
          {/* ISO 8601 (YYYY-MM-DD) whatever the browser locale; SIS rejects
              anything else. Nobody is born in the future. */}
          <DatePicker
            label="Date of birth"
            value={form.dateOfBirth ?? ""}
            onChange={(v) => set("dateOfBirth", v)}
            min="1900-01-01"
            max={todayISO()}
            autoComplete="bday"
          />
          <label>
            Status
            <select value={form.status} onChange={(e) => set("status", e.target.value)}>
              <option value="active">active</option>
              <option value="inactive">inactive</option>
              <option value="tobedeleted">tobedeleted</option>
            </select>
          </label>
          <label>
            sourcedId
            <input
              value={form.sourcedId ?? ""}
              placeholder={id ? "empty keeps the stored one" : "defaults to the id"}
              onChange={(e) => set("sourcedId", e.target.value)}
            />
          </label>
        </div>

        <fieldset className="group">
          <legend>Roles</legend>
          <div className="row wrap">
            {roleChoices.map((r) => (
              <label key={r} className="check">
                <input
                  type="checkbox"
                  checked={roles.includes(r)}
                  onChange={(e) => set("roles", e.target.checked ? [...roles, r] : roles.filter((x) => x !== r))}
                />
                {r}
              </label>
            ))}
          </div>
        </fieldset>

        <PairList
          legend="Identifiers"
          typePlaceholder="studentNumber, eppn, …"
          items={form.identifiers ?? []}
          onChange={(v) => set("identifiers", v)}
        />
        <PairList legend="Emails" typePlaceholder="work, home" items={form.emails ?? []} onChange={(v) => set("emails", v)} />
        <PairList legend="Phones" typePlaceholder="mobile, work" items={form.phones ?? []} onChange={(v) => set("phones", v)} />
      </fieldset>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="row">
        <button type="submit" disabled={submitting}>
          {id ? "Save" : "Create"}
        </button>
        {onCancel && (
          <button type="button" className="ghost" onClick={onCancel} disabled={submitting}>
            Cancel
          </button>
        )}
      </div>
    </form>
  );
}

// PairList edits a list of {type, value} pairs (identifiers, emails, phones).
function PairList({
  legend,
  typePlaceholder,
  items,
  onChange,
}: {
  legend: string;
  typePlaceholder: string;
  items: Contact[];
  onChange: (items: Contact[]) => void;
}) {
  const update = (i: number, next: Contact) => onChange(items.map((c, j) => (j === i ? next : c)));
  return (
    <fieldset className="group">
      <legend>{legend}</legend>
      {items.map((c, i) => (
        <div className="row pair" key={i}>
          <input
            aria-label={`${legend} ${i + 1} type`}
            placeholder={typePlaceholder}
            value={c.type}
            onChange={(e) => update(i, { ...c, type: e.target.value })}
          />
          <input
            aria-label={`${legend} ${i + 1} value`}
            placeholder="value"
            value={c.value}
            onChange={(e) => update(i, { ...c, value: e.target.value })}
          />
          <button type="button" className="ghost" aria-label={`Remove ${legend} ${i + 1}`} onClick={() => onChange(items.filter((_, j) => j !== i))}>
            ✕
          </button>
        </div>
      ))}
      <button type="button" className="ghost small" onClick={() => onChange([...items, { type: "", value: "" }])}>
        Add
      </button>
    </fieldset>
  );
}
