<script lang="ts">
	import { api, type Project } from '$lib/api';
	import ProjectCard from '$lib/components/ProjectCard.svelte';
	import TokenEntry from '$lib/components/TokenEntry.svelte';
	import { t } from '$lib/i18n';
	import { isAuthenticated } from '$lib/stores/auth';

	type State =
		| { status: 'loading' }
		| { status: 'error' }
		| { status: 'loaded'; projects: Project[] };

	let state = $state<State>({ status: 'loading' });
	let showOthers = $state(false);

	const pinnedProjects = $derived(
		state.status === 'loaded' ? state.projects.filter((p) => p.pinned) : []
	);
	const otherProjects = $derived(
		state.status === 'loaded' ? state.projects.filter((p) => !p.pinned) : []
	);
	const hasBothSections = $derived(pinnedProjects.length > 0 && otherProjects.length > 0);

	async function loadProjects() {
		state = { status: 'loading' };
		try {
			const projects = await api.getProjects();
			state = { status: 'loaded', projects };
		} catch {
			state = { status: 'error' };
		}
	}

	function handlePinToggle() {
		loadProjects();
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
			<!-- Pinned projects section -->
			{#if pinnedProjects.length > 0}
				{#if hasBothSections}
					<h2
						class="mb-3 flex items-center gap-2 text-sm font-medium text-slate-500 dark:text-slate-400"
					>
						<svg viewBox="0 0 24 24" fill="currentColor" class="size-3.5 text-sky-500" aria-hidden="true">
							<path
								d="M16 2a1 1 0 0 1 .8.4l3 4a1 1 0 0 1-.1 1.3l-2.4 2.4.7 4.2a1 1 0 0 1-.5 1l-3.2 1.8-1.3 5.5a1 1 0 0 1-1.9.1L10 17.5l-4.3 4.3a1 1 0 0 1-1.4-1.4L8.5 16l-5.2-2.1a1 1 0 0 1 .1-1.9l5.5-1.3 1.8-3.2a1 1 0 0 1 1-.5l4.2.7 2.4-2.4a1 1 0 0 1 .5-.3H16Z"
							/>
						</svg>
						{$t('projects.pinned')}
					</h2>
				{/if}
				<div class={gridClass}>
					{#each pinnedProjects as project (project.name)}
						<ProjectCard {project} onPinToggle={handlePinToggle} />
					{/each}
				</div>
			{/if}

			<!-- Other projects section -->
			{#if otherProjects.length > 0}
				{#if hasBothSections}
					<div class="mt-8">
						<button
							type="button"
							onclick={() => (showOthers = !showOthers)}
							class="mb-3 flex cursor-pointer items-center gap-2 text-sm font-medium text-slate-500 transition-colors hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200"
						>
							<svg
								viewBox="0 0 24 24"
								fill="none"
								stroke="currentColor"
								stroke-width="2"
								class="size-3.5 transition-transform {showOthers ? 'rotate-90' : ''}"
								aria-hidden="true"
							>
								<path stroke-linecap="round" stroke-linejoin="round" d="m9 5 7 7-7 7" />
							</svg>
							{showOthers ? $t('projects.hideOthers') : $t('projects.showOthers')}
							<span class="text-slate-400 dark:text-slate-500">({otherProjects.length})</span>
						</button>

						{#if showOthers}
							<div class={gridClass}>
								{#each otherProjects as project (project.name)}
									<ProjectCard {project} onPinToggle={handlePinToggle} />
								{/each}
							</div>
						{/if}
					</div>
				{:else}
					<!-- No pinned projects — show all directly -->
					<div class={gridClass}>
						{#each otherProjects as project (project.name)}
							<ProjectCard {project} onPinToggle={handlePinToggle} />
						{/each}
					</div>
				{/if}
			{/if}
		{/if}
	</div>
{/if}
