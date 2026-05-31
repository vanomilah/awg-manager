<script lang="ts">
	import type { WireguardServerPeer } from '$lib/types';
	import { formatRelativeTime, formatBytes } from '$lib/utils/format';
	import { IconButton } from '$lib/components/ui';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';

	interface Props {
		peers: WireguardServerPeer[];
		onDownloadConf?: (publicKey: string) => void;
	}

	let { peers, onDownloadConf }: Props = $props();

	function peerName(peer: WireguardServerPeer): string {
		return peer.description || `${peer.publicKey.slice(0, 8)}...`;
	}

	function peerStatus(peer: WireguardServerPeer): 'disabled' | 'online' | 'offline' {
		if (!peer.enabled) return 'disabled';
		return peer.online ? 'online' : 'offline';
	}

	function peerIP(peer: WireguardServerPeer): string {
		const raw =
			peer.allowedIPs?.find((ip) => ip.includes('/32')) ||
			peer.allowedIPs?.[0] ||
			'-';
		// Убираем маску одиночного хоста (/32, /128) — показываем и копируем чистый IP.
		return raw.replace(/\/(32|128)$/, '');
	}

	function splitEndpoint(endpoint: string): { host: string; port?: string } {
		const trimmed = endpoint.trim();
		if (!trimmed || trimmed === '-') return { host: '-' };
		const bracketMatch = /^(\[[^\]]+\]):(\d+)$/.exec(trimmed);
		if (bracketMatch) return { host: bracketMatch[1], port: `:${bracketMatch[2]}` };
		const lastColon = trimmed.lastIndexOf(':');
		if (lastColon <= 0) return { host: trimmed };
		const host = trimmed.slice(0, lastColon);
		const port = trimmed.slice(lastColon + 1);
		if (!/^\d+$/.test(port)) return { host: trimmed };
		if (host.includes(':')) return { host: trimmed };
		return { host, port: `:${port}` };
	}

	function splitHandshakeLabel(value: string): { main: string; suffix?: string } {
		const trimmed = value.trim();
		if (trimmed.endsWith(' назад')) return { main: trimmed.slice(0, -' назад'.length), suffix: 'назад' };
		return { main: trimmed };
	}

	async function copyCellValue(value: string, label: string): Promise<void> {
		if (!value || value === '-') {
			notifications.warning(`${label} отсутствует`, { duration: 2000 });
			return;
		}
		if (await copyToClipboard(value)) {
			notifications.success(`${label} скопирован: ${value}`, { duration: 2000 });
		} else {
			notifications.error(`Не удалось скопировать ${label.toLowerCase()}`);
		}
	}
</script>

