import DOMPurify from 'dompurify';
import { Marked } from 'marked';

const marked = new Marked({
	gfm: true,
	// Chat-style output rarely uses the blank-line-per-paragraph convention
	// consistently, so a single newline is treated as a line break rather
	// than being merged into the previous line.
	breaks: true
});

// Open links in a new tab without granting them a handle back to this page
// (reverse tabnabbing). Applies to every element DOMPurify keeps that has a
// `target` property - in practice just <a>.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
	if ('target' in node) {
		node.setAttribute('target', '_blank');
		node.setAttribute('rel', 'noopener noreferrer');
	}
});

/** Renders markdown to sanitized HTML, safe to pass to Svelte's {@html}. */
export function renderMarkdown(text: string): string {
	const html = marked.parse(text, { async: false });
	return DOMPurify.sanitize(html);
}
