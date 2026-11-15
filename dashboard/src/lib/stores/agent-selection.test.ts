import { describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { agentStore } from './agent';
import { assistantStore } from './assistant';

for (const [name, store] of [
	['project', agentStore],
	['assistant', assistantStore]
] as const) {
	describe(`${name} agent selection`, () => {
		it('clears incompatible models when switching to Codex and preserves edits within Codex', () => {
			store.reset();
			store.setAgent('codex');
			expect(get<{ model: string }>(store).model).toBe('');
			store.setModel('custom-codex-model');
			store.setAgent('codex');
			expect(get<{ model: string }>(store).model).toBe('custom-codex-model');
			store.setAgent('claude');
			expect(get<{ model: string }>(store).model).toBe('sonnet');
			store.reset();
		});
	});
}
