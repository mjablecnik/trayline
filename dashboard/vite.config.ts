/// <reference types="vitest/config" />
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	envPrefix: ['VITE_', 'PUBLIC_'],
	plugins: [tailwindcss(), sveltekit()],
	test: {
		environment: 'jsdom',
		include: ['src/**/*.test.ts']
	}
});