{#if peers.length === 0}
	<p class="text-muted">Нет пиров</p>
{:else}
	<div class="peer-table-wrap">
		<table class="peer-table">
			<thead>
				<tr>
					<th class="col-name">Имя</th>
					<th class="col-ip">IP</th>
					<th class="col-endpoint">Endpoint</th>
					<th class="col-traffic">Трафик</th>
					<th class="col-handshake">Handshake</th>
					{#if onDownloadConf}
						<th class="col-action"></th>
					{/if}
				</tr>
			</thead>
			<tbody>
				{#each peers as peer (peer.publicKey)}
					{@const status = peerStatus(peer)}
					{@const ipValue = peerIP(peer)}
					{@const endpointValue = peer.endpoint || '-'}
					<tr class:peer-offline={!peer.online} class:peer-disabled={!peer.enabled}>
						<td class="col-name peer-name-cell">
							<div class="peer-name-row">
								<span class="led" class:led-online={status === 'online'} class:led-offline={status === 'offline'} class:led-disabled={status === 'disabled'}></span>
								<span class="peer-name">{peerName(peer)}</span>
							</div>
							<div class="peer-status-sub">{status}</div>
						</td>
						<td class="mono tech-value col-ip">
							<button type="button" class="cell-copy mono tech-value" onclick={() => void copyCellValue(ipValue, 'IP')} title={`Скопировать IP ${ipValue}`}>{ipValue}</button>
						</td>
						<td class="col-endpoint">
							{#if true}
								{@const ep = splitEndpoint(endpointValue)}
								<button
									type="button"
									class="cell-copy endpoint-copy mono tech-value"
									onclick={() => void copyCellValue(endpointValue, 'Endpoint')}
									title={endpointValue !== '-' ? `Скопировать Endpoint ${endpointValue}` : 'Endpoint отсутствует'}
								>
									<span class="endpoint-text">{ep.host}</span>
									{#if ep.port}
										<span class="endpoint-port">{ep.port}</span>
									{/if}
								</button>
							{/if}
						</td>
						<td class="col-traffic">
							<div class="traffic-cell mono tech-value">
								<div>RX: {formatBytes(peer.rxBytes)}</div>
								<div>TX: {formatBytes(peer.txBytes)}</div>
							</div>
						</td>
						<td class="mono tech-value col-handshake">
							{#if peer.lastHandshake}
								{@const hs = splitHandshakeLabel(formatRelativeTime(peer.lastHandshake))}
								<span class="handshake-text">{hs.main}</span>
								{#if hs.suffix}
									<span class="handshake-suffix">{hs.suffix}</span>
								{/if}
							{:else}
								-
							{/if}
						</td>
						{#if onDownloadConf}
							<td class="col-action">
								<IconButton
									ariaLabel={`Скачать .conf для «${peerName(peer)}»`}
									title={`Скачать .conf для «${peerName(peer)}»`}
									onclick={() => onDownloadConf?.(peer.publicKey)}
								>
									<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
										<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
										<polyline points="7,10 12,15 17,10"/>
										<line x1="12" y1="15" x2="12" y2="3"/>
									</svg>
								</IconButton>
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<style>
	.peer-table-wrap {
		overflow-x: auto;
		margin-top: 0.75rem;
	}

	.peer-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.875rem;
		table-layout: auto;
	}

	.peer-table th {
		text-align: center;
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		color: var(--accent);
		border-bottom: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
	}

	.peer-table td {
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid var(--border);
		transition: background-color 0.15s ease, box-shadow 0.15s ease;
	}

	.peer-table tbody tr:hover td {
		background: color-mix(in srgb, var(--bg-hover) 70%, transparent);
		box-shadow: inset 2px 0 0 color-mix(in srgb, var(--accent) 35%, transparent);
	}

	.peer-table tbody tr.peer-offline:hover td,
	.peer-table tbody tr.peer-disabled:hover td {
		background: color-mix(in srgb, var(--bg-hover) 45%, transparent);
		box-shadow: inset 2px 0 0 color-mix(in srgb, var(--text-muted) 35%, transparent);
	}

	.peer-table th {
		font-weight: 500;
		font-size: 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		padding: 0.65rem 0.75rem;
		line-height: 1.2;
	}

	.peer-name-cell {
		text-align: left;
	}

	.peer-name-row {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		min-width: 0;
	}

	.peer-name {
		font-weight: 500;
		max-width: 150px;
		white-space: normal;
		overflow-wrap: anywhere;
		word-break: break-word;
		line-height: 1.25;
	}

	.peer-status-sub {
		font-size: 10px;
		color: var(--text-muted);
		line-height: 1.1;
		margin-top: 0.25rem;
	}

	.mono {
		font-family: var(--font-mono, monospace);
		color: var(--text-secondary);
	}
	.tech-value {
		font-size: 10px;
		line-height: 1.15;
	}

	.led {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 50%;
	}

	.led-online {
		background: var(--success, #10b981);
		box-shadow: 0 0 6px var(--success, #10b981);
	}

	.led-offline {
		background: var(--text-muted, #6b7280);
	}

	.led-disabled {
		background: var(--error, #ef4444);
		opacity: 0.6;
	}

	.peer-offline td {
		opacity: 0.6;
	}

	.peer-disabled td {
		opacity: 0.4;
	}

	.col-ip,
	.col-endpoint,
	.col-traffic,
	.col-handshake {
		text-align: center;
	}

	.col-action {
		text-align: right;
		white-space: nowrap;
	}

	.traffic-cell {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.1rem;
		line-height: 1.2;
		white-space: nowrap;
	}

	.endpoint-copy {
		display: inline-flex;
		flex-direction: column;
		align-items: center;
	}

	.endpoint-text {
		display: block;
		white-space: normal;
		overflow-wrap: anywhere;
		word-break: break-word;
		line-height: 1.15;
	}

	.endpoint-port {
		display: block;
		white-space: nowrap;
		line-height: 1.15;
	}

	.handshake-text,
	.handshake-suffix {
		display: block;
	}

	.cell-copy {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		cursor: pointer;
		text-align: center;
	}

	.cell-copy:hover {
		text-decoration: underline;
		color: var(--text-primary);
	}

	@media (max-width: 640px) {
		.peer-table {
			min-width: 680px;
		}
	}
</style>
