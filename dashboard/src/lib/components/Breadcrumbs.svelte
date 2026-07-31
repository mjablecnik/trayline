<script lang="ts">
	import { resolve } from '$app/paths';

	let { project, ref, path }: { project: string; ref: string; path: string } = $props();

	let refParam = $derived(encodeURIComponent(ref));

	type Crumb = { name: string; path: string };

	let segments = $derived(
		path
			.split('/')
			.map((s) => s.trim())
			.filter((s) => s.length > 0)
	);

	let crumbs = $derived<Crumb[]>([
		{ name: project, path: '' },
		...segments.map((name, i) => ({ name, path: segments.slice(0, i + 1).join('/') }))
	]);
</script>

<nav aria-label="Breadcrumb" class="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
	{#each crumbs as crumb, i (crumb.path)}
		{#if i > 0}
			<span class="text-slate-300 dark:text-slate-600" aria-hidden="true">/</span>
		{/if}
		{#if i === crumbs.length - 1}
			<span aria-current="page" class="font-medium text-slate-900 dark:text-slate-100">
				{crumb.name}
			</span>
		{:else}
			<a
				href={resolve(`/[project]/tree/[...path]?ref=${refParam}`, {
					project,
					path: crumb.path
				})}
				class="text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
			>
				{crumb.name}
			</a>
		{/if}
	{/each}
</nav>
