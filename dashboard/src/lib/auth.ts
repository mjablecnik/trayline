import { browser } from '$app/environment';

const STORAGE_KEY = 'trayline.token';

export function getToken(): string | null {
	if (!browser) return null;
	return localStorage.getItem(STORAGE_KEY);
}

export function setToken(token: string): void {
	if (!browser) return;
	localStorage.setItem(STORAGE_KEY, token);
}

export function clearToken(): void {
	if (!browser) return;
	localStorage.removeItem(STORAGE_KEY);
}
