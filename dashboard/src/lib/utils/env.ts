export interface EditableVariable {
	id: string;
	key: string;
	value: string;
	isNew: boolean;
}

export const ENV_KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

const SENSITIVE_PATTERNS = ['KEY', 'SECRET', 'TOKEN', 'PASSWORD', 'PRIVATE'];

export type EnvKeyError = 'env.error.empty_key' | 'env.error.invalid_key' | 'env.error.duplicate';

/** Validates a variable key against the other keys in the same file, returning an i18n key for the error (if any). */
export function validateRow(key: string, allKeys: string[]): EnvKeyError | null {
	if (!key) return 'env.error.empty_key';
	if (!ENV_KEY_REGEX.test(key)) return 'env.error.invalid_key';
	if (allKeys.filter((k) => k === key).length > 1) return 'env.error.duplicate';
	return null;
}

/** Whether a variable's value should be masked by default (name contains a sensitive-looking pattern). */
export function isSensitive(key: string): boolean {
	const upper = key.toUpperCase();
	return SENSITIVE_PATTERNS.some((pattern) => upper.includes(pattern));
}

/** Whether the current variables differ from the original snapshot (order-sensitive, like the file's line order). */
export function isDirty(
	current: { key: string; value: string }[],
	original: { key: string; value: string }[]
): boolean {
	if (current.length !== original.length) return true;
	return current.some((v, i) => v.key !== original[i].key || v.value !== original[i].value);
}
