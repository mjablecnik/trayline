import { derived } from 'svelte/store';
import { locale, SUPPORTED_LOCALES, type Locale } from '$lib/stores/locale';
import cs from './cs';
import en from './en';

export type { Locale };
export { locale, SUPPORTED_LOCALES };

const translations = { cs, en } satisfies Record<Locale, typeof en>;

export type TranslationKey = keyof typeof en;

export const t = derived(locale, ($locale) => {
	const dict = translations[$locale];
	return (key: TranslationKey): string => dict[key];
});
