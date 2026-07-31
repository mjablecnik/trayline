<script lang="ts">
	import { beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type EnvFile } from '$lib/api';
	import EnvEditor from '$lib/components/EnvEditor.svelte';
	import EnvFileTabs from '$lib/components/EnvFileTabs.svelte';
	import EnvReference from '$lib/components/EnvReference.svelte';
	import { t } from '$lib/i18n';
	import { SvelteSet } from 'svelte/reactivity';

	let projectName = $derived(page.params.project ?? '');

	type EnvState =
		| { status: 'loading' }
		| { status: 'error' }
		| { status: 'empty' }
		| { status: 'loaded'; files: EnvFile[] };

	let envState = $state<EnvState>({ status: 'loading' });
	let activeFile = $state('');
	let modified = new SvelteSet<string>();
	let saveMessage = $state<{ file: string; kind: 'success' | 'error'; text: string } | null>(null);

	async function load(name: string) {
		if (!name) return;
		envState = { status: 'loading' };
		saveMessage = null;
		modified.clear();
		try {
			const data = await api.getEnv(name);
			if (data.files.length === 0) {
				envState = { status: 'empty' };
			} else {
				envState = { status: 'loaded', files: data.files };
				activeFile = data.files[0].filename;
			}
		} catch {
			envState = { status: 'error' };
		}
	}

	$effect(() => {
		load(projectName);
	});

	function selectFile(filename: string) {
		if (filename === activeFile) return;
		if (modified.has(activeFile) && !window.confirm($t('env.confirmNavigate'))) return;
		saveMessage = null;
		activeFile = filename;
	}

	function handleDirtyChange(filename: string, dirty: boolean) {
		if (dirty) modified.add(filename);
		else modified.delete(filename);
	}

	async function handleSave(filename: string, variables: { key: string; value: string }[]) {
		if (envState.status !== 'loaded') return;
		const files = envState.files;
		try {
			const saved = await api.putEnv(projectName, { filename, variables });
			envState = {
				status: 'loaded',
				files: files.map((f) => (f.filename === filename ? saved : f))
			};
			modified.delete(filename);
			saveMessage = { file: filename, kind: 'success', text: $t('env.saveSuccess') };
		} catch (err) {
			const text = err instanceof ApiError ? err.message : $t('env.saveError');
			saveMessage = { file: filename, kind: 'error', text };
		}
	}

	beforeNavigate((nav) => {
		if (modified.size === 0) return;
		if (!window.confirm($t('env.confirmNavigate'))) {
			nav.cancel();
		}
	});

	let activeFileData = $derived(
		envState.status === 'loaded' ? envState.files.find((f) => f.filename === activeFile) : undefined
	);
	let referenceFile = $derived(
		envState.status === 'loaded' && activeFile !== '.env.example'
			? envState.files.find((f) => f.filename === '.env.example')
			: undefined
	);
</script>

{#if envState.status === 'loading'}
	<div class="flex flex-col gap-2">
		{#each [0, 1, 2, 3] as row (row)}
			<div class="h-10 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
		{/each}
	</div>
{:else if envState.status === 'error'}
	<div class="flex flex-col items-center gap-4 px-2 py-8 text-center">
		<p class="text-sm text-slate-500 dark:text-slate-400">{$t('env.error')}</p>
		<button
			type="button"
			onclick={() => load(projectName)}
			class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
		>
			{$t('common.retry')}
		</button>
	</div>
{:else if envState.status === 'empty'}
	<p class="px-2 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
		{$t('env.empty')}
	</p>
{:else if activeFileData}
	<div class="flex flex-col gap-4">
		<EnvFileTabs
			filenames={envState.files.map((f) => f.filename)}
			active={activeFile}
			{modified}
			onSelect={selectFile}
		/>

		{#key activeFile}
			<EnvEditor
				variables={activeFileData.variables}
				onSave={(vars) => handleSave(activeFile, vars)}
				onDirtyChange={(dirty) => handleDirtyChange(activeFile, dirty)}
			/>
		{/key}

		{#if saveMessage && saveMessage.file === activeFile}
			<p
				class="text-sm {saveMessage.kind === 'success'
					? 'text-emerald-600 dark:text-emerald-400'
					: 'text-red-600 dark:text-red-400'}"
			>
				{saveMessage.text}
			</p>
		{/if}

		{#if referenceFile}
			<EnvReference variables={referenceFile.variables} />
		{/if}
	</div>
{/if}
