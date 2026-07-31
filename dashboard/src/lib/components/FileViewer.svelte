<script lang="ts">
	import { highlightCode } from '$lib/highlight';
	import { t } from '$lib/i18n';
	import { formatFileSize } from '$lib/utils/format';

	let {
		path,
		content,
		language,
		size,
		binary = false,
		truncated = false
	}: {
		path: string;
		content: string | null;
		language?: string;
		size: number;
		binary?: boolean;
		truncated?: boolean;
	} = $props();

	let filename = $derived(path.split('/').filter(Boolean).pop() ?? path);
	let lines = $derived(content ? content.split('\n') : []);

	let highlighted = $state<string | null>(null);

	$effect(() => {
		if (!content) {
			highlighted = null;
			return;
		}
		let cancelled = false;
		highlighted = null;
		highlightCode(content, language).then((html) => {
			if (!cancelled) highlighted = html;
		});
		return () => {
			cancelled = true;
		};
	});

	function handleRaw() {
		if (content === null) return;
		const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		window.open(url, '_blank', 'noopener');
		setTimeout(() => URL.revokeObjectURL(url), 60_000);
	}
</script>

<div class="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
	<div
		class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 bg-slate-50 px-3 py-2 dark:border-slate-800 dark:bg-slate-900"
	>
		<div class="flex min-w-0 items-center gap-2">
			<span class="truncate font-mono text-sm text-slate-700 dark:text-slate-200">{filename}</span>
			{#if language}
				<span
					class="shrink-0 rounded bg-slate-200 px-1.5 py-0.5 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300"
				>
					{language}
				</span>
			{/if}
		</div>
		<div class="flex shrink-0 items-center gap-3">
			<span class="font-mono text-xs text-slate-400 dark:text-slate-500">
				{formatFileSize(size)}
			</span>
			{#if content !== null}
				<button
					type="button"
					onclick={handleRaw}
					class="rounded-md border border-slate-300 px-2 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
				>
					{$t('files.raw')}
				</button>
			{/if}
		</div>
	</div>

	{#if binary}
		<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('files.binary').replace('{size}', formatFileSize(size))}
		</p>
	{:else if truncated}
		<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('files.truncated').replace('{size}', formatFileSize(size))}
		</p>
	{:else if content === ''}
		<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('files.emptyFile')}
		</p>
	{:else}
		<div class="code-viewer overflow-x-auto text-sm">
			{#if highlighted}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- Shiki HTML-escapes the source text it wraps in syntax spans -->
				{@html highlighted}
			{:else}
				<pre class="bg-[#24292e] text-[#e1e4e8]"><code
						>{#each lines as line, i (i)}<span class="line">{line}</span>{i < lines.length - 1
								? '\n'
								: ''}{/each}</code
					></pre>
			{/if}
		</div>
	{/if}
</div>

<style>
	.code-viewer :global(pre) {
		margin: 0;
		padding: 0.75rem 1rem;
		font-family: var(--font-mono);
		line-height: 1.6;
	}

	.code-viewer :global(.line::before) {
		counter-increment: line;
		content: counter(line);
		display: inline-block;
		width: 2.5rem;
		margin-right: 1rem;
		text-align: right;
		color: rgb(100 116 139);
		user-select: none;
	}

	.code-viewer :global(code) {
		counter-reset: line;
	}
</style>
