<script lang="ts">
	import type { MonitoringSnapshot, MonitoringTarget, MonitoringTunnel, MonitoringCell } from '$lib/types';
	import type { BadgeVariant } from '$lib/components/ui/Badge.svelte';
	import MatrixCell from './MatrixCell.svelte';
	import { Badge, LatencySparkline, VersionBadge } from '$lib/components/ui';
	import { latencyTier } from '$lib/utils/latencyTier';
	import { latencyHistory } from '$lib/stores/singboxProxies';

	interface Props {
		snapshot: MonitoringSnapshot;
		onCellClick: (target: MonitoringTarget, tunnel: MonitoringTunnel) => void;
		excludedTunnelIds?: Set<string>;
		onToggleTunnelExcluded?: (tunnelId: string, excluded: boolean, tunnelName: string) => void;
	}

	let {
		snapshot,
		onCellClick,
		excludedTunnelIds = new Set<string>(),
		onToggleTunnelExcluded = () => {},
	}: Props = $props();

	const sortedTunnels = $derived(
		[...snapshot.tunnels].sort((a, b) => a.name.localeCompare(b.name)),
	);

	function isSystem(t: MonitoringTunnel): boolean {
		return t.id.startsWith('sys-');
	}

	function isSingbox(t: MonitoringTunnel): boolean {
		return t.source === 'singbox';
	}

	// Managed AWG tunnels open the pingcheck drawer on the monitoring page.
	// System tunnels and sing-box t2sX are read-only — neither has NDMS-side
	// pingcheck (Keenetic owns the system case; sing-box uses Clash urltest).
	function tunnelHref(t: MonitoringTunnel): string {
		return `/monitoring?pingcheck=${encodeURIComponent(t.id)}`;
	}

	const cellByKey = $derived.by(() => {
		const m = new Map<string, MonitoringCell>();
		for (const c of snapshot.cells) {
			m.set(`${c.targetId}|${c.tunnelId}`, c);
		}
		return m;
	});

	function findCell(targetId: string, tunnelId: string): MonitoringCell | null {
		return cellByKey.get(`${targetId}|${tunnelId}`) ?? null;
	}

	const GOOGLE_CONNECTIVITY_HOST = 'connectivitycheck.gstatic.com';

	function isGoogleConnectivityTarget(target: MonitoringTarget): boolean {
		return target.host === GOOGLE_CONNECTIVITY_HOST || target.name === GOOGLE_CONNECTIVITY_HOST;
	}

	function mobileTargetName(target: MonitoringTarget): string {
		return isGoogleConnectivityTarget(target) ? 'Google' : target.name;
	}

	function mobileHostDomain(host: string): string {
		const parts = host.split('.');
		return parts.length > 2 ? parts.slice(-2).join('.') : host;
	}

	function mobileTargetHost(target: MonitoringTarget): string {
		return isGoogleConnectivityTarget(target) ? mobileHostDomain(target.host) : target.host;
	}

	function isExcluded(tunnelId: string): boolean {
		return excludedTunnelIds.has(tunnelId);
	}

	// Matrix exclusions are intentionally available for all row sources
	// (awg/system/singbox): controls visibility/probing in the monitoring
	// matrix only, not per-source pingcheck engines.
	function tunnelMatrixExcludeLabel(tunnelId: string): string {
		return isExcluded(tunnelId)
			? 'Вернуть туннель в матрицу мониторинга'
			: 'Исключить туннель из матрицы мониторинга';
	}

	type TunnelBadge = {
		label: string;
		variant: BadgeVariant;
		mono?: boolean;
	};

	function normalizeProtoLabel(proto: string): string {
		switch (proto.toLowerCase()) {
			case 'vless':
				return 'VLESS';
			case 'hysteria2':
				return 'HY2';
			case 'shadowsocks':
				return 'SS';
			case 'trojan':
				return 'Trojan';
			case 'naive':
				return 'Naive';
			default:
				return proto.toUpperCase();
		}
	}

	function tunnelTypeBadges(t: MonitoringTunnel): TunnelBadge[] {
		const out: TunnelBadge[] = [];
		if (t.source === 'singbox') {
			if (t.subscription) out.push({ label: 'подписка', variant: 'warning' });
			if (t.protocol) out.push({ label: normalizeProtoLabel(t.protocol), variant: 'accent', mono: true });
			if (t.security?.toLowerCase() === 'reality') out.push({ label: 'Reality', variant: 'warning' });
			else if (t.security?.toLowerCase() === 'tls') out.push({ label: 'TLS', variant: 'info' });
			if (t.transport) out.push({ label: t.transport.toUpperCase(), variant: 'muted', mono: true });
			if (out.length === 0) out.push({ label: 'SINGBOX', variant: 'muted', mono: true });
		}
		return out;
	}

	function resolvedAwgBackend(t: MonitoringTunnel): 'kernel' | 'nativewg' | '' {
		if (t.source !== 'awg') return '';
		if (t.backend === 'nativewg' || t.backend === 'kernel') return t.backend;
		if (t.ifaceName?.startsWith('nwg')) return 'nativewg';
		if (t.ifaceName?.startsWith('opkgtun') || t.ifaceName?.startsWith('awg')) return 'kernel';
		return '';
	}
