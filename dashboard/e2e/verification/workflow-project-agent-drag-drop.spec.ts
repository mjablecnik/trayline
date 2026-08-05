// Verifies Workflow 3 from .agents/tmp/VERIFICATION_TASKS.md (steps 8-10):
// In the project-agent chat, a user can attach an image by dragging it onto
// the message log area (drag-and-drop), send it with a text prompt, and the
// real backing agent (claude/sonnet, running against the live
// trayline-server) replies with text proving it OCR'd the image's contents
// (the ocr-test.png fixture reads "TRAYLINE OCR 7492").
import { test, expect } from '@playwright/test';
import fs from 'node:fs';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';
const OCR_IMAGE_PATH = '/workspace/.agents/tmp/ocr-test.png';

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

test('project agent: attach image via drag-and-drop and get OCR text back', async ({ page }) => {
	test.setTimeout(180_000);

	await login(page);

	// Step 8: start the project-agent session, then drag-and-drop ocr-test.png
	// onto the message log area (role="log", which has ondragover/ondrop).
	await page.goto('/trayline/agent');
	await page.getByRole('button', { name: 'Start Agent' }).click();
	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();

	const dataTransfer = await dataTransferForFile(page, OCR_IMAGE_PATH, 'image/png');
	await log.dispatchEvent('dragover', { dataTransfer });
	await log.dispatchEvent('drop', { dataTransfer });

	await expect(page.getByText('ocr-test.png')).toBeVisible();
	const removeButton = page.getByRole('button', { name: 'Remove attachment' });
	await expect(removeButton).toBeVisible();

	// Step 9: type a prompt and send; expect upload system bubble then user bubble.
	await textarea.fill('What text is written in this image?');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText('📁 ocr-test.png uploaded')).toBeVisible({ timeout: 30_000 });
	await expect(log.getByText('What text is written in this image?')).toBeVisible();
	await expect(removeButton).not.toBeVisible();

	// Step 10: wait for the agent's reply and assert it contains the image's
	// exact OCR'd text, proving the drag-and-drop upload path works end to end.
	const lastAgentBubble = log.locator('.prose').last();
	await expect(lastAgentBubble).toBeVisible({ timeout: 120_000 });
	await expect
		.poll(async () => (await lastAgentBubble.textContent())?.length ?? 0, {
			timeout: 120_000,
			message: 'waiting for agent reply to finish streaming'
		})
		.toBeGreaterThan(20);

	const replyText = (await lastAgentBubble.textContent()) ?? '';
	expect(replyText).toContain('7492');
	expect(/trayline\s*ocr/i.test(replyText)).toBe(true);
});
