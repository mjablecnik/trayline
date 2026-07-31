<script lang="ts">
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth';

	let value = $state('');

	function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const trimmed = value.trim();
		if (!trimmed) return;
		auth.login(trimmed);
	}
</script>

<div class="flex flex-1 items-center justify-center px-4">
	<form onsubmit={handleSubmit} class="w-full max-w-sm space-y-4">
		<div class="space-y-1 text-center">
			<h1 class="text-xl font-semibold">{$t('auth.title')}</h1>
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('auth.subtitle')}</p>
		</div>
		<input
			type="password"
			bind:value
			placeholder={$t('auth.placeholder')}
			autocomplete="off"
			class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800"
		/>
		<button
			type="submit"
			class="w-full rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
		>
			{$t('auth.connect')}
		</button>
	</form>
</div>
