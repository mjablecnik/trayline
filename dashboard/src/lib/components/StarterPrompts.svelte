<script lang="ts">
	import type { StarterPrompt } from '$lib/api';
	import { t } from '$lib/i18n';

	let {
		prompts,
		selectedFilename,
		onSelect,
		error = null
	}: {
		prompts: StarterPrompt[];
		selectedFilename: string | null;
		onSelect: (filename: string | null) => void;
		error?: string | null;
	} = $props();

	const visiblePrompts = $derived(prompts.slice(0, 10));

	function preview(content: string): string {
		return content.length > 100 ? `${content.slice(0, 100)}…` : content;
	}

	function handleClick(filename: string) {
		onSelect(filename === selectedFilename ? null : filename);
	}
</script>

{#if error}
	<p class="text-sm text-amber-600 dark:text-amber-400">{error}</p>
{:else if visiblePrompts.length > 0}
	<div class="flex flex-col gap-2">
		<h3 class="text-sm font-medium text-slate-700 dark:text-slate-300">
			{$t('assistant.prompts')}
		</h3>
		<ul class="flex flex-col gap-1.5">
			{#each visiblePrompts as prompt (prompt.filename)}
				<li>
					<button
						type="button"
						onclick={() => handleClick(prompt.filename)}
						class="flex w-full flex-col gap-0.5 rounded-md border px-3 py-2 text-left transition-colors {prompt.filename ===
						selectedFilename
							? 'border-sky-400 bg-sky-50 dark:border-sky-600 dark:bg-sky-950'
							: 'border-slate-200 hover:border-slate-300 hover:bg-slate-50 dark:border-slate-800 dark:hover:border-slate-700 dark:hover:bg-slate-800/50'}"
					>
						<span class="text-sm font-medium text-slate-900 dark:text-slate-100">
							{prompt.display_name}
						</span>
						<span class="text-xs text-slate-500 dark:text-slate-400">
							{preview(prompt.content)}
						</span>
					</button>
				</li>
			{/each}
		</ul>
	</div>
{/if}
