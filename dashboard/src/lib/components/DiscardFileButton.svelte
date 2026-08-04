<script lang="ts">
	import { t } from '$lib/i18n';

	let {
		path,
		disabled = false,
		onDiscard
	}: { path: string; disabled?: boolean; onDiscard: () => void } = $props();

	function handleClick() {
		if (!confirm($t('changes.confirmDiscardFile').replace('{path}', path))) return;
		onDiscard();
	}
</script>

<button
	type="button"
	onclick={handleClick}
	{disabled}
	title={$t('changes.discardFile')}
	aria-label={$t('changes.discardFile')}
	class="shrink-0 rounded p-1 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-950 dark:hover:text-red-400"
>
	<svg
		viewBox="0 0 24 24"
		fill="none"
		stroke="currentColor"
		stroke-width="2"
		class="size-4"
		aria-hidden="true"
	>
		<path
			stroke-linecap="round"
			stroke-linejoin="round"
			d="M6 7h12M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2m-8 0h10l-.867 12.142A2 2 0 0 1 15.138 21H8.862a2 2 0 0 1-1.995-1.858L6 7Z"
		/>
	</svg>
</button>
