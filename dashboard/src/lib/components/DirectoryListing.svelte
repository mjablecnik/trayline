<script lang="ts">
	import { resolve } from '$app/paths';
	import type { TreeEntry } from '$lib/api';
	import { t } from '$lib/i18n';
	import { formatFileSize } from '$lib/utils/format';

	let {
		project,
		ref,
		path,
		entries
	}: { project: string; ref: string; path: string; entries: TreeEntry[] } = $props();

	let refParam = $derived(encodeURIComponent(ref));

	function childPath(name: string): string {
		return path ? `${path}/${name}` : name;
	}
</script>

{#if entries.length === 0}
	<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
		{$t('files.emptyDir')}
	</p>
{:else}
	<ul class="divide-y divide-slate-200 dark:divide-slate-800">
		{#each entries as entry (entry.name)}
			<li>
				<a
					href={resolve(`/[project]/tree/[...path]?ref=${refParam}`, {
						project,
						path: childPath(entry.name)
					})}
					class="flex items-center gap-2 px-2 py-2 text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
				>
					<span class="shrink-0" aria-hidden="true">{entry.type === 'directory' ? '📁' : '📄'}</span
					>
					<span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
						{entry.name}
					</span>
					{#if entry.type === 'file'}
						<span class="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">
							{formatFileSize(entry.size)}
						</span>
					{/if}
				</a>
			</li>
		{/each}
	</ul>
{/if}
