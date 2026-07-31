<script lang="ts">
	import DiffFileSection from '$lib/components/DiffFileSection.svelte';
	import { parseDiff } from '$lib/utils/diff';

	let { diff }: { diff: string } = $props();

	let files = $derived(parseDiff(diff));
</script>

<div class="flex flex-col gap-4">
	{#each files as file (file.path)}
		<DiffFileSection {file} />
	{/each}
</div>
