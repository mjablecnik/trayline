<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth';

	async function handleLogout() {
		await auth.logout();
		goto(resolve('/'));
	}
</script>

<button
	type="button"
	onclick={handleLogout}
	class="rounded-md px-2 py-1 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
>
	{$t('nav.logout')}
</button>
