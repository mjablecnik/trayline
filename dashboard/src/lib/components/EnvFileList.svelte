<script lang="ts">
	import { t } from '$lib/i18n';
	import type { EnvFile } from '$lib/api';

	let {
		groups,
		active,
		modified,
		onSelect
	}: {
		groups: { dir: string; files: EnvFile[] }[];
		active: string;
		modified: Set<string>;
		onSelect: (path: string) => void;
	} = $props();

	function basename(path: string): string {
		const idx = path.lastIndexOf('/');
		return idx === -1 ? path : path.slice(idx + 1);
	}
</script>

<nav aria-label={$t('env.files')} class="flex flex-col gap-3">
	{#each groups as group (group.dir)}
		<div class="flex flex-col gap-0.5">
			{#if group.dir}
				<p
					class="truncate px-2 text-xs font-medium text-slate-400 dark:text-slate-500"
					title={group.dir}
				>
					{group.dir}/
				</p>
			{/if}
			{#each group.files as file (file.path)}
				<button
					type="button"
					onclick={() => onSelect(file.path)}
					aria-current={file.path === active ? 'true' : undefined}
					class="relative flex items-center gap-1.5 rounded-md px-2 py-1.5 text-left font-mono text-sm transition-colors {file.path ===
					active
						? 'bg-sky-50 text-sky-700 dark:bg-sky-950 dark:text-sky-300'
						: 'text-slate-700 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-800/50'}"
				>
					<span class="min-w-0 flex-1 truncate">{basename(file.path)}</span>
					{#if modified.has(file.path)}
						<span
							class="size-1.5 shrink-0 rounded-full bg-sky-500"
							aria-hidden="true"
							title={$t('env.unsavedChanges')}
						></span>
					{/if}
				</button>
			{/each}
		</div>
	{/each}
</nav>
