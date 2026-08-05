// Verifies Workflow 5 from .agents/tmp/VERIFICATION_TASKS.md (steps 13-17):
// In the main/assistant agent chat, a user can attach an image via the 📎
// attach icon (file picker), send it with a text prompt, and the real
// backing agent (claude/sonnet, running against the live trayline-server)
// replies with text that recognizes the image's text content (OCR), proving
// the same file-attachment + vision capability works for the assistant
// agent, not just the project agent (see workflow-project-agent-attach-icon
// .spec.ts for that counterpart).
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';
const OCR_IMAGE_PATH = '/workspace/.agents/tmp/ocr-test.png';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('main/assistant agent: attach image via 📎 icon and get OCR text back', async ({ page }) => {
	test.setTimeout(180_000);

	await login(page);

	// Step 13: navigate to the assistant page; Chat/Files tabs, agent selector shown.
	await page.goto('/assistant');
	await expect(page.getByRole('button', { name: 'Chat' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Files' })).toBeVisible();
	await expect(page.locator('#assistant-agent-select')).toHaveValue('claude');
	await expect(page.locator('#assistant-model-input')).toHaveValue('sonnet');
	const startButton = page.getByRole('button', { name: 'Start Agent' });
	await expect(startButton).toBeVisible();

	// Step 14: start the session; chat view appears with log, textarea, attach + send buttons.
	await startButton.click();
	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();
	await expect(page.getByRole('button', { name: 'Attach file' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Send' })).toBeVisible();

	// Step 15: attach ocr-test.png via the hidden file input behind the 📎 button.
	await page.locator('input[type=file]').setInputFiles(OCR_IMAGE_PATH);
	await expect(page.getByText('ocr-test.png')).toBeVisible();
	const removeButton = page.getByRole('button', { name: 'Remove attachment' });
	await expect(removeButton).toBeVisible();

	// Step 16: type a prompt and send; expect an upload system bubble then the user bubble.
	// The assistant page's upload system message reads "File uploaded: <filename>"
	// (its own 'assistant.fileUploaded' i18n string), unlike the project agent's
	// "📁 <filename> uploaded" — a real, deliberate wording difference between the
	// two chat surfaces (see src/routes/assistant/+page.svelte vs ChatInterface.svelte).
	await textarea.fill('Read and quote the text shown in this image.');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText('File uploaded: ocr-test.png')).toBeVisible({ timeout: 30_000 });
	await expect(log.getByText('Read and quote the text shown in this image.')).toBeVisible();
	await expect(removeButton).not.toBeVisible();

	// Step 17: wait for the agent's reply and assert it quotes the image's exact text.
	const lastAgentBubble = log.locator('.prose').last();
	await expect(lastAgentBubble).toBeVisible({ timeout: 120_000 });
	await expect
		.poll(async () => (await lastAgentBubble.textContent())?.length ?? 0, {
			timeout: 120_000,
			message: 'waiting for agent reply to finish streaming'
		})
		.toBeGreaterThan(10);

	const replyText = (await lastAgentBubble.textContent()) ?? '';
	expect(replyText).toContain('7492');
	expect(/trayline\s*ocr/i.test(replyText), `expected reply to mention TRAYLINE OCR, got: ${replyText}`).toBe(
		true
	);
});
