// Sign-in at the configured token issuer: OAuth 2.0 authorization code flow
// with PKCE (RFC 7636), run entirely in the browser. SIS issues no tokens;
// it only tells us where to sign in (GET /auth/config) and checks the access
// token on every API call.

export interface AuthConfig {
  enabled: boolean;
  issuer?: string;
  client_id?: string;
  scope?: string;
}

interface Discovery {
  authorization_endpoint: string;
  token_endpoint: string;
  end_session_endpoint?: string;
}

// The pending sign-in lives in sessionStorage: it must survive the redirect
// to the issuer and back, but not outlive the tab.
const PENDING_KEY = "sis_oidc_pending";

const TOKEN_TIMEOUT_MS = 15_000;

interface Pending {
  state: string;
  verifier: string;
  tokenEndpoint: string;
  clientId: string;
  redirectUri: string;
}

export async function authConfig(): Promise<AuthConfig> {
  try {
    const res = await fetch("/auth/config");
    if (!res.ok) return { enabled: false };
    return (await res.json()) as AuthConfig;
  } catch {
    return { enabled: false };
  }
}

async function discover(issuer: string): Promise<Discovery> {
  const res = await fetch(issuer.replace(/\/$/, "") + "/.well-known/openid-configuration");
  if (!res.ok) throw new Error(`Cannot reach the sign-in service (${res.status}).`);
  return (await res.json()) as Discovery;
}

function randomString(bytes = 32): string {
  const b = new Uint8Array(bytes);
  crypto.getRandomValues(b);
  return base64url(b);
}

function base64url(b: Uint8Array): string {
  let s = "";
  for (const c of b) s += String.fromCharCode(c);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

async function challenge(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}

// The redirect URI is the web UI's root on this origin. It must be
// registered for the client at the issuer.
function redirectUri(): string {
  return location.origin + "/";
}

// signIn sends the browser to the issuer. It does not return.
export async function signIn(cfg: AuthConfig): Promise<void> {
  if (!cfg.enabled || !cfg.issuer || !cfg.client_id) throw new Error("Sign-in is not configured.");
  const d = await discover(cfg.issuer);
  const pending: Pending = {
    state: randomString(16),
    verifier: randomString(32),
    tokenEndpoint: d.token_endpoint,
    clientId: cfg.client_id,
    redirectUri: redirectUri(),
  };
  sessionStorage.setItem(PENDING_KEY, JSON.stringify(pending));
  // The endpoint may already carry a query (e.g. ?tenant=sis); merge into it.
  const u = new URL(d.authorization_endpoint);
  const q = {
    response_type: "code",
    client_id: cfg.client_id,
    redirect_uri: pending.redirectUri,
    scope: cfg.scope ?? "openid",
    state: pending.state,
    code_challenge: await challenge(pending.verifier),
    code_challenge_method: "S256",
  };
  for (const [k, v] of Object.entries(q)) u.searchParams.set(k, v);
  location.assign(u.toString());
}

// completeSignIn finishes a sign-in when the issuer has redirected back here
// with ?code=&state=, and returns the access token. It returns null when
// there is nothing to finish, and throws when the sign-in failed. The query
// is removed from the URL either way, so a reload does not retry it.
export async function completeSignIn(): Promise<string | null> {
  const params = new URLSearchParams(location.search);
  const code = params.get("code");
  const state = params.get("state");
  const error = params.get("error");
  if (!code && !error) return null;

  const raw = sessionStorage.getItem(PENDING_KEY);
  sessionStorage.removeItem(PENDING_KEY);
  history.replaceState(null, "", location.pathname);

  if (error) throw new Error(`Sign-in failed: ${params.get("error_description") || error}`);
  if (!raw) throw new Error("Sign-in failed: no sign-in was started in this tab. Try again.");
  const pending = JSON.parse(raw) as Pending;
  // The state binds the response to the sign-in this tab started, so a
  // crafted link cannot sign the user in as someone else.
  if (!state || state !== pending.state) throw new Error("Sign-in failed: state mismatch. Try again.");

  let data: { access_token?: unknown };
  try {
    const res = await fetch(pending.tokenEndpoint, {
      method: "POST",
      // The app is not rendered until this settles; a stalled issuer must
      // not leave the page blank.
      signal: AbortSignal.timeout(TOKEN_TIMEOUT_MS),
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        code: code!,
        redirect_uri: pending.redirectUri,
        client_id: pending.clientId,
        code_verifier: pending.verifier,
      }),
    });
    if (!res.ok) throw new Error(`Sign-in failed: the sign-in service answered ${res.status}.`);
    data = (await res.json()) as { access_token?: unknown };
  } catch (e) {
    if ((e as Error).name === "TimeoutError") {
      throw new Error("Sign-in failed: the sign-in service did not answer in time. Try again.");
    }
    throw e;
  }
  if (typeof data.access_token !== "string" || !data.access_token) {
    throw new Error("Sign-in failed: no access token in the response.");
  }
  return data.access_token;
}

// scopesOf reads the scope claim of a JWT access token, for showing what the
// user may do. Display only: SIS checks the token on every call.
export function scopesOf(token: string): string[] {
  try {
    const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    const claims = JSON.parse(atob(payload)) as { scope?: unknown };
    if (typeof claims.scope === "string") return claims.scope.split(/\s+/).filter(Boolean);
    if (Array.isArray(claims.scope)) return claims.scope.filter((s): s is string => typeof s === "string");
  } catch {
    // not a JWT we can read
  }
  return [];
}
