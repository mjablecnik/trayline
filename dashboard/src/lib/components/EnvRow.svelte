<script lang="ts">
	import MaskedInput from '$lib/components/MaskedInput.svelte';
	import { t } from '$lib/i18n';
	import type { EnvKeyError } from '$lib/utils/env';
	import { isSensitive } from '$lib/utils/env';

	let {
		key = $bindable(),
		value = $bindable(),
		isNew,
		error,
		onDelete
	}: {
		key: string;
		value: string;
		isNew: boolean;
		error: EnvKeyError | null;
		onDelete: () => void;
	} = $props();

	let sensitive = $derived(!isNew && isSensitive(key));

	function handleDelete() {
		if (window.confirm($t('env.confirmDelete'))) {
			onDelete();
		}
	}
</script>

<div class="flex flex-col gap-2 py-2 tablet:flex-row tablet:items-start tablet:gap-3">
	<div class="tablet:w-1/3 tablet:shrink-0">
		{#if isNew}
			<input
				type="text"
				bind:value={key}
				placeholder={$t('env.keyPlaceholder')}
				aria-label={$t('env.key')}
				autocomplete="off"
				spellcheck="false"
				class="w-full rounded-md border border-slate-300 px-2.5 py-1.5 font-mono text-sm focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800"
			/>
		{:else}
			<span class="block px-2.5 py-1.5 font-mono text-sm text-slate-700 dark:text-slate-200">
				{key}
			</span>
		{/if}
		{#if error}
			<p class="mt-1 px-2.5 text-xs text-red-600 dark:text-red-400">{$t(error)}</p>
		{/if}
	</div>

	<div class="flex flex-1 items-center gap-2">
		<div class="min-w-0 flex-1">
			<MaskedInput bind:value {sensitive} ariaLabel={$t('env.value')} />
		</div>
		<button
			type="button"
			onclick={handleDelete}
			aria-label={$t('env.delete')}
			class="shrink-0 rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-950/40 dark:hover:text-red-400"
		>
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="size-4">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"
				/>
			</svg>
		</button>
	</div>
</div>
