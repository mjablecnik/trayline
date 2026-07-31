import type { Locale } from '$lib/stores/locale';

const UNITS: { unit: Intl.RelativeTimeFormatUnit; seconds: number }[] = [
	{ unit: 'year', seconds: 31536000 },
	{ unit: 'month', seconds: 2592000 },
	{ unit: 'week', seconds: 604800 },
	{ unit: 'day', seconds: 86400 },
	{ unit: 'hour', seconds: 3600 },
	{ unit: 'minute', seconds: 60 }
];

/** Formats a date as a locale-aware relative string, e.g. "2h ago" / "in 3 days" / "just now". */
export function formatRelativeDate(date: string | Date, locale: Locale, now = Date.now()): string {
	const target = typeof date === 'string' ? new Date(date) : date;
	const diffSeconds = (target.getTime() - now) / 1000;
	const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });

	for (const { unit, seconds } of UNITS) {
		if (Math.abs(diffSeconds) >= seconds) {
			return rtf.format(Math.round(diffSeconds / seconds), unit);
		}
	}

	return rtf.format(Math.round(diffSeconds), 'second');
}
