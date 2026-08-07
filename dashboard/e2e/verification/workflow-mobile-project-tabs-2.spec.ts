// Verifies Workflow 6 from .agents/tmp/VERIFICATION_TASKS.md (steps 16-17):
// On a 375x812 mobile viewport, the remaining per-project tabs under
// /trayline/ must render without horizontal overflow:
//  - Workflows tab: list/empty-state renders single-column, action buttons
//    tappable within the viewport.
//  - Agent tab: agent selector fits the viewport; after starting a session,
//    the chat view (message log + textarea + Send) fits with no overflow.
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

test('mobile (375px): Workflows tab renders without overflow', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const tabBar = page.getByRole('navigation', { name: 'Tabs' });

	await page.goto('/trayline/workflows');
	await expect(page).toHaveURL(/\/trayline\/workflows/);
	const workflowsTab = tabBar.getByRole('link', { name: 'Workflows', exact: true });
	await expect(workflowsTab).toBeVisible();
	await expect(workflowsTab).toHaveClass(/border-sky-500/);

	// Either an empty-state message or a workflow list renders; the "+ New" action
	// button must be visible and fit within the viewport either way.
	const newButton = page.getByRole('button', { name: /new/i });
	await expect(newButton).toBeVisible({ timeout: 15_000 });
	const newButtonBox = await newButton.boundingBox();
	expect(newButtonBox?.x).toBeGreaterThanOrEqual(0);
	expect((newButtonBox?.x ?? 0) + (newButtonBox?.width ?? 0)).toBeLessThanOrEqual(375);

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});

test('mobile (375px): Agent tab renders without overflow, before and after starting a session', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const tabBar = page.getByRole('navigation', { name: 'Tabs' });

	await page.goto('/trayline/agent');
	await expect(page).toHaveURL(/\/trayline\/agent/);
	const agentTab = tabBar.getByRole('link', { name: 'Agent', exact: true });
	await expect(agentTab).toBeVisible();
	await expect(agentTab).toHaveClass(/border-sky-500/);

	// Before starting: session list is collapsed behind a <details> on mobile,
	// and the agent selector (Start button) must fit within the viewport.
	const startButton = page.getByRole('button', { name: /start/i }).first();
	await expect(startButton).toBeVisible({ timeout: 15_000 });
	expect(await hasNoHorizontalOverflow(page)).toBe(true);

	await startButton.click();

	const messageLog = page.getByRole('log');
	await expect(messageLog).toBeVisible({ timeout: 15_000 });

	const textarea = page.getByPlaceholder('Message the agent...');
	const sendButton = page.getByRole('button', { name: 'Send' });

	await expect(textarea).toBeVisible();
	await expect(sendButton).toBeVisible();

	for (const locator of [messageLog, textarea, sendButton]) {
		const box = await locator.boundingBox();
		expect(box?.x).toBeGreaterThanOrEqual(0);
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(375);
	}

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
