import fc from 'fast-check';
import { get } from 'svelte/store';
import { describe, expect, it } from 'vitest';
import { agentStore } from '$lib/stores/agent';
import { canSubmitMessage } from './chat';

// Feature: project-ai-agent, Property 7: Valid message submission updates history and clears input
describe('canSubmitMessage — valid submission', () => {
	it('accepts any string containing a non-whitespace character, and submitting it appends to history', () => {
		fc.assert(
			fc.property(
				fc.string({ minLength: 1 }).filter((s) => s.trim().length > 0),
				(text) => {
					expect(canSubmitMessage(text)).toBe(true);

					agentStore.reset();
					agentStore.setConnected(crypto.randomUUID());
					agentStore.addUserMessage(text);

					const messages = get(agentStore).messages;
					expect(messages).toHaveLength(1);
					expect(messages[0]).toMatchObject({ role: 'user', content: text, complete: true });
				}
			)
		);
	});
});

// Feature: project-ai-agent, Property 8: Whitespace-only messages are rejected
describe('canSubmitMessage — whitespace-only rejection', () => {
	it('rejects the empty string and any string composed entirely of whitespace characters', () => {
		fc.assert(
			fc.property(
				fc
					.array(fc.constantFrom(' ', '\t', '\n', '\r'), { maxLength: 20 })
					.map((chars) => chars.join('')),
				(text) => {
					expect(canSubmitMessage(text)).toBe(false);
				}
			)
		);
	});
});
