<script lang="ts">
	import { api, type Workflow } from '$lib/api';
	import { t } from '$lib/i18n';

	let {
		projectName,
		workflow,
		onClose,
		onSuccess,
		onConflict
	}: {
		projectName: string;
		workflow: Workflow;
		onClose: () => void;
		onSuccess: () => void;
		onConflict: (message: string) => void;
	} = $props();

	let cancelling = $state(false);

	function displayName(pipeline: string): string {
		const name = pipeline.includes('/') ? pipeline.slice(pipeline.indexOf('/') + 1) : pipeline;
		return name.replaceAll('-', ' ');
	}

	async function handleConfirm() {
		if (cancelling) return;
		cancelling = true;
		try {
			await api.cancelWorkflow(projectName, workflow.id);
			onSuccess();
		} catch {
			onConflict($t('workflows.cancelDialog.error'));
		} finally {
			cancelling = false;
		}
	}
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
	<div
		role="dialog"
		aria-modal="true"
		aria-labelledby="workflow-cancel-title"
		class="flex w-full max-w-sm flex-col gap-4 rounded-lg bg-white p-6 shadow-xl dark:bg-slate-900"
	>
		<h2 id="workflow-cancel-title" class="text-lg font-semibold text-slate-800 dark:text-slate-100">
			{$t('workflows.cancelDialog.title')}
		</h2>

		<p class="text-sm text-slate-600 dark:text-slate-300">
			{$t('workflows.cancelDialog.message').replace('{pipeline}', displayName(workflow.pipeline))}
		</p>

		<div class="mt-2 flex items-center justify-end gap-3">
			<button
				type="button"
				onclick={onClose}
				disabled={cancelling}
				class="rounded-md border border-slate-300 px-4 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
			>
				{$t('workflows.cancelDialog.keep')}
			</button>
			<button
				type="button"
				onclick={handleConfirm}
				disabled={cancelling}
				class="rounded-md bg-red-600 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{$t('workflows.cancelDialog.confirm')}
			</button>
		</div>
	</div>
</div>
