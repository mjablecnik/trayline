<script lang="ts">
	import { page } from '$app/state';
	import { ApiError, api, type BlobResponse, type TreeResponse } from '$lib/api';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import DirectoryListing from '$lib/components/DirectoryListing.svelte';
	import FileViewer from '$lib/components/FileViewer.svelte';
	import { t } from '$lib/i18n';
	import { project } from '$lib/stores/project';

	let projectName = $derived(page.params.project ?? '');
	let path = $derived(page.params.path ?? '');

	type State =
		| { status: 'loading' }
		| { status: 'directory'; data: TreeResponse }
		| { status: 'file'; data: BlobResponse }
		| { status: 'notfound' }
		| { status: 'error' };

	let state = $state<State>({ status: 'loading' });

	async function load(name: string, ref: string, treePath: string) {
		if (!name || !ref) return;
		state = { status: 'loading' };
		try {
			const tree = await api.getTree(name, ref, treePath);
			state = { status: 'directory', data: tree };
		} catch (err) {
			if (!(err instanceof ApiError) || err.status !== 404) {
				state = { status: 'error' };
				return;
			}
			try {
				const blob = await api.getBlob(name, ref, treePath);
				state = { status: 'file', data: blob };
			} catch (err2) {
				state =
					err2 instanceof ApiError && err2.status === 404
						? { status: 'notfound' }
						: { status: 'error' };
			}
		}
	}

	$effect(() => {
		load(projectName, $project.ref, path);
	});
</script>

<div class="flex flex-col gap-4">
	<Breadcrumbs project={projectName} ref={$project.ref} {path} />

	{#if state.status === 'loading'}
		<div class="flex flex-col gap-2">
			{#each [0, 1, 2, 3, 4] as key (key)}
				<div class="h-8 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if state.status === 'notfound'}
		<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('files.notFound')}
		</p>
	{:else if state.status === 'error'}
		<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('files.error')}</p>
			<button
				type="button"
				onclick={() => load(projectName, $project.ref, path)}
				class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if state.status === 'directory'}
		<DirectoryListing
			project={projectName}
			ref={$project.ref}
			{path}
			entries={state.data.entries}
		/>
	{:else if state.status === 'file' && state.data.content !== null}
		<FileViewer
			path={state.data.path}
			content={state.data.content}
			language={state.data.language}
			size={state.data.size}
		/>
	{:else if state.status === 'file'}
		<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
			{$t('files.error')}
		</p>
	{/if}
</div>
