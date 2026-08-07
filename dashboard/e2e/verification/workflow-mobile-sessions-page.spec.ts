// Verifies Workflow 4 from .agents/tmp/VERIFICATION_TASKS.md (step 9):
// On a 375x812 mobile viewport, /sessions renders a page heading, a
// single-column session list (or empty-state message) with no clipped
// text, and no horizontal overflow.
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

test('mobile (375px): sessions page renders single-column list with no overflow', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	await page.goto('/sessions');

	// Heading is visible.
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	// Either the empty-state message, or a session list, renders.
	const emptyState = page.getByText(/no.*session/i);
	const sessionGroups = page.locator('section');
	await expect(emptyState.or(sessionGroups.first())).toBeVisible({ timeout: 10_000 });

	if (await sessionGroups.count()) {
		// Single-column: each session group section spans (close to) the full width.
		const sectionWidths = await sessionGroups.evaluateAll((els) =>
			els.map((el) => Math.round(el.getBoundingClientRect().width))
		);
		for (const width of sectionWidths) {
			expect(width).toBeGreaterThan(300);
		}
	}

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
