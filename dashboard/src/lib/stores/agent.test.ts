import fc from 'fast-check';
import { get } from 'svelte/store';
import { describe, expect, it } from 'vitest';
import { agentStore } from './agent';

// Feature: project-ai-agent, Property 9: Output chunks accumulate correctly
describe('agentStore output accumulation', () => {
	it('concatenates output chunks in the order received', () => {
		fc.assert(
			fc.property(fc.array(fc.string(), { minLength: 1, maxLength: 20 }), (chunks) => {
				agentStore.reset();
				agentStore.setConnected(crypto.randomUUID());
				agentStore.addUserMessage('prompt');

				for (const chunk of chunks) {
					agentStore.appendAgentOutput(chunk);
				}

				const state = get(agentStore);
				const agentMessages = state.messages.filter((m) => m.role === 'agent');
				expect(agentMessages).toHaveLength(1);
				expect(agentMessages[0].content).toBe(chunks.join(''));
				expect(agentMessages[0].complete).toBe(false);
			})
		);
	});

	it('marking done completes the last agent message without altering its content', () => {
		fc.assert(
			fc.property(fc.array(fc.string(), { minLength: 1, maxLength: 20 }), (chunks) => {
				agentStore.reset();
				agentStore.setConnected(crypto.randomUUID());

				for (const chunk of chunks) {
					agentStore.appendAgentOutput(chunk);
				}
				agentStore.markAgentDone();

				const state = get(agentStore);
				const last = state.messages[state.messages.length - 1];
				expect(last.complete).toBe(true);
				expect(last.content).toBe(chunks.join(''));
			})
		);
	});
});

// Regression: switching projects must not leave another project's chat visible.
describe('agentStore project tracking', () => {
	it('reset() clears the project so a stale store is detectable', () => {
		agentStore.setConnecting('project-a');
		expect(get(agentStore).project).toBe('project-a');

		agentStore.reset();
		expect(get(agentStore).project).toBeNull();
	});

	it('retains the project it was connecting/connected under across state transitions', () => {
		agentStore.reset();
		agentStore.setConnecting('project-a');
		const sessionId = crypto.randomUUID();
		agentStore.setConnected(sessionId);

		expect(get(agentStore).project).toBe('project-a');
	});
});

// Feature: project-ai-agent, Property 10: Client message history survives session switches and connection errors
describe('agentStore session history preservation', () => {
	it('preserves a session history after switching away and back', () => {
		fc.assert(
			fc.property(
				fc.array(fc.record({ role: fc.constantFrom('user', 'agent'), content: fc.string() }), {
					minLength: 1,
					maxLength: 10
				}),
				(history) => {
					agentStore.reset();
					const sessionA = crypto.randomUUID();
					const sessionB = crypto.randomUUID();

					agentStore.setConnected(sessionA);
					for (const entry of history) {
						if (entry.role === 'user') {
							agentStore.addUserMessage(entry.content);
						} else {
							agentStore.appendAgentOutput(entry.content);
							agentStore.markAgentDone();
						}
					}
					const before = get(agentStore).messages;

					// Switch to a different (empty) session, then back.
					agentStore.switchToSession(sessionB);
					expect(get(agentStore).messages).toEqual([]);

					agentStore.switchToSession(sessionA);
					const after = get(agentStore).messages;

					expect(after).toEqual(before);
				}
			)
		);
	});

	it('preserves history across a disconnect/reconnect (connection error) cycle', () => {
		fc.assert(
			fc.property(fc.array(fc.string(), { minLength: 1, maxLength: 10 }), (chunks) => {
				agentStore.reset();
				const sessionId = crypto.randomUUID();
				agentStore.setConnected(sessionId);

				for (const chunk of chunks) {
					agentStore.appendAgentOutput(chunk);
				}
				const before = get(agentStore).messages;

				// Simulate a connection error: disconnect, then reconnect to the same session.
				agentStore.setDisconnected();
				agentStore.setConnected(sessionId);

				expect(get(agentStore).messages).toEqual(before);
			})
		);
	});
});
