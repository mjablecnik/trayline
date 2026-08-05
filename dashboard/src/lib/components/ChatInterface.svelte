<script lang="ts">
	import { tick, untrack } from 'svelte';
	import AgentSelector from '$lib/components/AgentSelector.svelte';
	import ChatMessageBubble from '$lib/components/ChatMessage.svelte';
	import { buildWsUrl } from '$lib/api';
	import { getToken } from '$lib/auth';
	import { t } from '$lib/i18n';
	import { agentStore } from '$lib/stores/agent';
	import { canSubmitMessage } from '$lib/utils/chat';
	import { encodeUploadFrame } from '$lib/utils/upload';

	let {
		projectName,
		sessionId = null,
		onSessionChange
	}: {
		projectName: string;
		sessionId?: string | null;
		onSessionChange?: () => void;
	} = $props();

	const CONNECT_TIMEOUT_MS = 10000;
	const MAX_TEXTAREA_HEIGHT = 240;
	const SCROLL_THRESHOLD = 50;

	// Auto-reconnect configuration
	const RECONNECT_BASE_DELAY_MS = 1000;
	const RECONNECT_MAX_DELAY_MS = 30000;
	const RECONNECT_MAX_ATTEMPTS = 10;

	type Banner =
		| { kind: 'none' }
		| { kind: 'startError'; message: string }
		| { kind: 'connectionError' }
		| { kind: 'sessionLost' };

	let ws = $state<WebSocket | null>(null);
	let banner = $state<Banner>({ kind: 'none' });
	let processing = $state(false);
	let input = $state('');
	let lastSessionId = $state<string | null>(null);
	let userScrolledUp = $state(false);
	// Session ID a reconnect attempt is currently in flight for. Needed because
	// $agentStore is a legacy store subscription: any update() call on it (e.g.
	// setConnecting, which never touches sessionId) re-triggers every $effect
	// that reads $agentStore.*, not just ones whose read value actually changed.
	// Without this guard, the sessionId-watching effect below would treat its
	// own in-flight connect() as "not yet connected" on every such re-run and
	// keep tearing down and recreating the socket before it could ever finish
	// its handshake.
	let reconnectingTarget = $state<string | null>(null);

	let messagesEl = $state<HTMLDivElement | undefined>(undefined);
	let textareaEl = $state<HTMLTextAreaElement | undefined>(undefined);
	let fileInputEl = $state<HTMLInputElement | undefined>(undefined);
	let pendingFiles = $state<File[]>([]);
	let uploading = $state(false);

	// Auto-reconnect state
	let reconnectAttempt = 0;
	let reconnectTimerId: number | undefined;
	let autoReconnecting = $state(false);

	// Set right before an intentional ws.close() so the onclose handler
	// can distinguish it from an unexpected drop.
	let clientInitiatedClose = false;

	function clearReconnectTimer() {
		if (reconnectTimerId !== undefined) {
			window.clearTimeout(reconnectTimerId);
			reconnectTimerId = undefined;
		}
	}

	function resetReconnectState() {
		reconnectAttempt = 0;
		autoReconnecting = false;
		clearReconnectTimer();
	}

	function scheduleAutoReconnect(targetSessionId: string) {
		if (reconnectAttempt >= RECONNECT_MAX_ATTEMPTS) {
			// Give up after max attempts — show sessionLost banner so user can start fresh
			autoReconnecting = false;
			banner = { kind: 'sessionLost' };
			agentStore.setDisconnected();
			return;
		}

		autoReconnecting = true;
		banner = { kind: 'none' }; // Clear any previous error banner during auto-reconnect
		const delay = Math.min(
			RECONNECT_BASE_DELAY_MS * Math.pow(2, reconnectAttempt),
			RECONNECT_MAX_DELAY_MS
		);
		reconnectAttempt++;

		reconnectTimerId = window.setTimeout(() => {
			reconnectTimerId = undefined;
			// Only attempt if we're still in a state that wants reconnection
			if (!autoReconnecting || clientInitiatedClose) return;
			reconnectTo(targetSessionId);
		}, delay);
	}

	function busyMessage(event: Event): string | null {
		if (
			event instanceof CloseEvent &&
			(event.code === 1013 || /capacity|busy/i.test(event.reason))
		) {
			return $t('agent.serverBusy');
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
			agentStore.setDisconnected();
			banner = asStart
				? { kind: 'startError', message: $t('agent.connectionError') }
				: { kind: 'sessionLost' };
		}, CONNECT_TIMEOUT_MS);

		socket.onopen = () => {
			// Send auth message as first frame after connection opens.
			const token = getToken();
			if (token) {
				socket.send(JSON.stringify({ type: 'auth', token }));
			}
		};

		socket.onmessage = (event) => {
			let msg: {
				type: string;
				sessionId?: string;
				data?: string;
				message?: string;
				messages?: { role: 'user' | 'agent' | 'system'; content: string; complete: boolean }[];
			};
			try {
				msg = JSON.parse(event.data as string);
			} catch {
				return;
			}

			if (!settled && msg.type === expectType && msg.sessionId) {
				settled = true;
				reconnectingTarget = null;
				resetReconnectState();
				window.clearTimeout(timeoutId);
				lastSessionId = msg.sessionId;
				agentStore.setConnected(msg.sessionId);
				onSessionChange?.();
				userScrolledUp = false;
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
			const busyMsg = busyMessage(event);
			if (busyMsg) {
				// Server is at capacity — don't auto-reconnect, show error immediately
				resetReconnectState();
				banner = asStart ? { kind: 'startError', message: busyMsg } : { kind: 'sessionLost' };
				return;
			}
			const message = $t('agent.connectionError');
			// If auto-reconnecting, let onclose handle the retry scheduling
			if (autoReconnecting && !asStart) return;
			banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
		};

		socket.onclose = (event) => {
			window.clearTimeout(timeoutId);
			ws = null;
			if (clientInitiatedClose) return;

			if (!settled) {
				settled = true;
				reconnectingTarget = null;
				const message = busyMessage(event) ?? $t('agent.connectionError');
				// If this was an auto-reconnect attempt that failed before settling,
				// schedule another retry without touching the store (keep chat visible).
				if (autoReconnecting && !asStart && lastSessionId) {
					scheduleAutoReconnect(lastSessionId);
					return;
				}
				banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
				agentStore.setDisconnected();
				return;
			}

			// Was fully connected and then dropped unexpectedly.
			reconnectingTarget = null;
			processing = false;

			// Auto-reconnect: if we have a session ID, try to reconnect automatically.
			// Don't call setDisconnected() — keep the chat messages visible during reconnection.
			if (lastSessionId) {
				scheduleAutoReconnect(lastSessionId);
			} else {
				banner = { kind: 'connectionError' };
				agentStore.setDisconnected();
			}
		};

		ws = socket;
	}

	function startNewSession(agent: string, model: string) {
		agentStore.setAgent(agent);
		agentStore.setModel(model);
		agentStore.setConnecting(projectName);
		connect(buildWsUrl(projectName, agent, model || undefined), 'session_started', true);
	}

	function reconnectTo(id: string) {
		reconnectingTarget = id;
		// During auto-reconnect, don't change the store state to 'connecting'
		// so the chat messages remain visible.
		if (!autoReconnecting) {
			agentStore.setConnecting(projectName);
		}
		connect(buildWsUrl(projectName, '', undefined, id), 'session_resumed', false);
	}

	function handleReconnectClick() {
		resetReconnectState();
		banner = { kind: 'none' };
		if (lastSessionId) {
			agentStore.setConnecting(projectName);
			reconnectTo(lastSessionId);
		}
	}

	function handleStartNewSessionClick() {
		resetReconnectState();
		banner = { kind: 'none' };
		agentStore.setDisconnected();
	}

	function handleServerMessage(msg: {
		type: string;
		data?: string;
		message?: string;
		messages?: { role: 'user' | 'agent' | 'system'; content: string; complete: boolean }[];
	}) {
		switch (msg.type) {
			case 'history':
				agentStore.setHistory(msg.messages ?? []);
				userScrolledUp = false;
				tick().then(scrollToBottom);
				break;
			case 'output':
				agentStore.appendAgentOutput(msg.data ?? '');
				if (!userScrolledUp) tick().then(scrollToBottom);
				break;
			case 'done':
				agentStore.markAgentDone();
				processing = false;
				break;
			case 'error':
				processing = false;
				agentStore.markLastUserMessageError(msg.message ?? $t('agent.sendError'));
				break;
			case 'file_uploaded':
				agentStore.addSystemMessage($t('agent.fileUploaded').replace('{filename}', msg.data ?? ''));
				break;
			case 'terminated':
				clientInitiatedClose = true;
				ws?.close();
				ws = null;
				agentStore.setDisconnected();
				onSessionChange?.();
				break;
			default:
				break;
		}
	}

	function handleScroll() {
		if (!messagesEl) return;
		userScrolledUp =
			messagesEl.scrollTop + messagesEl.clientHeight < messagesEl.scrollHeight - SCROLL_THRESHOLD;
	}

	function scrollToBottom() {
		if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
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
		if (processing || uploading) return;
		const text = input;
		const hasFiles = pendingFiles.length > 0;
		if (!canSubmitMessage(text) && !hasFiles) return;

		if (!ws || ws.readyState !== WebSocket.OPEN) {
			if (text) {
				agentStore.addUserMessage(text);
				agentStore.markLastUserMessageError($t('agent.sendError'));
			} else {
				agentStore.addSystemMessage($t('agent.uploadDisconnected'));
			}
			return;
		}

		input = '';
		tick().then(autoGrow);

		// Upload pending files first, then send the text message
		const filesToSend = [...pendingFiles];
		pendingFiles = [];

		if (filesToSend.length > 0) {
			uploading = true;
			sendFilesAndMessage(filesToSend, text);
		} else if (text) {
			sendTextMessage(text);
		}
	}

	async function sendFilesAndMessage(files: File[], text: string) {
		try {
			for (const file of files) {
				const data = new Uint8Array(await file.arrayBuffer());
				ws!.send(encodeUploadFrame(file.name, data));
			}
			// After all files are uploaded, send the text message if any
			if (text) {
				sendTextMessage(text);
			}
		} catch {
			agentStore.addSystemMessage($t('agent.uploadError'));
		} finally {
			uploading = false;
		}
	}

	function sendTextMessage(text: string) {
		try {
			ws!.send(JSON.stringify({ type: 'message', prompt: text }));
		} catch {
			agentStore.addUserMessage(text);
			agentStore.markLastUserMessageError($t('agent.sendError'));
			input = text;
			return;
		}
		agentStore.addUserMessage(text);
		processing = true;
		userScrolledUp = false;
		tick().then(scrollToBottom);
	}

	function stageFile(file: File) {
		pendingFiles = [...pendingFiles, file];
	}

	function removePendingFile(index: number) {
		pendingFiles = pendingFiles.filter((_, i) => i !== index);
	}

	function handleFileInputChange(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (file) stageFile(file);
		input.value = '';
	}

	function handleAttachClick() {
		fileInputEl?.click();
	}

	// Lets a screenshot copied to the clipboard (e.g. Win+Shift+S on Windows,
	// or a screenshot shared via paste on iOS) be staged as an attachment with
	// Ctrl+V / Cmd+V / long-press Paste, same as drag-and-drop.
	function handlePaste(event: ClipboardEvent) {
		const items = event.clipboardData?.items;
		if (items) {
			for (const item of items) {
				if (item.kind !== 'file' || !item.type.startsWith('image/')) continue;
				const file = item.getAsFile();
				if (!file) continue;
				event.preventDefault();
				const ext = item.type.split('/')[1]?.split('+')[0] || 'png';
				stageFile(new File([file], `clipboard-${Date.now()}.${ext}`, { type: item.type }));
				return;
			}
		}
		// iOS Safari may expose images as blob URLs in the DataTransfer.files list
		// instead of clipboardData.items when pasting from Photos or screenshot.
		const files = event.clipboardData?.files;
		if (files && files.length > 0) {
			const file = files[0];
			if (file.type.startsWith('image/')) {
				event.preventDefault();
				const ext = file.type.split('/')[1]?.split('+')[0] || 'png';
				stageFile(new File([file], `clipboard-${Date.now()}.${ext}`, { type: file.type }));
			}
		}
	}

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		const file = event.dataTransfer?.files?.[0];
		if (file) stageFile(file);
	}

	function sendInterrupt() {
		if (ws && ws.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify({ type: 'interrupt' }));
		}
	}

	function sendTerminate() {
		if (ws && ws.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify({ type: 'terminate' }));
		}
	}

	// Disconnect and reset when the project changes (user navigated to a different project).
	let prevProjectName = $state<string>(untrack(() => projectName));
	$effect(() => {
		if (projectName !== prevProjectName) {
			prevProjectName = projectName;
			resetReconnectState();
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
				ws = null;
			}
			banner = { kind: 'none' };
			processing = false;
			lastSessionId = null;
			reconnectingTarget = null;
			userScrolledUp = false;
		}
	});

	// Reconnect automatically when the parent points us at a different (existing) session,
	// e.g. the user picked one from SessionList.
	$effect(() => {
		const target = sessionId;
		if (target && target !== $agentStore.sessionId && target !== reconnectingTarget) {
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
				ws = null;
			}
			reconnectTo(target);
		}
	});

	$effect(() => {
		return () => {
			resetReconnectState();
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
			}
		};
	});

	// When the page becomes visible again (e.g. after laptop sleep/wake or tab switch),
	// check if the WebSocket is still alive. If not, trigger auto-reconnect immediately.
	$effect(() => {
		function handleVisibilityChange() {
			if (document.visibilityState !== 'visible') return;
			if (!lastSessionId) return;
			if (autoReconnecting) return; // already reconnecting

			// Check if the socket is dead
			if (!ws || ws.readyState !== WebSocket.OPEN) {
				// Connection died while tab was hidden — start auto-reconnect
				if ($agentStore.connectionState === 'connected') {
					processing = false;
					scheduleAutoReconnect(lastSessionId);
				}
			}
		}

		document.addEventListener('visibilitychange', handleVisibilityChange);
		return () => document.removeEventListener('visibilitychange', handleVisibilityChange);
	});
