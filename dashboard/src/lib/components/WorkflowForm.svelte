<script lang="ts">
	import { untrack } from 'svelte';
	import {
		ApiError,
		api,
		type PipelinesResponse,
		type PipelineType,
		type Spec,
		type Workflow
	} from '$lib/api';
	import { t } from '$lib/i18n';

	const DEFAULT_PIPELINE = 'processes/4-create-code';
	const PIPELINE_TYPES: PipelineType[] = ['tasks', 'processes', 'workflows'];

	let {
		projectName,
		editingWorkflow = null,
		onClose,
		onSuccess,
		onConflict
	}: {
		projectName: string;
		editingWorkflow?: Workflow | null;
		onClose: () => void;
		onSuccess: (message: string) => void;
		onConflict: (message: string) => void;
	} = $props();

	// editingWorkflow is fixed for this component's lifetime (parent remounts
	// it fresh per open), so only the initial value is needed here.
	const isEdit = untrack(() => editingWorkflow !== null);

	let pipelines = $state<PipelinesResponse | null>(null);
	let loadingPipelines = $state(true);
	let pipelinesError = $state(false);

	let selectedPipeline = $state(untrack(() => editingWorkflow?.pipeline ?? DEFAULT_PIPELINE));
	let pipelineDefaults = $state<Record<string, string>>({});
	let values = $state<Record<string, string>>({});
	let loadingDetail = $state(false);
	let detailError = $state(false);
	let fieldErrors = $state<Record<string, string>>({});

	let specs = $state<Spec[]>([]);

	let submitting = $state(false);
	let submitError = $state<string | null>(null);

	$effect(() => {
		api
			.getPipelines(projectName)
			.then((res) => {
				pipelines = res;
			})
			.catch(() => {
				pipelinesError = true;
			})
			.finally(() => {
				loadingPipelines = false;
			});
	});

	function splitRef(ref: string): [string, string] {
		const idx = ref.indexOf('/');
		if (idx < 0) return ['', ''];
		return [ref.slice(0, idx), ref.slice(idx + 1)];
	}

	function isSkipFlag(key: string): boolean {
		return key.startsWith('skip-');
	}

	async function loadPipelineDetail(ref: string) {
		const [type, name] = splitRef(ref);
		if (!type || !name) return;
		loadingDetail = true;
		detailError = false;
		try {
			const detail = await api.getPipelineDetail(projectName, type, name);
			pipelineDefaults = detail.variables;
			const initial: Record<string, string> = {};
			for (const [key, def] of Object.entries(detail.variables)) {
				initial[key] =
					isEdit && editingWorkflow!.pipeline === ref && key in editingWorkflow!.variables
						? editingWorkflow!.variables[key]
						: def;
			}
			values = initial;
			fieldErrors = {};
			if ('specs-name' in detail.variables) {
				api
					.getSpecs(projectName)
					.then((res) => {
						specs = res;
					})
					.catch(() => {
						specs = [];
					});
			}
		} catch {
			detailError = true;
		} finally {
			loadingDetail = false;
		}
	}

	$effect(() => {
		loadPipelineDetail(selectedPipeline);
	});

	function handlePipelineChange(event: Event) {
		selectedPipeline = (event.target as HTMLSelectElement).value;
	}

	function labelFor(key: string): string {
		if (key === 'specs-name') return $t('workflows.form.specsName');
		if (key === 'path') return $t('workflows.form.path');
		return key;
	}

	function typeLabel(type: PipelineType): string {
		return type.charAt(0).toUpperCase() + type.slice(1);
	}

	function validate(): boolean {
		const errors: Record<string, string> = {};
		for (const [key, def] of Object.entries(pipelineDefaults)) {
			if (isSkipFlag(key)) continue;
			if (def === '' && !values[key]?.trim()) {
				errors[key] = $t('workflows.form.required');
			}
		}
		fieldErrors = errors;
		return Object.keys(errors).length === 0;
	}

	async function handleSubmit() {
		if (submitting || loadingDetail || detailError) return;
		if (!validate()) return;
		submitting = true;
		submitError = null;
		try {
			if (isEdit && editingWorkflow) {
				await api.updateWorkflow(projectName, editingWorkflow.id, {
					pipeline: selectedPipeline,
					variables: values
				});
			} else {
				await api.createWorkflow(projectName, {
					pipeline: selectedPipeline,
					variables: values
				});
			}
			onSuccess($t('workflows.form.scheduled'));
		} catch (err) {
			if (isEdit && err instanceof ApiError && err.status === 409) {
				onConflict($t('workflows.form.editConflict'));
				return;
			}
			submitError = $t('workflows.form.submitError');
		} finally {
			submitting = false;
		}
	}
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
	<div
		role="dialog"
		aria-modal="true"
		aria-labelledby="workflow-form-title"
		class="flex max-h-[90vh] w-full max-w-lg flex-col gap-4 overflow-y-auto rounded-lg bg-white p-6 shadow-xl dark:bg-slate-900"
	>
		<h2 id="workflow-form-title" class="text-lg font-semibold text-slate-800 dark:text-slate-100">
			{isEdit ? $t('workflows.form.titleEdit') : $t('workflows.form.titleNew')}
		</h2>

		<div class="space-y-1">
			<label for="workflow-pipeline" class="text-sm font-medium text-slate-700 dark:text-slate-300">
				{$t('workflows.form.pipeline')}
			</label>
			{#if loadingPipelines}
				<p class="text-sm text-slate-400 dark:text-slate-500">
					{$t('workflows.form.loadingPipelines')}
				</p>
			{:else if pipelinesError}
				<p class="text-sm text-red-600 dark:text-red-400">{$t('workflows.form.pipelinesError')}</p>
			{:else}
				<select
					id="workflow-pipeline"
					value={selectedPipeline}
					onchange={handlePipelineChange}
					disabled={submitting}
					class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
				>
					{#each PIPELINE_TYPES as type (type)}
						{#if pipelines?.[type]?.length}
							<optgroup label={typeLabel(type)}>
								{#each pipelines[type] as pipeline (pipeline.name)}
									<option value="{pipeline.type}/{pipeline.name}">{pipeline.display_name}</option>
								{/each}
							</optgroup>
						{/if}
					{/each}
				</select>
			{/if}
		</div>

		{#if loadingDetail}
			<div class="h-24 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
		{:else if detailError}
			<p class="text-sm text-red-600 dark:text-red-400">{$t('workflows.form.pipelineError')}</p>
		{:else}
			<div class="flex flex-col gap-3">
				{#each Object.keys(pipelineDefaults) as key (key)}
					<div class="space-y-1">
						<label
							for="workflow-var-{key}"
							class="text-sm font-medium text-slate-700 dark:text-slate-300"
						>
							{labelFor(key)}
						</label>

						{#if isSkipFlag(key)}
							<label class="flex items-center gap-2">
								<input
									id="workflow-var-{key}"
									type="checkbox"
									checked={values[key] === 'true'}
									onchange={(e) => {
										values[key] = (e.target as HTMLInputElement).checked ? 'true' : 'false';
									}}
									disabled={submitting}
									class="size-4 rounded border-slate-300 text-sky-500 focus:ring-sky-500 dark:border-slate-700"
								/>
								<span class="text-sm text-slate-500 dark:text-slate-400">{values[key]}</span>
							</label>
						{:else if key === 'specs-name'}
							<select
								id="workflow-var-{key}"
								bind:value={values[key]}
								disabled={submitting}
								class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
							>
								<option value="">{$t('workflows.form.specsPlaceholder')}</option>
								{#each specs as spec (spec.name)}
									<option value={spec.name}>{spec.name}</option>
								{/each}
							</select>
						{:else}
							<input
								id="workflow-var-{key}"
								type="text"
								bind:value={values[key]}
								disabled={submitting}
								class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
							/>
						{/if}

						{#if fieldErrors[key]}
							<p class="text-xs text-red-600 dark:text-red-400">{fieldErrors[key]}</p>
						{/if}
					</div>
				{/each}
			</div>
		{/if}

		{#if submitError}
			<p class="text-sm text-red-600 dark:text-red-400">{submitError}</p>
		{/if}

		<div class="mt-2 flex items-center justify-end gap-3">
			<button
				type="button"
				onclick={onClose}
				disabled={submitting}
				class="rounded-md border border-slate-300 px-4 py-1.5 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
			>
				{$t('workflows.form.cancel')}
			</button>
			<button
				type="button"
				onclick={handleSubmit}
				disabled={submitting || loadingPipelines || pipelinesError || loadingDetail || detailError}
				class="rounded-md bg-sky-500 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{isEdit ? $t('workflows.form.save') : $t('workflows.form.schedule')}
			</button>
		</div>
	</div>
</div>
