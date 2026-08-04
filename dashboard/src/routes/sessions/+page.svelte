<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, type AgentSession } from '$lib/api';
	import { locale, t } from '$lib/i18n';
	import { formatRelativeDate } from '$lib/utils/date';
	import { groupSessionsByProject } from '$lib/utils/sessions';

	type LoadState =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; sessions: AgentSession[] };

	let loadState = $state<LoadState>({ status: 'loading' });
	let terminatingId = $state<string | null>(null);

	async function load() {
		loadState = { status: 'loading' };
		try {
			const sessions = await api.getSessions();
			loadState = { status: 'loaded', sessions };
		} catch {
			loadState = { status: 'error' };
		}
	}

	$effect(() => {
		load();
	});

	let groups = $derived(
		loadState.status === 'loaded' ? groupSessionsByProject(loadState.sessions) : []
	);

	async function handleTerminate(session: AgentSession) {
		terminatingId = session.session_id;
		try {
			await api.terminateSession(session.session_id);
			await load();
		} finally {
			terminatingId = null;
		}
	}

	const skeletonKeys = [0, 1, 2];
</script>

<div class="mx-auto flex w-full max-w-6xl flex-1 flex-col gap-4 px-4 py-6">
	<div class="flex shrink-0 items-center justify-between">
		<h1 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
			{$t('sessions.title')}
		</h1>
		<button
			type="button"
			onclick={load}
			disabled={loadState.status === 'loading'}
			class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
		>
			⟳ {$t('sessions.refresh')}
		</button>
	</div>

	{#if loadState.status === 'loading'}
		<div class="flex flex-col gap-3">
			{#each skeletonKeys as key (key)}
				<div class="h-24 animate-pulse rounded-lg bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if loadState.status === 'error'}
		<div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
			<p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{$t('sessions.error')}</p>
			<button
				type="button"
				onclick={load}
				class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if groups.length === 0}
		<div class="flex flex-1 flex-col items-center justify-center gap-3 text-center">
			<svg
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="1.5"
				class="size-10 text-slate-300 dark:text-slate-700"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8-1.06 0-2.077-.163-3.02-.463L3 21l1.5-4.5C3.55 15.15 3 13.62 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8Z"
				/>
			</svg>
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('sessions.empty')}</p>
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#each groups as group (group.project ?? '')}
				<section class="flex flex-col gap-2">
					<div class="flex items-center justify-between">
						<h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100">
							{#if group.project}
								<a href={resolve('/[project]', { project: group.project })} class="hover:underline">
									{group.project}
								</a>
							{:else}
								{$t('sessions.noProject')}
							{/if}
						</h2>
						<span class="text-xs text-slate-500 dark:text-slate-400">{group.sessions.length}</span>
					</div>

					<ul
						class="flex flex-col gap-1 rounded-lg border border-slate-200 p-2 dark:border-slate-800"
					>
						{#each group.sessions as session (session.session_id)}
							<li
								class="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-slate-50 dark:hover:bg-slate-800/50"
							>
								<div class="flex min-w-0 flex-1 flex-col">
									<span class="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
										{session.agent}{#if session.model}<span
												class="font-normal text-slate-500 dark:text-slate-400"
											>
												/ {session.model}</span
											>{/if}
									</span>
									<span class="text-xs text-slate-500 dark:text-slate-400">
										{formatRelativeDate(session.last_message_at, $locale)}
									</span>
								</div>
								{#if session.project}
									<a
										href={resolve(
											`/[project]/agent?session=${encodeURIComponent(session.session_id)}`,
											{ project: session.project }
										)}
										class="shrink-0 rounded-md bg-sky-500 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-sky-600"
									>
										{$t('sessions.switch')}
									</a>
								{/if}
								<button
									type="button"
									onclick={() => handleTerminate(session)}
									disabled={terminatingId === session.session_id}
									aria-label={$t('agent.terminate')}
									class="shrink-0 rounded p-1 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-950 dark:hover:text-red-400"
								>
									✕
								</button>
							</li>
						{/each}
					</ul>
				</section>
			{/each}
		</div>
	{/if}
</div>
