<script lang="ts">
	import { api, type Workflow, type WorkflowStatus } from '$lib/api';
	import WorkflowLogViewer from '$lib/components/WorkflowLogViewer.svelte';
	import { t, type TranslationKey } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate } from '$lib/utils/date';

	let {
		projectName,
		workflows,
		onEdit,
		onCancel
	}: {
		projectName: string;
		workflows: Workflow[];
		onEdit: (workflow: Workflow) => void;
		onCancel: (workflow: Workflow) => void;
	} = $props();

	let expandedId = $state<string | null>(null);
	let detailCache = $state<Record<string, Workflow>>({});
	let loadingDetailId = $state<string | null>(null);

	const badgeClass: Record<WorkflowStatus, string> = {
		queued: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
		running: 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
		completed: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
		failed: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300',
		cancelled: 'bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500'
	};

	const statusLabelKey: Record<WorkflowStatus, TranslationKey> = {
		queued: 'workflows.status.queued',
		running: 'workflows.status.running',
		completed: 'workflows.status.completed',
		failed: 'workflows.status.failed',
		cancelled: 'workflows.status.cancelled'
	};

	function displayName(pipeline: string): string {
		const name = pipeline.includes('/') ? pipeline.slice(pipeline.indexOf('/') + 1) : pipeline;
		return name.replaceAll('-', ' ');
	}

	function variableSummary(workflow: Workflow): string[] {
		const summary: string[] = [];
		if (workflow.variables['specs-name']) summary.push(workflow.variables['specs-name']);
		if (workflow.variables['path']) summary.push(workflow.variables['path']);
		return summary;
	}

	async function toggleExpand(workflow: Workflow) {
		if (expandedId === workflow.id) {
			expandedId = null;
			return;
		}
		expandedId = workflow.id;
		if (detailCache[workflow.id]) return;
		loadingDetailId = workflow.id;
		try {
			const detail = await api.getWorkflow(projectName, workflow.id);
			detailCache = { ...detailCache, [workflow.id]: detail };
		} finally {
			if (loadingDetailId === workflow.id) loadingDetailId = null;
		}
	}
</script>

<ul class="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
	{#each workflows as workflow (workflow.id)}
		<li>
			<button
				type="button"
				onclick={() => toggleExpand(workflow)}
				class="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-2 py-2 text-left text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
			>
				<span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
					{displayName(workflow.pipeline)}
				</span>

				{#if variableSummary(workflow).length > 0}
					<span class="shrink-0 truncate text-xs text-slate-400 dark:text-slate-500">
						{variableSummary(workflow).join(' · ')}
					</span>
				{/if}

				<span
					class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {badgeClass[
						workflow.status
					]}"
				>
					{#if workflow.status === 'running'}
						<span class="size-1.5 animate-pulse rounded-full bg-sky-500" aria-hidden="true"></span>
					{/if}
					{$t(statusLabelKey[workflow.status])}
				</span>

				<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">
					{formatRelativeDate(workflow.created_at, $locale)}
				</span>

				{#if workflow.status === 'queued'}
					<span class="flex shrink-0 gap-2">
						<span
							role="button"
							tabindex="0"
							onclick={(e) => {
								e.stopPropagation();
								onEdit(workflow);
							}}
							onkeydown={(e) => {
								if (e.key === 'Enter' || e.key === ' ') {
									e.preventDefault();
									e.stopPropagation();
									onEdit(workflow);
								}
							}}
							class="rounded px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
						>
							{$t('workflows.edit')}
						</span>
						<span
							role="button"
							tabindex="0"
							onclick={(e) => {
								e.stopPropagation();
								onCancel(workflow);
							}}
							onkeydown={(e) => {
								if (e.key === 'Enter' || e.key === ' ') {
									e.preventDefault();
									e.stopPropagation();
									onCancel(workflow);
								}
							}}
							class="rounded px-2 py-1 text-xs font-medium text-red-500 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-950 dark:hover:text-red-300"
						>
							{$t('workflows.cancel')}
						</span>
					</span>
				{:else if workflow.status === 'running'}
					<span
						role="button"
						tabindex="0"
						onclick={(e) => {
							e.stopPropagation();
							onCancel(workflow);
						}}
						onkeydown={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								e.preventDefault();
								e.stopPropagation();
								onCancel(workflow);
							}
						}}
						class="shrink-0 rounded px-2 py-1 text-xs font-medium text-red-500 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-950 dark:hover:text-red-300"
					>
						{$t('workflows.cancel')}
					</span>
				{/if}
			</button>

			{#if expandedId === workflow.id}
				<div
					class="flex flex-col gap-3 border-t border-slate-100 px-2 py-3 dark:border-slate-800/60"
				>
					{#if workflow.error}
						<p class="text-sm text-red-600 dark:text-red-400">{workflow.error}</p>
					{/if}

					<div>
						<p class="mb-1 text-xs font-medium text-slate-500 dark:text-slate-400">
							{$t('workflows.variables')}
						</p>
						{#if Object.keys(workflow.variables).length === 0}
							<p class="text-sm text-slate-400 dark:text-slate-500">
								{$t('workflows.noVariables')}
							</p>
						{:else}
							<table class="w-full text-sm">
								<tbody>
									{#each Object.entries(workflow.variables) as [key, value] (key)}
										<tr class="border-b border-slate-100 last:border-0 dark:border-slate-800/60">
											<td
												class="py-1 pr-3 align-top font-mono text-xs text-slate-500 dark:text-slate-400"
											>
												{key}
											</td>
											<td class="py-1 font-mono text-xs text-slate-700 dark:text-slate-200">
												{value}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						{/if}
					</div>

					<div>
						<p class="mb-1 text-xs font-medium text-slate-500 dark:text-slate-400">
							{$t('workflows.log')}
						</p>
						{#if loadingDetailId === workflow.id}
							<div class="h-20 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
						{:else}
							<WorkflowLogViewer
								{projectName}
								workflowId={workflow.id}
								status={workflow.status}
								initialLog={detailCache[workflow.id]?.log ?? ''}
								initialTruncated={detailCache[workflow.id]?.truncated ?? false}
								initialExitCode={detailCache[workflow.id]?.exit_code ?? workflow.exit_code ?? null}
							/>
						{/if}
					</div>
				</div>
			{/if}
		</li>
	{/each}
</ul>
