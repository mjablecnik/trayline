<script lang="ts">
	import { t } from '$lib/i18n';

	let {
		value = $bindable(''),
		sensitive,
		placeholder,
		ariaLabel
	}: {
		value: string;
		sensitive: boolean;
		placeholder?: string;
		ariaLabel?: string;
	} = $props();

	let revealed = $state(false);

	let masked = $derived(sensitive && !revealed);
</script>

<div class="relative flex items-center">
	<input
		type={masked ? 'password' : 'text'}
		bind:value
		{placeholder}
		aria-label={ariaLabel}
		autocomplete="off"
		spellcheck="false"
		class="w-full rounded-md border border-slate-300 px-2.5 py-1.5 font-mono text-sm focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 {sensitive
			? 'pr-8'
			: ''}"
	/>
	{#if sensitive}
		<button
			type="button"
			onclick={() => (revealed = !revealed)}
			aria-label={revealed ? $t('env.hide') : $t('env.reveal')}
			class="absolute right-1.5 text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
		>
			{#if revealed}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="size-4">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M3.98 8.223A10.477 10.477 0 0 0 1.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0 1 12 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 0 1-4.293 5.774M6.228 6.228 3 3m3.228 3.228 3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 1 0-4.243-4.243m4.242 4.242L9.88 9.88"
					/>
				</svg>
			{:else}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="size-4">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z"
					/>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
					/>
				</svg>
			{/if}
		</button>
	{/if}
</div>
