<script lang="ts">
	import { t } from '$lib/i18n';

	let { variables }: { variables: { key: string; value: string }[] } = $props();
</script>

<div class="mt-2 flex flex-col gap-2 border-t border-slate-200 pt-4 dark:border-slate-800">
	<p class="text-xs font-medium text-slate-400 uppercase">{$t('env.reference')}</p>
	<div class="flex flex-col gap-1">
		{#each variables as variable (variable.key)}
			<p class="font-mono text-xs text-slate-400 dark:text-slate-500">
				{variable.key} = {variable.value}
			</p>
		{/each}
	</div>
</div>
