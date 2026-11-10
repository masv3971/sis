// Hash routing: #/<page>?<params>, e.g. #/person?id=… or #/courses. A hash
// route never reaches the server, so the sign-in redirect URI stays the bare
// origin and a reload or shared link opens the same page.

import { useEffect, useState } from "react";

export interface Route {
  page: string;
  params: URLSearchParams;
}

export function parseRoute(hash: string): Route {
  // Some link rewriters (port forwarders) send #%2Fcourses. Decode the whole
  // fragment only then: otherwise it would turn %2B and %26 in a value into
  // + and &, which URLSearchParams reads as a space and a separator.
  if (/^#%2f/i.test(hash)) {
    try {
      hash = decodeURIComponent(hash);
    } catch {
      // malformed escape: parse it as is
    }
  }
  const raw = hash.replace(/^#\/?/, "");
  const i = raw.indexOf("?");
  return i < 0
    ? { page: raw, params: new URLSearchParams() }
    : { page: raw.slice(0, i), params: new URLSearchParams(raw.slice(i + 1)) };
}

export function href(page: string, params: Record<string, string> = {}): string {
  const q = new URLSearchParams(params).toString();
  return `#/${page}${q ? `?${q}` : ""}`;
}

// studentsParams are the route parameters of a student search: its query and
// the offset of the page shown (omitted for the first page).
export function studentsParams(q: string, offset: number): Record<string, string> {
  const params: Record<string, string> = {};
  if (q) params.q = q;
  if (offset > 0) params.offset = String(offset);
  return params;
}

// offsetOf reads the search offset of a route; anything but a positive
// integer is the first page.
export function offsetOf(params: URLSearchParams): number {
  const n = Number(params.get("offset"));
  return Number.isInteger(n) && n > 0 ? n : 0;
}

// go navigates to page; hashchange then re-renders the app.
export function go(page: string, params: Record<string, string> = {}): void {
  location.hash = href(page, params);
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(location.hash));
  useEffect(() => {
    const onHash = () => setRoute(parseRoute(location.hash));
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  return route;
}
