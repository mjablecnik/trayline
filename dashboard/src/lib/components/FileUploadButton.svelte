<script lang="ts">
	import { t } from '$lib/i18n';

	let {
		disabled = false,
		uploading = false,
		onFile
	}: {
		disabled?: boolean;
		uploading?: boolean;
		onFile: (file: File) => void;
	} = $props();

	let fileInputEl = $state<HTMLInputElement | undefined>(undefined);

	function handleChange(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (file) onFile(file);
		input.value = '';
	}
</script>

<input
	bind:this={fileInputEl}
	type="file"
	accept="image/*,application/pdf,.txt,.md,.json,.csv,.xml,.yaml,.yml"
	class="hidden"
	onchange={handleChange}
/>
<button
	type="button"
	onclick={() => fileInputEl?.click()}
	disabled={disabled || uploading}
	title={$t('agent.attachFile')}
	aria-label={$t('agent.attachFile')}
	class="rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
>
	{#if uploading}
		<span class="inline-block animate-spin">⏳</span>
	{:else}
		📎
	{/if}
</button>
