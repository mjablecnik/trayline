/**
 * Encodes a filename and file content into the WebSocket binary frame format
 * expected by the server: [4 bytes: filename length (big-endian)][filename][content].
 */
export function encodeUploadFrame(filename: string, data: Uint8Array): Uint8Array {
	const nameBytes = new TextEncoder().encode(filename);
	const frame = new Uint8Array(4 + nameBytes.length + data.length);
	new DataView(frame.buffer).setUint32(0, nameBytes.length);
	frame.set(nameBytes, 4);
	frame.set(data, 4 + nameBytes.length);
	return frame;
}
