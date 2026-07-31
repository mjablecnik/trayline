<script lang="ts">
	import { resolve } from '$app/paths';
	import { t } from '$lib/i18n';
	import { isAuthenticated } from '$lib/stores/auth';
	import LanguageSwitcher from './LanguageSwitcher.svelte';
	import LogoutButton from './LogoutButton.svelte';

	let mobileMenuOpen = $state(false);
</script>

<header
	class="sticky top-0 z-10 border-b border-slate-200 bg-white/90 backdrop-blur dark:border-slate-800 dark:bg-slate-900/90"
>
	<div class="mx-auto flex h-14 max-w-6xl items-center justify-between px-4">
		<a href={resolve('/')} class="text-lg font-semibold tracking-tight">{$t('app.name')}</a>

		<div class="hidden items-center gap-2 tablet:flex">
			<LanguageSwitcher />
			{#if $isAuthenticated}
				<LogoutButton />
			{/if}
		</div>

		<button
			type="button"
			class="inline-flex items-center justify-center rounded-md p-2 text-slate-600 hover:bg-slate-100 tablet:hidden dark:text-slate-300 dark:hover:bg-slate-800"
			aria-label={$t('nav.menu')}
			aria-expanded={mobileMenuOpen}
			onclick={() => (mobileMenuOpen = !mobileMenuOpen)}
		>
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="size-5">
				{#if mobileMenuOpen}
					<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
				{:else}
					<path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" />
				{/if}
			</svg>
		</button>
	</div>

	{#if mobileMenuOpen}
		<div
			class="flex flex-col items-start gap-3 border-t border-slate-200 px-4 py-3 tablet:hidden dark:border-slate-800"
		>
			<LanguageSwitcher />
			{#if $isAuthenticated}
				<LogoutButton />
			{/if}
		</div>
	{/if}
</header>
