// Verifies Workflow 2 from .agents/tmp/VERIFICATION_TASKS.md (steps 3-7):
// In the project-agent chat, a user can attach an image via the 📎 attach
// icon (file picker), send it with a text prompt, and the real backing
// agent (claude/sonnet, running against the live trayline-server) replies
// with a description that recognizes the image's known subject
// (a fox/jackal-like canine, per .agents/tmp/VERIFICATION_SETUP.md).
import { test, expect } from '@playwright/test';
import path from 'node:path';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';
const PHOTO_PATH = '/workspace/.agents/tmp/photo-test.jpg';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('project agent: attach image via 📎 icon and get a description', async ({ page }) => {
	test.setTimeout(180_000);

	await login(page);

	// Step 3: navigate to the project agent chat; agent selector shown, claude/sonnet preselected.
	await page.goto('/trayline/agent');
	const startButton = page.getByRole('button', { name: 'Start Agent' });
	await expect(startButton).toBeVisible();
	await expect(page.locator('#agent-select')).toHaveValue('claude');
	await expect(page.locator('#agent-model')).toHaveValue('sonnet');

	// Step 4: start the session; chat view replaces the selector.
	await startButton.click();
	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();
	await expect(page.getByRole('button', { name: 'Attach file' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Send' })).toBeVisible();

	// Step 5: attach photo-test.jpg via the hidden file input behind the 📎 button.
	await page.locator('input[type=file]').setInputFiles(PHOTO_PATH);
	await expect(page.getByText('photo-test.jpg')).toBeVisible();
	const removeButton = page.getByRole('button', { name: 'Remove attachment' });
	await expect(removeButton).toBeVisible();

	// Step 6: type a prompt and send; expect upload system bubble then user bubble.
	await textarea.fill('Describe this image in one sentence.');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText('📁 photo-test.jpg uploaded')).toBeVisible({ timeout: 30_000 });
	await expect(log.getByText('Describe this image in one sentence.')).toBeVisible();
	await expect(removeButton).not.toBeVisible();

	// Step 7: wait for the agent's reply and assert it names the known subject.
	const lastAgentBubble = log.locator('.prose').last();
	await expect(lastAgentBubble).toBeVisible({ timeout: 120_000 });
	await expect
		.poll(async () => (await lastAgentBubble.textContent())?.length ?? 0, {
			timeout: 120_000,
			message: 'waiting for agent reply to finish streaming'
		})
		.toBeGreaterThan(20);

	const replyText = ((await lastAgentBubble.textContent()) ?? '').toLowerCase();
	expect(
		/fox|jackal|dog|canine|animal/.test(replyText),
		`expected agent reply to mention the photo's subject, got: ${replyText}`
	).toBe(true);
});
