// Verifies Workflow 4 from .agents/tmp/VERIFICATION_TASKS.md (steps 11-12):
// In the project-agent chat, a user can attach an image by copy-pasting it
// (Ctrl/Cmd+V of an image on the clipboard) into the message textarea, send
// it with a text prompt, and the real backing agent (claude/sonnet, running
// against the live trayline-server) replies with text proving it OCR'd the
// image's contents (the ocr-test.png fixture reads "TRAYLINE OCR 7492").
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

// Dispatches a real `paste` ClipboardEvent on the given element, carrying the
// local file's bytes as an image/* clipboard file item — mirrors what a
// browser produces for Ctrl/Cmd+V of an image copied to the OS clipboard.
async function pasteImageFile(locator, filePath: string, mimeType: string) {
	const base64 = fs.readFileSync(filePath).toString('base64');
	await locator.evaluate(
		(el, { base64, mimeType }) => {
			const binary = atob(base64);
			const bytes = new Uint8Array(binary.length);
			for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
			const file = new File([bytes], 'source-clipboard-image', { type: mimeType });
			const dt = new DataTransfer();
			dt.items.add(file);
			const event = new ClipboardEvent('paste', {
				clipboardData: dt,
				bubbles: true,
				cancelable: true
			});
			el.dispatchEvent(event);
		},
		{ base64, mimeType }
	);
}

test('project agent: attach image via copy-paste and get OCR text back', async ({ page }) => {
	test.setTimeout(180_000);

	await login(page);

	// Steps 3-4 (prerequisite): start the project-agent session.
	await page.goto('/trayline/agent');
	await page.getByRole('button', { name: 'Start Agent' }).click();
	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();

	// Step 11: focus the textarea and paste ocr-test.png from the clipboard.
	await textarea.click();
	await pasteImageFile(textarea, OCR_IMAGE_PATH, 'image/png');

	await expect(page.getByText(/^clipboard-\d+\.png$/)).toBeVisible();
	const removeButton = page.getByRole('button', { name: 'Remove attachment' });
	await expect(removeButton).toBeVisible();
	// No image markup should have been pasted as text into the textarea.
	await expect(textarea).toHaveValue('');

	// Step 12: type a prompt and send; expect upload system bubble then user bubble.
	await textarea.fill('Read the text in the pasted image.');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText(/^📁 clipboard-\d+\.png uploaded$/)).toBeVisible({
		timeout: 30_000
	});
	await expect(log.getByText('Read the text in the pasted image.')).toBeVisible();
	await expect(removeButton).not.toBeVisible();

	// Wait for the agent's reply and assert it contains the image's exact
	// OCR'd text, proving the copy-paste upload path works end to end.
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
