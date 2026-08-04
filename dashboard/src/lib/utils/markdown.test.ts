import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown — formatting', () => {
	it('renders bold and italic', () => {
		const html = renderMarkdown('**bold** and *italic*');
		expect(html).toContain('<strong>bold</strong>');
		expect(html).toContain('<em>italic</em>');
	});

	it('renders headings', () => {
		expect(renderMarkdown('# Title')).toContain('<h1>Title</h1>');
	});

	it('renders fenced code blocks', () => {
		const html = renderMarkdown('```\nconst x = 1;\n```');
		expect(html).toContain('<pre>');
		expect(html).toContain('const x = 1;');
	});

	it('renders GFM strikethrough', () => {
		expect(renderMarkdown('~~gone~~')).toContain('<del>gone</del>');
	});

	it('treats a single newline as a line break (chat-style breaks)', () => {
		const html = renderMarkdown('line one\nline two');
		expect(html).toContain('<br>');
	});

	it('adds target and rel to links', () => {
		const html = renderMarkdown('[click me](https://example.com)');
		expect(html).toContain('target="_blank"');
		expect(html).toContain('rel="noopener noreferrer"');
		expect(html).toContain('href="https://example.com"');
	});
});

describe('renderMarkdown — sanitizes untrusted agent output', () => {
	it('strips <script> tags', () => {
		const html = renderMarkdown('before<script>alert(1)</script>after');
		expect(html).not.toContain('<script');
		expect(html).not.toContain('alert(1)');
	});

	it('strips inline event handler attributes', () => {
		const html = renderMarkdown('<img src="x" onerror="alert(1)">');
		expect(html).not.toContain('onerror');
	});

	it('neutralizes javascript: URIs in links', () => {
		const html = renderMarkdown('[click](javascript:alert(1))');
		expect(html).not.toContain('javascript:');
	});

	it('escapes raw HTML-looking text inside a fenced code block instead of executing it', () => {
		const html = renderMarkdown('```\n<script>alert(1)</script>\n```');
		expect(html).not.toContain('<script>alert');
		expect(html).toContain('&lt;script&gt;');
	});
});
