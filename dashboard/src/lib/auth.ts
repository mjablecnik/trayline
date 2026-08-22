import { browser } from '$app/environment';

const BASE_URL = import.meta.env.PUBLIC_API_URL as string;

// The API bearer token itself is never held here — it lives only in the
// HttpOnly `trayline_session` cookie the server sets at /auth/login, which
// this code (like all JS on the page) can never read. Only the CSRF token
// is kept in memory: unlike the session cookie, it MUST be readable by this
// code so it can be attached as the X-CSRF-Token header on mutating
// requests (see api.ts) — that's exactly what makes it safe to hold in a
// plain JS variable rather than something more protected.
let csrfToken: string | null = null;

export function getCSRFToken(): string | null {
	return csrfToken;
}

interface SessionResponse {
	ok: boolean;
	csrfToken: string;
}

/**
 * Exchanges the shared API token for an HttpOnly session cookie. Throws if
 * the token is wrong or the server is unreachable.
 */
export async function login(token: string): Promise<void> {
	const res = await fetch(`${BASE_URL}/auth/login`, {
		method: 'POST',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ token })
	});
	if (!res.ok) {
		throw new Error('invalid token');
	}
	const data = (await res.json()) as SessionResponse;
	csrfToken = data.csrfToken;
}

/** Clears the session server-side (best-effort) and the local CSRF token. */
export async function logout(): Promise<void> {
	csrfToken = null;
	if (!browser) return;
	try {
		await fetch(`${BASE_URL}/auth/logout`, { method: 'POST', credentials: 'include' });
	} catch {
		// Best-effort — the local state is already cleared either way, and the
		// cookie (if the request didn't reach the server) simply expires on
		// its own MaxAge.
	}
}

/**
 * Checks whether a valid session cookie already exists (e.g. on page load,
 * since the cookie is HttpOnly and can't be inspected directly) and, if so,
 * obtains a fresh CSRF token — the one from login isn't persisted across a
 * reload. Returns false on any failure, including no session existing yet.
 */
export async function checkSession(): Promise<boolean> {
	try {
		const res = await fetch(`${BASE_URL}/auth/session`, { credentials: 'include' });
		if (!res.ok) return false;
		const data = (await res.json()) as SessionResponse;
		csrfToken = data.csrfToken;
		return true;
	} catch {
		return false;
	}
}
