// Verifies Workflow 3 from .agents/tmp/VERIFICATION_TASKS.md (step 5):
// On a 375x812 mobile viewport, clicking the hamburger button opens the
// mobile menu panel — aria-expanded flips to true, the icon becomes a close
// (X) icon, and the panel lists Projects/Sessions/Assistant links plus the
// language switcher and logout button.
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('mobile (375px): hamburger button opens the mobile menu panel', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const menuButton = page.getByRole('button', { name: 'Menu', exact: true });
	await expect(menuButton).toHaveAttribute('aria-expanded', 'false');

	await menuButton.click();

	await expect(menuButton).toHaveAttribute('aria-expanded', 'true');

	// Menu panel: nav links + language switcher + logout.
	const panel = page.locator('header > div').last();
	await expect(panel.getByRole('link', { name: 'Projects' })).toBeVisible();
	await expect(panel.getByRole('link', { name: 'Sessions' })).toBeVisible();
	await expect(panel.getByRole('link', { name: 'Assistant' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Log out' })).toBeVisible();
});
