<script lang="ts">
	import EnvRow from '$lib/components/EnvRow.svelte';
	import { t } from '$lib/i18n';
	import { isDirty, validateRow, type EditableVariable } from '$lib/utils/env';

	let {
		variables,
		onSave,
		onDirtyChange
	}: {
		variables: { key: string; value: string }[];
		onSave: (variables: { key: string; value: string }[]) => void;
		onDirtyChange?: (dirty: boolean) => void;
	} = $props();

	function toEditable(vars: { key: string; value: string }[]): EditableVariable[] {
		return vars.map((v) => ({ id: crypto.randomUUID(), key: v.key, value: v.value, isNew: false }));
	}

	let rows = $derived(toEditable(variables));

	let allKeys = $derived(rows.map((row) => row.key));
	let rowErrors = $derived(rows.map((row) => validateRow(row.key, allKeys)));
	let hasErrors = $derived(rowErrors.some((error) => error !== null));
	let dirty = $derived(isDirty(rows, variables));

	$effect(() => {
		onDirtyChange?.(dirty);
	});

	function addRow() {
		rows.push({ id: crypto.randomUUID(), key: '', value: '', isNew: true });
	}

	function removeRow(id: string) {
		rows = rows.filter((row) => row.id !== id);
	}

	function handleSave() {
		onSave(rows.map((row) => ({ key: row.key, value: row.value })));
	}
</script>

<div class="flex flex-col">
	<div
		class="hidden gap-3 border-b border-slate-200 pb-2 text-xs font-medium text-slate-400 uppercase tablet:flex dark:border-slate-800"
	>
		<span class="w-1/3 shrink-0">{$t('env.key')}</span>
		<span class="flex-1">{$t('env.value')}</span>
	</div>

	<div class="divide-y divide-slate-200 dark:divide-slate-800">
		{#each rows as row (row.id)}
			<EnvRow
				bind:key={row.key}
				bind:value={row.value}
				isNew={row.isNew}
				error={validateRow(row.key, allKeys)}
				onDelete={() => removeRow(row.id)}
			/>
		{/each}
	</div>

	<div class="mt-4 flex items-center justify-between gap-3">
		<button
			type="button"
			onclick={addRow}
			class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
		>
			+ {$t('env.addVariable')}
		</button>
		<button
			type="button"
			onclick={handleSave}
			disabled={hasErrors}
			class="rounded-md bg-sky-500 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
		>
			{$t('env.save')}
		</button>
	</div>
</div>
