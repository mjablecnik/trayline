<script lang="ts">
	import { t } from '$lib/i18n';

	let {
		branches,
		value,
		onSelect
	}: { branches: string[]; value: string; onSelect: (branch: string) => void } = $props();

	function handleChange(event: Event) {
		onSelect((event.target as HTMLSelectElement).value);
	}
</script>

<div class="relative inline-flex shrink-0 items-center">
	<select
		{value}
		onchange={handleChange}
		aria-label={$t('projects.branch')}
		class="cursor-pointer appearance-none rounded-full border border-slate-300 bg-slate-50 py-1 pr-7 pl-3 font-mono text-sm text-slate-700 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
	>
		{#each branches as branch (branch)}
			<option value={branch}>{branch}</option>
		{/each}
	</select>
	<svg
		viewBox="0 0 24 24"
		fill="none"
		stroke="currentColor"
		stroke-width="2"
		class="pointer-events-none absolute right-2 size-3.5 text-slate-400"
		aria-hidden="true"
	>
		<path stroke-linecap="round" stroke-linejoin="round" d="m6 9 6 6 6-6" />
	</svg>
</div>
