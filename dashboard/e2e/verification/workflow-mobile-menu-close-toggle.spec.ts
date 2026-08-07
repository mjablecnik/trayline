// Verifies Workflow 3 from .agents/tmp/VERIFICATION_TASKS.md (step 6):
// On a 375x812 mobile viewport, with the mobile menu open, clicking the
// hamburger button again closes it — aria-expanded flips back to false and
// the panel is removed from the DOM.
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('mobile (375px): hamburger button toggles the mobile menu panel closed', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const header = page.locator('header');
	const menuButton = header.getByRole('button', { name: 'Menu', exact: true });
	await menuButton.click();
	await expect(menuButton).toHaveAttribute('aria-expanded', 'true');
	await expect(header.getByRole('link', { name: 'Projects', exact: true })).toBeVisible();

	await menuButton.click();

	await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
	await expect(header.getByRole('link', { name: 'Sessions', exact: true })).not.toBeVisible();
});
