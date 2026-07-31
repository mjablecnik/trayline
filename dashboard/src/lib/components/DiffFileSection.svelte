<script lang="ts">
	import type { DiffFile, DiffLineType } from '$lib/utils/diff';
	import { t } from '$lib/i18n';

	let { file }: { file: DiffFile } = $props();

	let expanded = $state(true);

	function lineClass(type: DiffLineType): string {
		if (type === 'add') return 'bg-green-50 dark:bg-green-900/20';
		if (type === 'del') return 'bg-red-50 dark:bg-red-900/20';
		return '';
	}

	function marker(type: DiffLineType): string {
		if (type === 'add') return '+';
		if (type === 'del') return '-';
		return ' ';
	}
</script>

<div class="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
	<button
		type="button"
		onclick={() => (expanded = !expanded)}
		class="flex w-full items-center justify-between gap-3 bg-slate-50 px-3 py-2 text-left dark:bg-slate-900"
		class:border-b={expanded}
		class:border-slate-200={expanded}
		class:dark:border-slate-800={expanded}
	>
		<span class="flex min-w-0 items-center gap-2">
			<svg
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				class="size-3.5 shrink-0 text-slate-400 transition-transform dark:text-slate-500"
				class:-rotate-90={!expanded}
				aria-hidden="true"
			>
				<path stroke-linecap="round" stroke-linejoin="round" d="m6 9 6 6 6-6" />
			</svg>
			<span class="truncate font-mono text-sm text-slate-700 dark:text-slate-200">{file.path}</span>
		</span>
		<span class="flex shrink-0 items-center gap-2 font-mono text-xs font-medium">
			<span class="text-green-600 dark:text-green-400">+{file.insertions}</span>
			<span class="text-red-600 dark:text-red-400">-{file.deletions}</span>
		</span>
	</button>

	{#if expanded}
		{#if file.tooLarge}
			<p class="px-3 py-6 text-center text-sm text-slate-500 dark:text-slate-400">
				{$t('diff.tooLarge')}
			</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full border-collapse font-mono text-xs leading-5">
					<tbody>
						{#each file.hunks as hunk (hunk.header)}
							<tr class="bg-slate-100 dark:bg-slate-800/60">
								<td colspan="3" class="px-3 py-1 text-slate-500 dark:text-slate-400"
									>{hunk.header}</td
								>
							</tr>
							{#each hunk.lines as line, i (i)}
								<tr class={lineClass(line.type)}>
									<td
										class="hidden w-10 select-none px-2 text-right text-slate-400 min-[400px]:table-cell dark:text-slate-600"
									>
										{line.oldLineNo ?? ''}
									</td>
									<td
										class="hidden w-10 select-none px-2 text-right text-slate-400 min-[400px]:table-cell dark:text-slate-600"
									>
										{line.newLineNo ?? ''}
									</td>
									<td class="whitespace-pre px-2 text-slate-700 dark:text-slate-300"
										>{marker(line.type)}{line.content}</td
									>
								</tr>
							{/each}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</div>
