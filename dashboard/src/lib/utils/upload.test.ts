import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { encodeUploadFrame, extractDroppedFile, extractPastedImageFile } from './upload';

function decodeFrame(frame: Uint8Array): { filename: string; data: Uint8Array } {
	const nameLen = new DataView(frame.buffer, frame.byteOffset).getUint32(0);
	const filename = new TextDecoder().decode(frame.slice(4, 4 + nameLen));
	const data = frame.slice(4 + nameLen);
	return { filename, data };
}

describe('encodeUploadFrame', () => {
	it('round-trips filename and content through the binary frame format', () => {
		fc.assert(
			fc.property(
				fc.string({ minLength: 1, maxLength: 50 }).filter((s) => !s.includes('\0')),
				fc.uint8Array({ maxLength: 200 }),
				(filename, content) => {
					const frame = encodeUploadFrame(filename, content);
					const decoded = decodeFrame(frame);
					expect(decoded.filename).toBe(filename);
					expect(Array.from(decoded.data)).toEqual(Array.from(content));
				}
			)
		);
	});
});

describe('extractDroppedFile', () => {
	it('returns the first file from the drop event', () => {
		const file = new File(['content'], 'notes.txt');
		const event = { dataTransfer: { files: [file] } } as unknown as DragEvent;
		expect(extractDroppedFile(event)).toBe(file);
	});

	it('returns null when no file was dropped', () => {
		const event = { dataTransfer: { files: [] } } as unknown as DragEvent;
		expect(extractDroppedFile(event)).toBeNull();
	});

	it('returns null when dataTransfer is missing', () => {
		const event = {} as DragEvent;
		expect(extractDroppedFile(event)).toBeNull();
	});
});

describe('extractPastedImageFile', () => {
	function clipboardEventWith(items: { kind: string; type: string; file: File | null }[]) {
		return {
			clipboardData: {
				items: items.map((i) => ({
					kind: i.kind,
					type: i.type,
					getAsFile: () => i.file
				}))
			}
		} as unknown as ClipboardEvent;
	}

	it('extracts an image item and names it with a png-style extension', () => {
		const image = new File(['bytes'], 'ignored.png', { type: 'image/png' });
		const event = clipboardEventWith([{ kind: 'file', type: 'image/png', file: image }]);
		const result = extractPastedImageFile(event);
		expect(result).not.toBeNull();
		expect(result?.name).toMatch(/^clipboard-.+\.png$/);
		expect(result?.type).toBe('image/png');
	});

	it('ignores non-image clipboard items', () => {
		const event = clipboardEventWith([{ kind: 'string', type: 'text/plain', file: null }]);
		expect(extractPastedImageFile(event)).toBeNull();
	});

	it('returns null when clipboardData is missing', () => {
		expect(extractPastedImageFile({} as ClipboardEvent)).toBeNull();
	});
});
