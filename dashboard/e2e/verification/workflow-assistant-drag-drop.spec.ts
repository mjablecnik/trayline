// Verifies Workflow 6 from .agents/tmp/VERIFICATION_TASKS.md (steps 18-20):
// In the main/assistant agent chat, a user can attach an image by dragging it
// onto the message log area (drag-and-drop), send it with a text prompt, and
// the real backing agent (claude/sonnet, running against the live
// trayline-server) replies with a coherent description of the image's
// subject — proving the drag-and-drop attachment path (not just the 📎
// file-picker path, see workflow-assistant-attach-icon.spec.ts) works for the
// assistant agent as well as the project agent (see
// workflow-project-agent-drag-drop.spec.ts for that counterpart).
import { test, expect } from '@playwright/test';
import fs from 'node:fs';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';
const PHOTO_IMAGE_PATH = '/workspace/.agents/tmp/photo-test.jpg';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

// Builds a browser-side DataTransfer carrying the given local file's bytes,
// since Playwright's dispatchEvent can't read the filesystem itself — the
// bytes must be shipped into the page as base64 and reassembled into a File.
async function dataTransferForFile(page, filePath: string, mimeType: string) {
	const base64 = fs.readFileSync(filePath).toString('base64');
	const fileName = filePath.split('/').pop()!;
	return page.evaluateHandle(
		({ base64, fileName, mimeType }) => {
			const binary = atob(base64);
			const bytes = new Uint8Array(binary.length);
			for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
			const file = new File([bytes], fileName, { type: mimeType });
			const dt = new DataTransfer();
			dt.items.add(file);
			return dt;
		},
		{ base64, fileName, mimeType }
	);
}

test('main/assistant agent: attach image via drag-and-drop and get a description back', async ({
	page
}) => {
	test.setTimeout(180_000);

	await login(page);

	// Step 18: start the assistant session, then drag-and-drop photo-test.jpg
	// onto the message log area (role="log", which has ondragover/ondrop).
	await page.goto('/assistant');
	await page.getByRole('button', { name: 'Start Agent' }).click();
	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();

	const dataTransfer = await dataTransferForFile(page, PHOTO_IMAGE_PATH, 'image/jpeg');
	await log.dispatchEvent('dragover', { dataTransfer });
	await log.dispatchEvent('drop', { dataTransfer });

	await expect(page.getByText('photo-test.jpg')).toBeVisible();
	const removeButton = page.getByRole('button', { name: 'Remove attachment' });
	await expect(removeButton).toBeVisible();

	// Step 19: type a prompt and send; expect the assistant page's upload
	// system bubble ("File uploaded: <filename>", see workflow-assistant
	// -attach-icon.spec.ts for the wording difference vs the project agent's
	// "📁 <filename> uploaded"), then the user's message bubble.
	await textarea.fill('Describe what is in this image.');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText('File uploaded: photo-test.jpg')).toBeVisible({ timeout: 30_000 });
	await expect(log.getByText('Describe what is in this image.')).toBeVisible();
	await expect(removeButton).not.toBeVisible();

	// Step 20: wait for the agent's reply and assert it describes the known
	// subject of photo-test.jpg — a fox/jackal-like canine on dirt ground
	// (see VERIFICATION_SETUP.md) — confirming the main agent recognizes and
	// describes images attached via drag-and-drop.
	const lastAgentBubble = log.locator('.prose').last();
	await expect(lastAgentBubble).toBeVisible({ timeout: 120_000 });
	await expect
		.poll(async () => (await lastAgentBubble.textContent())?.length ?? 0, {
			timeout: 120_000,
			message: 'waiting for agent reply to finish streaming'
		})
		.toBeGreaterThan(20);

	const replyText = (await lastAgentBubble.textContent()) ?? '';
	expect(
		/fox|jackal|canine|dog|coyote|wolf/i.test(replyText),
		`expected reply to describe a fox/jackal-like canine, got: ${replyText}`
	).toBe(true);
});
