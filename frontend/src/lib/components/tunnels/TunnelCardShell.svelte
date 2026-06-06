<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { SingboxLayoutMode } from '$lib/constants/singboxLayout';

	type ShellMode = SingboxLayoutMode | 'cards';

	interface Props {
		mode: ShellMode;
		borderClass?: string;
		stateClass?: string;
		header?: Snippet;
		headerAside?: Snippet;
		body?: Snippet;
		footer?: Snippet;
		charts?: Snippet;
		children?: Snippet;
	}

	let {
		mode,
		borderClass = '',
		stateClass = '',
		header,
		headerAside,
		body,
		footer,
		charts,
		children,
	}: Props = $props();

	const isDense = $derived(mode === 'dense' || mode === 'cards');
</script>

<div
	class="tunnel-card-shell card {stateClass} {borderClass}"
	class:tunnel-card-shell--dense={isDense}
	class:tunnel-card-shell--compact={!isDense}
	class:view-dense={isDense}
	class:view-compact={!isDense}
>
	{#if header || headerAside}
		<div class="tunnel-card-shell__header">
			{#if header}
				<div class="tunnel-card-shell__header-main">
					{@render header()}
				</div>
			{/if}
			{#if headerAside}
				<div class="tunnel-card-shell__header-aside">
					{@render headerAside()}
				</div>
			{/if}
		</div>
	{/if}

	{#if body}
		<div class="tunnel-card-shell__body">
			{@render body()}
		</div>
	{:else if children}
		<div class="tunnel-card-shell__body">
			{@render children()}
		</div>
	{/if}

	{#if footer}
		<div class="tunnel-card-shell__footer">
			{@render footer()}
		</div>
	{/if}

	{#if charts}
		<div class="tunnel-card-shell__charts">
			{@render charts()}
		</div>
	{/if}
</div>
