<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import type { TunnelListItem, SingboxTunnel, Subscription } from '$lib/types';
	import type { DiagnosticsTargetSeed } from '$lib/stores/diagnostics';
	import { PageContainer, PageHeader } from '$lib/components/layout';
	import { Tabs } from '$lib/components/ui';
	import { LogsTerminal } from '$lib/components/diagnostics';
	import { usageLevel } from '$lib/stores/settings';
	import ConnectionsTab from './ConnectionsTab.svelte';
	import ChecksTab from './ChecksTab.svelte';
	import AwgConfigAnalyzerTab from './AwgConfigAnalyzerTab.svelte';

	type ActiveTab = 'logs' | 'connections' | 'checks' | 'awgConfig';

	function singboxKind(protocol: string, security?: string): string {
		if (protocol === 'vless' && security === 'reality') return 'xray';
		if (protocol === 'vless') return 'vless';
		if (protocol === 'hysteria2') return 'hy2';
		if (protocol === 'naive') return 'ss';
		return protocol;
	}

	let activeTab = $state<ActiveTab>('logs');
	let tunnels = $state<DiagnosticsTargetSeed[]>([]);

	const diagnosticsTabs = $derived.by((): { id: ActiveTab; label: string }[] => {
		const base: { id: ActiveTab; label: string }[] = [
			{ id: 'logs', label: 'Журнал' },
			{ id: 'connections', label: 'Соединения' },
			{ id: 'checks', label: 'Проверки' },
		];
		if ($usageLevel === 'expert') {
			base.push({ id: 'awgConfig', label: 'Конфиг AWG' });
		}
		return base;
	});

	$effect(() => {
		if ($usageLevel === 'expert') return;
		if (activeTab === 'awgConfig') {
			activeTab = 'logs';
		}
		const tab = $page.url.searchParams.get('tab');
		if (tab === 'awgConfig') {
			const url = new URL($page.url);
			url.searchParams.delete('tab');
			const q = url.searchParams.toString();
			const target = url.pathname + (q ? `?${q}` : '') + url.hash;
			void goto(target, { replaceState: true, keepFocus: true, noScroll: true });
		}
	});

	// Legacy URL sanitizer — rewrite ?tab=tests / ?tab=dnscheck (which used
	// to render the health rail inside the logs tab) to ?tab=checks BEFORE
	// the Tabs primitive reads the URL. Runs synchronously at init.
	{
		const sp = new URLSearchParams($page.url.search);
		const t = sp.get('tab');
		if (t === 'tests' || t === 'dnscheck') {
			sp.set('tab', 'checks');
			const url = $page.url.pathname + (sp.toString() ? `?${sp}` : '') + $page.url.hash;
			void goto(url, { replaceState: true, keepFocus: true, noScroll: true });
		}
	}

	onMount(async () => {
		// Combine three target sources for the diagnostics rail:
		//   1. AWG/managed tunnels (snap.tunnels) — system NativeWG and external
		//      adopted tunnels are excluded; diagnostics must not run against them.
		//   2. Sing-box tunnels (one row per outbound).
		//   3. Active+enabled subscription members (sing-box prefixed).
		// Failures in optional sources degrade silently to empty list.
		try {
			const [snap, singboxTunnels, subscriptions] = await Promise.all([
				api.getTunnelsAll(),
				api.singboxListTunnels().catch(() => [] as SingboxTunnel[]),
				api.listSubscriptions().catch(() => [] as Subscription[]),
			]);

			const awg: DiagnosticsTargetSeed[] = (snap.tunnels ?? []).map((t: TunnelListItem) => ({
				id: t.id,
				name: t.name,
				status: t.status,
				kind: t.awgVersion ?? 'awg',
			}));

			const singbox: DiagnosticsTargetSeed[] = singboxTunnels.map((t) => ({
				id: `singbox:${t.tag}`,
				name: t.tag,
				status: t.running ? 'running' : 'stopped',
				kind: singboxKind(t.protocol, t.security),
			}));

			const subscriptionMembers: DiagnosticsTargetSeed[] = [];
			for (const sub of subscriptions) {
				if (!sub.enabled) continue;
				const activeTag =
					(sub.activeMember && sub.memberTags.includes(sub.activeMember)
						? sub.activeMember
						: sub.memberTags[0]) ?? '';
				if (!activeTag) continue;
				const m = (sub.members ?? []).find((member) => member.tag === activeTag);
				subscriptionMembers.push({
					id: `singbox:${activeTag}`,
					// Prefer subscription label so the user sees the subscription
					// name rather than a raw outbound tag.
					name: sub.label || m?.label || activeTag,
					kind: m?.protocol ? singboxKind(m.protocol) : undefined,
					// Members are checked through the sing-box process,
					// so default to 'running' for rail visibility.
					status: 'running',
				});
			}

			// Subscription members come before raw sing-box tunnels so their
			// friendly names win the dedup map when the ids collide.
			const uniq = new Map<string, DiagnosticsTargetSeed>();
			for (const t of [...awg, ...subscriptionMembers, ...singbox]) {
				if (!uniq.has(t.id)) uniq.set(t.id, t);
			}
			tunnels = Array.from(uniq.values());
		} catch {
			tunnels = [];
		}
	});

	const pageTitle = $derived(
		activeTab === 'connections' ? 'Соединения · Диагностика' :
		activeTab === 'checks' ? 'Проверки · Диагностика' :
		activeTab === 'awgConfig' ? 'Конфиг AWG · Диагностика' :
		'Журнал · Диагностика',
	);
</script>

<svelte:head>
	<title>{pageTitle} - AWG Manager</title>
</svelte:head>

<PageContainer width="full">
	<PageHeader title="Диагностика" />

	<Tabs
		tabs={diagnosticsTabs}
		active={activeTab}
		onchange={(id) => (activeTab = id as ActiveTab)}
		urlParam="tab"
		defaultTab="logs"
	/>

	{#if activeTab === 'logs'}
		<LogsTerminal />
	{:else if activeTab === 'connections'}
		<ConnectionsTab />
	{:else if activeTab === 'checks'}
		<ChecksTab {tunnels} />
	{:else if activeTab === 'awgConfig'}
		<AwgConfigAnalyzerTab />
	{/if}
</PageContainer>