</script>

{#if $agentStore.connectionState !== 'connected'}
	<div class="flex flex-1 flex-col gap-3">
		<AgentSelector
			connecting={$agentStore.connectionState === 'connecting'}
			error={banner.kind === 'startError' ? banner.message : null}
			onStart={startNewSession}
		/>
		{#if banner.kind === 'sessionLost'}
			<div
				class="flex flex-col items-center gap-2 rounded-lg border border-slate-200 p-4 text-center dark:border-slate-800"
			>
				<p class="text-sm text-slate-500 dark:text-slate-400">{$t('agent.sessionLost')}</p>
				<button
					type="button"
					onclick={handleStartNewSessionClick}
					class="rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600"
				>
					{$t('agent.newSession')}
				</button>
			</div>
		{/if}
	</div>
{:else}
	<div class="flex min-h-0 flex-1 flex-col gap-3">
		{#if banner.kind === 'connectionError' || autoReconnecting}
			<div
				class="flex items-center justify-between gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300"
			>
				<span>
					{#if autoReconnecting}
						{$t('agent.reconnecting')}
					{:else}
						{$t('agent.connectionError')}
					{/if}
				</span>
				{#if !autoReconnecting}
					<button
						type="button"
						onclick={handleReconnectClick}
						class="shrink-0 rounded-md bg-amber-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-amber-700"
					>
						{$t('agent.reconnect')}
					</button>
				{/if}
			</div>
		{/if}

		<div
			bind:this={messagesEl}
			onscroll={handleScroll}
			ondragover={handleDragOver}
			ondrop={handleDrop}
			role="log"
			class="min-h-0 flex-1 overflow-y-auto rounded-lg border border-slate-200 p-3 dark:border-slate-800"
		>
			<div class="flex flex-col gap-3">
				{#each $agentStore.messages as message (message.id)}
					<ChatMessageBubble {message} />
				{/each}
				{#if processing}
					<p class="text-xs text-slate-400 dark:text-slate-500">{$t('agent.thinking')}</p>
				{/if}
			</div>
		</div>

		{#if userScrolledUp}
			<button
				type="button"
				onclick={() => {
					userScrolledUp = false;
					scrollToBottom();
				}}
				class="self-center rounded-full bg-slate-800 px-3 py-1 text-xs text-white shadow transition-colors hover:bg-slate-900 dark:bg-slate-200 dark:text-slate-900 dark:hover:bg-white"
			>
				{$t('agent.scrollToBottom')}
			</button>
		{/if}

		<div class="flex items-center justify-end gap-2">
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
			<input
				bind:this={fileInputEl}
				type="file"
				accept="image/*,application/pdf,.txt,.md,.json,.csv,.xml,.yaml,.yml"
				class="hidden"
				onchange={handleFileInputChange}
			/>
			<button
				type="button"
				onclick={handleAttachClick}
				disabled={processing || uploading}
				title={$t('agent.attachFile')}
				aria-label={$t('agent.attachFile')}
				class="rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
			>
				{#if uploading}
					<span class="inline-block animate-spin">⏳</span>
				{:else}
					📎
				{/if}
			</button>
			<div class="flex min-w-0 flex-1 flex-col gap-1">
				{#if pendingFiles.length > 0}
					<div
						class="flex flex-wrap gap-2 rounded-md border border-slate-200 bg-slate-50 px-2 py-1.5 dark:border-slate-700 dark:bg-slate-800/50"
					>
						{#each pendingFiles as file, i (file.name + i)}
							<div
								class="group relative flex items-center gap-1.5 rounded border border-slate-300 bg-white px-2 py-1 text-xs dark:border-slate-600 dark:bg-slate-800"
							>
								{#if file.type.startsWith('image/')}
									<img
										src={URL.createObjectURL(file)}
										alt={file.name}
										class="h-8 w-8 rounded object-cover"
									/>
								{:else}
									<span>📄</span>
								{/if}
								<span class="max-w-32 truncate text-slate-600 dark:text-slate-300">{file.name}</span
								>
								<button
									type="button"
									onclick={() => removePendingFile(i)}
									class="ml-1 rounded-full text-slate-400 transition-colors hover:text-red-500 dark:text-slate-500 dark:hover:text-red-400"
									title={$t('agent.removeAttachment')}
									aria-label={$t('agent.removeAttachment')}
								>
									✕
								</button>
							</div>
						{/each}
					</div>
				{/if}
				<textarea
					bind:this={textareaEl}
					value={input}
					oninput={handleInput}
					onkeydown={handleKeydown}
					onpaste={handlePaste}
					rows="3"
					placeholder={$t('agent.inputPlaceholder')}
					class="max-h-60 min-h-[4.5rem] flex-1 resize-none rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
				></textarea>
			</div>
			<button
				type="button"
				onclick={handleSubmit}
				disabled={(!canSubmitMessage(input) && pendingFiles.length === 0) ||
					processing ||
					uploading}
				class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{$t('agent.send')}
			</button>
		</div>
	</div>
{/if}
