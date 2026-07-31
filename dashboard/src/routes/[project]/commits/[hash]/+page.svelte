<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api, type CommitDetail } from '$lib/api';
	import DiffViewer from '$lib/components/DiffViewer.svelte';
	import { t } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { project } from '$lib/stores/project';
	import { formatRelativeDate } from '$lib/utils/date';

	let projectName = $derived(page.params.project ?? '');
	let hash = $derived(page.params.hash ?? '');

	type State =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; commit: CommitDetail };

	let state = $state<State>({ status: 'loading' });

	async function load(name: string, commitHash: string) {
		if (!name || !commitHash) return;
		state = { status: 'loading' };
		try {
			const commit = await api.getCommit(name, commitHash);
			state = { status: 'loaded', commit };
		} catch {
			state = { status: 'error' };
		}
	}

	$effect(() => {
		load(projectName, hash);
	});
</script>

<div class="flex flex-col gap-4">
	<a
		href={resolve(`/[project]/commits?ref=${encodeURIComponent($project.ref)}`, {
			project: projectName
		})}
		class="inline-flex w-fit items-center gap-1.5 text-sm text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
	>
		<svg
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			class="size-4"
			aria-hidden="true"
		>
			<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
		</svg>
		{$t('commits.detail.back')}
	</a>

	{#if state.status === 'loading'}
		<div class="flex flex-col gap-2">
			<div class="h-6 w-2/3 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			<div class="h-4 w-1/3 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
		</div>
	{:else if state.status === 'error'}
		<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('commits.detail.error')}</p>
			<button
				type="button"
				onclick={() => load(projectName, hash)}
				class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else}
		<div class="flex flex-col gap-1 border-b border-slate-200 pb-4 dark:border-slate-800">
			<h1 class="text-lg font-medium text-slate-900 dark:text-slate-100">
				{state.commit.message}
			</h1>
			<p class="flex flex-wrap items-center gap-x-2 text-sm text-slate-500 dark:text-slate-400">
				<span>{state.commit.author}</span>
				<span aria-hidden="true">•</span>
				<span>{formatRelativeDate(state.commit.date, $locale)}</span>
				<span aria-hidden="true">•</span>
				<span
					>{$t('commits.detail.files').replace('{count}', String(state.commit.files_changed))}</span
				>
				<span class="font-mono text-green-600 dark:text-green-400">+{state.commit.insertions}</span>
				<span class="font-mono text-red-600 dark:text-red-400">-{state.commit.deletions}</span>
			</p>
		</div>

		<DiffViewer diff={state.commit.diff} />
	{/if}
</div>
