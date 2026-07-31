<script lang="ts">
	let {
		filenames,
		active,
		modified,
		onSelect
	}: {
		filenames: string[];
		active: string;
		modified: Set<string>;
		onSelect: (filename: string) => void;
	} = $props();
</script>

<div
	role="tablist"
	aria-label="Environment files"
	class="flex gap-1 overflow-x-auto border-b border-slate-200 dark:border-slate-800"
>
	{#each filenames as filename (filename)}
		<button
			type="button"
			role="tab"
			aria-selected={filename === active}
			onclick={() => onSelect(filename)}
			class="relative shrink-0 border-b-2 px-3 py-2 font-mono text-sm font-medium transition-colors {filename ===
			active
				? 'border-sky-500 text-sky-600 dark:text-sky-400'
				: 'border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'}"
		>
			{filename}
			{#if modified.has(filename)}
				<span class="absolute top-1.5 right-1 size-1.5 rounded-full bg-sky-500" aria-hidden="true"
				></span>
			{/if}
		</button>
	{/each}
</div>
