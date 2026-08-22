import { derived, writable } from 'svelte/store';
import { checkSession, login as apiLogin, logout as apiLogout } from '$lib/auth';

export type AuthState = 'checking' | 'authenticated' | 'unauthenticated';

function createAuthStore() {
	const { subscribe, set } = writable<AuthState>('checking');

	return {
		subscribe,
		/** Call once on app load — resolves whether a session cookie already exists. */
		async init() {
			set((await checkSession()) ? 'authenticated' : 'unauthenticated');
		},
		async login(token: string) {
			await apiLogin(token);
			set('authenticated');
		},
		async logout() {
			await apiLogout();
			set('unauthenticated');
		}
	};
}

export const auth = createAuthStore();
export const isAuthenticated = derived(auth, ($state) => $state === 'authenticated');
export const isCheckingAuth = derived(auth, ($state) => $state === 'checking');
