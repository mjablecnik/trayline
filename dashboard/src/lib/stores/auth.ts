import { derived, writable } from 'svelte/store';
import { clearToken, getToken, setToken } from '$lib/auth';

function createAuthStore() {
	const { subscribe, set } = writable<string | null>(getToken());

	return {
		subscribe,
		login(token: string) {
			setToken(token);
			set(token);
		},
		logout() {
			clearToken();
			set(null);
		}
	};
}

export const auth = createAuthStore();
export const isAuthenticated = derived(auth, ($token) => $token !== null);
