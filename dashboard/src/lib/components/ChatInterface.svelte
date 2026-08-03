<script lang="ts">
	import { tick } from 'svelte';
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
	const MAX_TEXTAREA_HEIGHT = 160;
	const SCROLL_THRESHOLD = 50;

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

	let messagesEl = $state<HTMLDivElement | undefined>(undefined);
	let textareaEl = $state<HTMLTextAreaElement | undefined>(undefined);
	let fileInputEl = $state<HTMLInputElement | undefined>(undefined);

	// Set right before an intentional ws.close() so the onclose handler
	// can distinguish it from an unexpected drop.
	let clientInitiatedClose = false;

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
			let msg: { type: string; sessionId?: string; data?: string; message?: string };
			try {
				msg = JSON.parse(event.data as string);
			} catch {
				return;
			}

			if (!settled && msg.type === expectType && msg.sessionId) {
				settled = true;
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
			window.clearTimeout(timeoutId);
			const message = busyMessage(event) ?? $t('agent.connectionError');
			banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
		};

		socket.onclose = (event) => {
			window.clearTimeout(timeoutId);
			ws = null;
			if (clientInitiatedClose) return;

			if (!settled) {
				settled = true;
				const message = busyMessage(event) ?? $t('agent.connectionError');
				banner = asStart ? { kind: 'startError', message } : { kind: 'sessionLost' };
				agentStore.setDisconnected();
				return;
			}

			// Was fully connected and then dropped unexpectedly.
			processing = false;
			banner = { kind: 'connectionError' };
			agentStore.setDisconnected();
		};

		ws = socket;
	}

	function startNewSession(agent: string, model: string) {
		agentStore.setAgent(agent);
		agentStore.setModel(model);
		agentStore.setConnecting();
		connect(buildWsUrl(projectName, agent, model || undefined), 'session_started', true);
	}

	function reconnectTo(id: string) {
		agentStore.setConnecting();
		connect(buildWsUrl(projectName, '', undefined, id), 'session_resumed', false);
	}

	function handleReconnectClick() {
		if (lastSessionId) reconnectTo(lastSessionId);
	}

	function handleStartNewSessionClick() {
		banner = { kind: 'none' };
		agentStore.setDisconnected();
	}

	function handleServerMessage(msg: { type: string; data?: string; message?: string }) {
		switch (msg.type) {
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
		if (processing) return;
		const text = input;
		if (!canSubmitMessage(text)) return;

		input = '';
		tick().then(autoGrow);

		if (!ws || ws.readyState !== WebSocket.OPEN) {
			agentStore.addUserMessage(text);
			agentStore.markLastUserMessageError($t('agent.sendError'));
			input = text;
			return;
		}

		try {
			ws.send(JSON.stringify({ type: 'message', prompt: text }));
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

	async function sendFile(file: File) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		const data = new Uint8Array(await file.arrayBuffer());
		ws.send(encodeUploadFrame(file.name, data));
	}

	function handleFileInputChange(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (file) sendFile(file);
		input.value = '';
	}

	function handleAttachClick() {
		fileInputEl?.click();
	}

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		const file = event.dataTransfer?.files?.[0];
		if (file) sendFile(file);
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
	let prevProjectName = $state<string>(projectName);
	$effect(() => {
		if (projectName !== prevProjectName) {
			prevProjectName = projectName;
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
				ws = null;
			}
			banner = { kind: 'none' };
			processing = false;
			lastSessionId = null;
			userScrolledUp = false;
		}
	});

	// Reconnect automatically when the parent points us at a different (existing) session,
	// e.g. the user picked one from SessionList.
	$effect(() => {
		const target = sessionId;
		if (target && target !== $agentStore.sessionId) {
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
			if (ws) {
				clientInitiatedClose = true;
				ws.close();
			}
		};
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
	<div class="flex flex-1 flex-col gap-3">
		{#if banner.kind === 'connectionError'}
			<div
				class="flex items-center justify-between gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300"
			>
				<span>{$t('agent.connectionError')}</span>
				<button
					type="button"
					onclick={handleReconnectClick}
					class="shrink-0 rounded-md bg-amber-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-amber-700"
				>
					{$t('agent.reconnect')}
				</button>
			</div>
		{/if}

		<div
			bind:this={messagesEl}
			onscroll={handleScroll}
			ondragover={handleDragOver}
			ondrop={handleDrop}
			role="log"
			class="flex-1 overflow-y-auto rounded-lg border border-slate-200 p-3 dark:border-slate-800"
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
			<input bind:this={fileInputEl} type="file" class="hidden" onchange={handleFileInputChange} />
			<button
				type="button"
				onclick={handleAttachClick}
				disabled={processing}
				title={$t('agent.attachFile')}
				aria-label={$t('agent.attachFile')}
				class="rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
			>
				📎
			</button>
			<textarea
				bind:this={textareaEl}
				value={input}
				oninput={handleInput}
				onkeydown={handleKeydown}
				disabled={processing}
				rows="1"
				placeholder={$t('agent.inputPlaceholder')}
				class="max-h-40 flex-1 resize-none rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
			></textarea>
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
{/if}
