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

/** Extracts the first dropped file from a drag-and-drop event, if any. */
export function extractDroppedFile(event: DragEvent): File | null {
	return event.dataTransfer?.files?.[0] ?? null;
}

/**
 * Extracts an image pasted from the clipboard (e.g. a screenshot copied with
 * Win+Shift+S) as a File, so it can be sent the same way as a drag-and-drop
 * upload. Also handles iOS Safari which may expose pasted images via
 * clipboardData.files rather than clipboardData.items.
 * Returns null when the clipboard has no image content.
 */
export function extractPastedImageFile(event: ClipboardEvent): File | null {
	const items = event.clipboardData?.items;
	if (items) {
		for (const item of items) {
			if (item.kind !== 'file' || !item.type.startsWith('image/')) continue;
			const file = item.getAsFile();
			if (!file) continue;
			const ext = item.type.split('/')[1]?.split('+')[0] || 'png';
			return new File([file], `clipboard-${crypto.randomUUID()}.${ext}`, { type: item.type });
		}
	}
	// iOS Safari fallback — images may appear in clipboardData.files instead of items
	const files = event.clipboardData?.files;
	if (files && files.length > 0) {
		const file = files[0];
		if (file.type.startsWith('image/')) {
			const ext = file.type.split('/')[1]?.split('+')[0] || 'png';
			return new File([file], `clipboard-${crypto.randomUUID()}.${ext}`, { type: file.type });
		}
	}
	return null;
}
