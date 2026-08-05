<script lang="ts">
	import { tick } from 'svelte';
	import AssistantBrowser from '$lib/components/AssistantBrowser.svelte';
	import ChatMessageBubble from '$lib/components/ChatMessage.svelte';
	import FileUploadButton from '$lib/components/FileUploadButton.svelte';
	import StarterPrompts from '$lib/components/StarterPrompts.svelte';
	import { api, buildAssistantWsUrl, type AssistantSession, type StarterPrompt } from '$lib/api';
	import { getToken } from '$lib/auth';
	import { locale, t } from '$lib/i18n';
	import { assistantStore, type AssistantTab } from '$lib/stores/assistant';
	import { canSubmitMessage } from '$lib/utils/chat';
	import { formatRelativeDate } from '$lib/utils/date';
	import { encodeUploadFrame, extractDroppedFile, extractPastedImageFile } from '$lib/utils/upload';

	const CONNECT_TIMEOUT_MS = 10000;
	const MAX_TEXTAREA_HEIGHT = 160;
	const SUMMARIZE_PROMPT =
		'Summarize this entire conversation concisely, covering: key topics discussed, decisions made, important information shared, and any pending action items. Save the summary to /workspace/summary.md (overwrite any existing content). Output the summary content in your response so I can review it.';

	type Banner =
		| { kind: 'none' }
		| { kind: 'startError'; message: string }
		| { kind: 'connectionError' }
		| { kind: 'sessionLost' }
		| { kind: 'resetWarning' };

	type SessionListState =
		| { status: 'loading' }
		| { status: 'error' }
		| { status: 'loaded'; sessions: AssistantSession[] };

	type PromptsState =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; prompts: StarterPrompt[] };

	let connecting = $derived($assistantStore.connectionState === 'connecting');

	let ws = $state<WebSocket | null>(null);
	let banner = $state<Banner>({ kind: 'none' });
	let processing = $state(false);
	let input = $state('');
	let sessionsState = $state<SessionListState>({ status: 'loading' });
	let promptsState = $state<PromptsState>({ status: 'loading' });
	let refreshTrigger = $state(0);
	let reconnectingTarget = $state<string | null>(null);
	let lastSessionId = $state<string | null>(null);
	let resetDialogOpen = $state(false);
	let resetBusy = $state(false);
	let filesClean = $state<boolean | null>(null);
	let messagesEl = $state<HTMLDivElement | undefined>(undefined);
	let textareaEl = $state<HTMLTextAreaElement | undefined>(undefined);
	let uploading = $state(false);
	let pendingFileName = $state<string | null>(null);

	// Set right before an intentional ws.close() so the onclose handler can
	// distinguish it from an unexpected drop.
	let clientInitiatedClose = false;

	async function loadSessions() {
		sessionsState = { status: 'loading' };
		try {
			const sessions = await api.getAssistantSessions();
			sessionsState = { status: 'loaded', sessions };
		} catch {
			sessionsState = { status: 'error' };
		}
	}

	async function loadPrompts() {
		promptsState = { status: 'loading' };
		try {
			const prompts = await api.getAssistantPrompts();
			promptsState = { status: 'loaded', prompts };
		} catch {
			promptsState = { status: 'error' };
		}
	}

	function selectedPromptContent(): string | null {
		const filename = $assistantStore.selectedPrompt;
		if (!filename || promptsState.status !== 'loaded') return null;
		return promptsState.prompts.find((p) => p.filename === filename)?.content ?? null;
	}

	$effect(() => {
		void refreshTrigger;
		loadSessions();
		loadPrompts();
	});

	function tabClass(tab: AssistantTab) {
		return $assistantStore.activeTab === tab
			? 'shrink-0 border-b-2 border-sky-500 px-1 pb-2 text-sm font-medium text-sky-600 dark:text-sky-400'
			: 'shrink-0 border-b-2 border-transparent px-1 pb-2 text-sm font-medium text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100';
	}

	function handleAgentChange(event: Event) {
		assistantStore.setAgent((event.target as HTMLSelectElement).value);
	}

	function handleModelChange(event: Event) {
		assistantStore.setModel((event.target as HTMLInputElement).value);
	}

	function scrollToBottom() {
		if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
	}

	function busyMessage(event: Event): string | null {
		if (
			event instanceof CloseEvent &&
			(event.code === 1013 || /capacity|busy/i.test(event.reason))
		) {
			return $t('assistant.serverBusy');
		}
		return null;
	}

	function connect(
		url: string,
		expectType: 'session_started' | 'session_resumed',
		asStart: boolean
	) {
		banner = { kind: 'none' };
		clientInitiatedClose = false;
		let settled = false;
		const socket = new WebSocket(url);

		const timeoutId = window.setTimeout(() => {
			if (settled) return;
			settled = true;
			reconnectingTarget = null;
			clientInitiatedClose = true;
			socket.close();
			ws = null;
			assistantStore.setDisconnected();
			banner = asStart
				? { kind: 'startError', message: $t('assistant.connectionError') }
				: { kind: 'sessionLost' };
		}, CONNECT_TIMEOUT_MS);

		socket.onopen = () => {
			const token = getToken();
			if (token) socket.send(JSON.stringify({ type: 'auth', token }));
		};

		socket.onmessage = (event) => {
			let msg: {
				type: string;
				sessionId?: string;
				agent?: string;
				model?: string;
				data?: string;
				message?: string;
				messages?: { role: string; content: string; complete: boolean }[];
			};
			try {
				msg = JSON.parse(event.data as string);
			} catch {
				return;
			}

			if (!settled && msg.type === expectType && msg.sessionId) {
				settled = true;
				reconnectingTarget = null;
				window.clearTimeout(timeoutId);
				lastSessionId = msg.sessionId;
				if (msg.type === 'session_resumed') {
					if (msg.agent) assistantStore.setAgent(msg.agent);
					if (msg.model) assistantStore.setModel(msg.model);
				}
				assistantStore.setConnected(msg.sessionId);
				refreshTrigger++;
				tick().then(scrollToBottom);
				return;
			}

			handleServerMessage(msg);
		};

		socket.onerror = (event) => {
			if (settled) return;
			settled = true;
			reconnectingTarget = null;
			window.clearTimeout(timeoutId);
			const message = busyMessage(event) ?? $t('assistant.connectionError');
			banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
		};

		socket.onclose = (event) => {
			window.clearTimeout(timeoutId);
			ws = null;
			if (clientInitiatedClose) return;

			if (!settled) {
				settled = true;
				reconnectingTarget = null;
				const message = busyMessage(event) ?? $t('assistant.connectionError');
				banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
				assistantStore.setDisconnected();
				return;
			}

			// Was fully connected and then dropped unexpectedly.
			reconnectingTarget = null;
			processing = false;
			banner = { kind: 'connectionError' };
			assistantStore.setDisconnected();
		};

		ws = socket;
	}

	function handleStart() {
		if (!$assistantStore.agent || connecting) return;
		const promptContent = selectedPromptContent();
		assistantStore.setConnecting();
		connect(
			buildAssistantWsUrl($assistantStore.agent, $assistantStore.model || undefined),
			'session_started',
			true
		);
		if (promptContent) {
			input = promptContent;
			tick().then(autoGrow);
		}
		assistantStore.selectPrompt(null);
	}

	function reconnectTo(id: string) {
		reconnectingTarget = id;
		assistantStore.setConnecting();
		connect(buildAssistantWsUrl('', undefined, id), 'session_resumed', false);
	}

	function handleReconnectClick() {
		if (lastSessionId) reconnectTo(lastSessionId);
	}

	function disconnectView() {
		if (ws) {
			clientInitiatedClose = true;
			ws.close();
			ws = null;
		}
		banner = { kind: 'none' };
		assistantStore.setDisconnected();
	}

	function handleNewSessionClick() {
		disconnectView();
		refreshTrigger++;
	}

	function handleDismissSessionLost() {
		banner = { kind: 'none' };
	}

	function handleSessionSelect(session: AssistantSession) {
		if (session.session_id === $assistantStore.sessionId) return;
		assistantStore.setAgent(session.agent);
		assistantStore.setModel(session.model ?? '');
		if (ws) {
			clientInitiatedClose = true;
			ws.close();
			ws = null;
		}
		// Persist the currently viewed session's messages before switching away.
		assistantStore.setDisconnected();
		reconnectTo(session.session_id);
	}

	function handleServerMessage(msg: {
		type: string;
		data?: string;
		message?: string;
		messages?: { role: string; content: string; complete: boolean }[];
	}) {
		switch (msg.type) {
			case 'history':
				assistantStore.setHistory(msg.messages ?? []);
				tick().then(scrollToBottom);
				break;
			case 'output':
				assistantStore.appendAgentOutput(msg.data ?? '');
				tick().then(scrollToBottom);
				break;
			case 'done':
				assistantStore.markAgentDone();
				processing = false;
				break;
			case 'error':
				processing = false;
				assistantStore.reportError(msg.message ?? $t('assistant.sendError'));
				break;
			case 'file_uploaded':
				uploading = false;
				pendingFileName = null;
				assistantStore.addSystemMessage(`${$t('assistant.fileUploaded')}: ${msg.data ?? ''}`);
				break;
			case 'context_compacted':
				assistantStore.addSystemMessage($t('assistant.contextCompacted'));
				break;
			case 'terminated':
				clientInitiatedClose = true;
				ws?.close();
				ws = null;
				assistantStore.setDisconnected();
				refreshTrigger++;
				break;
			default:
				break;
		}
	}

	function autoGrow() {
		if (!textareaEl) return;
		textareaEl.style.height = 'auto';
		textareaEl.style.height = `${Math.min(textareaEl.scrollHeight, MAX_TEXTAREA_HEIGHT)}px`;
	}

	function handleInput(event: Event) {
		input = (event.target as HTMLTextAreaElement).value;
		autoGrow();
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			event.preventDefault();
			handleSubmit();
		}
	}

	function handleSubmit() {
		if (processing) return;
		const text = input;
		if (!canSubmitMessage(text)) return;

		input = '';
		tick().then(autoGrow);

		if (!ws || ws.readyState !== WebSocket.OPEN) {
			assistantStore.addUserMessage(text);
			assistantStore.reportError($t('assistant.sendError'));
			input = text;
			return;
		}

		try {
			ws.send(JSON.stringify({ type: 'message', prompt: text }));
		} catch {
			assistantStore.addUserMessage(text);
			assistantStore.reportError($t('assistant.sendError'));
			input = text;
			return;
		}

		assistantStore.addUserMessage(text);
		processing = true;
		tick().then(scrollToBottom);
	}

	async function sendFile(file: File) {
		if (!ws || ws.readyState !== WebSocket.OPEN) {
			assistantStore.addSystemMessage($t('agent.uploadDisconnected'));
			return;
		}
		uploading = true;
		pendingFileName = file.name;
		try {
			const data = new Uint8Array(await file.arrayBuffer());
			ws.send(encodeUploadFrame(file.name, data));
		} catch {
			assistantStore.addSystemMessage($t('assistant.uploadError'));
		} finally {
			uploading = false;
			pendingFileName = null;
		}
	}

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		const file = extractDroppedFile(event);
		if (file) sendFile(file);
	}

	function handlePaste(event: ClipboardEvent) {
		const file = extractPastedImageFile(event);
		if (file) {
			event.preventDefault();
			sendFile(file);
		}
	}

	function handleSummarize() {
		if (processing || !ws || ws.readyState !== WebSocket.OPEN) return;

		try {
			ws.send(JSON.stringify({ type: 'message', prompt: SUMMARIZE_PROMPT }));
		} catch {
			assistantStore.reportError($t('assistant.sendError'));
			return;
		}

		assistantStore.addUserMessage(SUMMARIZE_PROMPT);
		assistantStore.setSummarizeInProgress();
		processing = true;
		tick().then(scrollToBottom);
	}

	function sendInterrupt() {
		if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'interrupt' }));
	}

	function sendTerminate() {
		if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'terminate' }));
	}

	function handleResetClick() {
		if (resetBusy) return;
		if (!ws || ws.readyState !== WebSocket.OPEN) {
			handleResetChoice(false);
			return;
		}
		resetDialogOpen = true;
	}

	function handleResetDialogCancel() {
		resetDialogOpen = false;
	}

	async function handleResetChoice(withSummary: boolean) {
		if (resetBusy) return;
		resetBusy = true;

		const sessionId = $assistantStore.sessionId;
		let prefill: string | null = null;
		let noSummaryFound = false;

		if (withSummary) {
			try {
				const file = await api.getAssistantSummary();
				if (file.content && file.content.trim().length > 0) {
					prefill = file.content;
				} else {
					noSummaryFound = true;
				}
			} catch {
				noSummaryFound = true;
			}
		}

		if (ws) {
			clientInitiatedClose = true;
			ws.close();
			ws = null;
		}

		if (sessionId) {
			try {
				await api.terminateAssistantSession(sessionId);
			} catch {
				// Best-effort — session may already be gone (e.g. idle timeout).
			}
			assistantStore.clearSessionHistory(sessionId);
		}

		resetDialogOpen = false;
		resetBusy = false;
		processing = false;
		assistantStore.setDisconnected();
		refreshTrigger++;

		if (prefill) {
			input = prefill;
			tick().then(autoGrow);
		}

		banner = noSummaryFound ? { kind: 'resetWarning' } : { kind: 'none' };
	}

	$effect(() => {
		return () => {
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
			}
		};
	});
