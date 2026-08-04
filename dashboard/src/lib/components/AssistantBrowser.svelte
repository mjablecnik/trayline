<script lang="ts">
	import {
		api,
		ApiError,
		type AssistantFileContentResponse,
		type AssistantFileEntry,
		type GitCommitEntry,
		type GitStatusResponse
	} from '$lib/api';
	import { t, type TranslationKey } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate } from '$lib/utils/date';
	import { formatFileSize } from '$lib/utils/format';
	import { renderMarkdown } from '$lib/utils/markdown';

	let { onStatusChange }: { onStatusChange?: (clean: boolean) => void } = $props();

	type ViewState =
		| { kind: 'directory'; entries: AssistantFileEntry[] }
		| { kind: 'file'; file: AssistantFileContentResponse };

	let currentPath = $state<string[]>([]);
	let view = $state<ViewState | null>(null);
	let commits = $state<GitCommitEntry[]>([]);
	let status = $state<GitStatusResponse | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	const statusDotClass: Record<string, string> = {
		modified: 'bg-amber-500',
		added: 'bg-green-500',
		untracked: 'bg-slate-400',
		deleted: 'bg-red-500'
	};

	const statusLabelKey: Record<string, TranslationKey> = {
		modified: 'changes.status.modified',
		added: 'changes.status.added',
		untracked: 'changes.status.untracked',
		deleted: 'changes.status.deleted'
	};

	function isMarkdown(filename: string): boolean {
		return /\.(md|markdown)$/i.test(filename);
	}

	async function loadPath(segments: string[]) {
		loading = true;
		error = null;
		try {
			const result = await api.getAssistantFiles(segments.join('/') || undefined);
			view =
				'entries' in result
					? { kind: 'directory', entries: result.entries }
					: { kind: 'file', file: result };
			currentPath = segments;
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('assistant.filesError');
		} finally {
			loading = false;
		}
	}

	async function loadStatusAndCommits() {
		try {
			const [statusResult, commitsResult] = await Promise.all([
				api.getAssistantFileStatus(),
				api.getAssistantFileCommits(5)
			]);
			status = statusResult;
			commits = commitsResult;
			onStatusChange?.(statusResult.clean);
		} catch {
			status = null;
			commits = [];
		}
	}

	function refresh() {
		loadPath(currentPath);
		loadStatusAndCommits();
	}

	$effect(() => {
		loadPath([]);
		loadStatusAndCommits();
	});

	function openEntry(name: string) {
		loadPath([...currentPath, name]);
	}

	function openBreadcrumb(depth: number) {
		loadPath(currentPath.slice(0, depth));
	}
</script>

