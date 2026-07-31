<script lang="ts">
	import { t } from '$lib/i18n';

	let { onretry }: { onretry?: () => void } = $props();

	function handleRetry() {
		if (onretry) {
			onretry();
		} else {
			location.reload();
		}
	}
</script>

<div class="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-16 text-center">
	<h1 class="text-xl font-semibold">{$t('error.connection.title')}</h1>
	<p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">
		{$t('error.connection.message')}
	</p>
	<button
		type="button"
		onclick={handleRetry}
		class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
	>
		{$t('error.connection.retry')}
	</button>
</div>
