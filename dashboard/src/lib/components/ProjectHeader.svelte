<script lang="ts">
	import { resolve } from '$app/paths';
	import { t } from '$lib/i18n';

	let { name }: { name: string } = $props();
</script>

<nav aria-label="Breadcrumb" class="flex min-w-0 items-center gap-1.5 text-sm">
	<a
		href={resolve('/')}
		class="flex shrink-0 items-center gap-1 text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
	>
		<svg
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			class="size-4"
			aria-hidden="true"
		>
			<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
		</svg>
		{$t('nav.projects')}
	</a>
	<span class="shrink-0 text-slate-300 dark:text-slate-600">/</span>
	<span class="truncate font-medium text-slate-900 dark:text-slate-100">{name}</span>
</nav>
