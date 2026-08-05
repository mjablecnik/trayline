import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';
const IMAGE_PATH = process.env.ADHOC_IMAGE_PATH || '/workspace/.agents/tmp/data/samsung-photo1.jpg';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('adhoc: attach a real user-provided image and get a description back', async ({ page }) => {
	test.setTimeout(180_000);

	await login(page);

	await page.goto('/assistant');
	const startButton = page.getByRole('button', { name: 'Start Agent' });
	await expect(startButton).toBeVisible();
	await startButton.click();

	const log = page.getByRole('log');
	await expect(log).toBeVisible({ timeout: 30_000 });
	const textarea = page.getByPlaceholder('Message the agent...');
	await expect(textarea).toBeVisible();

	const fileName = IMAGE_PATH.split('/').pop();
	await page.locator('input[type=file]').setInputFiles(IMAGE_PATH);
	await expect(page.getByText(fileName)).toBeVisible();

	await textarea.fill('Describe what is in this image in detail.');
	await page.getByRole('button', { name: 'Send' }).click();

	await expect(page.getByText(`File uploaded: ${fileName}`)).toBeVisible({ timeout: 30_000 });

	const lastAgentBubble = log.locator('.prose').last();
	await expect(lastAgentBubble).toBeVisible({ timeout: 150_000 });
	await expect
		.poll(async () => (await lastAgentBubble.textContent())?.length ?? 0, {
			timeout: 150_000,
			message: 'waiting for agent reply to finish streaming'
		})
		.toBeGreaterThan(10);

	const replyText = (await lastAgentBubble.textContent()) ?? '';
	console.log('AGENT REPLY:', replyText);
	expect(replyText.length).toBeGreaterThan(10);
});
