// Verifies Workflow 2 from .agents/tmp/VERIFICATION_TASKS.md (steps 3-4):
// On a 375x812 mobile viewport, the projects grid renders as a single column
// (cards stack, full-width, "trayline" card visible, no clipped text/badges,
// no horizontal overflow) and the sticky header remains visible/aligned at
// the top of the viewport after scrolling.
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

test('mobile (375px): projects grid is single-column and header stays sticky on scroll', async ({
	page
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await login(page);

	// Step 3: single-column grid, "trayline" card full-width, no overflow.
	const traylineCard = page.getByRole('button', { name: /trayline/i });
	await expect(traylineCard).toBeVisible();

	// Compare x-positions of all project cards: in a single column they all share
	// the same left edge (no side-by-side cards at differing x offsets).
	const cardLefts = await page
		.locator('button')
		.filter({ has: page.locator('h2') })
		.evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().left)));
	expect(new Set(cardLefts).size).toBeLessThanOrEqual(1);

	const traylineBox = await traylineCard.boundingBox();
	expect(traylineBox?.width).toBeGreaterThan(300); // near full 375px width minus padding

	await expect(page.getByText('trayline', { exact: true })).toBeVisible();
	expect(await hasNoHorizontalOverflow(page)).toBe(true);

	// Step 4: sticky header remains visible/aligned after scrolling the page.
	const header = page.locator('header');
	await expect(header).toBeVisible();
	const headerBoxBefore = await header.boundingBox();

	await page.evaluate(() => window.scrollBy(0, 400));
	await page.waitForTimeout(200);

	await expect(page.getByRole('link', { name: 'Trayline' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Menu', exact: true })).toBeVisible();
	const headerBoxAfter = await header.boundingBox();
	expect(headerBoxAfter?.y).toBe(headerBoxBefore?.y); // still pinned at the same y (top)
	expect(headerBoxAfter?.y).toBeLessThanOrEqual(0.5);
	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
