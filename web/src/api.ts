// Thin client for the native /api/v1 surface plus /auth/login. The JWT is kept
// in localStorage and attached as a Bearer token to API calls.

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
// as a URL fragment (#access_token=...), stores it, and cleans the URL.
export function consumeTokenFromHash(): void {
  if (!location.hash.startsWith("#access_token=")) return;
  const token = decodeURIComponent(location.hash.slice("#access_token=".length));
  if (token) setToken(token);
  history.replaceState(null, "", location.pathname + location.search);
}

export class UnauthorizedError extends Error {}

export async function login(username: string, password: string): Promise<void> {
  const res = await fetch("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) throw new Error("Login failed");
  const data = (await res.json()) as { access_token: string };
  setToken(data.access_token);
}

async function authFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = getToken();
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body) headers.set("Content-Type", "application/json");
  const res = await fetch(path, { ...init, headers });
  if (res.status === 401) {
    clearToken();
    throw new UnauthorizedError("unauthorized");
  }
  return res;
}

async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as {
      error?: { title?: string; details?: unknown };
    };
    const e = body.error;
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

export interface Person {
  id?: string;
  sourcedId?: string;
  status: string;
  givenName: string;
  familyName: string;
  middleName?: string;
  roles?: string[];
  emails?: Contact[];
}

interface PersonList {
  persons: Person[] | null;
  total: number;
}

export async function listPersons(): Promise<Person[]> {
  const res = await authFetch("/api/v1/persons?limit=200");
  if (!res.ok) throw new Error(await errorMessage(res, "Failed to load persons"));
  const data = (await res.json()) as PersonList;
  return data.persons ?? [];
}

export async function createPerson(p: Person): Promise<Person> {
  const res = await authFetch("/api/v1/persons", { method: "POST", body: JSON.stringify(p) });
  if (!res.ok) throw new Error(await errorMessage(res, "Create failed"));
  return (await res.json()) as Person;
}

export async function updatePerson(id: string, p: Person): Promise<Person> {
  const res = await authFetch(`/api/v1/persons/${id}`, { method: "PUT", body: JSON.stringify(p) });
  if (!res.ok) throw new Error(await errorMessage(res, "Update failed"));
  return (await res.json()) as Person;
}

export async function deletePerson(id: string): Promise<void> {
  const res = await authFetch(`/api/v1/persons/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error(await errorMessage(res, "Delete failed"));
}
