<script lang="ts">
	import { locale, SUPPORTED_LOCALES } from '$lib/i18n';
</script>

<div class="inline-flex overflow-hidden rounded-md border border-slate-300 dark:border-slate-700">
	{#each SUPPORTED_LOCALES as code (code)}
		<button
			type="button"
			class="px-2 py-1 text-xs font-medium uppercase transition-colors {$locale === code
				? 'bg-sky-500 text-white'
				: 'bg-transparent text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'}"
			aria-pressed={$locale === code}
			onclick={() => locale.set(code)}
		>
			{code}
		</button>
	{/each}
</div>
