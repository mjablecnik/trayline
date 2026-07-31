<script lang="ts">
	import { resolve } from '$app/paths';
	import type { CommitLogEntry } from '$lib/api';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate } from '$lib/utils/date';

	let { project, commit }: { project: string; commit: CommitLogEntry } = $props();
</script>

<a
	href={resolve('/[project]/commits/[hash]', { project, hash: commit.hash })}
	class="flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-2 text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
>
	<span class="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">
		{commit.short_hash}
	</span>
	<span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
		{commit.message}
	</span>
	<span class="shrink-0 truncate text-slate-500 dark:text-slate-400">{commit.author}</span>
	<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">
		{formatRelativeDate(commit.date, $locale)}
	</span>
</a>
