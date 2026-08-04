<script lang="ts">
	import { page } from '$app/state';
	import type { Workflow } from '$lib/api';
	import WorkflowCancelDialog from '$lib/components/WorkflowCancelDialog.svelte';
	import WorkflowForm from '$lib/components/WorkflowForm.svelte';
	import WorkflowList from '$lib/components/WorkflowList.svelte';
	import { t } from '$lib/i18n';
	import { workflowStore } from '$lib/stores/workflow';

	let projectName = $derived(page.params.project ?? '');

	let showForm = $state(false);
	let editingWorkflow = $state<Workflow | null>(null);
	let cancellingWorkflow = $state<Workflow | null>(null);
	let notice = $state<{ type: 'success' | 'error'; message: string } | null>(null);
	let noticeTimer: ReturnType<typeof setTimeout> | null = null;

	$effect(() => {
		workflowStore.start(projectName);
		return () => workflowStore.stop();
	});

	function showNotice(type: 'success' | 'error', message: string, autoDismiss: boolean) {
		if (noticeTimer) clearTimeout(noticeTimer);
		notice = { type, message };
		noticeTimer = autoDismiss
			? setTimeout(() => {
					notice = null;
				}, 3000)
			: null;
	}

	function handleNewWorkflow() {
		editingWorkflow = null;
		showForm = true;
	}

	function handleEdit(workflow: Workflow) {
		editingWorkflow = workflow;
		showForm = true;
	}

	function handleCancel(workflow: Workflow) {
		cancellingWorkflow = workflow;
	}

	function handleCancelDialogClose() {
		cancellingWorkflow = null;
	}

	function handleCancelSuccess() {
		cancellingWorkflow = null;
		workflowStore.refresh();
	}

	function handleCancelConflict(message: string) {
		cancellingWorkflow = null;
		workflowStore.refresh();
		showNotice('error', message, false);
	}

	function handleFormClose() {
		showForm = false;
		editingWorkflow = null;
	}

	function handleFormSuccess(message: string) {
		showForm = false;
		editingWorkflow = null;
		workflowStore.refresh();
		showNotice('success', message, true);
	}

	function handleFormConflict(message: string) {
		showForm = false;
		editingWorkflow = null;
		workflowStore.refresh();
		showNotice('error', message, false);
	}
</script>

<div class="flex min-h-0 flex-1 flex-col gap-4">
	<div class="flex shrink-0 items-center justify-between">
		<button
			type="button"
			onclick={handleNewWorkflow}
			class="rounded-md bg-sky-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600"
		>
			+ {$t('workflows.new')}
		</button>
	</div>

	{#if notice}
		<p
			class="shrink-0 rounded-md px-3 py-2 text-sm {notice.type === 'success'
				? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
				: 'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300'}"
		>
			{notice.message}
		</p>
	{/if}

	{#if $workflowStore.loading && $workflowStore.workflows.length === 0}
		<div class="flex flex-col gap-2">
			{#each [0, 1, 2] as row (row)}
				<div class="h-12 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if $workflowStore.error}
		<div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('workflows.error')}</p>
			<button
				type="button"
				onclick={() => workflowStore.refresh()}
				class="rounded-md bg-sky-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if $workflowStore.workflows.length === 0}
		<div class="flex flex-1 flex-col items-center justify-center gap-2 text-center">
			<p class="text-sm text-slate-500 dark:text-slate-400">{$t('workflows.empty')}</p>
			<p class="text-xs text-slate-400 dark:text-slate-500">{$t('workflows.emptyCta')}</p>
		</div>
	{:else}
		<WorkflowList
			{projectName}
			workflows={$workflowStore.workflows}
			onEdit={handleEdit}
			onCancel={handleCancel}
		/>
	{/if}
</div>

{#if showForm}
	<WorkflowForm
		{projectName}
		{editingWorkflow}
		onClose={handleFormClose}
		onSuccess={handleFormSuccess}
		onConflict={handleFormConflict}
	/>
{/if}

{#if cancellingWorkflow}
	<WorkflowCancelDialog
		{projectName}
		workflow={cancellingWorkflow}
		onClose={handleCancelDialogClose}
		onSuccess={handleCancelSuccess}
		onConflict={handleCancelConflict}
	/>
{/if}
