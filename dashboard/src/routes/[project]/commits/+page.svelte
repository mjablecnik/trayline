<script lang="ts">
	import { page } from '$app/state';
	import { api, type CommitLogEntry } from '$lib/api';
	import CommitRow from '$lib/components/CommitRow.svelte';
	import { t } from '$lib/i18n';
	import { project } from '$lib/stores/project';

	const LIMIT = 50;

	let projectName = $derived(page.params.project ?? '');

	type State =
		| { status: 'loading' }
		| { status: 'error' }
		| { status: 'loaded'; commits: CommitLogEntry[]; hasMore: boolean; offset: number };

	let commitsState = $state<State>({ status: 'loading' });
	let loadingMore = $state(false);

	async function load(name: string, ref: string) {
		if (!name || !ref) return;
		commitsState = { status: 'loading' };
		try {
			const res = await api.getCommits(name, ref, LIMIT, 0);
			commitsState = {
				status: 'loaded',
				commits: res.commits,
				hasMore: res.has_more,
				offset: LIMIT
			};
		} catch {
			commitsState = { status: 'error' };
		}
	}

	async function loadMore() {
		if (commitsState.status !== 'loaded' || loadingMore) return;
		const { commits: prevCommits, offset } = commitsState;
		loadingMore = true;
		try {
			const res = await api.getCommits(projectName, $project.ref, LIMIT, offset);
			commitsState = {
				status: 'loaded',
				commits: [...prevCommits, ...res.commits],
				hasMore: res.has_more,
				offset: offset + LIMIT
			};
		} finally {
			loadingMore = false;
		}
	}

	$effect(() => {
		load(projectName, $project.ref);
	});
</script>

{#if commitsState.status === 'loading'}
	<div class="flex flex-col gap-2">
		{#each [0, 1, 2, 3, 4, 5] as key (key)}
			<div class="h-10 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
		{/each}
	</div>
{:else if commitsState.status === 'error'}
	<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
		<p class="text-sm text-slate-500 dark:text-slate-400">{$t('commits.error')}</p>
		<button
			type="button"
			onclick={() => load(projectName, $project.ref)}
			class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
		>
			{$t('common.retry')}
		</button>
	</div>
{:else if commitsState.commits.length === 0}
	<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
		{$t('commits.empty')}
	</p>
{:else}
	<div class="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
		{#each commitsState.commits as commit (commit.hash)}
			<CommitRow project={projectName} {commit} />
		{/each}
	</div>
	{#if commitsState.hasMore}
		<div class="mt-4 flex justify-center">
			<button
				type="button"
				onclick={loadMore}
				disabled={loadingMore}
				class="rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
			>
				{$t('commits.loadMore')}
			</button>
		</div>
	{/if}
{/if}
