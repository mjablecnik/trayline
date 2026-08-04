import fc from 'fast-check';
import { get } from 'svelte/store';
import { describe, expect, it } from 'vitest';
import { assistantStore } from './assistant';

// Feature: personal-assistant-agent, Property 11: Session message history is
// preserved across state transitions
describe('assistantStore session history preservation', () => {
	it('preserves a session history after switching away and back', () => {
		fc.assert(
			fc.property(
				fc.array(fc.record({ role: fc.constantFrom('user', 'agent'), content: fc.string() }), {
					minLength: 1,
					maxLength: 10
				}),
				(history) => {
					assistantStore.reset();
					const sessionA = crypto.randomUUID();
					const sessionB = crypto.randomUUID();

					assistantStore.setConnected(sessionA);
					for (const entry of history) {
						if (entry.role === 'user') {
							assistantStore.addUserMessage(entry.content);
						} else {
							assistantStore.appendAgentOutput(entry.content);
							assistantStore.markAgentDone();
						}
					}
					const before = get(assistantStore).messages;

					// Switch to a different (empty) session, then back.
					assistantStore.switchToSession(sessionB);
					expect(get(assistantStore).messages).toEqual([]);

					assistantStore.switchToSession(sessionA);
					const after = get(assistantStore).messages;

					expect(after).toEqual(before);
				}
			)
		);
	});

	it('preserves history across a disconnect/reconnect (connection error) cycle', () => {
		fc.assert(
			fc.property(fc.array(fc.string(), { minLength: 1, maxLength: 10 }), (chunks) => {
				assistantStore.reset();
				const sessionId = crypto.randomUUID();
				assistantStore.setConnected(sessionId);

				for (const chunk of chunks) {
					assistantStore.appendAgentOutput(chunk);
				}
				const before = get(assistantStore).messages;

				// Simulate a connection error: disconnect, then reconnect to the same session.
				assistantStore.setDisconnected();
				assistantStore.setConnected(sessionId);

				expect(get(assistantStore).messages).toEqual(before);
			})
		);
	});

	it('clearing a reset session leaves other sessions untouched', () => {
		fc.assert(
			fc.property(
				fc.array(fc.string(), { minLength: 1, maxLength: 10 }),
				fc.array(fc.string(), { minLength: 1, maxLength: 10 }),
				(chunksA, chunksB) => {
					assistantStore.reset();
					const sessionA = crypto.randomUUID();
					const sessionB = crypto.randomUUID();

					assistantStore.setConnected(sessionA);
					for (const chunk of chunksA) assistantStore.appendAgentOutput(chunk);

					assistantStore.switchToSession(sessionB);
					for (const chunk of chunksB) assistantStore.appendAgentOutput(chunk);
					const sessionBMessages = get(assistantStore).messages;

					// Reset session A (e.g. after termination): clear only its history.
					assistantStore.clearSessionHistory(sessionA);
					assistantStore.switchToSession(sessionA);
					expect(get(assistantStore).messages).toEqual([]);

					assistantStore.switchToSession(sessionB);
					expect(get(assistantStore).messages).toEqual(sessionBMessages);
				}
			)
		);
	});
});

// Feature: personal-assistant-agent, Property 12: History on reconnect
// contains full transcript
describe('assistantStore history on reconnect', () => {
	it('setHistory replaces stale local state with the full server transcript', () => {
		fc.assert(
			fc.property(
				fc.array(fc.string(), { minLength: 0, maxLength: 5 }),
				fc.array(
					fc.record({
						role: fc.constantFrom('user', 'agent'),
						content: fc.string(),
						complete: fc.boolean()
					}),
					{ minLength: 0, maxLength: 10 }
				),
				(staleChunks, transcript) => {
					assistantStore.reset();
					const sessionId = crypto.randomUUID();
					assistantStore.setConnected(sessionId);

					// Simulate stale local state left over from before the reconnect.
					for (const chunk of staleChunks) {
						assistantStore.appendAgentOutput(chunk);
					}

					assistantStore.setHistory(transcript);
					const messages = get(assistantStore).messages;

					expect(messages).toHaveLength(transcript.length);
					expect(
						messages.map((m) => ({ role: m.role, content: m.content, complete: m.complete }))
					).toEqual(transcript);
				}
			)
		);
	});
});