<div class="flex flex-col gap-4">
	<div class="flex items-center justify-between gap-3">
		<nav aria-label="Breadcrumb" class="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
			{#if currentPath.length === 0}
				<span aria-current="page" class="font-medium text-slate-900 dark:text-slate-100">
					{$t('assistant.breadcrumbRoot')}
				</span>
			{:else}
				<button
					type="button"
					onclick={() => openBreadcrumb(0)}
					class="text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
				>
					{$t('assistant.breadcrumbRoot')}
				</button>
			{/if}
			{#each currentPath as segment, i (i)}
				<span class="text-slate-300 dark:text-slate-600" aria-hidden="true">/</span>
				{#if i === currentPath.length - 1}
					<span aria-current="page" class="font-medium text-slate-900 dark:text-slate-100">
						{segment}
					</span>
				{:else}
					<button
						type="button"
						onclick={() => openBreadcrumb(i + 1)}
						class="text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
					>
						{segment}
					</button>
				{/if}
			{/each}
		</nav>
		<button
			type="button"
			onclick={refresh}
			class="shrink-0 rounded-md border border-slate-300 px-2 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
		>
			{$t('assistant.refresh')}
		</button>
	</div>

	{#if loading}
		<div class="flex flex-col gap-2">
			{#each [0, 1, 2] as key (key)}
				<div class="h-8 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if error}
		<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{error}</p>
			<button
				type="button"
				onclick={refresh}
				class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if view?.kind === 'directory'}
		{#if view.entries.length === 0 && currentPath.length === 0}
			<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
				{$t('assistant.filesEmpty')}
			</p>
		{:else}
			<ul class="divide-y divide-slate-200 dark:divide-slate-800">
				{#if currentPath.length > 0}
					<li>
						<button
							type="button"
							onclick={() => openBreadcrumb(currentPath.length - 1)}
							class="flex w-full items-center gap-2 px-2 py-2 text-left text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
						>
							<span aria-hidden="true">📁</span>
							<span class="text-slate-500 dark:text-slate-400">..</span>
						</button>
					</li>
				{/if}
				{#each view.entries as entry (entry.name)}
					<li>
						<button
							type="button"
							onclick={() => openEntry(entry.name)}
							class="flex w-full items-center gap-2 px-2 py-2 text-left text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
						>
							<span class="shrink-0" aria-hidden="true"
								>{entry.type === 'directory' ? '📁' : '📄'}</span
							>
							<span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
								{entry.name}
							</span>
							{#if entry.type === 'file'}
								<span class="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">
									{formatFileSize(entry.size)}
								</span>
							{/if}
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	{:else if view?.kind === 'file'}
		{@const file = view.file}
		<div class="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
			<div
				class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 bg-slate-50 px-3 py-2 dark:border-slate-800 dark:bg-slate-900"
			>
				<span class="truncate font-mono text-sm text-slate-700 dark:text-slate-200"
					>{file.filename}</span
				>
				<span class="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">
					{formatFileSize(file.size)}
				</span>
			</div>
			{#if file.truncated || file.content === null}
				<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
					{$t('files.truncated').replace('{size}', formatFileSize(file.size))}
				</p>
			{:else if file.content === ''}
				<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
					{$t('files.emptyFile')}
				</p>
			{:else if isMarkdown(file.filename)}
				<div class="prose prose-sm dark:prose-invert max-w-none px-3 py-3">
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- renderMarkdown sanitizes its output -->
					{@html renderMarkdown(file.content)}
				</div>
			{:else}
				<pre
					class="overflow-x-auto px-3 py-3 text-sm whitespace-pre-wrap text-slate-700 dark:text-slate-200">{file.content}</pre>
			{/if}
		</div>
	{/if}

	<div class="flex flex-col gap-2">
		<h3 class="text-sm font-medium text-slate-700 dark:text-slate-300">
			{$t('assistant.history')}
		</h3>
		{#if commits.length === 0}
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('assistant.historyEmpty')}</p>
		{:else}
			<ul class="divide-y divide-slate-200 dark:divide-slate-800">
				{#each commits as commit (commit.hash)}
					<li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-2 text-sm">
						<span class="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">
							{commit.short_hash}
						</span>
						<span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
							{commit.message}
						</span>
						<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">
							{formatRelativeDate(commit.date, $locale)}
						</span>
					</li>
				{/each}
			</ul>
		{/if}
	</div>

	{#if status}
		<div class="flex flex-col gap-2">
			{#if status.clean}
				<p class="text-sm text-slate-500 dark:text-slate-400">{$t('assistant.statusClean')}</p>
			{:else}
				<h3 class="text-sm font-medium text-slate-700 dark:text-slate-300">
					{$t('assistant.statusDirty')}
				</h3>
				<ul class="divide-y divide-slate-200 dark:divide-slate-800">
					{#each status.files as file (file.path)}
						<li class="flex items-center gap-2 px-2 py-2 text-sm">
							<span class="inline-flex shrink-0 items-center gap-1.5 text-xs font-medium">
								<span
									class="size-2 rounded-full {statusDotClass[file.status] ?? 'bg-slate-400'}"
									aria-hidden="true"
								></span>
								{statusLabelKey[file.status] ? $t(statusLabelKey[file.status]) : file.status}
							</span>
							<span class="min-w-0 flex-1 truncate font-mono text-slate-700 dark:text-slate-200">
								{file.path}
							</span>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}
</div>
