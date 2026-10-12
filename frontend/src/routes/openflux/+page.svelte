<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	onMount(() => {
		goto('/proxy?tab=share&instance=openflux-server:default', { replaceState: true });
	});
</script>
