// Shared form/table pieces for the entity pages: the reference lookups
// (pickers and id -> name labels) and one input per field kind.

import { useEffect, useRef, useState } from "react";
import { type Entity, listAllEntities } from "./api";
import { DatePicker } from "./DatePicker";
import { type Field, type RefTarget, refLabel } from "./entities";


// RefOptions holds, per referenced collection, the records a picker offers
// and an id -> label map for tables.
export type RefOptions = Partial<
  Record<RefTarget, { items: Entity[]; labels: Map<string, string>; truncated: boolean; offset: number }>
>;

// useRefs loads the collections in targets (for the pickers and to show
// names instead of ids). reload fetches them again, e.g. after a write to a
// collection that refers to itself (organization parent). loadMore fetches
// the next pages of one truncated collection. Errors go to onError; only the
// latest load commits.
export function useRefs(
  targets: RefTarget[],
  onError: (e: unknown) => void,
): [RefOptions, () => Promise<void>, (t: RefTarget) => Promise<void>] {
  const [refs, setRefs] = useState<RefOptions>({});
  const refsRef = useRef(refs);
  refsRef.current = refs;
  const gen = useRef(0);
  const key = targets.join(",");

  async function reload(): Promise<void> {
    const mine = ++gen.current;
    try {
      const loaded = await Promise.all(targets.map((t) => listAllEntities(t)));
      if (mine !== gen.current) return;
      const next: RefOptions = {};
      targets.forEach((t, i) => {
        const { items, truncated, offset } = loaded[i];
        const labels = new Map(items.map((e) => [e.id ?? "", refLabel(t, e) || (e.sourcedId ?? e.id ?? "")]));
        next[t] = { items, labels, truncated, offset };
      });
      setRefs(next);
    } catch (e) {
      if (mine === gen.current) onError(e);
    }
  }

  async function loadMore(t: RefTarget): Promise<void> {
    const current = refsRef.current[t];
    if (!current?.truncated) return;
    // A reload that starts meanwhile bumps gen and replaces this result.
    const mine = gen.current;
    try {
      const more = await listAllEntities(t, current.offset);
      if (mine !== gen.current) return;
      // Merge into the pending state, not a render-time snapshot: another
      // picker's load may have finished since.
      setRefs((prev) => {
        const base = prev[t] ?? current;
        // Records added or removed since the first load shift the pages.
        const have = new Set(base.items.map((e) => e.id));
        const added = more.items.filter((e) => !have.has(e.id));
        const labels = new Map(base.labels);
        for (const e of added) labels.set(e.id ?? "", refLabel(t, e) || (e.sourcedId ?? e.id ?? ""));
        return {
          ...prev,
          [t]: { items: [...base.items, ...added], labels, truncated: more.truncated, offset: more.offset },
        };
      });
    } catch (e) {
      if (mine === gen.current) onError(e);
    }
  }

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return [refs, reload, loadMore];
}

// Cell renders one table cell of e. A reference shows the referenced
// record's name; an id that matches no loaded record (deleted, or beyond
// what the picker fetched) is shown as is.
export function Cell({ field, e, k, refs }: { field?: Field; e: Entity; k: string; refs: RefOptions }) {
  const value = e[k];
  if (value === undefined || value === null || value === "") return <td />;
  if (field?.kind === "ref" && field.ref) {
    const label = refs[field.ref]?.labels.get(String(value));
    return label ? <td>{label}</td> : <td className="mono">{String(value)}</td>;
  }
  return <td className={k === "sourcedId" ? "mono" : undefined}>{String(value)}</td>;
}

export function FieldInput({
  field,
  value,
  refs,
  onChange,
  onLoadMore,
}: {
  field: Field;
  value: unknown;
  refs: RefOptions;
  onChange: (v: unknown) => void;
  onLoadMore?: (t: RefTarget) => Promise<void>;
}) {
  const [loadingMore, setLoadingMore] = useState(false);
  const text = value === undefined || value === null ? "" : String(value);
  // Empty optional strings are sent as "" (the server treats them as unset);
  // an emptied number is dropped so the server sees no credits.
  switch (field.kind) {
    case "select":
      return (
        <label>
          {field.label}
          <select value={text} onChange={(e) => onChange(e.target.value)} required={field.required}>
            {!field.required && <option value="">—</option>}
            {/* Keep a stored value the list does not know. */}
            {text && !field.options?.includes(text) && <option value={text}>{text}</option>}
            {field.options?.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
        </label>
      );
    case "ref": {
      const target = field.ref ? refs[field.ref] : undefined;
      const known = target?.items.some((e) => e.id === text) ?? false;
      return (
        <label>
          {field.label}
          <select value={text} onChange={(e) => onChange(e.target.value)} required={field.required}>
            <option value="">{field.required ? "Select…" : "—"}</option>
            {/* A stored id the picker did not fetch (deleted target, or a
                truncated list) stays selectable so a save keeps it. */}
            {text && !known && <option value={text}>{text}</option>}
            {target?.items.map((e) => (
              <option key={e.id} value={e.id}>
                {target.labels.get(e.id ?? "")}
              </option>
            ))}
          </select>
          {target?.truncated && (
            <span className="row">
              <span className="muted">Only the first records are listed.</span>
              {onLoadMore && field.ref && (
                <button
                  type="button"
                  className="ghost small"
                  disabled={loadingMore}
                  onClick={async () => {
                    setLoadingMore(true);
                    try {
                      await onLoadMore(field.ref!);
                    } finally {
                      setLoadingMore(false);
                    }
                  }}
                >
                  {loadingMore ? "Loading…" : "Load more"}
                </button>
              )}
            </span>
          )}
        </label>
      );
    }
    case "number":
      return (
        <label>
          {field.label}
          <input
            type="number"
            min={0}
            step="any"
            value={text}
            required={field.required}
            onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
          />
        </label>
      );
    case "date":
      // ISO 8601 (YYYY-MM-DD) whatever the browser locale, typed or picked
      // from the calendar. A stored value in another format is shown as
      // is and flagged, never blanked.
      return <DatePicker label={field.label} value={text} required={field.required} onChange={onChange} />;
    default:
      return (
        <label>
          {field.label}
          <input
            value={text}
            placeholder={field.placeholder}
            required={field.required}
            onChange={(e) => onChange(e.target.value)}
          />
        </label>
      );
  }
}
