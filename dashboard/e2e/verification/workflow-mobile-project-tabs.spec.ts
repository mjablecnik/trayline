// Verifies Workflow 6 from .agents/tmp/VERIFICATION_TASKS.md (steps 12-15):
// On a 375x812 mobile viewport, the per-project pages under /trayline/ must
// render without horizontal overflow: the project header + branch selector
// wrap onto their own line(s), the TabBar (Files/Commits/Changes/Env/
// Workflows/Agent) scrolls horizontally itself rather than the page
// overflowing, and each tab's content (Files, Commits, Changes, Env) stacks
// in a single readable column.
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

test('mobile (375px): project tabs (Files/Commits/Changes/Env) render without overflow', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const tabBar = page.getByRole('navigation', { name: 'Tabs' });

	// Step 12: Files tab (default /trayline/tree/ view).
	await page.goto('/trayline/tree/');
	await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).first()).toBeVisible();

	const filesTab = tabBar.getByRole('link', { name: 'Files', exact: true });
	await expect(filesTab).toBeVisible();
	await expect(filesTab).toHaveClass(/border-sky-500/);

	// TabBar itself scrolls horizontally rather than the page overflowing.
	const tabBarBox = await tabBar.boundingBox();
	expect(tabBarBox?.x).toBeGreaterThanOrEqual(0);
	const tabBarScrollWidth = await tabBar.evaluate((el) => el.scrollWidth);
	const tabBarClientWidth = await tabBar.evaluate((el) => el.clientWidth);
	expect(tabBarScrollWidth).toBeGreaterThan(tabBarClientWidth); // confirms it overflows internally...
	expect(await hasNoHorizontalOverflow(page)).toBe(true); // ...but the page itself does not.

	// Step 13: Commits tab.
	await tabBar.getByRole('link', { name: 'Commits', exact: true }).click();
	await expect(page).toHaveURL(/\/trayline\/commits/);
	const commitsTab = tabBar.getByRole('link', { name: 'Commits', exact: true });
	await expect(commitsTab).toHaveClass(/border-sky-500/);
	await page.waitForLoadState('networkidle');
	expect(await hasNoHorizontalOverflow(page)).toBe(true);

	// Step 14: Changes tab.
	await tabBar.getByRole('link', { name: 'Changes', exact: true }).click();
	await expect(page).toHaveURL(/\/trayline\/changes/);
	const changesTab = tabBar.getByRole('link', { name: 'Changes', exact: true });
	await expect(changesTab).toHaveClass(/border-sky-500/);
	await page.waitForLoadState('networkidle');
	expect(await hasNoHorizontalOverflow(page)).toBe(true);

	// Step 15: Env tab.
	await tabBar.getByRole('link', { name: 'Environment', exact: true }).click();
	await expect(page).toHaveURL(/\/trayline\/env/);
	const envTab = tabBar.getByRole('link', { name: 'Environment', exact: true });
	await expect(envTab).toHaveClass(/border-sky-500/);
	await page.waitForLoadState('networkidle');
	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
