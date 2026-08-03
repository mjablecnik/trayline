<script lang="ts">
	import { t } from '$lib/i18n';
	import { agentStore } from '$lib/stores/agent';

	let {
		connecting = false,
		error = null,
		onStart
	}: {
		connecting?: boolean;
		error?: string | null;
		onStart: (agent: string, model: string) => void;
	} = $props();

	function handleAgentChange(event: Event) {
		agentStore.setAgent((event.target as HTMLSelectElement).value);
	}

	function handleModelChange(event: Event) {
		agentStore.setModel((event.target as HTMLInputElement).value);
	}

	function handleStart() {
		if (!$agentStore.agent || connecting) return;
		onStart($agentStore.agent, $agentStore.model);
	}
</script>

<div class="flex flex-col gap-4 rounded-lg border border-slate-200 p-6 dark:border-slate-800">
	<div class="space-y-1">
		<label for="agent-select" class="text-sm font-medium text-slate-700 dark:text-slate-300">
			{$t('agent.selectAgent')}
		</label>
		<select
			id="agent-select"
			value={$agentStore.agent}
			onchange={handleAgentChange}
			disabled={connecting}
			class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
		>
			<option value="" disabled>{$t('agent.selectAgent')}</option>
			<option value="kiro">kiro</option>
			<option value="claude">claude</option>
		</select>
	</div>

	<div class="space-y-1">
		<label for="agent-model" class="text-sm font-medium text-slate-700 dark:text-slate-300">
			{$t('agent.selectModel')}
		</label>
		<input
			id="agent-model"
			type="text"
			value={$agentStore.model}
			oninput={handleModelChange}
			maxlength={100}
			placeholder={$t('agent.selectModel')}
			disabled={connecting}
			class="w-full rounded-md border border-slate-300 px-3 py-2 focus:border-sky-500 focus:ring-1 focus:ring-sky-500 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-800"
		/>
	</div>

	{#if error}
		<p class="text-sm text-red-600 dark:text-red-400">{error}</p>
	{/if}

	<button
		type="button"
		onclick={handleStart}
		disabled={!$agentStore.agent || connecting}
		class="rounded-md bg-sky-500 px-4 py-2 font-medium text-white transition-colors hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50"
	>
		{connecting ? $t('agent.connecting') : $t('agent.start')}
	</button>
</div>
