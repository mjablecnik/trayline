// Verifies Workflow 5 from .agents/tmp/VERIFICATION_TASKS.md (step 11):
// On a 375x812 mobile viewport, starting an assistant session with the
// default agent/model must produce a chat view that fits the viewport:
// message log, full-width textarea (placeholder "Message the agent..."),
// the attach (📎) button, and the Send button all visible with no
// horizontal overflow, and the input row itself does not overflow 375px.
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

async function hasNoHorizontalOverflow(page) {
	return page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1);
}

test('mobile (375px): started assistant chat view fits the viewport', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	await page.goto('/assistant');

	// Default agent is already "claude" — Start Agent is enabled immediately.
	const startButton = page.getByRole('button', { name: 'Start Agent' });
	await expect(startButton).toBeEnabled();
	await startButton.click();

	const messageLog = page.getByRole('log');
	await expect(messageLog).toBeVisible({ timeout: 15_000 });

	const textarea = page.getByPlaceholder('Message the agent...');
	const attachButton = page.getByRole('button', { name: 'Attach file' });
	const sendButton = page.getByRole('button', { name: 'Send' });

	await expect(textarea).toBeVisible();
	await expect(attachButton).toBeVisible();
	await expect(sendButton).toBeVisible();

	for (const locator of [messageLog, textarea, attachButton, sendButton]) {
		const box = await locator.boundingBox();
		expect(box?.x).toBeGreaterThanOrEqual(0);
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(375);
	}

	// The input row (attach + textarea + send) as a whole must fit within 375px.
	const inputRowBox = await attachButton.boundingBox();
	const sendBox = await sendButton.boundingBox();
	expect((sendBox?.x ?? 0) + (sendBox?.width ?? 0)).toBeLessThanOrEqual(375);
	expect(inputRowBox?.x).toBeGreaterThanOrEqual(0);

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
