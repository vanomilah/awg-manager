<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import type { SingboxRouterPreset } from '$lib/types';
	import { singboxWizard } from '$lib/stores/singboxWizard';

	interface Props {
		presets: SingboxRouterPreset[];
	}
	let { presets }: Props = $props();

	const wizardState = singboxWizard.state;

	const selectedPresets = $derived(
		$wizardState.presetIds
			.map((id) => presets.find((p) => p.id === id))
			.filter((p): p is SingboxRouterPreset => !!p),
	);
	const ruleSetCount = $derived(
		selectedPresets.reduce((acc, p) => acc + p.ruleSets.length, 0),
	);

	// Autodetect DNS from tunnel.interface.dns once we land on the summary.
	// tunnelTag has the form "awg-<id>" (managed) or "awg-sys-<id>" (system) —
	// strip the prefix to recover the AWG tunnel ID accepted by getTunnel().
	// System tunnels do not expose a wg-quick-style DNS field, so we only
	// attempt detection on the managed prefix. On any failure we leave
	// dnsServer null and the orchestrator falls back to Cloudflare 1.1.1.1.
	function tagToTunnelId(tag: string): string | null {
		if (tag.startsWith('awg-sys-')) return null;
		if (tag.startsWith('awg-')) return tag.slice(4);
		return null;
	}
	let existingPolicyDescription = $state('');

	onMount(async () => {
		const tag = $wizardState.tunnelTag;
		if (!tag) return;
		const id = tagToTunnelId(tag);
		if (!id) return;
		try {
			const tunnel = await api.getTunnel(id);
			const dns = tunnel.interface?.dns?.trim();
			if (dns) singboxWizard.setDnsServer(dns);
		} catch {
			// silent; orchestrator will use the Cloudflare fallback
		}

		if ($wizardState.policyMode === 'existing' && $wizardState.existingPolicyName) {
			try {
				const policies = await api.singboxRouterListPolicies();
				const p = policies.find((x) => x.name === $wizardState.existingPolicyName);
				existingPolicyDescription = p?.description ?? $wizardState.existingPolicyName;
			} catch {
				existingPolicyDescription = $wizardState.existingPolicyName ?? '';
			}
		}
	});

	const initialMacs = $derived($wizardState.initialDeviceMacs);
	const selectedMacs = $derived($wizardState.deviceMacs);
	const toBind = $derived(selectedMacs.filter((m) => !initialMacs.includes(m)));
	const toUnbind = $derived(initialMacs.filter((m) => !selectedMacs.includes(m)));
</script>

<div class="title">Что будет сделано</div>

<div class="row">
	<div class="lbl">Policy</div>
	<div class="val">
		{#if $wizardState.policyMode === 'create'}
			создаётся <b>{$wizardState.policyName}</b>
		{:else}
			используется существующая <b>{existingPolicyDescription || $wizardState.existingPolicyName}</b>
		{/if}
	</div>
</div>
<div class="row">
	<div class="lbl">Устройства</div>
	<div class="val">
		{#if toBind.length === 0 && toUnbind.length === 0}
			без изменений
		{:else}
			{#if toBind.length > 0}+{toBind.length} новых{/if}
			{#if toBind.length > 0 && toUnbind.length > 0}, {/if}
			{#if toUnbind.length > 0}-{toUnbind.length} убрать{/if}
		{/if}
	</div>
</div>
<div class="row"><div class="lbl">Туннель</div><div class="val">{$wizardState.tunnelTag}</div></div>
<div class="row">
	<div class="lbl">Пресеты</div>
	<div class="val">{selectedPresets.map((p) => p.name).join(', ')} — итого {selectedPresets.length} правил, {ruleSetCount} rule_set</div>
</div>
<div class="row">
	<div class="lbl">DNS</div>
	<div class="val">сервер {$wizardState.dnsServer ?? '1.1.1.1'} через {$wizardState.tunnelTag}; rule только для доменов из выбранных пресетов</div>
</div>
<div class="row"><div class="lbl">Движок</div><div class="val">включается автоматически</div></div>

<style>
	.title { font-size: 1.05rem; color: var(--color-text-primary); font-weight: 600; margin-bottom: 0.6rem; }
	.row { display: flex; padding: 0.45rem 0; border-bottom: 1px solid var(--color-border); }
	.row:last-child { border: 0; }
	.lbl { width: 130px; color: var(--color-text-muted); font-size: 0.82rem; }
	.val { color: var(--color-text-primary); font-size: 0.82rem; flex: 1; }
</style>
