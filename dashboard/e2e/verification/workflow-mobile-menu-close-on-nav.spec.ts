// Verifies Workflow 3 from .agents/tmp/VERIFICATION_TASKS.md (steps 7-8):
// On a 375x812 mobile viewport, opening the mobile menu and tapping a nav
// link must both navigate AND close the menu panel automatically. Covers
// Projects -> Sessions (step 7) and Sessions -> Assistant (step 8) to
// corroborate the behaviour across two navigations.
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

async function login(page) {
	await page.goto('/');
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await tokenInput.fill(API_TOKEN);
	await page.getByRole('button', { name: 'Connect' }).click();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
}

test('mobile (375px): mobile menu closes automatically after tapping a nav link', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	const header = page.locator('header');
	const menuButton = header.getByRole('button', { name: 'Menu', exact: true });

	// Step 7: open menu on "/", tap Sessions -> navigates to /sessions AND menu closes.
	await menuButton.click();
	await expect(menuButton).toHaveAttribute('aria-expanded', 'true');
	await header.getByRole('link', { name: 'Sessions', exact: true }).click();

	await expect(page).toHaveURL(/\/sessions$/);
	await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
	await expect(header.getByRole('link', { name: 'Assistant', exact: true })).not.toBeVisible();

	// Step 8: from /sessions, open menu, tap Assistant -> navigates to /assistant AND menu closes.
	await menuButton.click();
	await expect(menuButton).toHaveAttribute('aria-expanded', 'true');
	await header.getByRole('link', { name: 'Assistant', exact: true }).click();

	await expect(page).toHaveURL(/\/assistant$/);
	await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
	await expect(header.getByRole('link', { name: 'Projects', exact: true })).not.toBeVisible();
});
