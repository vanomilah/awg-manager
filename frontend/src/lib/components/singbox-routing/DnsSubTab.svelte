<script lang="ts">
	import { onMount } from 'svelte';
	import { singboxRouter } from '$lib/stores/singboxRouter';
	import { StatRow } from '$lib/components/ui';
	import type { StatTile } from '$lib/components/ui';
	import type { SingboxRouterRuleSet } from '$lib/types';
	import { DNSTab } from '$lib/components/routing/singboxRouter';

	const dnsServersStore = singboxRouter.dnsServers;
	const dnsRulesStore = singboxRouter.dnsRules;
	const dnsRewritesStore = singboxRouter.dnsRewrites;
	const dnsGlobalsStore = singboxRouter.dnsGlobals;
	const optionsStore = singboxRouter.options;
	const ruleSetsStore = singboxRouter.ruleSets;

	const servers = $derived($dnsServersStore);
	const rules = $derived($dnsRulesStore);
	const rewrites = $derived($dnsRewritesStore);
	const globals = $derived($dnsGlobalsStore);
	const outboundOptions = $derived($optionsStore);
	const ruleSets = $derived<SingboxRouterRuleSet[]>($ruleSetsStore);

	async function refresh(): Promise<void> {
		await singboxRouter.loadAll();
	}

	// Bootstrap data on mount in case this sub-tab is the first one visited
	// (e.g. direct navigation to ?sub=dns skips EngineSubTab.onMount).
	onMount(() => {
		void refresh();
	});

	// ── Stat tiles ─────────────────────────────────────────────────
	const statTiles = $derived<StatTile[]>([
		{ label: 'DNS серверов', value: servers.length },
		{ label: 'DNS правил', value: rules.length, title: rules.length > 0 ? 'first-match-wins' : '' },
		{ label: 'Перезаписей', value: rewrites.length },
		{ label: 'Strategy', value: globals.strategy || '— default —', title: globals.final ? `final: ${globals.final}` : 'final: —' },
	]);
</script>

<div class="stat-row-wrap">
	<StatRow tiles={statTiles} columns={4} />
</div>

<DNSTab
	{servers}
	{rules}
	{rewrites}
	{globals}
	{ruleSets}
	{outboundOptions}
	onChange={refresh}
/>

<style>
	.stat-row-wrap {
		margin-bottom: 1rem;
	}
</style>
