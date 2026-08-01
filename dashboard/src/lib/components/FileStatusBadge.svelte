<script lang="ts">
	import type { StatusFileState } from '$lib/api';
	import { t } from '$lib/i18n';

	let { status }: { status: StatusFileState } = $props();

	const dotClass: Record<StatusFileState, string> = {
		modified: 'bg-amber-500',
		added: 'bg-green-500',
		untracked: 'bg-slate-400',
		deleted: 'bg-red-500'
	};

	const textClass: Record<StatusFileState, string> = {
		modified: 'text-amber-600 dark:text-amber-400',
		added: 'text-green-600 dark:text-green-400',
		untracked: 'text-slate-500 dark:text-slate-400',
		deleted: 'text-red-600 dark:text-red-400'
	};

	const labelKey = {
		modified: 'changes.status.modified',
		added: 'changes.status.added',
		untracked: 'changes.status.untracked',
		deleted: 'changes.status.deleted'
	} as const;
</script>

<span class="inline-flex shrink-0 items-center gap-1.5 text-xs font-medium {textClass[status]}">
	<span class="size-2 rounded-full {dotClass[status]}" aria-hidden="true"></span>
	{$t(labelKey[status])}
</span>
