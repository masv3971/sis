import { useEffect, useId, useRef, useState, type FocusEvent, type KeyboardEvent } from "react";

// DatePicker is a date field that always shows and stores ISO 8601 calendar
// dates (YYYY-MM-DD), whatever the browser's locale; a native date input
// would display e.g. 03/14/2001. The value can be typed, or picked from a
// calendar that drops down from the button next to it.
//
// The field is invalid (the form will not submit, with a message) unless it
// is empty or a date that exists within [min, max]. All date arithmetic is
// in UTC so time zones cannot shift a day.

const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];
// ISO 8601 weeks start on Monday.
const WEEKDAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];
const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

interface Ymd {
  y: number;
  m: number; // 1-12
  d: number;
}

// utc builds a UTC date; setUTCFullYear keeps years below 100 literal
// (Date.UTC would read 0099 as 1999).
function utc(y: number, monthIndex: number, d: number): Date {
  const t = new Date(0);
  t.setUTCFullYear(y, monthIndex, d);
  return t;
}

function daysIn(y: number, m: number): number {
  return utc(y, m, 0).getUTCDate(); // day 0 of the next month
}

const pad = (n: number, width = 2) => String(n).padStart(width, "0");

function iso(y: number, m: number, d: number): string {
  return `${pad(y, 4)}-${pad(m)}-${pad(d)}`;
}

function fromDate(t: Date): string {
  return iso(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate());
}

// parseDate returns the parts of an ISO 8601 calendar date that exists, or
// null (wrong format, or e.g. 2001-02-29).
export function parseDate(s: string): Ymd | null {
  const match = ISO_DATE.exec(s);
  if (!match) return null;
  const [y, m, d] = [Number(match[1]), Number(match[2]), Number(match[3])];
  if (m < 1 || m > 12 || d < 1 || d > daysIn(y, m)) return null;
  return { y, m, d };
}

// todayISO is today's date where the user is.
export function todayISO(): string {
  const t = new Date();
  return iso(t.getFullYear(), t.getMonth() + 1, t.getDate());
}

function addDays(s: string, n: number): string {
  const p = parseDate(s)!;
  return fromDate(utc(p.y, p.m - 1, p.d + n));
}

// addMonths keeps the day, clamped to the target month's length.
function addMonths(s: string, n: number): string {
  const p = parseDate(s)!;
  const first = utc(p.y, p.m - 1 + n, 1);
  const y = first.getUTCFullYear();
  const m = first.getUTCMonth() + 1;
  return iso(y, m, Math.min(p.d, daysIn(y, m)));
}

// ISO dates with four-digit years compare correctly as strings.
function clamp(s: string, min?: string, max?: string): string {
  if (min && s < min) return min;
  if (max && s > max) return max;
  return s;
}

