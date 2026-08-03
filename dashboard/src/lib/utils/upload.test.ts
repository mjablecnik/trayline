import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { encodeUploadFrame } from './upload';

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
