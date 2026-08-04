<script lang="ts">
	import type { ChatMessage } from '$lib/stores/agent';
	import { renderMarkdown } from '$lib/utils/markdown';

	let { message }: { message: ChatMessage } = $props();

	let html = $derived(message.role === 'agent' ? renderMarkdown(message.content) : '');
</script>

{#if message.role === 'system'}
	<div class="flex justify-center">
		<p class="text-xs text-slate-400 dark:text-slate-500">{message.content}</p>
	</div>
{:else}
	<div class="flex {message.role === 'user' ? 'justify-end' : 'justify-start'}">
		<div class="max-w-[80%]">
			<div
				class="rounded-lg px-3 py-2 text-sm {message.role === 'user'
					? 'bg-sky-500 whitespace-pre-wrap text-white'
					: 'bg-slate-100 text-slate-900 dark:bg-slate-800 dark:text-slate-100'}"
			>
				{#if message.role === 'agent'}
					<div
						class="prose prose-sm dark:prose-invert max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
					>
						<!-- eslint-disable-next-line svelte/no-at-html-tags -->
						{@html html}
					</div>
					{#if !message.complete}<span class="animate-pulse">▊</span>{/if}
				{:else}
					{message.content}
				{/if}
			</div>
			{#if message.error}
				<p class="mt-1 text-xs text-red-600 dark:text-red-400">{message.error}</p>
			{/if}
		</div>
	</div>
{/if}
