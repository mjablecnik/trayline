// Verifies Workflow 1 from .agents/tmp/VERIFICATION_TASKS.md:
// 1. The token entry screen is shown on first visit to the dashboard.
// 2. Submitting a valid API token authenticates and reveals the projects grid,
//    including a card for the real "trayline" project served by the live
//    trayline-server backend (no mocking).
import { test, expect } from '@playwright/test';

const API_TOKEN = '66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a';

test('login with API token reveals the projects grid', async ({ page }) => {
	await page.goto('/');

	// Step 1: token entry screen visible
	await expect(page.getByRole('heading', { name: 'Trayline Dashboard' })).toBeVisible();
	const tokenInput = page.getByPlaceholder('Enter API token...');
	await expect(tokenInput).toBeVisible();
	const connectButton = page.getByRole('button', { name: 'Connect' });
	await expect(connectButton).toBeVisible();

	// Step 2: submit token, projects grid loads with the "trayline" project
	await tokenInput.fill(API_TOKEN);
	await connectButton.click();

	await expect(page.getByRole('heading', { name: 'Trayline Dashboard' })).not.toBeVisible();
	await expect(page.getByRole('button', { name: /trayline/i })).toBeVisible({ timeout: 15_000 });
});
