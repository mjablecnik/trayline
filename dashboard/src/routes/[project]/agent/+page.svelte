<script lang="ts">
	import { page } from '$app/state';
	import ChatInterface from '$lib/components/ChatInterface.svelte';
	import SessionList from '$lib/components/SessionList.svelte';
	import { t } from '$lib/i18n';
	import { agentStore } from '$lib/stores/agent';

	let projectName = $derived(page.params.project ?? '');

	let activeSessionId = $state<string | null>(null);

	function handleSelect(sessionId: string) {
		activeSessionId = sessionId;
	}

	function handleTerminate(sessionId: string) {
		if (sessionId === activeSessionId) {
			activeSessionId = null;
			agentStore.reset();
		}
	}

	function handleNewSession() {
		activeSessionId = null;
		agentStore.reset();
	}

	function handleSessionChange() {
		activeSessionId = $agentStore.sessionId;
	}
</script>

<div class="flex flex-1 flex-col gap-4 md:flex-row">
	<details class="rounded-lg border border-slate-200 md:hidden dark:border-slate-800">
		<summary
			class="cursor-pointer px-3 py-2 text-sm font-medium text-slate-700 dark:text-slate-300"
		>
			{$t('agent.sessions')}
		</summary>
		<div class="border-t border-slate-200 p-2 dark:border-slate-800">
			<SessionList
				{projectName}
				{activeSessionId}
				onSelect={handleSelect}
				onTerminate={handleTerminate}
				onNewSession={handleNewSession}
			/>
		</div>
	</details>

	<div class="hidden md:block md:w-64 md:shrink-0">
		<SessionList
			{projectName}
			{activeSessionId}
			onSelect={handleSelect}
			onTerminate={handleTerminate}
			onNewSession={handleNewSession}
		/>
	</div>

	<div class="flex flex-1 flex-col">
		<ChatInterface
			{projectName}
			sessionId={activeSessionId}
			onSessionChange={handleSessionChange}
		/>
	</div>
</div>
