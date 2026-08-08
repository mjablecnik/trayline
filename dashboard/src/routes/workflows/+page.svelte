<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, type GlobalWorkflow, type WorkflowStatus } from '$lib/api';
	import WorkflowLogViewer from '$lib/components/WorkflowLogViewer.svelte';
	import { t, type TranslationKey } from '$lib/i18n';
	import { locale } from '$lib/stores/locale';
	import { formatRelativeDate, formatTimeCzech } from '$lib/utils/date';

	type LoadState =
		| { status: 'loading' }
		| { status: 'error' }
		| { status: 'loaded'; workflows: GlobalWorkflow[] };

	let loadState = $state<LoadState>({ status: 'loading' });
	let expandedId = $state<string | null>(null);
	let detailCache = $state<Record<string, GlobalWorkflow>>({});
	let loadingDetailId = $state<string | null>(null);

	async function load() {
		loadState = { status: 'loading' };
		try {
			const workflows = await api.getAllWorkflows();
			loadState = { status: 'loaded', workflows };
		} catch {
			loadState = { status: 'error' };
		}
	}

	$effect(() => {
		load();
	});

	const groups = $derived(
		loadState.status === 'loaded' ? groupByProject(loadState.workflows) : []
	);

	function groupByProject(
		workflows: GlobalWorkflow[]
	): { project: string; workflows: GlobalWorkflow[] }[] {
		const map = new Map<string, GlobalWorkflow[]>();
		for (const wf of workflows) {
			const existing = map.get(wf.project);
			if (existing) {
				existing.push(wf);
			} else {
				map.set(wf.project, [wf]);
			}
		}
		return Array.from(map.entries()).map(([project, workflows]) => ({ project, workflows }));
	}

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

	function variableSummary(workflow: GlobalWorkflow): string[] {
		const summary: string[] = [];
		if (workflow.variables['specs-name']) summary.push(workflow.variables['specs-name']);
		if (workflow.variables['path']) summary.push(workflow.variables['path']);
		return summary;
	}

	async function toggleExpand(workflow: GlobalWorkflow) {
		if (expandedId === workflow.id) {
			expandedId = null;
			return;
		}
		expandedId = workflow.id;
		if (detailCache[workflow.id]) return;
		loadingDetailId = workflow.id;
		try {
			const detail = await api.getWorkflow(workflow.project, workflow.id);
			detailCache = { ...detailCache, [workflow.id]: { ...detail, project: workflow.project } };
		} finally {
			if (loadingDetailId === workflow.id) loadingDetailId = null;
		}
	}

	async function handleCancel(workflow: GlobalWorkflow) {
		try {
			await api.cancelWorkflow(workflow.project, workflow.id);
			await load();
		} catch {
			// Silently fail
		}
	}

	const skeletonKeys = [0, 1, 2];
</script>

<div class="mx-auto flex w-full max-w-6xl flex-1 flex-col gap-4 px-4 py-6">
	<div class="flex shrink-0 items-center justify-between">
		<h1 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
			{$t('workflows.overview.title')}
		</h1>
		<button
			type="button"
			onclick={load}
			disabled={loadState.status === 'loading'}
			class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800/50"
		>
			⟳ {$t('workflows.overview.refresh')}
		</button>
	</div>

	{#if loadState.status === 'loading'}
		<div class="flex flex-col gap-3">
			{#each skeletonKeys as key (key)}
				<div class="h-16 animate-pulse rounded-lg bg-slate-200 dark:bg-slate-800"></div>
			{/each}
		</div>
	{:else if loadState.status === 'error'}
		<div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
			<p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{$t('workflows.error')}</p>
			<button
				type="button"
				onclick={load}
				class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600"
			>
				{$t('common.retry')}
			</button>
		</div>
	{:else if loadState.workflows.length === 0}
		<div class="flex flex-1 flex-col items-center justify-center gap-3 text-center">
			<svg
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="1.5"
				class="size-10 text-slate-300 dark:text-slate-700"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="M3.75 12h16.5m-16.5 3.75h16.5M3.75 19.5h16.5M5.625 4.5h12.75a1.875 1.875 0 0 1 0 3.75H5.625a1.875 1.875 0 0 1 0-3.75Z"
				/>
			</svg>
			<p class="text-sm text-slate-500 dark:text-slate-400">
				{$t('workflows.overview.empty')}
			</p>
			<p class="text-xs text-slate-400 dark:text-slate-500">
				{$t('workflows.overview.emptyDescription')}
			</p>
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#each groups as group (group.project)}
				<section class="flex flex-col gap-2">
					<div class="flex items-center justify-between">
						<h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100">
							<a
								href={resolve('/[project]/workflows', { project: group.project })}
								class="hover:underline"
							>
								{group.project}
							</a>
						</h2>
						<span class="text-xs text-slate-500 dark:text-slate-400"
							>{group.workflows.length}</span
						>
					</div>

					<ul
						class="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800"
					>
						{#each group.workflows as workflow (workflow.id)}
							<li>
								<button
									type="button"
									onclick={() => toggleExpand(workflow)}
									class="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 text-left text-sm transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
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
											<span
												class="size-1.5 animate-pulse rounded-full bg-sky-500"
												aria-hidden="true"
											></span>
										{/if}
										{$t(statusLabelKey[workflow.status])}
									</span>

									{#if workflow.started_at}
										<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">
											{formatTimeCzech(workflow.started_at)}{#if workflow.completed_at}–{formatTimeCzech(workflow.completed_at)}{/if}
										</span>
									{/if}

									<span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">
										{formatRelativeDate(workflow.created_at, $locale)}
									</span>

									{#if workflow.status === 'queued' || workflow.status === 'running'}
										<span
											role="button"
											tabindex="0"
											onclick={(e) => {
												e.stopPropagation();
												handleCancel(workflow);
											}}
											onkeydown={(e) => {
												if (e.key === 'Enter' || e.key === ' ') {
													e.preventDefault();
													e.stopPropagation();
													handleCancel(workflow);
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
										class="flex flex-col gap-3 border-t border-slate-100 px-3 py-3 dark:border-slate-800/60"
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
															<tr
																class="border-b border-slate-100 last:border-0 dark:border-slate-800/60"
															>
																<td
																	class="py-1 pr-3 align-top font-mono text-xs text-slate-500 dark:text-slate-400"
																>
																	{key}
																</td>
																<td
																	class="py-1 font-mono text-xs text-slate-700 dark:text-slate-200"
																>
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
												<div
													class="h-20 animate-pulse rounded bg-slate-200 dark:bg-slate-800"
												></div>
											{:else}
												<WorkflowLogViewer
													projectName={workflow.project}
													workflowId={workflow.id}
													status={workflow.status}
													initialLog={detailCache[workflow.id]?.log ?? ''}
													initialTruncated={detailCache[workflow.id]?.truncated ?? false}
													initialExitCode={detailCache[workflow.id]?.exit_code ??
														workflow.exit_code ??
														null}
												/>
											{/if}
										</div>
									</div>
								{/if}
							</li>
						{/each}
					</ul>
				</section>
			{/each}
		</div>
	{/if}
</div>
