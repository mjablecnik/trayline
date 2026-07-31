<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { t } from '$lib/i18n';

	let { project, ref }: { project: string; ref: string } = $props();

	let activeSegment = $derived(page.url.pathname.split('/')[2] ?? 'tree');
	let refParam = $derived(encodeURIComponent(ref));

	function tabClass(segment: string) {
		return activeSegment === segment
			? 'shrink-0 border-b-2 border-sky-500 px-1 pb-2 text-sm font-medium text-sky-600 dark:text-sky-400'
			: 'shrink-0 border-b-2 border-transparent px-1 pb-2 text-sm font-medium text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100';
	}
</script>

<nav
	aria-label="Tabs"
	class="flex gap-4 overflow-x-auto border-b border-slate-200 dark:border-slate-800"
>
	<a
		href={resolve(`/[project]/tree/[...path]?ref=${refParam}`, { project, path: '' })}
		class={tabClass('tree')}
	>
		{$t('tabs.files')}
	</a>
	<a href={resolve(`/[project]/commits?ref=${refParam}`, { project })} class={tabClass('commits')}>
		{$t('tabs.commits')}
	</a>
	<a href={resolve(`/[project]/changes?ref=${refParam}`, { project })} class={tabClass('changes')}>
		{$t('tabs.changes')}
	</a>
	<a href={resolve(`/[project]/env?ref=${refParam}`, { project })} class={tabClass('env')}>
		{$t('tabs.env')}
	</a>
</nav>
