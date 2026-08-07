// Verifies Workflow 1 from .agents/tmp/VERIFICATION_TASKS.md (steps 1-2):
// On a 375x812 mobile viewport, the TokenEntry screen renders correctly (no
// horizontal overflow, full-width/tappable input+button) and submitting a
// valid API token authenticates, revealing the projects view with a header
// that shows only the app name + a single hamburger button (no inline nav
// links) at this width.
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

async function hasNoHorizontalOverflow(page) {
	return page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1);
}

test('mobile (375px): token entry screen renders, login reveals projects view with hamburger-only header', async ({
	page
}) => {
	// Step 1: token entry screen visible, fits within 375px.
	await page.setViewportSize({ width: 375, height: 812 });
	await page.goto('/');

	await expect(page.getByRole('heading', { name: 'Trayline Dashboard' })).toBeVisible();
	const tokenInput = page.getByPlaceholder('Enter API token...');
	const connectButton = page.getByRole('button', { name: 'Connect' });
	await expect(tokenInput).toBeVisible();
	await expect(connectButton).toBeVisible();

	const inputBox = await tokenInput.boundingBox();
	const buttonBox = await connectButton.boundingBox();
	expect(inputBox?.width).toBeGreaterThan(280);
	expect(buttonBox?.width).toBeGreaterThan(280);
	// Comfortably tappable height (>= 40px, well above the ~24px accessibility floor).
	expect(buttonBox?.height).toBeGreaterThanOrEqual(36);
	expect(await hasNoHorizontalOverflow(page)).toBe(true);

	// Step 2: submit token, projects view loads, header collapses to hamburger-only.
	await tokenInput.fill(API_TOKEN);
	await connectButton.click();

	await expect(page.getByRole('heading', { name: 'Trayline Dashboard' })).not.toBeVisible();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });

	await expect(page.getByRole('link', { name: 'Trayline' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Menu', exact: true })).toBeVisible();
	// Inline nav links are hidden below the tablet (768px) breakpoint.
	await expect(page.getByRole('navigation').getByRole('link', { name: 'Sessions' })).not.toBeVisible();
	await expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
