<script lang="ts">
	import { api, type Project } from '$lib/api';
	import ProjectCard from '$lib/components/ProjectCard.svelte';
	import TokenEntry from '$lib/components/TokenEntry.svelte';
	import { t } from '$lib/i18n';
	import { isAuthenticated } from '$lib/stores/auth';

	type State =
		{ status: 'loading' } | { status: 'error' } | { status: 'loaded'; projects: Project[] };

	let state = $state<State>({ status: 'loading' });

	async function loadProjects() {
		state = { status: 'loading' };
		try {
			const projects = await api.getProjects();
			state = { status: 'loaded', projects };
		} catch {
			state = { status: 'error' };
		}
	}

	$effect(() => {
		if ($isAuthenticated) loadProjects();
	});

	const gridClass = 'grid grid-cols-1 gap-4 tablet:grid-cols-2 desktop:grid-cols-3';
	const skeletonKeys = [0, 1, 2, 3, 4, 5];
</script>

{#if !$isAuthenticated}
	<TokenEntry />
{:else}
	<div class="mx-auto flex w-full max-w-6xl flex-1 flex-col px-4 py-6">
		{#if state.status === 'loading'}
			<div class={gridClass}>
				{#each skeletonKeys as key (key)}
					<div class="h-32 animate-pulse rounded-lg bg-slate-200 dark:bg-slate-800"></div>
				{/each}
			</div>
		{:else if state.status === 'error'}
			<div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
				<p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{$t('projects.error')}</p>
				<button
					type="button"
					onclick={loadProjects}
					class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
				>
					{$t('common.retry')}
				</button>
			</div>
		{:else if state.projects.length === 0}
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
						d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"
					/>
				</svg>
				<p class="text-sm text-slate-500 dark:text-slate-400">{$t('projects.empty')}</p>
			</div>
		{:else}
			<div class={gridClass}>
				{#each state.projects as project (project.name)}
					<ProjectCard {project} />
				{/each}
			</div>
		{/if}
	</div>
{/if}
