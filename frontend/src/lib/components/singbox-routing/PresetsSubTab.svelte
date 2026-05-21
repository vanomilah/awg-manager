<script lang="ts">
	import { api } from '$lib/api/client';
	import { singboxRouter } from '$lib/stores/singboxRouter';
	import type { SingboxRouterPreset, SingboxRouterDNSRule } from '$lib/types';
	import {
		PresetsGallery,
		PresetApplyModal,
		PresetsPreflightBanner,
		PresetsBulkBar,
		DNSServerEditModal,
		type PreflightStatus,
	} from '$lib/components/routing/singboxRouter';
	import { ConfirmModal } from '$lib/components/ui';
	import { notifications } from '$lib/stores/notifications';

	const presetsStore = singboxRouter.presets;
	const optionsStore = singboxRouter.options;
	const dnsServersStore = singboxRouter.dnsServers;
	const statusStore = singboxRouter.status;

	const presets = $derived($presetsStore);
	const outboundOptions = $derived($optionsStore);
	const dnsServers = $derived($dnsServersStore);
	const status = $derived($statusStore);

	const preflight = $derived.by<PreflightStatus>(() => {
		if (!status) return 'loading';
		if ((status.deviceMode ?? 'policy') === 'all') return 'ok';
		if (!status.policyName) return 'no-policy';
		if (!status.policyExists) return 'no-policy-in-ndms';
		if (status.deviceCount === 0) return 'no-devices';
		return 'ok';
	});

	let selectedIds = $state<Set<string>>(new Set());
	let modalPresets = $state<SingboxRouterPreset[] | null>(null);
	let pendingApplyPresets = $state<SingboxRouterPreset[] | null>(null);
	let createDnsModalOpen = $state(false);
	let confirmResetOpen = $state(false);

	function toggleSelect(id: string): void {
		const next = new Set(selectedIds);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selectedIds = next;
	}

	function clearSelection(): void {
		selectedIds = new Set();
	}

	function openSingleApply(p: SingboxRouterPreset): void {
		modalPresets = [p];
	}

	function openBatchApply(): void {
		if (selectedIds.size === 0) return;
		modalPresets = presets.filter((p) => selectedIds.has(p.id));
	}

	function closeApplyModal(): void {
		modalPresets = null;
	}

	async function handleApply(params: {
		presetIds: string[];
		outboundTag: string;
		createDnsRule: boolean;
		dnsServerTag: string | null;
	}): Promise<void> {
		// 1. Apply each preset (idempotent on backend)
		for (const id of params.presetIds) {
			await api.singboxRouterApplyPreset(id, params.outboundTag);
		}
		// 2. Optional DNS rule
		if (params.createDnsRule && params.dnsServerTag) {
			// Collect tags only from presets with at least one tunnel-action rule.
			const tunnelPresets = presets.filter(
				(p) => params.presetIds.includes(p.id) && p.rules.some((r) => r.actionTarget === 'tunnel'),
			);
			const tags = Array.from(
				new Set(tunnelPresets.flatMap((p) => p.ruleSets.map((rs) => rs.tag))),
			);
			if (tags.length > 0) {
				const existing: SingboxRouterDNSRule[] = await api.singboxRouterListDNSRules();
				const idx = existing.findIndex((r) => r.server === params.dnsServerTag);
				if (idx >= 0) {
					const merged = Array.from(new Set([...(existing[idx].rule_set ?? []), ...tags]));
					await api.singboxRouterUpdateDNSRule(idx, {
						...existing[idx],
						rule_set: merged,
						server: params.dnsServerTag,
					});
				} else {
					await api.singboxRouterAddDNSRule({ rule_set: tags, server: params.dnsServerTag });
				}
			}
		}
		await singboxRouter.loadAll();
		clearSelection();
		closeApplyModal();
	}

	function openCreateDnsServer(): void {
		pendingApplyPresets = modalPresets;
		closeApplyModal();
		createDnsModalOpen = true;
	}

	function closeCreateDnsServer(): void {
		createDnsModalOpen = false;
		if (pendingApplyPresets) {
			modalPresets = pendingApplyPresets;
			pendingApplyPresets = null;
		}
	}

	function handleResetPolicyName(): void {
		confirmResetOpen = true;
	}

	async function doResetPolicyName(): Promise<void> {
		try {
			const settings = await api.singboxRouterGetSettings();
			await api.singboxRouterPutSettings({ ...settings, policyName: '' });
			await singboxRouter.loadAll();
		} catch (e) {
			notifications.error(`Не удалось сбросить политику: ${(e as Error).message}`);
		} finally {
			confirmResetOpen = false;
		}
	}
</script>

<PresetsPreflightBanner
	status={preflight}
	policyName={status?.policyName ?? null}
	onResetPolicyName={preflight === 'no-policy-in-ndms' ? handleResetPolicyName : undefined}
/>

<PresetsGallery
	{presets}
	{selectedIds}
	onToggleSelect={toggleSelect}
	onPresetClick={openSingleApply}
/>

<PresetsBulkBar
	selectedCount={selectedIds.size}
	preflightStatus={preflight}
	onApply={openBatchApply}
	onClear={clearSelection}
/>

{#if modalPresets}
	<PresetApplyModal
		presets={modalPresets}
		{outboundOptions}
		{dnsServers}
		onClose={closeApplyModal}
		onApply={handleApply}
		onCreateDnsServer={openCreateDnsServer}
	/>
{/if}

{#if createDnsModalOpen}
	<DNSServerEditModal
		servers={dnsServers}
		{outboundOptions}
		onClose={closeCreateDnsServer}
		onSave={async (server) => {
			await api.singboxRouterAddDNSServer(server);
			await singboxRouter.loadAll();
			closeCreateDnsServer();
		}}
	/>
{/if}

{#if confirmResetOpen}
	<ConfirmModal
		open={confirmResetOpen}
		title="Сбросить политику"
		message={`Сбросить связь с политикой "${status?.policyName ?? ''}"?`}
		secondary="Sing-box не сможет работать пока вы не выберете действующую политику."
		confirmLabel="Сбросить"
		variant="danger"
		onConfirm={doResetPolicyName}
		onClose={() => (confirmResetOpen = false)}
	/>
{/if}
