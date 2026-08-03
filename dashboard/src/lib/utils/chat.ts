/** Returns true when the given input contains at least one non-whitespace character. */
export function canSubmitMessage(text: string): boolean {
	return text.trim().length > 0;
}
