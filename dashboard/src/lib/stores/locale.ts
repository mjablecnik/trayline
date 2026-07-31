import { browser } from '$app/environment';
import { writable } from 'svelte/store';

export const SUPPORTED_LOCALES = ['cs', 'en'] as const;
export type Locale = (typeof SUPPORTED_LOCALES)[number];

const DEFAULT_LOCALE: Locale = 'en';
const STORAGE_KEY = 'trayline.locale';

function isSupportedLocale(value: string | null): value is Locale {
	return value !== null && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

function detectLocale(): Locale {
	if (!browser) return DEFAULT_LOCALE;

	const stored = localStorage.getItem(STORAGE_KEY);
	if (isSupportedLocale(stored)) return stored;

	const browserLocale = navigator.language.slice(0, 2);
	return isSupportedLocale(browserLocale) ? browserLocale : DEFAULT_LOCALE;
}

function createLocaleStore() {
	const { subscribe, set } = writable<Locale>(detectLocale());

	return {
		subscribe,
		set(value: Locale) {
			if (browser) {
				localStorage.setItem(STORAGE_KEY, value);
				document.documentElement.lang = value;
			}
			set(value);
		}
	};
}

export const locale = createLocaleStore();

if (browser) {
	document.documentElement.lang = detectLocale();
}
