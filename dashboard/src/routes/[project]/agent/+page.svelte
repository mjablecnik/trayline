<script lang="ts">
	import { page } from '$app/state';
	import ChatInterface from '$lib/components/ChatInterface.svelte';
	import SessionList from '$lib/components/SessionList.svelte';
	import { t } from '$lib/i18n';
	import { agentStore } from '$lib/stores/agent';

	let projectName = $derived(page.params.project ?? '');

	let activeSessionId = $state<string | null>(null);
	// Bumped whenever a session starts, so SessionList (which otherwise only
	// re-fetches when the project changes) re-fetches too. Termination
	// doesn't need this: SessionList's own terminate handler already
	// re-fetches right after the server call resolves.
	let sessionListVersion = $state(0);

	// Reset agent state when it belongs to a different project than the current
	// route. Comparing against the store's own `project` (not local component
	// state) matters because agentStore is a module-level singleton: navigating
	// away through an unrelated route and back destroys and recreates this page
	// component, which would make a local "previous project" guard forget it
	// ever saw a project and skip the reset, leaving the old project's chat
	// showing under the new one.
	$effect(() => {
		if (projectName && $agentStore.project && $agentStore.project !== projectName) {
			activeSessionId = null;
			agentStore.reset();
		}
	});

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
		sessionListVersion++;
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
				refreshTrigger={sessionListVersion}
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
			refreshTrigger={sessionListVersion}
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
