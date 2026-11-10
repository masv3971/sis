// Thin client for the native /api/v1 surface. The access token comes from
// the token issuer (see oidc.ts), is kept in localStorage, and is attached
// as a Bearer token to API calls.

const TOKEN_KEY = "sis_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string): void {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export class UnauthorizedError extends Error {}

// NotFoundError is a 404: the record does not exist (any more).
export class NotFoundError extends Error {}

async function authFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = getToken();
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body) headers.set("Content-Type", "application/json");
  const res = await fetch(path, { ...init, headers });
  if (res.status === 401) {
    // Only protect a *different* session: a delayed 401 from an earlier
    // session (logout + sign-in while it was pending) must not clear the
    // newly issued token or log the user out again. If the token is
    // already gone (a concurrent request with the same expired token got
    // its 401 first), this request is unauthorized too, so every caller
    // sees UnauthorizedError and none can leave the UI signed in without a
    // token.
    const current = getToken();
    if (current !== null && current !== token) {
      throw new Error("Your session changed while the request was pending. Try again.");
    }
    clearToken();
    throw new UnauthorizedError("unauthorized");
  }
  return res;
}

async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as {
      error?: string | { title?: string; details?: unknown };
      message?: unknown;
    };
    const e = body.error;
    // The auth middleware answers 401/403 with a flat string envelope:
    // {"error": "forbidden", "message": "token lacks scope sis.write"}.
    if (typeof e === "string") {
      if (typeof body.message === "string" && body.message) return body.message;
      return e;
    }
    if (e) {
      const d = e.details;
      if (typeof d === "string") return d;
      // Validation errors: {"field": "personId", "error": "must be ..."}.
      if (d && typeof d === "object" && "error" in d && typeof d.error === "string") {
        return "field" in d && typeof d.field === "string" && d.field ? `${d.field}: ${d.error}` : d.error;
      }
      if (typeof e.title === "string") return e.title;
    }
  } catch {
    // fall through to the fallback
  }
  return fallback;
}

export interface Contact {
  type: string;
  value: string;
}

export interface Identifier {
  type: string;
  value: string;
}

// Person mirrors domain.Person. Every field the server returns is listed so a
// read-modify-write round trip (PUT is a full replacement) keeps them all.
export interface Person {
  id?: string;
  sourcedId?: string;
  status: string;
  givenName: string;
  familyName: string;
  middleName?: string;
  roles?: string[];
  identifiers?: Identifier[];
  emails?: Contact[];
  phones?: Contact[];
  dateOfBirth?: string;
  createdAt?: string;
  updatedAt?: string;
  // Set only on records loaded with a ttl (POST /api/v1/populate);
  // server-managed, writes ignore it.
  expiresAt?: string;
}

export interface PersonPage {
  persons: Person[];
  total: number;
  limit: number;
  offset: number;
}

// searchPersons lists the persons matching every whitespace-separated term
// of q (name, id, sourcedId, identifier or email); an empty q lists all.
export async function searchPersons(q: string, limit: number, offset: number): Promise<PersonPage> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (q.trim()) params.set("q", q.trim());
  const res = await authFetch(`/api/v1/persons?${params}`);
  if (!res.ok) throw new Error(await errorMessage(res, "Failed to load persons"));
  const data = (await res.json()) as Omit<PersonPage, "persons"> & { persons: Person[] | null };
  return { ...data, persons: data.persons ?? [] };
}

export async function getPerson(id: string): Promise<Person> {
  const res = await authFetch(`/api/v1/persons/${encodeURIComponent(id)}`);
  if (res.status === 404) throw new NotFoundError("No such person.");
  if (!res.ok) throw new Error(await errorMessage(res, "Failed to load the person"));
  return (await res.json()) as Person;
}

// toWritable strips server-managed fields; the API rejects unknown fields and
// sets id/timestamps itself.
function toWritable(p: Person): Person {
  const { id, createdAt, updatedAt, expiresAt, ...rest } = p;
  return rest;
}

