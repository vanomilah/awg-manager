<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	// FreeTurn живёт вкладкой на главной (раздел «Туннели»), а не отдельной
	// страницей. Маршрут оставлен как redirect для старых закладок/ссылок.
	onMount(() => {
		goto('/?tab=freeturn', { replaceState: true });
	});
</script>
