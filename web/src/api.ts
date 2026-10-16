// Thin client for the native /api/v1 surface plus /auth. The JWT is kept in
// localStorage and attached as a Bearer token to API calls.

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

// consumeTokenFromHash picks up an access token delivered by the OIDC callback
// as a URL fragment (#access_token=...), stores it, and cleans the URL. It
// runs before the UI mounts, so it must never throw: a malformed fragment
// (e.g. a stray "%") is discarded and the user simply sees the login page.
export function consumeTokenFromHash(): void {
  if (!location.hash.startsWith("#access_token=")) return;
  let token = "";
  try {
    token = decodeURIComponent(location.hash.slice("#access_token=".length));
  } catch {
    // URIError on malformed percent escapes: ignore the fragment.
  }
  if (token) setToken(token);
  history.replaceState(null, "", location.pathname + location.search);
}

export class UnauthorizedError extends Error {}

// InvalidCredentialsError is a 401 from /auth/login: wrong username or
// password. Any other failure (network, 5xx) is a plain Error so the UI can
// tell the user to retry instead of blaming their password.
export class InvalidCredentialsError extends Error {}

export async function login(username: string, password: string): Promise<void> {
  let res: Response;
  try {
    res = await fetch("/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
  } catch {
    throw new Error("Cannot reach the server. Check your connection and try again.");
  }
  if (res.status === 401) throw new InvalidCredentialsError("Invalid username or password");
  if (!res.ok) throw new Error(`Sign-in failed (server error ${res.status}). Try again later.`);
  const data = (await res.json()) as { access_token: string };
  setToken(data.access_token);
}

export interface AuthConfig {
  oidc: boolean;
}

// authConfig reports which login methods the server offers. On failure it
// assumes none beyond local login, so the UI never shows a dead SSO link.
export async function authConfig(): Promise<AuthConfig> {
  try {
    const res = await fetch("/auth/config");
    if (!res.ok) return { oidc: false };
    return (await res.json()) as AuthConfig;
  } catch {
    return { oidc: false };
  }
}

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
    // {"error": "forbidden", "message": "insufficient permissions"}.
    if (typeof e === "string") {
      if (typeof body.message === "string" && body.message) return body.message;
      return e;
    }
    if (e) {
      if (typeof e.details === "string") return e.details;
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
}

export interface PersonPage {
  persons: Person[];
  total: number;
  limit: number;
  offset: number;
}

export async function listPersons(limit: number, offset: number): Promise<PersonPage> {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  const res = await authFetch(`/api/v1/persons?${q}`);
  if (!res.ok) throw new Error(await errorMessage(res, "Failed to load persons"));
  const data = (await res.json()) as Omit<PersonPage, "persons"> & { persons: Person[] | null };
  return { ...data, persons: data.persons ?? [] };
}

// toWritable strips server-managed fields; the API rejects unknown fields and
// sets id/timestamps itself.
function toWritable(p: Person): Person {
  const { id, createdAt, updatedAt, ...rest } = p;
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
