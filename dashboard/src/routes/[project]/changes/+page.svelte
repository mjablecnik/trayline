<script lang="ts">
	import { page } from '$app/state';
	import { api, type StatusResponse } from '$lib/api';
	import DiffFileSection from '$lib/components/DiffFileSection.svelte';
	import FileStatusBadge from '$lib/components/FileStatusBadge.svelte';
	import { t } from '$lib/i18n';
	import { parseDiff } from '$lib/utils/diff';

	let projectName = $derived(page.params.project ?? '');

	type State =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; result: StatusResponse };

	let changesState = $state<State>({ status: 'loading' });

	async function load(name: string) {
		if (!name) return;
		changesState = { status: 'loading' };
		try {
			const result = await api.getStatus(name);
			changesState = { status: 'loaded', result };
		} catch {
			changesState = { status: 'error' };
		}
	}

	$effect(() => {
		load(projectName);
	});
</script>

{#if changesState.status === 'loading'}
	<div class="flex flex-col gap-2">
		{#each [0, 1, 2] as key (key)}
			<div class="h-10 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
		{/each}
	</div>
{:else if changesState.status === 'error'}
	<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
		<p class="text-sm text-slate-500 dark:text-slate-400">{$t('changes.error')}</p>
		<button
			type="button"
			onclick={() => load(projectName)}
			class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
		>
			{$t('common.retry')}
		</button>
	</div>
{:else if changesState.result.clean}
	<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
		{$t('changes.clean')}
	</p>
{:else}
	{@const summary = changesState.result.summary}
	<div class="flex flex-col gap-4">
		<p class="text-sm text-slate-500 dark:text-slate-400">
			{$t('changes.summary')
				.replace('{files}', String(summary.files_changed))
				.replace('{insertions}', String(summary.insertions))
				.replace('{deletions}', String(summary.deletions))}
		</p>
		<div class="flex flex-col gap-3">
			{#each changesState.result.files as file, i (file.path)}
				{#if file.diff}
					{@const diffFile = parseDiff(file.diff)[0]}
					{#if diffFile}
						<DiffFileSection file={diffFile} initialExpanded={i === 0}>
							{#snippet leading()}
								<FileStatusBadge status={file.status} />
							{/snippet}
						</DiffFileSection>
					{/if}
				{:else}
					<div
						class="flex items-center gap-2 rounded-lg border border-slate-200 px-3 py-2 dark:border-slate-800"
					>
						<FileStatusBadge status={file.status} />
						<span
							class="min-w-0 flex-1 truncate font-mono text-sm text-slate-700 dark:text-slate-200"
							>{file.path}</span
						>
						<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500"
							>{$t('changes.noDiff')}</span
						>
					</div>
				{/if}
			{/each}
		</div>
	</div>
{/if}
