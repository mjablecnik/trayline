import { writable } from 'svelte/store';

export type ConnectionState = 'disconnected' | 'connecting' | 'connected';

export interface ChatMessage {
	id: string;
	role: 'user' | 'agent' | 'system';
	content: string;
	complete: boolean; // false while agent is still streaming
	error?: string; // inline error for failed sends
}

export interface AgentSessionState {
	sessionId: string | null;
	// Project this state belongs to. Compared against the current route's
	// project to detect stale state left over from a different project —
	// this store is a module-level singleton, so it outlives any single
	// page component instance (e.g. navigating away through an unrelated
	// route and back destroys/recreates the agent page component, but not
	// this store).
	project: string | null;
	agent: string; // "kiro" | "claude" | ""
	model: string;
	connectionState: ConnectionState;
	messages: ChatMessage[];
}

// Per-session message history keyed by session ID.
// Kept in memory so switching sessions preserves context.
const sessionHistories = new Map<string, ChatMessage[]>();

function createAgentStore() {
	const { subscribe, set, update } = writable<AgentSessionState>({
		sessionId: null,
		project: null,
		agent: '',
		model: '',
		connectionState: 'disconnected',
		messages: []
	});

	return {
		subscribe,
		setAgent(agent: string) {
			update((s) => ({ ...s, agent }));
		},
		setModel(model: string) {
			update((s) => ({ ...s, model }));
		},
		setConnecting(project: string) {
			update((s) => ({ ...s, project, connectionState: 'connecting' }));
		},
		setConnected(sessionId: string) {
			update((s) => ({
				...s,
				sessionId,
				connectionState: 'connected',
				messages: sessionHistories.get(sessionId) ?? []
			}));
		},
		setDisconnected() {
			update((s) => {
				if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
				return { ...s, sessionId: null, connectionState: 'disconnected' };
			});
		},
		addUserMessage(content: string) {
			const id = crypto.randomUUID();
			update((s) => ({
				...s,
				messages: [...s.messages, { id, role: 'user', content, complete: true }]
			}));
		},
		addSystemMessage(content: string) {
			const id = crypto.randomUUID();
			update((s) => ({
				...s,
				messages: [...s.messages, { id, role: 'system', content, complete: true }]
			}));
		},
		appendAgentOutput(text: string) {
			update((s) => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'agent' && !last.complete) {
					msgs[msgs.length - 1] = { ...last, content: last.content + text };
				} else {
					msgs.push({ id: crypto.randomUUID(), role: 'agent', content: text, complete: false });
				}
				return { ...s, messages: msgs };
			});
		},
		markAgentDone() {
			update((s) => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'agent') {
					msgs[msgs.length - 1] = { ...last, complete: true };
				}
				return { ...s, messages: msgs };
			});
		},
		markLastUserMessageError(error: string) {
			update((s) => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'user') {
					msgs[msgs.length - 1] = { ...last, error };
				}
				return { ...s, messages: msgs };
			});
		},
		switchToSession(sessionId: string) {
			update((s) => {
				if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
				return {
					...s,
					sessionId,
					messages: sessionHistories.get(sessionId) ?? []
				};
			});
		},
		reset() {
			set({
				sessionId: null,
				project: null,
				agent: '',
				model: '',
				connectionState: 'disconnected',
				messages: []
			});
		}
	};
}

export const agentStore = createAgentStore();