</script>

<div class="mx-auto flex min-h-0 w-full max-w-6xl flex-1 flex-col px-4 py-6">
	<nav
		aria-label="Tabs"
		class="mb-6 flex shrink-0 gap-4 overflow-x-auto border-b border-slate-200 dark:border-slate-800"
	>
		<button type="button" onclick={() => assistantStore.setTab('chat')} class={tabClass('chat')}>
			{$t('assistant.chatTab')}
		</button>
		<button type="button" onclick={() => assistantStore.setTab('files')} class={tabClass('files')}>
			{$t('assistant.filesTab')}
			{#if filesClean === false}
				<span
					aria-label={$t('assistant.statusDirty')}
					title={$t('assistant.statusDirty')}
					class="ml-1 inline-block size-1.5 rounded-full bg-amber-500 align-middle"
				></span>
			{/if}
		</button>
	</nav>

	<div class="flex min-h-0 flex-1 flex-col">
		{#if $assistantStore.activeTab === 'chat'}
			<div class="flex min-h-0 flex-1 flex-col gap-4 md:flex-row">
				<div
					class="flex min-h-0 flex-col gap-2 rounded-lg border border-slate-200 p-3 md:w-64 md:shrink-0 dark:border-slate-800"
				>
					<h2 class="text-sm font-medium text-slate-700 dark:text-slate-300">
						{$t('agent.sessions')}
					</h2>
					<div class="min-h-0 flex-1 overflow-y-auto">
						{#if sessionsState.status === 'loading'}
							<div class="flex flex-col gap-2">
								{#each [0, 1] as row (row)}
									<div class="h-12 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
								{/each}
							</div>
						{:else if sessionsState.status === 'error'}
							<div class="flex flex-col items-center gap-2 py-4 text-center">
								<p class="text-sm text-slate-500 dark:text-slate-400">
									{$t('agent.sessionsError')}
								</p>
								<button
									type="button"
									onclick={loadSessions}
									class="rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600"
								>
									{$t('common.retry')}
								</button>
							</div>
						{:else if sessionsState.sessions.length === 0}
							<p class="py-4 text-center text-sm text-slate-500 dark:text-slate-400">
								{$t('agent.noSessions')}
							</p>
						{:else}
							<ul class="flex flex-col gap-1">
								{#each sessionsState.sessions as session (session.session_id)}
									<li>
										<button
											type="button"
											onclick={() => handleSessionSelect(session)}
											disabled={reconnectingTarget === session.session_id}
											class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left disabled:cursor-not-allowed disabled:opacity-50 {session.session_id ===
											$assistantStore.sessionId
												? 'bg-sky-50 dark:bg-sky-950'
												: 'hover:bg-slate-50 dark:hover:bg-slate-800/50'}"
										>
											<span aria-hidden="true">⭐</span>
											<span class="flex min-w-0 flex-1 flex-col items-start">
												<span
													class="truncate text-sm font-medium {session.session_id ===
													$assistantStore.sessionId
														? 'text-sky-700 dark:text-sky-300'
														: 'text-slate-900 dark:text-slate-100'}"
												>
													{$t('nav.assistant')}
													<span class="font-normal text-slate-500 dark:text-slate-400">
														/ {session.agent}{#if session.model}/ {session.model}{/if}
													</span>
												</span>
												<span class="text-xs text-slate-500 dark:text-slate-400">
													{formatRelativeDate(session.last_message_at, $locale)}
												</span>
											</span>
										</button>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
					<button
						type="button"
						onclick={handleNewSessionClick}
						class="shrink-0 rounded-md border border-dashed border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:border-sky-400 hover:text-sky-600 dark:border-slate-700 dark:text-slate-400 dark:hover:border-sky-500 dark:hover:text-sky-400"
					>
						+ {$t('agent.newSession')}
					</button>
				</div>

				{#if $assistantStore.connectionState === 'connected' || banner.kind === 'connectionError'}
					<div class="flex min-h-0 flex-1 flex-col gap-3">
						{#if banner.kind === 'connectionError'}
							<div
								class="flex items-center justify-between gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300"
							>
								<span>{$t('assistant.connectionError')}</span>
								<button
									type="button"
									onclick={handleReconnectClick}
									class="shrink-0 rounded-md bg-amber-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-amber-700"
								>
									{$t('assistant.reconnect')}
								</button>
							</div>
						{/if}

						<div
							bind:this={messagesEl}
							ondragover={handleDragOver}
							ondrop={handleDrop}
							role="log"
							class="min-h-0 flex-1 overflow-y-auto rounded-lg border border-slate-200 p-3 dark:border-slate-800"
						>
							<div class="flex flex-col gap-3">
								{#each $assistantStore.messages as message (message.id)}
									<ChatMessageBubble {message} />
								{/each}
								{#if processing}
									<p class="text-xs text-slate-400 dark:text-slate-500">{$t('agent.thinking')}</p>
								{/if}
							</div>
						</div>

						<div class="flex items-center justify-end gap-2">
							<button
								type="button"
								onclick={handleSummarize}
								disabled={processing}
								class="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
							>
								{$t('assistant.summarize')}
							</button>
							<button
								type="button"
								onclick={handleResetClick}
								disabled={resetBusy}
								class="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
							>
								{$t('assistant.reset')}
							</button>
							<button
								type="button"
								onclick={sendInterrupt}
								class="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
							>
								{$t('agent.interrupt')}
							</button>
							<button
								type="button"
								onclick={sendTerminate}
								class="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-red-50 hover:text-red-600 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-red-950 dark:hover:text-red-400"
							>
								{$t('agent.terminate')}
							</button>
						</div>

						<div class="flex items-end gap-2">
							<FileUploadButton disabled={processing} {uploading} onFile={sendFile} />
							<div class="flex min-w-0 flex-1 flex-col gap-1">
								{#if pendingFileName}
									<div
										class="flex items-center gap-1.5 rounded-md border border-sky-200 bg-sky-50 px-2 py-1 text-xs text-sky-700 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-300"
									>
										<span class="inline-block animate-spin">⏳</span>
										<span class="truncate">{pendingFileName}</span>
									</div>
								{/if}
								<textarea
									bind:this={textareaEl}
									value={input}
									oninput={handleInput}
									onkeydown={handleKeydown}
									onpaste={handlePaste}
									rows="1"
									placeholder={$t('agent.inputPlaceholder')}
									class="max-h-40 flex-1 resize-none rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
								></textarea>
							</div>
							<button
								type="button"
								onclick={handleSubmit}
								disabled={!canSubmitMessage(input) || processing}
								class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
							>
								{$t('agent.send')}
							</button>
						</div>
					</div>
				{:else}
					<div class="flex min-h-0 flex-1 flex-col items-center justify-center gap-4">
						<div
							class="flex w-full max-w-md flex-col gap-4 rounded-lg border border-slate-200 p-6 dark:border-slate-800"
						>
							<div class="space-y-1">
								<label
									for="assistant-agent-select"
									class="text-sm font-medium text-slate-700 dark:text-slate-300"
								>
									{$t('agent.selectAgent')}
								</label>
								<select
									id="assistant-agent-select"
									value={$assistantStore.agent}
									onchange={handleAgentChange}
									disabled={connecting}
									class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
								>
									<option value="" disabled>{$t('agent.selectAgent')}</option>
									<option value="kiro">kiro</option>
									<option value="claude">claude</option>
								</select>
							</div>

							<div class="space-y-1">
								<label
									for="assistant-model-input"
									class="text-sm font-medium text-slate-700 dark:text-slate-300"
								>
									{$t('agent.selectModel')}
								</label>
								<input
									id="assistant-model-input"
									type="text"
									value={$assistantStore.model}
									oninput={handleModelChange}
									maxlength={100}
									placeholder={$t('agent.selectModel')}
									disabled={connecting}
									class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
								/>
							</div>

							<StarterPrompts
								prompts={promptsState.status === 'loaded' ? promptsState.prompts : []}
								selectedFilename={$assistantStore.selectedPrompt}
								onSelect={(filename) => assistantStore.selectPrompt(filename)}
								error={promptsState.status === 'error' ? $t('assistant.promptsError') : null}
							/>

							{#if banner.kind === 'startError'}
								<p class="text-sm text-red-600 dark:text-red-400">{banner.message}</p>
							{/if}

							<button
								type="button"
								onclick={handleStart}
								disabled={!$assistantStore.agent || connecting}
								class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
							>
								{connecting ? $t('agent.connecting') : $t('agent.start')}
							</button>
						</div>

						{#if banner.kind === 'sessionLost'}
							<div
								class="flex flex-col items-center gap-2 rounded-lg border border-slate-200 p-4 text-center dark:border-slate-800"
							>
								<p class="text-sm text-slate-500 dark:text-slate-400">
									{$t('assistant.sessionLost')}
								</p>
								<button
									type="button"
									onclick={handleDismissSessionLost}
									class="rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600"
								>
									{$t('assistant.newSession')}
								</button>
							</div>
						{:else if banner.kind === 'resetWarning'}
							<div
								class="max-w-md rounded-lg border border-amber-300 bg-amber-50 p-4 text-center text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300"
							>
								{$t('assistant.noSummaryWarning')}
							</div>
						{/if}
					</div>
				{/if}
			</div>
		{:else}
			<AssistantBrowser onStatusChange={(clean) => (filesClean = clean)} />
		{/if}
	</div>
</div>

{#if resetDialogOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="assistant-reset-title"
			class="flex w-full max-w-sm flex-col gap-4 rounded-lg bg-white p-6 shadow-xl dark:bg-slate-900"
		>
			<h2
				id="assistant-reset-title"
				class="text-lg font-semibold text-slate-800 dark:text-slate-100"
			>
				{$t('assistant.resetDialog')}
			</h2>

			<div class="flex flex-col gap-2">
				<button
					type="button"
					onclick={() => handleResetChoice(true)}
					disabled={resetBusy}
					class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
				>
					{$t('assistant.resetWithSummary')}
				</button>
				<button
					type="button"
					onclick={() => handleResetChoice(false)}
					disabled={resetBusy}
					class="rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
				>
					{$t('assistant.resetWithoutSummary')}
				</button>
			</div>

			<div class="flex items-center justify-end">
				<button
					type="button"
					onclick={handleResetDialogCancel}
					disabled={resetBusy}
					class="rounded-md px-3 py-1.5 text-sm font-medium text-slate-500 transition-colors hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800"
				>
					{$t('assistant.resetCancel')}
				</button>
			</div>
		</div>
	</div>
{/if}
