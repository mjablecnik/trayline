// Verifies Workflow 5 from .agents/tmp/VERIFICATION_TASKS.md (step 10):
// On a 375x812 mobile viewport, /assistant shows the Chat/Files tab bar
// (Chat active, both tappable) and the agent selector (agent/model
// dropdowns + Start button) fits within the 375px viewport with no
// horizontal overflow.
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

test('mobile (375px): assistant page tab bar and agent selector fit the viewport', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	await page.goto('/assistant');

	// Tab bar: Chat active, Files present, both tappable within 375px.
	const chatTab = page.getByRole('button', { name: 'Chat' });
	const filesTab = page.getByRole('button', { name: 'Files' });
	await expect(chatTab).toBeVisible();
	await expect(filesTab).toBeVisible();
	await expect(chatTab).toHaveClass(/border-sky-500/);

	const chatTabBox = await chatTab.boundingBox();
	const filesTabBox = await filesTab.boundingBox();
	expect(chatTabBox?.x).toBeGreaterThanOrEqual(0);
	expect((filesTabBox?.x ?? 0) + (filesTabBox?.width ?? 0)).toBeLessThanOrEqual(375);

	// Agent selector: agent/model dropdowns + Start button fit within 375px.
	const agentSelect = page.locator('#assistant-agent-select');
	const modelInput = page.locator('#assistant-model-input');
	const startButton = page.getByRole('button', { name: 'Start Agent' });
	await expect(agentSelect).toBeVisible();
	await expect(modelInput).toBeVisible();
	await expect(startButton).toBeVisible();

	for (const locator of [agentSelect, modelInput, startButton]) {
		const box = await locator.boundingBox();
		expect(box?.x).toBeGreaterThanOrEqual(0);
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(375);
	}

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
