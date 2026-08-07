<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { buildWorkflowLogWsUrl, type WorkflowStatus } from '$lib/api';
	import { ansiToHtml } from '$lib/ansi';
	import { getToken } from '$lib/auth';
	import { t, type TranslationKey } from '$lib/i18n';

	let {
		projectName,
		workflowId,
		status,
		initialLog = '',
		initialTruncated = false,
		initialExitCode = null
	}: {
		projectName: string;
		workflowId: string;
		status: WorkflowStatus;
		initialLog?: string;
		initialTruncated?: boolean;
		initialExitCode?: number | null;
	} = $props();

	const RECONNECT_TIMEOUT_MS = 10000;
	const SCROLL_THRESHOLD = 50;
	const TERMINAL_STATUSES: WorkflowStatus[] = ['completed', 'failed', 'cancelled'];

	const statusLabelKey: Record<WorkflowStatus, TranslationKey> = {
		queued: 'workflows.status.queued',
		running: 'workflows.status.running',
		completed: 'workflows.status.completed',
		failed: 'workflows.status.failed',
		cancelled: 'workflows.status.cancelled'
	};

	type Phase =
		'connecting' | 'waiting' | 'streaming' | 'reconnecting' | 'disconnected' | 'finished';

	// This component is only ever mounted fresh per expand (the parent wraps
	// it in an {#if expandedId === workflow.id} block), so `status` is a
	// one-shot "initial value" by construction — it never needs to resync if
	// the underlying workflow later changes status while collapsed.
	const isTerminal = untrack(() => TERMINAL_STATUSES.includes(status));

	let phase = $state<Phase>(isTerminal ? 'finished' : 'connecting');
	let logText = $state(isTerminal ? untrack(() => initialLog) : '');
	let truncated = $state(isTerminal ? untrack(() => initialTruncated) : false);
	let finalStatus = $state<WorkflowStatus | null>(isTerminal ? untrack(() => status) : null);
	let finalExitCode = $state<number | null>(isTerminal ? untrack(() => initialExitCode) : null);
	let userScrolledUp = $state(false);
	let logEl = $state<HTMLDivElement | undefined>(undefined);

	let ws: WebSocket | null = null;
	let hasReconnected = false;
	let reconnectTimeoutId: number | undefined;
	let clientClosed = false;

	function scrollToBottom() {
		if (logEl) logEl.scrollTop = logEl.scrollHeight;
	}

	function handleScroll() {
		if (!logEl) return;
		userScrolledUp = logEl.scrollTop + logEl.clientHeight < logEl.scrollHeight - SCROLL_THRESHOLD;
	}

	function appendOutput(data: string) {
		logText += data;
		if (!userScrolledUp) tick().then(scrollToBottom);
	}

	function handleMessage(event: MessageEvent) {
		let msg: {
			type: string;
			data?: string;
			status?: WorkflowStatus;
			exit_code?: number;
			truncated?: boolean;
		};
		try {
			msg = JSON.parse(event.data as string);
		} catch {
			return;
		}

		if (phase === 'reconnecting') {
			window.clearTimeout(reconnectTimeoutId);
		}

		switch (msg.type) {
			case 'waiting':
				phase = 'waiting';
				break;
			case 'output':
				phase = 'streaming';
				appendOutput(msg.data ?? '');
				break;
			case 'finished':
				phase = 'finished';
				finalStatus = msg.status ?? null;
				finalExitCode = msg.exit_code ?? null;
				truncated = msg.truncated ?? false;
				break;
			default:
				break;
		}
	}

	function connect() {
		clientClosed = false;
		// Clear accumulated log — the backend always replays the full buffer
		// from the start on each new connection.
		logText = '';
		const socket = new WebSocket(buildWorkflowLogWsUrl(projectName, workflowId));

		socket.onopen = () => {
			const token = getToken();
			if (token) socket.send(JSON.stringify({ type: 'auth', token }));
		};

		socket.onmessage = handleMessage;

		socket.onclose = () => {
			ws = null;
			if (clientClosed || phase === 'finished') return;

			// Only retry a connection that had already started delivering
			// content — a hard failure on the very first attempt (e.g. the
			// workflow no longer exists) isn't worth retrying.
			if (phase === 'connecting' || hasReconnected) {
				phase = 'disconnected';
				return;
			}

			hasReconnected = true;
			phase = 'reconnecting';
			reconnectTimeoutId = window.setTimeout(() => {
				if (phase === 'reconnecting') phase = 'disconnected';
			}, RECONNECT_TIMEOUT_MS);
			connect();
		};

		ws = socket;
	}

	$effect(() => {
		if (isTerminal) return;
		connect();
		return () => {
			clientClosed = true;
			window.clearTimeout(reconnectTimeoutId);
			ws?.close();
			ws = null;
		};
	});

	$effect(() => {
		if (isTerminal) tick().then(scrollToBottom);
	});
</script>

<div class="flex flex-col gap-1">
	{#if truncated}
		<p
			class="rounded-md bg-amber-50 px-2 py-1 text-xs text-amber-700 dark:bg-amber-950 dark:text-amber-300"
		>
			{$t('workflows.logs.truncated')}
		</p>
	{/if}

	<div
		bind:this={logEl}
		onscroll={handleScroll}
		role="log"
		class="max-h-64 overflow-y-auto rounded-md bg-slate-900 p-3 font-mono text-xs whitespace-pre-wrap text-slate-100"
	>
		{#if phase === 'waiting' && !logText}
			{$t('workflows.logs.waiting')}
		{:else}
			{@html ansiToHtml(logText)}
		{/if}
	</div>

	{#if phase === 'reconnecting'}
		<p class="text-xs text-amber-500 dark:text-amber-400">{$t('workflows.logs.reconnecting')}</p>
	{:else if phase === 'disconnected'}
		<p class="text-xs text-red-500 dark:text-red-400">{$t('workflows.logs.disconnected')}</p>
	{/if}

	{#if finalStatus}
		<p class="text-xs text-slate-400 dark:text-slate-500">
			{$t('workflows.logs.finished').replace(
				'{status}',
				$t(statusLabelKey[finalStatus])
			)}{#if finalExitCode !== null}
				· {$t('workflows.exitCode').replace('{code}', String(finalExitCode))}{/if}
		</p>
	{/if}
</div>
