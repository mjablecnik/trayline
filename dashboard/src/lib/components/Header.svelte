<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { t } from '$lib/i18n';
	import { isAuthenticated } from '$lib/stores/auth';
	import LanguageSwitcher from './LanguageSwitcher.svelte';
	import LogoutButton from './LogoutButton.svelte';

	let mobileMenuOpen = $state(false);

	let isProjectsActive = $derived(page.url.pathname === resolve('/'));
	let isSessionsActive = $derived(page.url.pathname.startsWith(resolve('/sessions')));
	let isWorkflowsActive = $derived(page.url.pathname.startsWith(resolve('/workflows')));
	let isAssistantActive = $derived(page.url.pathname.startsWith(resolve('/assistant')));

	function navLinkClass(active: boolean): string {
		return active
			? 'text-sm font-medium text-sky-700 dark:text-sky-300'
			: 'text-sm font-medium text-slate-600 transition-colors hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-100';
	}
</script>

<header
	class="sticky top-0 z-10 border-b border-slate-200 bg-white/90 backdrop-blur dark:border-slate-800 dark:bg-slate-900/90"
>
	<div class="mx-auto flex h-14 max-w-6xl items-center justify-between gap-4 px-4">
		<div class="flex min-w-0 items-center gap-5">
			<a href={resolve('/')} class="shrink-0 text-lg font-semibold tracking-tight"
				>{$t('app.name')}</a
			>
			<nav class="hidden items-center gap-4 tablet:flex">
				<a href={resolve('/')} class={navLinkClass(isProjectsActive)}>{$t('nav.projects')}</a>
				<a href={resolve('/sessions')} class={navLinkClass(isSessionsActive)}
					>{$t('nav.sessions')}</a
				>
				<a href={resolve('/workflows')} class={navLinkClass(isWorkflowsActive)}
					>{$t('nav.workflows')}</a
				>
				<a href={resolve('/assistant')} class={navLinkClass(isAssistantActive)}
					>{$t('nav.assistant')}</a
				>
			</nav>
		</div>

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
			<a
				href={resolve('/')}
				class={navLinkClass(isProjectsActive)}
				onclick={() => (mobileMenuOpen = false)}>{$t('nav.projects')}</a
			>
			<a
				href={resolve('/sessions')}
				class={navLinkClass(isSessionsActive)}
				onclick={() => (mobileMenuOpen = false)}>{$t('nav.sessions')}</a
			>
			<a
				href={resolve('/workflows')}
				class={navLinkClass(isWorkflowsActive)}
				onclick={() => (mobileMenuOpen = false)}>{$t('nav.workflows')}</a
			>
			<a
				href={resolve('/assistant')}
				class={navLinkClass(isAssistantActive)}
				onclick={() => (mobileMenuOpen = false)}>{$t('nav.assistant')}</a
			>
			<LanguageSwitcher />
			{#if $isAuthenticated}
				<LogoutButton />
			{/if}
		</div>
	{/if}
</header>
