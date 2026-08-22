<script lang="ts">
	import { onMount } from 'svelte';
	import '../app.css';
	import ErrorFallback from '$lib/components/ErrorFallback.svelte';
	import Header from '$lib/components/Header.svelte';
	import { auth } from '$lib/stores/auth';

	let { children } = $props();

	onMount(() => {
		auth.init();
	});
</script>

<div class="flex h-dvh flex-col overflow-hidden">
	<Header />
	<main class="flex min-h-0 flex-1 flex-col overflow-y-auto">
		<svelte:boundary>
			{@render children()}

			{#snippet failed(error, reset)}
				<ErrorFallback {error} {reset} />
			{/snippet}
		</svelte:boundary>
	</main>
</div>
