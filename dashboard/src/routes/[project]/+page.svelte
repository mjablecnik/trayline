<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';

	onMount(() => {
		goto(resolve('/[project]/tree/[...path]', { project: page.params.project ?? '', path: '' }), {
			replaceState: true
		});
	});
</script>
