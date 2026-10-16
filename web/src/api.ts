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

// OIDC_LOGIN_MARKER is the fragment the OIDC callback appends to the
// post-login URL. It carries no token: it only says a handoff may be waiting.
const OIDC_LOGIN_MARKER = "#oidc_login";

// completeOIDCLogin collects the token the OIDC callback parked in an
// HttpOnly cookie bound to this browser, then cleans the URL. The token is
// never read from the URL, so a crafted link cannot replace the current
// session with someone else's (login CSRF). It runs before the UI mounts, so
// it must never throw: on any failure the existing session (if any) is kept
// and the user sees whatever page they would have seen anyway.
export async function completeOIDCLogin(): Promise<void> {
  if (location.hash !== OIDC_LOGIN_MARKER) return;
  history.replaceState(null, "", location.pathname + location.search);
  try {
    const res = await fetch("/auth/oidc/session", { method: "POST", credentials: "same-origin" });
    if (!res.ok) return;
    const data = (await res.json()) as { access_token?: unknown };
    if (typeof data.access_token === "string" && data.access_token) setToken(data.access_token);
  } catch {
    // Network error or malformed body: keep the current state.
  }
}

export class UnauthorizedError extends Error {}

// InvalidCredentialsError is a 401 from /auth/login: wrong username or
// password. Any other failure (network, 5xx) is a plain Error so the UI can
// tell the user to retry instead of blaming their password.
export class InvalidCredentialsError extends Error {}

// LoginValidationError is a 400 from /auth/login: the request itself is
// incomplete (an empty username or password). Retrying cannot fix it, so the
// UI asks the user to fill in the fields instead of reporting a server error.
export class LoginValidationError extends Error {}

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
  if (res.status === 400) throw new LoginValidationError("Enter both a username and a password.");
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
