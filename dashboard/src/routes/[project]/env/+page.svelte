<script lang="ts">
	import { beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type EnvFile } from '$lib/api';
	import EnvEditor from '$lib/components/EnvEditor.svelte';
	import EnvFileList from '$lib/components/EnvFileList.svelte';
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
	let saveMessage = $state<{ path: string; kind: 'success' | 'error'; text: string } | null>(null);

	function dirOf(path: string): string {
		const idx = path.lastIndexOf('/');
		return idx === -1 ? '' : path.slice(0, idx);
	}

	function baseOf(path: string): string {
		const idx = path.lastIndexOf('/');
		return idx === -1 ? path : path.slice(idx + 1);
	}

	// Groups files by directory - root-level files first, then subdirectories
	// alphabetically - so the list reads like a small file tree instead of a
	// flat dump of full paths.
	function groupByDir(files: EnvFile[]): { dir: string; files: EnvFile[] }[] {
		const byDir: Record<string, EnvFile[]> = {};
		for (const f of files) {
			const dir = dirOf(f.path);
			(byDir[dir] ??= []).push(f);
		}
		const dirs = Object.keys(byDir).sort((a, b) =>
			a === '' ? -1 : b === '' ? 1 : a.localeCompare(b)
		);
		return dirs.map((dir) => ({ dir, files: byDir[dir] }));
	}

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
				activeFile = data.files[0].path;
			}
		} catch {
			envState = { status: 'error' };
		}
	}

	$effect(() => {
		load(projectName);
	});

	function selectFile(path: string) {
		if (path === activeFile) return;
		if (modified.has(activeFile) && !window.confirm($t('env.confirmNavigate'))) return;
		saveMessage = null;
		activeFile = path;
	}

	function handleDirtyChange(path: string, dirty: boolean) {
		if (dirty) modified.add(path);
		else modified.delete(path);
	}

	async function handleSave(path: string, variables: { key: string; value: string }[]) {
		if (envState.status !== 'loaded') return;
		const files = envState.files;
		try {
			const saved = await api.putEnv(projectName, { path, variables });
			envState = {
				status: 'loaded',
				files: files.map((f) => (f.path === path ? saved : f))
			};
			modified.delete(path);
			saveMessage = { path, kind: 'success', text: $t('env.saveSuccess') };
		} catch (err) {
			const text = err instanceof ApiError ? err.message : $t('env.saveError');
			saveMessage = { path, kind: 'error', text };
		}
	}

	beforeNavigate((nav) => {
		if (modified.size === 0) return;
		if (!window.confirm($t('env.confirmNavigate'))) {
			nav.cancel();
		}
	});

	let groups = $derived(envState.status === 'loaded' ? groupByDir(envState.files) : []);
	let activeFileData = $derived(
		envState.status === 'loaded' ? envState.files.find((f) => f.path === activeFile) : undefined
	);
	let referenceFile = $derived(
		envState.status === 'loaded' && baseOf(activeFile) !== '.env.example'
			? envState.files.find(
					(f) => dirOf(f.path) === dirOf(activeFile) && baseOf(f.path) === '.env.example'
				)
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
	<div class="flex flex-1 flex-col gap-4 md:flex-row">
		<div class="md:w-56 md:shrink-0">
			<EnvFileList {groups} active={activeFile} {modified} onSelect={selectFile} />
		</div>

		<div class="flex flex-1 flex-col gap-4">
			{#key activeFile}
				<EnvEditor
					variables={activeFileData.variables}
					onSave={(vars) => handleSave(activeFile, vars)}
					onDirtyChange={(dirty) => handleDirtyChange(activeFile, dirty)}
				/>
			{/key}

			{#if saveMessage && saveMessage.path === activeFile}
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
	</div>
{/if}
