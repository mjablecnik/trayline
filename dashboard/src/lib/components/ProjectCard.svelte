<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, type Project } from '$lib/api';
	import { t } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate } from '$lib/utils/date';

	let { project, onPinToggle }: { project: Project; onPinToggle?: () => void } = $props();

	function handleClick() {
		goto(resolve('/[project]', { project: project.name }));
	}

	async function handlePinToggle(e: MouseEvent) {
		e.stopPropagation();
		try {
			if (project.pinned) {
				await api.unpinProject(project.name);
			} else {
				await api.pinProject(project.name);
			}
			onPinToggle?.();
		} catch {
			// Silently fail — next reload will correct
		}
	}
</script>

<button
	type="button"
	onclick={handleClick}
	class="group relative flex w-full cursor-pointer flex-col gap-2 rounded-lg border border-slate-200 bg-white p-4 text-left transition-all hover:-translate-y-0.5 hover:border-slate-300 hover:shadow-md dark:border-slate-800 dark:bg-slate-900 dark:hover:border-slate-700"
>
	<button
		type="button"
		onclick={handlePinToggle}
		class="absolute top-2 right-2 rounded p-1.5 transition-colors hover:bg-slate-100 dark:hover:bg-slate-800 {project.pinned
			? 'text-sky-500'
			: 'text-slate-300 opacity-0 group-hover:opacity-100 dark:text-slate-600'}"
		title={project.pinned ? $t('projects.unpin') : $t('projects.pin')}
		aria-label={project.pinned ? $t('projects.unpin') : $t('projects.pin')}
	>
		<svg viewBox="0 0 24 24" fill="currentColor" class="size-4" aria-hidden="true">
			{#if project.pinned}
				<path
					d="M16 2a1 1 0 0 1 .8.4l3 4a1 1 0 0 1-.1 1.3l-2.4 2.4.7 4.2a1 1 0 0 1-.5 1l-3.2 1.8-1.3 5.5a1 1 0 0 1-1.9.1L10 17.5l-4.3 4.3a1 1 0 0 1-1.4-1.4L8.5 16l-5.2-2.1a1 1 0 0 1 .1-1.9l5.5-1.3 1.8-3.2a1 1 0 0 1 1-.5l4.2.7 2.4-2.4a1 1 0 0 1 .5-.3H16Z"
				/>
			{:else}
				<path
					d="M16 2a1 1 0 0 1 .8.4l3 4a1 1 0 0 1-.1 1.3l-2.4 2.4.7 4.2a1 1 0 0 1-.5 1l-3.2 1.8-1.3 5.5a1 1 0 0 1-1.9.1L10 17.5l-4.3 4.3a1 1 0 0 1-1.4-1.4L8.5 16l-5.2-2.1a1 1 0 0 1 .1-1.9l5.5-1.3 1.8-3.2a1 1 0 0 1 1-.5l4.2.7 2.4-2.4a1 1 0 0 1 .5-.3H16Zm-.5 2.7L13 7.2a1 1 0 0 1-.6.3l-3.8-.6-1.4 2.5a1 1 0 0 1-.4.4l-3.9.9 3.6 1.5a1 1 0 0 1 .5.5l1.5 3.6.9-3.9a1 1 0 0 1 .4-.4l2.5-1.4-.6-3.8a1 1 0 0 1 .3-.6l2.5-2.5-1.6-2Z"
				/>
			{/if}
		</svg>
	</button>

	<div class="flex items-center gap-2">
		<svg
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			class="size-4 shrink-0 text-slate-400 dark:text-slate-500"
			aria-hidden="true"
		>
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"
			/>
		</svg>
		<h2 class="truncate font-medium text-slate-900 dark:text-slate-100">{project.name}</h2>
	</div>

	<p class="text-sm text-slate-500 dark:text-slate-400">
		<span class="font-mono">{project.branch}</span>
		{#if project.last_commit}
			• {formatRelativeDate(project.last_commit.date, $locale)}
		{/if}
	</p>

	{#if project.last_commit}
		<p class="truncate text-sm text-slate-600 dark:text-slate-300">
			{project.last_commit.message}
		</p>
	{/if}

	{#if project.has_uncommitted_changes}
		<div class="flex items-center justify-end gap-1.5 text-xs text-amber-600 dark:text-amber-400">
			<span class="size-2 rounded-full bg-amber-500" aria-hidden="true"></span>
			{$t('projects.uncommittedChanges')}
		</div>
	{/if}
</button>
