// Verifies Workflow 7 from .agents/tmp/VERIFICATION_TASKS.md (steps 18-20):
// The `tablet` (768px) breakpoint switches the header from the mobile
// hamburger to the inline nav, and the projects grid goes from 1 column
// (mobile) to 2 columns (tablet, >=768px) to 3 columns (desktop, >=1280px),
// staying centered within max-w-6xl with no horizontal overflow.
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

// Reads the number of CSS grid columns actually being rendered for the
// projects grid, via computed `grid-template-columns`.
async function columnCount(page) {
	return page.evaluate(() => {
		const grid = document.querySelector('[class*="grid-cols"]');
		if (!grid) return 0;
		const style = getComputedStyle(grid);
		return style.gridTemplateColumns.split(' ').filter(Boolean).length;
	});
}

test('tablet (768px): inline nav visible, hamburger hidden', async ({ page }) => {
	await page.setViewportSize({ width: 768, height: 1024 });
	await login(page);

	const header = page.locator('header');
	await expect(header.getByRole('link', { name: 'Projects', exact: true })).toBeVisible();
	await expect(header.getByRole('link', { name: 'Sessions', exact: true })).toBeVisible();
	await expect(header.getByRole('link', { name: 'Assistant', exact: true })).toBeVisible();
	await expect(header.getByRole('button', { name: 'Menu', exact: true })).toBeHidden();

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});

test('tablet (768px): projects grid shows two columns, no overflow', async ({ page }) => {
	await page.setViewportSize({ width: 768, height: 1024 });
	await login(page);
	await page.goto('/');
	await expect(page.getByRole('button', { name: /trayline/i }).first()).toBeVisible({
		timeout: 15_000
	});

	expect(await columnCount(page)).toBe(2);
	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});

test('desktop (1280px): inline nav, hamburger hidden, three-column grid, centered container', async ({
	page
}) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await login(page);
	await page.goto('/');
	await expect(page.getByRole('button', { name: /trayline/i }).first()).toBeVisible({
		timeout: 15_000
	});

	const header = page.locator('header');
	await expect(header.getByRole('link', { name: 'Projects', exact: true })).toBeVisible();
	await expect(header.getByRole('button', { name: 'Menu', exact: true })).toBeHidden();

	expect(await columnCount(page)).toBe(3);

	// Content is centered within max-w-6xl (1152px) inside the 1280px viewport,
	// so the container's left/right margins should be roughly equal.
	const container = page.locator('main > div').first();
	const box = await container.boundingBox();
	const leftMargin = box?.x ?? 0;
	const rightMargin = 1280 - ((box?.x ?? 0) + (box?.width ?? 0));
	expect(Math.abs(leftMargin - rightMargin)).toBeLessThanOrEqual(2);

	expect(await hasNoHorizontalOverflow(page)).toBe(true);
});