export async function createPerson(p: Person): Promise<Person> {
  const res = await authFetch("/api/v1/persons", { method: "POST", body: JSON.stringify(toWritable(p)) });
  if (!res.ok) throw new Error(await errorMessage(res, "Create failed"));
  return (await res.json()) as Person;
}

export async function updatePerson(id: string, p: Person): Promise<Person> {
  const res = await authFetch(`/api/v1/persons/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(toWritable(p)),
  });
  if (!res.ok) throw new Error(await errorMessage(res, "Update failed"));
  return (await res.json()) as Person;
}

export async function deletePerson(id: string): Promise<void> {
  const res = await authFetch(`/api/v1/persons/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!res.ok) throw new Error(await errorMessage(res, "Delete failed"));
}

// Entity is a record of one of the generic /api/v1 collections
// (organizations, academic-sessions, courses, offerings, enrollments,
// programs, results). The fields differ per type (see entities.ts); every
// field the server returns is kept so a PUT (full replacement) sends them
// all back. A person's enrollments and results are the collections
// persons/<id>/enrollments and persons/<id>/results.
export type Entity = { id?: string; sourcedId?: string; status?: string } & Record<string, unknown>;

export interface EntityPage {
  items: Entity[];
  total: number;
  limit: number;
  offset: number;
}

// listEntities lists one page of the collection at /api/v1/<path>. Persons
// have their own envelope ({persons, ...}); it is normalised to items.
export async function listEntities(path: string, limit: number, offset: number): Promise<EntityPage> {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  const res = await authFetch(`/api/v1/${path}?${q}`);
  if (!res.ok) throw new Error(await errorMessage(res, `Failed to load ${path}`));
  const data = (await res.json()) as Omit<EntityPage, "items"> & {
    items?: Entity[] | null;
    persons?: Entity[] | null;
  };
  return { total: data.total, limit: data.limit, offset: data.offset, items: data.items ?? data.persons ?? [] };
}

// MAX_PAGES bounds listAllEntities: a reference picker is no place for a
// collection of hundreds of thousands of records.
const MAX_PAGES = 20;

// listAllEntities fetches the collection at /api/v1/<path> (for reference
// pickers) from offset, up to MAX_PAGES pages of the server's max_limit.
// truncated reports that more records exist than were fetched; call again
// with the returned offset to get the next pages.
export async function listAllEntities(
  path: string,
  offset = 0,
): Promise<{ items: Entity[]; truncated: boolean; offset: number }> {
  const items: Entity[] = [];
  for (let i = 0; i < MAX_PAGES; i++) {
    // The server clamps the limit to its max_limit and reports the
    // effective one, which is what the next offset must advance by.
    const page = await listEntities(path, 1000, offset);
    items.push(...page.items);
    offset += page.limit > 0 ? page.limit : page.items.length;
    if (page.items.length === 0 || offset >= page.total) return { items, truncated: false, offset };
  }
  return { items, truncated: true, offset };
}

function toWritableEntity(e: Entity): Entity {
  const { id, createdAt, updatedAt, expiresAt, ...rest } = e;
  return rest;
}

export async function createEntity(path: string, e: Entity): Promise<Entity> {
  const res = await authFetch(`/api/v1/${path}`, { method: "POST", body: JSON.stringify(toWritableEntity(e)) });
  if (!res.ok) throw new Error(await errorMessage(res, "Create failed"));
  return (await res.json()) as Entity;
}

export async function updateEntity(path: string, id: string, e: Entity): Promise<Entity> {
  const res = await authFetch(`/api/v1/${path}/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(toWritableEntity(e)),
  });
  if (!res.ok) throw new Error(await errorMessage(res, "Update failed"));
  return (await res.json()) as Entity;
}

export async function deleteEntity(path: string, id: string): Promise<void> {
  const res = await authFetch(`/api/v1/${path}/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!res.ok) throw new Error(await errorMessage(res, "Delete failed"));
}
