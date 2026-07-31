<script lang="ts">
	import '../app.css';
	import ErrorFallback from '$lib/components/ErrorFallback.svelte';
	import Header from '$lib/components/Header.svelte';

	let { children } = $props();
</script>

<div class="flex min-h-screen flex-col">
	<Header />
	<main class="flex flex-1 flex-col">
		<svelte:boundary>
			{@render children()}

			{#snippet failed(error, reset)}
				<ErrorFallback {error} {reset} />
			{/snippet}
		</svelte:boundary>
	</main>
</div>
