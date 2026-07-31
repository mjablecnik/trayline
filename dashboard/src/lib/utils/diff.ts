export type DiffLineType = 'add' | 'del' | 'context';

export interface DiffLine {
	type: DiffLineType;
	oldLineNo?: number;
	newLineNo?: number;
	content: string;
}

export interface DiffHunk {
	header: string;
	lines: DiffLine[];
}

export interface DiffFile {
	path: string;
	insertions: number;
	deletions: number;
	hunks: DiffHunk[];
	tooLarge: boolean;
}

const MAX_DIFF_FILE_BYTES = 500 * 1024;

const HUNK_HEADER_RE = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

function extractPath(section: string): string {
	const plusMatch = section.match(/^\+\+\+ b\/(.+)$/m);
	if (plusMatch && plusMatch[1] !== '/dev/null') return plusMatch[1];
	const minusMatch = section.match(/^--- a\/(.+)$/m);
	if (minusMatch) return minusMatch[1];
	const gitMatch = section.match(/^diff --git a\/(.+) b\/(.+)$/m);
	if (gitMatch) return gitMatch[2];
	return '';
}

function parseHunks(section: string): { hunks: DiffHunk[]; insertions: number; deletions: number } {
	const hunks: DiffHunk[] = [];
	let insertions = 0;
	let deletions = 0;
	let current: DiffHunk | null = null;
	let oldLineNo = 0;
	let newLineNo = 0;

	for (const line of section.split('\n')) {
		const headerMatch = line.match(HUNK_HEADER_RE);
		if (headerMatch) {
			oldLineNo = Number(headerMatch[1]);
			newLineNo = Number(headerMatch[2]);
			current = { header: line, lines: [] };
			hunks.push(current);
			continue;
		}
		if (!current || line.startsWith('\\')) continue;

		const marker = line[0];
		const content = line.slice(1);

		if (marker === '+') {
			current.lines.push({ type: 'add', newLineNo, content });
			newLineNo += 1;
			insertions += 1;
		} else if (marker === '-') {
			current.lines.push({ type: 'del', oldLineNo, content });
			oldLineNo += 1;
			deletions += 1;
		} else if (marker === ' ') {
			current.lines.push({ type: 'context', oldLineNo, newLineNo, content });
			oldLineNo += 1;
			newLineNo += 1;
		}
	}

	return { hunks, insertions, deletions };
}

/** Parses a unified diff string (as produced by `git diff`/`git show`) into structured per-file sections. */
export function parseDiff(diff: string): DiffFile[] {
	if (!diff.trim()) return [];

	const sections = diff.split(/(?=^diff --git )/m).filter((section) => section.trim().length > 0);

	return sections.map((section) => {
		const path = extractPath(section) || 'unknown';
		const byteSize = new TextEncoder().encode(section).length;

		if (byteSize > MAX_DIFF_FILE_BYTES) {
			return { path, insertions: 0, deletions: 0, hunks: [], tooLarge: true };
		}

		const { hunks, insertions, deletions } = parseHunks(section);
		return { path, insertions, deletions, hunks, tooLarge: false };
	});
}
