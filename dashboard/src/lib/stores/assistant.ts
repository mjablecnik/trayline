import { writable } from 'svelte/store';
import type { ChatMessage, ConnectionState } from './agent';

export type AssistantTab = 'chat' | 'files';

export interface AssistantState {
	sessionId: string | null;
	agent: string;
	model: string;
	connectionState: ConnectionState;
	messages: ChatMessage[];
	activeTab: AssistantTab;
	summarizeInProgress: boolean;
	summaryContent: string | null;
	selectedPrompt: string | null;
}

// Per-session message history keyed by session ID, mirrors the pattern in
// stores/agent.ts — lets switching between assistant sessions preserve
// context without a server round-trip.
const sessionHistories = new Map<string, ChatMessage[]>();

function createAssistantStore() {
	const { subscribe, set, update } = writable<AssistantState>({
		sessionId: null,
		agent: '',
		model: '',
		connectionState: 'disconnected',
		messages: [],
		activeTab: 'chat',
		summarizeInProgress: false,
		summaryContent: null,
		selectedPrompt: null
	});

	return {
		subscribe,
		setAgent(agent: string) {
			update((s) => ({
				...s,
				agent,
				// Claude's model field is easy to forget to fill in; default it to
				// the recommended model rather than leaving it blank. Only kicks in
				// when the field is still empty so it never clobbers a user edit.
				model: agent === 'claude' && !s.model ? 'sonnet' : s.model
			}));
		},
		setModel(model: string) {
			update((s) => ({ ...s, model }));
		},
		setTab(tab: AssistantTab) {
			update((s) => ({ ...s, activeTab: tab }));
		},
		setConnecting() {
			update((s) => ({ ...s, connectionState: 'connecting' }));
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
		// Attaches an inline error to the message it belongs to (the last user
		// message, e.g. a failed send). If there is no such message to attach to
		// (e.g. a failed file upload, which is not a reply to any particular user
		// turn), surfaces it as a standalone system message instead of dropping it.
		reportError(message: string) {
			update((s) => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'user') {
					msgs[msgs.length - 1] = { ...last, error: message };
					return { ...s, messages: msgs };
				}
				return {
					...s,
					messages: [
						...msgs,
						{ id: crypto.randomUUID(), role: 'system', content: message, complete: true }
					]
				};
			});
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
				return { ...s, messages: msgs, summarizeInProgress: false };
			});
		},
		// Replaces the message list with the transcript the server sent for this
		// session (its "history" message, sent on session_resumed).
		setHistory(messages: { role: string; content: string; complete: boolean }[]) {
			update((s) => ({
				...s,
				messages: messages.map((m) => ({
					id: crypto.randomUUID(),
					role: m.role as 'user' | 'agent',
					content: m.content,
					complete: m.complete
				}))
			}));
		},
		setSummarizeInProgress() {
			update((s) => ({ ...s, summarizeInProgress: true }));
		},
		setSummaryContent(content: string | null) {
			update((s) => ({ ...s, summaryContent: content }));
		},
		selectPrompt(filename: string | null) {
			update((s) => ({ ...s, selectedPrompt: filename }));
		},
		switchToSession(sessionId: string) {
			update((s) => {
				if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
				return { ...s, sessionId, messages: sessionHistories.get(sessionId) ?? [] };
			});
		},
		clearSessionHistory(sessionId: string) {
			sessionHistories.delete(sessionId);
		},
		reset() {
			set({
				sessionId: null,
				agent: '',
				model: '',
				connectionState: 'disconnected',
				messages: [],
				activeTab: 'chat',
				summarizeInProgress: false,
				summaryContent: null,
				selectedPrompt: null
			});
		}
	};
}

export const assistantStore = createAssistantStore();