</script>

{#if sortedTunnels.length === 0}
	<div class="empty">Нет работающих туннелей. Запустите хотя бы один туннель для отображения матрицы.</div>
{:else}
	<div class="wrap">
		<table class="matrix">
			<thead>
				<tr>
					<th class="th-target">Target</th>
					{#each sortedTunnels as t (t.id)}
						{@const typeBadges = tunnelTypeBadges(t)}
						{@const awgBackendValue = resolvedAwgBackend(t)}
						{@const showTypeRow = typeBadges.length > 0 || (t.source === 'awg' && (!!awgBackendValue || !!t.awgVersion))}
						<th class="th-tunnel">
							<div class="tunnel-head">
								<div class="tunnel-title-row">
									{#if isSystem(t)}
										<span class="tunnel-system" title="Системный туннель роутера — pingcheck управляется в системе">
											{t.name}
										</span>
									{:else if isSingbox(t)}
										<span class="tunnel-system" title="Sing-box туннель — мониторинг через Clash urltest, NDMS pingcheck не применяется">
											{t.name}
										</span>
									{:else}
										<a href={tunnelHref(t)} class="tunnel-link tunnel-name" title="Открыть настройки pingcheck">
											{t.name}
											<span class="settings-icon" aria-hidden="true">›</span>
										</a>
										{#if t.source === 'awg' && t.defaultRoute}
											<Badge variant="accent" size="sm">default</Badge>
										{/if}
									{/if}
								</div>

								<div class="tunnel-toggle-row">
									{#if isExcluded(t.id)}
										<button
											type="button"
											class="exclude-btn exclude-btn-restore"
											onclick={() => onToggleTunnelExcluded(t.id, false, t.name)}
											title={tunnelMatrixExcludeLabel(t.id)}
											aria-label={tunnelMatrixExcludeLabel(t.id)}
										>
											Вернуть
										</button>
									{:else}
										<button
											type="button"
											class="exclude-btn"
											onclick={() => onToggleTunnelExcluded(t.id, true, t.name)}
											title={tunnelMatrixExcludeLabel(t.id)}
											aria-label={tunnelMatrixExcludeLabel(t.id)}
										>
											Исключить
										</button>
									{/if}
								</div>
							</div>
							{#if showTypeRow}
								<div class="tunnel-type-row">
									{#if t.source === 'awg'}
										{#if awgBackendValue}
											<VersionBadge kind="backend" value={awgBackendValue} />
										{/if}
										{#if t.awgVersion}
											<VersionBadge kind="awg" value={t.awgVersion} />
										{/if}
									{:else}
										{#each typeBadges as b, idx (`${t.id}-type-${idx}-${b.label}`)}
											<Badge variant={b.variant} size="sm" mono={b.mono ?? false}>{b.label}</Badge>
										{/each}
									{/if}
								</div>
							{/if}
							{#if t.source === 'singbox' && t.clashDelay && t.clashDelay > 0}
								<div class="tunnel-badge-row">
									<Badge
										variant={latencyTier(t.clashDelay)}
										size="sm"
										mono
										title={`Источник: urltest группа "${t.urltestGroup ?? ''}"`}
									>
										<span class="clash-num">clash: <span class="clash-val">{t.clashDelay}</span>ms</span>
										<LatencySparkline
											history={$latencyHistory.get(t.singboxTag ?? '') ?? []}
											width={36}
											height={10}
										/>
									</Badge>
								</div>
							{/if}
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each snapshot.targets as target (target.id)}
					<tr>
						<th class="td-target" scope="row">
							{#if isGoogleConnectivityTarget(target)}
								<div class="target-desktop">
									<span class="target-name">{target.name}</span>
									<span class="target-host">{target.host}</span>
								</div>

								<div class="target-mobile-google" title={target.host}>
									<div class="target-name">{mobileTargetName(target)}</div>
									<div class="target-host">{mobileTargetHost(target)}</div>
								</div>
							{:else}
								<span class="target-name">{target.name}</span>
								<span class="target-host">{target.host}</span>
							{/if}
						</th>
						{#each sortedTunnels as tunnel (tunnel.id)}
							{@const cell = findCell(target.id, tunnel.id)}
							<td class="td-cell">
								{#if cell}
									<MatrixCell
										latencyMs={cell.latencyMs}
										ok={cell.ok}
										activeForRestart={cell.activeForRestart}
										onClick={() => onCellClick(target, tunnel)}
										ariaLabel="{target.name} × {tunnel.name}"
									/>
								{:else}
									<MatrixCell latencyMs={null} ok={false} activeForRestart={false} ariaLabel="no data" />
								{/if}
							</td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>

		<div class="legend">
			<span class="legend-item"><span class="swatch tone-good"></span>&lt;100ms</span>
			<span class="legend-item"><span class="swatch tone-warn"></span>100-250ms</span>
			<span class="legend-item"><span class="swatch tone-bad"></span>&gt;250ms</span>
			<span class="legend-item"><span class="swatch tone-failed"></span>failed</span>
			<span class="legend-item">★ — активный pingcheck target</span>
			<span class="legend-item">Клик на имя туннеля — настройки pingcheck</span>
		</div>
	</div>
{/if}

<style>
	.wrap {
		overflow-x: auto;
	}

	.clash-num {
		font-variant-numeric: tabular-nums;
	}

	.clash-val {
		display: inline-block;
		min-width: 3ch;
		text-align: right;
	}

	.matrix {
		border-collapse: separate;
		border-spacing: 0.375rem;
		width: 100%;
	}

	.th-target,
	.th-tunnel {
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
		padding: 0.4375rem 0.5rem;
		text-align: left;
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		position: sticky;
		top: 0;
	}

	.th-tunnel {
		min-width: 100px;
		text-align: center;
		z-index: 1;
	}

	.tunnel-head {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		flex-wrap: wrap;
		justify-content: flex-end;
	}

	.tunnel-title-row {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
	}

	.tunnel-toggle-row {
		display: flex;
		justify-content: flex-end;
	}

	.tunnel-badge-row {
		display: flex;
		justify-content: center;
		width: 100%;
		margin-top: 6px;
	}

	.tunnel-type-row {
		display: flex;
		justify-content: center;
		flex-wrap: wrap;
		gap: 0.25rem;
		width: 100%;
		margin-top: 4px;
	}

	@media (max-width: 768px) {
		.th-tunnel {
			padding: 0.5rem 0.5rem 0.625rem;
			text-align: center;
			vertical-align: middle;
		}

		.th-tunnel > .tunnel-head {
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
			gap: 6px;
			width: 100%;
			min-width: 0;
			margin: 0 auto;
		}

		.th-tunnel > .tunnel-head > .tunnel-title-row,
		.th-tunnel > .tunnel-head > .tunnel-toggle-row {
			display: flex;
			justify-content: center;
			align-items: center;
			width: 100%;
			min-width: 0;
			margin: 0 auto;
			text-align: center;
		}

		.th-tunnel > .tunnel-head > .tunnel-title-row > .tunnel-link,
		.th-tunnel > .tunnel-head > .tunnel-title-row > .tunnel-system {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			max-width: 100%;
			min-width: 0;
			margin: 0 auto;
			text-align: center;
		}

		.th-tunnel > .tunnel-head > .tunnel-title-row > .tunnel-name,
		.th-tunnel > .tunnel-head > .tunnel-title-row > .tunnel-link {
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		.exclude-btn {
			margin: 0 auto;
		}

		.th-tunnel > .tunnel-badge-row {
			display: flex;
			justify-content: center;
			align-items: center;
			width: 100%;
			min-width: 0;
			margin-top: 6px;
			text-align: center;
		}

		.th-tunnel > .tunnel-type-row {
			display: flex;
			justify-content: center;
			align-items: center;
			flex-wrap: wrap;
			gap: 0.25rem;
			width: 100%;
			min-width: 0;
			margin-top: 4px;
			text-align: center;
		}
	}

	.tunnel-link {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		color: inherit;
		text-decoration: none;
		padding: 0.125rem 0.375rem;
		border-radius: var(--radius-sm);
		transition: color var(--t-fast) ease, background var(--t-fast) ease;
	}
	.tunnel-link:hover {
		color: var(--color-accent);
		background: var(--color-bg-hover);
	}
	.settings-icon {
		font-size: 14px;
		opacity: 0.7;
	}

	.exclude-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		height: 22px;
		padding: 0 0.5rem;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
		color: var(--color-text-muted);
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.02em;
		cursor: pointer;
		transition:
			background var(--t-fast) ease,
			color var(--t-fast) ease,
			border-color var(--t-fast) ease,
			box-shadow var(--t-fast) ease;
		white-space: nowrap;
	}
	.exclude-btn:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}
	.exclude-btn:focus-visible {
		outline: none;
		box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-accent) 45%, transparent);
	}
	.exclude-btn-restore {
		border-color: color-mix(in srgb, var(--color-error) 45%, var(--color-border));
		color: var(--color-error);
	}
	.exclude-btn-restore:hover {
		background: color-mix(in srgb, var(--color-error) 12%, var(--color-bg-hover));
		color: var(--color-error);
	}

	.tunnel-system {
		display: inline-block;
		padding: 0.125rem 0.375rem;
		color: var(--color-text-muted);
		cursor: help;
	}

	.th-target {
		left: 0;
		z-index: 2;
	}

	.td-target {
		padding: 0.375rem 0.5rem;
		text-align: left;
		font-size: 12px;
		background: var(--color-bg-secondary);
		position: sticky;
		left: 0;
		z-index: 1;
		min-width: 160px;
	}

	.target-name {
		display: block;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.target-host {
		display: block;
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.target-desktop {
		display: block;
	}

	.target-mobile-google {
		display: none;
	}

	.target-mobile-google .target-host {
		overflow-wrap: anywhere;
		line-height: 1.25;
	}

	@media (max-width: 768px) {
		.target-mobile-google {
			display: block;
		}

		.target-desktop {
			display: none;
		}

		.target-host {
			overflow-wrap: anywhere;
			line-height: 1.25;
		}
	}

	.td-cell {
		padding: 0.125rem;
		text-align: center;
	}

	.empty {
		padding: 3rem 1rem;
		text-align: center;
		color: var(--color-text-muted);
		font-size: 14px;
		border: 1px dashed var(--color-border);
		border-radius: var(--radius);
	}

	.legend {
		display: flex;
		gap: 1rem;
		flex-wrap: wrap;
		margin-top: 0.75rem;
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.legend-item {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
	}

	.swatch {
		display: inline-block;
		width: 12px;
		height: 12px;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
	}

	.swatch.tone-good { background: color-mix(in srgb, var(--color-success) 50%, transparent); }
	.swatch.tone-warn { background: color-mix(in srgb, var(--color-warning) 50%, transparent); }
	.swatch.tone-bad { background: color-mix(in srgb, var(--color-error) 50%, transparent); }
	.swatch.tone-failed { background: var(--color-muted-tint); }
</style>
