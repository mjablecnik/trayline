import { createHighlighter, type Highlighter } from 'shiki';

const THEME = 'github-dark';

const LANGS = [
	'go',
	'typescript',
	'javascript',
	'python',
	'yaml',
	'json',
	'markdown',
	'bash',
	'html',
	'css',
	'svelte',
	'sql',
	'toml',
	'rust'
] as const;

type SupportedLang = (typeof LANGS)[number];

function isSupportedLang(lang: string | undefined): lang is SupportedLang {
	return !!lang && (LANGS as readonly string[]).includes(lang);
}

let highlighterPromise: Promise<Highlighter> | null = null;

function getHighlighter(): Promise<Highlighter> {
	if (!highlighterPromise) {
		highlighterPromise = createHighlighter({ themes: [THEME], langs: [...LANGS] });
	}
	return highlighterPromise;
}

/**
 * Returns syntax-highlighted HTML for `code`, or `null` if `language` isn't
 * supported (caller should render plain text instead).
 */
export async function highlightCode(
	code: string,
	language: string | undefined
): Promise<string | null> {
	if (!isSupportedLang(language)) return null;
	const highlighter = await getHighlighter();
	return highlighter.codeToHtml(code, { lang: language, theme: THEME });
}