export function DatePicker({
  label,
  value,
  onChange,
  required,
  min,
  max,
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  // min and max (ISO dates) bound both the typed value and the calendar.
  min?: string;
  max?: string;
  autoComplete?: string;
}) {
  const id = useId();
  const errorId = `${id}-error`;
  const [open, setOpen] = useState(false);
  const [touched, setTouched] = useState(false);
  // view is the month the calendar shows; cursor the day that has focus.
  const [view, setView] = useState({ y: 2000, m: 1 });
  const [cursor, setCursor] = useState("");
  const wrapRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const gridRef = useRef<HTMLTableElement>(null);
  // focusDay: move focus to the cursor day after the next render (on open
  // and keyboard moves, not when the month/year selects change).
  const focusDay = useRef(false);

  const parsed = parseDate(value);
  let problem = "";
  if (value !== "" && !parsed) problem = "Use an ISO 8601 date: YYYY-MM-DD, e.g. 2001-03-14.";
  else if (value !== "" && min && value < min) problem = `Must be on or after ${min}.`;
  else if (value !== "" && max && value > max) problem = `Must be on or before ${max}.`;

  // The browser blocks submit on a custom validity message and shows it.
  useEffect(() => {
    inputRef.current?.setCustomValidity(problem);
  }, [problem]);

  useEffect(() => {
    if (!open || !focusDay.current) return;
    focusDay.current = false;
    gridRef.current?.querySelector<HTMLButtonElement>(`[data-date="${cursor}"]`)?.focus();
  }, [open, cursor, view]);

  // A click anywhere outside closes the calendar.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  function moveTo(day: string, focus: boolean) {
    const p = parseDate(day)!;
    setCursor(day);
    setView({ y: p.y, m: p.m });
    focusDay.current = focus;
  }

  function openCalendar() {
    moveTo(clamp(parsed ? value : todayISO(), min, max), true);
    setOpen(true);
  }

  // showMonth switches the calendar to another month, keeping the cursor's
  // day so the grid always holds a focusable day. The cursor is clamped to
  // [min, max]; a clamped cursor can sit in another month, which then shows.
  function showMonth(y: number, m: number) {
    const d = Math.min(parseDate(cursor)?.d ?? 1, daysIn(y, m));
    const next = clamp(iso(y, m, d), min, max);
    const p = parseDate(next)!;
    setView({ y: p.y, m: p.m });
    setCursor(next);
  }

  function pick(day: string) {
    onChange(day);
    setTouched(true);
    setOpen(false);
    inputRef.current?.focus();
  }

  function onGridKey(e: KeyboardEvent<HTMLTableElement>) {
    const moves: Record<string, (s: string) => string> = {
      ArrowLeft: (s) => addDays(s, -1),
      ArrowRight: (s) => addDays(s, 1),
      ArrowUp: (s) => addDays(s, -7),
      ArrowDown: (s) => addDays(s, 7),
      PageUp: (s) => addMonths(s, -1),
      PageDown: (s) => addMonths(s, 1),
    };
    const move = moves[e.key];
    if (!move) return;
    e.preventDefault();
    moveTo(clamp(move(cursor), min, max), true);
  }

  function onKey(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Escape" && open) {
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
      toggleRef.current?.focus();
    }
  }

  // Tabbing to a control outside closes the calendar. relatedTarget is null
  // for a click on a non-focusable spot, which the mousedown handler judges.
  function onBlur(e: FocusEvent<HTMLDivElement>) {
    const next = e.relatedTarget as Node | null;
    if (open && next && !wrapRef.current?.contains(next)) setOpen(false);
  }

  // Year choices: the bounds when given, else 120 years back and 10 ahead;
  // the shown year is always included (e.g. a stored date out of range).
  const thisYear = new Date().getFullYear();
  const hiYear = max ? Number(max.slice(0, 4)) : thisYear + 10;
  const loYear = min ? Number(min.slice(0, 4)) : hiYear - 130;
  const years: number[] = [];
  for (let y = Math.max(hiYear, view.y); y >= Math.min(loYear, view.y); y--) years.push(y);

  const offset = (utc(view.y, view.m - 1, 1).getUTCDay() + 6) % 7;
  const cells: (string | null)[] = [...Array<null>(offset).fill(null)];
  for (let d = 1; d <= daysIn(view.y, view.m); d++) cells.push(iso(view.y, view.m, d));
  while (cells.length % 7 !== 0) cells.push(null);
  const weeks: (string | null)[][] = [];
  for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7));

  const today = todayISO();
  const prevMonth = addMonths(iso(view.y, view.m, 1), -1);
  const nextMonth = addMonths(iso(view.y, view.m, 1), 1);
  const showError = touched && problem !== "";

  return (
    <div className="field date-field" ref={wrapRef} onKeyDown={onKey} onBlur={onBlur}>
      <label htmlFor={id}>{label}</label>
      <div className="row date-input">
        <input
          id={id}
          ref={inputRef}
          inputMode="numeric"
          placeholder="YYYY-MM-DD"
          maxLength={10}
          autoComplete={autoComplete ?? "off"}
          value={value}
          required={required}
          aria-invalid={showError || undefined}
          aria-describedby={showError ? errorId : undefined}
          onChange={(e) => onChange(e.target.value.trim())}
          onBlur={() => setTouched(true)}
        />
        <button
          type="button"
          ref={toggleRef}
          className="ghost icon"
          aria-label={`Choose ${label.toLowerCase()} from a calendar`}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => (open ? setOpen(false) : openCalendar())}
        >
          <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
            <rect x="1.5" y="3" width="13" height="11.5" rx="2" fill="none" stroke="currentColor" strokeWidth="1.3" />
            <path d="M1.5 6.5h13M5 1.5v3M11 1.5v3" stroke="currentColor" strokeWidth="1.3" />
          </svg>
        </button>
      </div>
      {showError && (
        <span id={errorId} className="field-error">
          {problem}
        </span>
      )}

      {open && (
        <div className="calendar" role="dialog" aria-label={`Choose ${label.toLowerCase()}`}>
          <div className="calendar-head">
            <button
              type="button"
              className="ghost icon"
              aria-label="Previous month"
              disabled={!!min && prevMonth.slice(0, 7) < min.slice(0, 7)}
              onClick={() => {
                const p = parseDate(prevMonth)!;
                showMonth(p.y, p.m);
              }}
            >
              ‹
            </button>
            <select aria-label="Month" value={view.m} onChange={(e) => showMonth(view.y, Number(e.target.value))}>
              {MONTHS.map((name, i) => (
                <option key={name} value={i + 1}>
                  {name}
                </option>
              ))}
            </select>
            <select aria-label="Year" value={view.y} onChange={(e) => showMonth(Number(e.target.value), view.m)}>
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="ghost icon"
              aria-label="Next month"
              disabled={!!max && nextMonth.slice(0, 7) > max.slice(0, 7)}
              onClick={() => {
                const p = parseDate(nextMonth)!;
                showMonth(p.y, p.m);
              }}
            >
              ›
            </button>
          </div>
          <table ref={gridRef} onKeyDown={onGridKey}>
            <thead>
              <tr>
                {WEEKDAYS.map((w) => (
                  <th key={w} scope="col">
                    {w}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {weeks.map((week, i) => (
                <tr key={i}>
                  {week.map((day, j) =>
                    day === null ? (
                      <td key={j} />
                    ) : (
                      <td key={j}>
                        <button
                          type="button"
                          data-date={day}
                          tabIndex={day === cursor ? 0 : -1}
                          className={[day === value ? "selected" : "", day === today ? "today" : ""].join(" ").trim() || undefined}
                          aria-label={`${Number(day.slice(8))} ${MONTHS[view.m - 1]} ${view.y}`}
                          aria-pressed={day === value}
                          aria-current={day === today ? "date" : undefined}
                          disabled={(!!min && day < min) || (!!max && day > max)}
                          onClick={() => pick(day)}
                        >
                          {Number(day.slice(8))}
                        </button>
                      </td>
                    ),
                  )}
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted calendar-hint">Arrow keys move, Page Up/Down change month, Esc closes.</p>
        </div>
      )}
    </div>
  );
}
