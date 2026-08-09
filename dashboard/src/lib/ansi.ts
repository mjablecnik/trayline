/**
 * Lightweight ANSI SGR escape sequence to HTML converter.
 *
 * Supports:
 * - Standard colors (30-37, 40-47)
 * - Bright colors (90-97, 100-107)
 * - Bold (1), dim (2), italic (3), underline (4)
 * - Reset (0)
 *
 * Does NOT support 256-color or truecolor (24-bit) sequences — they are stripped.
 */

// eslint-disable-next-line no-control-regex -- \x1b (ESC) is the ANSI SGR sequence prefix we're parsing
const ANSI_REGEX = /\x1b\[([0-9;]*)m/g;

const FG_COLORS: Record<number, string> = {
	30: '#4e4e4e', // black
	31: '#e06c75', // red
	32: '#98c379', // green
	33: '#e5c07b', // yellow
	34: '#61afef', // blue
	35: '#c678dd', // magenta
	36: '#56b6c2', // cyan
	37: '#dcdfe4', // white
	90: '#5c6370', // bright black (gray)
	91: '#e06c75', // bright red
	92: '#98c379', // bright green
	93: '#e5c07b', // bright yellow
	94: '#61afef', // bright blue
	95: '#c678dd', // bright magenta
	96: '#56b6c2', // bright cyan
	97: '#ffffff' // bright white
};

const BG_COLORS: Record<number, string> = {
	40: '#4e4e4e',
	41: '#e06c75',
	42: '#98c379',
	43: '#e5c07b',
	44: '#61afef',
	45: '#c678dd',
	46: '#56b6c2',
	47: '#dcdfe4',
	100: '#5c6370',
	101: '#e06c75',
	102: '#98c379',
	103: '#e5c07b',
	104: '#61afef',
	105: '#c678dd',
	106: '#56b6c2',
	107: '#ffffff'
};

interface AnsiState {
	fg: string | null;
	bg: string | null;
	bold: boolean;
	dim: boolean;
	italic: boolean;
	underline: boolean;
}

function escapeHtml(text: string): string {
	return text
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');
}

function buildStyle(state: AnsiState): string {
	const parts: string[] = [];
	if (state.fg) parts.push(`color:${state.fg}`);
	if (state.bg) parts.push(`background-color:${state.bg}`);
	if (state.bold) parts.push('font-weight:bold');
	if (state.dim) parts.push('opacity:0.7');
	if (state.italic) parts.push('font-style:italic');
	if (state.underline) parts.push('text-decoration:underline');
	return parts.join(';');
}

function hasStyle(state: AnsiState): boolean {
	return !!(state.fg || state.bg || state.bold || state.dim || state.italic || state.underline);
}

function resetState(): AnsiState {
	return { fg: null, bg: null, bold: false, dim: false, italic: false, underline: false };
}

/**
 * Convert a string with ANSI escape codes to safe HTML with inline styles.
 */
export function ansiToHtml(input: string): string {
	const state: AnsiState = resetState();
	let result = '';
	let lastIndex = 0;
	let match: RegExpExecArray | null;

	ANSI_REGEX.lastIndex = 0;

	while ((match = ANSI_REGEX.exec(input)) !== null) {
		// Flush text before this escape
		const textBefore = input.slice(lastIndex, match.index);
		if (textBefore) {
			if (hasStyle(state)) {
				result += `<span style="${buildStyle(state)}">${escapeHtml(textBefore)}</span>`;
			} else {
				result += escapeHtml(textBefore);
			}
		}
		lastIndex = ANSI_REGEX.lastIndex;

		// Parse SGR params
		const params = match[1] ? match[1].split(';').map(Number) : [0];

		for (let i = 0; i < params.length; i++) {
			const code = params[i];

			if (code === 0) {
				Object.assign(state, resetState());
			} else if (code === 1) {
				state.bold = true;
			} else if (code === 2) {
				state.dim = true;
			} else if (code === 3) {
				state.italic = true;
			} else if (code === 4) {
				state.underline = true;
			} else if (code === 22) {
				state.bold = false;
				state.dim = false;
			} else if (code === 23) {
				state.italic = false;
			} else if (code === 24) {
				state.underline = false;
			} else if (code === 39) {
				state.fg = null;
			} else if (code === 49) {
				state.bg = null;
			} else if (FG_COLORS[code]) {
				state.fg = FG_COLORS[code];
			} else if (BG_COLORS[code]) {
				state.bg = BG_COLORS[code];
			} else if (code === 38 || code === 48) {
				// 256-color (38;5;N) or truecolor (38;2;R;G;B) — skip params
				if (params[i + 1] === 5) {
					i += 2; // skip ;5;N
				} else if (params[i + 1] === 2) {
					i += 4; // skip ;2;R;G;B
				}
			}
		}
	}

	// Flush remaining text
	const remaining = input.slice(lastIndex);
	if (remaining) {
		if (hasStyle(state)) {
			result += `<span style="${buildStyle(state)}">${escapeHtml(remaining)}</span>`;
		} else {
			result += escapeHtml(remaining);
		}
	}

	return result;
}
