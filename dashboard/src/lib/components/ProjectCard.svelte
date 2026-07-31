<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import type { Project } from '$lib/api';
	import { t } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate } from '$lib/utils/date';

	let { project }: { project: Project } = $props();

	function handleClick() {
		goto(resolve('/[project]', { project: project.name }));
	}
</script>

<button
	type="button"
	onclick={handleClick}
	class="flex w-full cursor-pointer flex-col gap-2 rounded-lg border border-slate-200 bg-white p-4 text-left transition-all hover:-translate-y-0.5 hover:border-slate-300 hover:shadow-md dark:border-slate-800 dark:bg-slate-900 dark:hover:border-slate-700"
>
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
