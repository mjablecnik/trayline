<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import BranchSelector from '$lib/components/BranchSelector.svelte';
	import ProjectHeader from '$lib/components/ProjectHeader.svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import { t } from '$lib/i18n';
	import { project } from '$lib/stores/project';

	let { children } = $props();

	let projectName = $derived(page.params.project ?? '');

	type State = { status: 'loading' } | { status: 'error' } | { status: 'loaded' };

	let state = $state<State>({ status: 'loading' });

	async function load(name: string) {
		state = { status: 'loading' };
		project.reset();
		try {
			const detail = await api.getProject(name);
			project.setDetail(detail, page.url.searchParams.get('ref') ?? undefined);
			state = { status: 'loaded' };
		} catch {
			state = { status: 'error' };
		}
	}

	$effect(() => {
		load(projectName);
	});

	function handleBranchSelect(branch: string) {
		project.setRef(branch);
		const url = new URL(window.location.href);
		url.searchParams.set('ref', branch);
		window.history.replaceState(window.history.state, '', url);
	}
</script>

<div class="mx-auto flex w-full max-w-6xl flex-1 flex-col px-4 py-6">
	{#if state.status === 'loading'}
		<div class="h-8 w-48 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
	{:else if state.status === 'error'}
		<div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
			<p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{$t('project.error')}</p>
			<button
				type="button"
				onclick={() => load(projectName)}
				class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if $project.detail}
		<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
			<ProjectHeader name={$project.detail.name} />
			<BranchSelector
				branches={$project.detail.branches}
				value={$project.ref}
				onSelect={handleBranchSelect}
			/>
		</div>

		<div class="mb-6">
			<TabBar project={projectName} ref={$project.ref} />
		</div>

		{@render children()}
	{/if}
</div>
