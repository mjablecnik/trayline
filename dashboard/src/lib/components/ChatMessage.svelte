<script lang="ts">
	import type { ChatMessage } from '$lib/stores/agent';

	let { message }: { message: ChatMessage } = $props();
</script>

<div class="flex {message.role === 'user' ? 'justify-end' : 'justify-start'}">
	<div class="max-w-[80%]">
		<div
			class="rounded-lg px-3 py-2 text-sm whitespace-pre-wrap {message.role === 'user'
				? 'bg-sky-500 text-white'
				: 'bg-slate-100 text-slate-900 dark:bg-slate-800 dark:text-slate-100'}"
		>
			{message.content}{#if !message.complete && message.role === 'agent'}<span
					class="animate-pulse">▊</span
				>{/if}
		</div>
		{#if message.error}
			<p class="mt-1 text-xs text-red-600 dark:text-red-400">{message.error}</p>
		{/if}
	</div>
</div>
