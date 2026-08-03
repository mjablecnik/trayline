<script lang="ts">
	import { api, type AgentSession } from '$lib/api';
	import { locale, t } from '$lib/i18n';
	import { formatRelativeDate } from '$lib/utils/date';

	let {
		projectName,
		activeSessionId,
		onSelect,
		onTerminate,
		onNewSession
	}: {
		projectName: string;
		activeSessionId: string | null;
		onSelect: (sessionId: string) => void;
		onTerminate: (sessionId: string) => void;
		onNewSession: () => void;
	} = $props();

	type ListState =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; sessions: AgentSession[] };

	let listState = $state<ListState>({ status: 'loading' });
	let terminatingId = $state<string | null>(null);

	async function load() {
		listState = { status: 'loading' };
		try {
			const sessions = await api.getProjectSessions(projectName);
			listState = { status: 'loaded', sessions };
		} catch {
			listState = { status: 'error' };
		}
	}

	export function refresh() {
		load();
	}

	$effect(() => {
		// Re-run whenever projectName changes (navigated to a different project)
		void projectName;
		load();
	});

	function handleSelect(sessionId: string) {
		if (sessionId === activeSessionId) return;
		onSelect(sessionId);
	}

	async function handleTerminate(sessionId: string) {
		terminatingId = sessionId;
		try {
			await api.terminateProjectSession(projectName, sessionId);
			await load();
			onTerminate(sessionId);
		} catch {
			terminatingId = null;
		}
	}
</script>

<div class="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 dark:border-slate-800">
	{#if listState.status === 'loading'}
		<div class="flex flex-col gap-2">
			{#each [0, 1] as row (row)}
				<div class="h-12 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if listState.status === 'error'}
		<div class="flex flex-col items-center gap-2 py-4 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('agent.sessionsError')}</p>
			<button
				type="button"
				onclick={load}
				class="rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if listState.sessions.length === 0}
		<p class="py-4 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('agent.noSessions')}
		</p>
	{:else}
		<ul class="flex flex-col gap-1">
			{#each listState.sessions as session (session.session_id)}
				<li>
					<div
						class="flex items-center gap-2 rounded-md px-2 py-1.5 {session.session_id ===
						activeSessionId
							? 'bg-sky-50 dark:bg-sky-950'
							: 'hover:bg-slate-50 dark:hover:bg-slate-800/50'}"
					>
						<button
							type="button"
							onclick={() => handleSelect(session.session_id)}
							class="flex min-w-0 flex-1 flex-col items-start text-left"
						>
							<span
								class="truncate text-sm font-medium {session.session_id === activeSessionId
									? 'text-sky-700 dark:text-sky-300'
									: 'text-slate-900 dark:text-slate-100'}"
							>
								{session.agent}{#if session.model}<span
										class="font-normal text-slate-500 dark:text-slate-400"
									>
										/ {session.model}</span
									>{/if}
							</span>
							<span class="text-xs text-slate-500 dark:text-slate-400">
								{formatRelativeDate(session.last_message_at, $locale)}
							</span>
						</button>
						<button
							type="button"
							onclick={() => handleTerminate(session.session_id)}
							disabled={terminatingId === session.session_id}
							aria-label={$t('agent.terminate')}
							class="shrink-0 rounded p-1 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-950 dark:hover:text-red-400"
						>
							✕
						</button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}

	<button
		type="button"
		onclick={onNewSession}
		class="rounded-md border border-dashed border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:border-sky-400 hover:text-sky-600 dark:border-slate-700 dark:text-slate-400 dark:hover:border-sky-500 dark:hover:text-sky-400"
	>
		+ {$t('agent.newSession')}
	</button>
</div>
