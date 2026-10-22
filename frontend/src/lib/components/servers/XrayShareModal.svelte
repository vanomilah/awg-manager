<script lang="ts">
	import { Modal, Button } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import type { XrayShareLinks, CDNRouteRule } from '$lib/types';
	import QRCode from 'qrcode';
	import { Copy, Download, Send } from 'lucide-svelte';

	interface Props {
		open: boolean;
		clientId: string;
		clientRemark: string;
		onclose: () => void;
	}

	let { open = $bindable(false), clientId, clientRemark, onclose }: Props = $props();

	let links = $state<XrayShareLinks | null>(null);
	let loading = $state(false);
	let activeTab = $state<'vless' | 'happ' | 'singbox' | 'mihomo'>('vless');
	let qrDataUrl = $state('');
	let qrGenerating = $state(false);
	let cdnRoutes = $state<CDNRouteRule[]>([]);
	let selectedDomain = $state('');

	$effect(() => {
		if (open && clientId) {
			void initModal();
		} else {
			links = null;
			qrDataUrl = '';
			cdnRoutes = [];
			selectedDomain = '';
		}
	});

	async function initModal() {
		loading = true;
		try {
			const dispCfg = await api.getDispatcherConfig().catch(() => null);
			if (dispCfg?.routes) {
				cdnRoutes = dispCfg.routes.filter((r) => r.enabled && r.route_xray);
			} else {
				cdnRoutes = [];
			}
			if (cdnRoutes.length > 0 && !selectedDomain) {
				selectedDomain = cdnRoutes[0].domain;
			}
			await loadLinks(selectedDomain || undefined);
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка загрузки данных подключения');
		} finally {
			loading = false;
		}
	}

	async function onDomainChange(domain: string) {
		selectedDomain = domain;
		await loadLinks(domain || undefined);
	}

	async function loadLinks(domainOverride?: string) {
		try {
			links = await api.getXrayClientLinks(clientId, domainOverride);
			if (links) {
				const content = activeTab === 'happ' ? links.happ_json : links.vless_url;
				generateQR(content);
			}
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка получения ссылок');
		}
	}

	async function generateQR(content: string) {
		qrGenerating = true;
		try {
			qrDataUrl = await QRCode.toDataURL(content, {
				width: 320,
				margin: 2,
				errorCorrectionLevel: 'L',
				color: { dark: '#000000', light: '#ffffff' },
			});
		} catch {
			qrDataUrl = '';
		} finally {
			qrGenerating = false;
		}
	}

	async function copyCurrent() {
		if (!links) return;
		let text = '';
		let label = '';
		switch (activeTab) {
			case 'vless':
				text = links.vless_url;
				label = 'VLESS ссылка';
				break;
			case 'happ':
				text = links.happ_json;
				label = 'Happ JSON';
				break;
			case 'singbox':
				text = links.singbox_json;
				label = 'Sing-box outbound JSON';
				break;
			case 'mihomo':
				text = links.mihomo_yaml;
				label = 'Clash YAML';
				break;
		}
		if (text && (await copyToClipboard(text))) {
			notifications.success(`${label} скопирована`);
		}
	}

	function shareToTelegram() {
		if (!links?.vless_url) return;
		const label = selectedDomain ? `${clientRemark || 'Клиент'} · ${selectedDomain}` : clientRemark || 'Клиент';
		const tgUrl = `https://t.me/share/url?url=${encodeURIComponent(links.vless_url)}&text=${encodeURIComponent(`VLESS CDN (${label})`)}`;
		window.open(tgUrl, '_blank');
	}

	function downloadConfig() {
		if (!links) return;
		let content = '';
		let filename = '';
		let type = 'application/json';

		switch (activeTab) {
			case 'vless':
				content = links.vless_url;
				filename = `vless-${clientRemark || 'client'}.txt`;
				type = 'text/plain';
				break;
			case 'happ':
				content = links.happ_json;
				filename = `happ-${clientRemark || 'client'}.json`;
				break;
			case 'singbox':
				content = links.singbox_json;
				filename = `singbox-outbound-${clientRemark || 'client'}.json`;
				break;
			case 'mihomo':
				content = links.mihomo_yaml;
				filename = `clash-proxy-${clientRemark || 'client'}.yaml`;
				type = 'text/yaml';
				break;
		}

		const blob = new Blob([content], { type });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
		URL.revokeObjectURL(url);
		notifications.success(`Файл ${filename} скачан`);
	}
</script>

<Modal bind:open title={`Подключение: ${clientRemark || 'Клиент'}`} {onclose} size="lg">
	{#if loading}
		<div class="loading-box">Загрузка конфигурации...</div>
	{:else if links}
		<div class="share-container">
			<!-- QR Code Section -->
			<div class="qr-col">
				{#if qrGenerating}
					<div class="qr-placeholder">Генерация QR...</div>
				{:else if qrDataUrl}
					<div class="qr-wrapper">
						<img src={qrDataUrl} alt="QR-код VLESS" class="qr-img" />
					</div>
					<div class="qr-hint">Отсканируйте камерой или в приложении Happ / v2rayNG / Streisand</div>
				{:else}
					<div class="qr-placeholder">QR-код недоступен</div>
				{/if}
			</div>

			<!-- Format Tabs and Preview Section -->
			<div class="data-col">
				{#if cdnRoutes.length > 0}
					<div class="endpoint-picker">
						<span class="picker-label">CDN-эндпоинт (Диспетчер):</span>
						<select
							class="picker-select font-mono"
							value={selectedDomain}
							onchange={(e) => void onDomainChange(e.currentTarget.value)}
						>
							{#each cdnRoutes as route (route.id)}
								<option value={route.domain}>
									{route.domain}{route.description ? ` (${route.description})` : ''}
								</option>
							{/each}
						</select>
					</div>
				{:else}
					<div class="endpoint-picker endpoint-picker-empty">
						<span class="picker-hint">
							В карточке «Диспетчер» не включен ни один CDN-узел для VLESS (используется домен по умолчанию).
						</span>
					</div>
				{/if}

				<div class="tabs">
					<button
						type="button"
						class="tab"
						class:active={activeTab === 'vless'}
						onclick={() => { activeTab = 'vless'; if (links) generateQR(links.vless_url); }}
					>
						VLESS URL
					</button>
					<button
						type="button"
						class="tab"
						class:active={activeTab === 'happ'}
						onclick={() => { activeTab = 'happ'; if (links) generateQR(links.happ_json); }}
					>
						Happ JSON
					</button>
					<button
						type="button"
						class="tab"
						class:active={activeTab === 'singbox'}
						onclick={() => { activeTab = 'singbox'; }}
					>
						Sing-box
					</button>
					<button
						type="button"
						class="tab"
						class:active={activeTab === 'mihomo'}
						onclick={() => { activeTab = 'mihomo'; }}
					>
						Clash YAML
					</button>
				</div>

				<div class="content-preview">
					{#if activeTab === 'vless'}
						<div class="url-view">
							<input type="text" readonly value={links.vless_url} class="url-input" />
						</div>
					{:else if activeTab === 'happ'}
						<pre class="code-box">{links.happ_json}</pre>
					{:else if activeTab === 'singbox'}
						<pre class="code-box">{links.singbox_json}</pre>
					{:else if activeTab === 'mihomo'}
						<pre class="code-box">{links.mihomo_yaml}</pre>
					{/if}
				</div>

				<div class="actions-row">
					<Button variant="primary" size="sm" onclick={copyCurrent}>
						<Copy size={14} class="mr-1" />
						Копировать
					</Button>
					<Button variant="secondary" size="sm" onclick={downloadConfig}>
						<Download size={14} class="mr-1" />
						Скачать
					</Button>
					<span class="tg-share-wrap">
						<Button variant="secondary" size="sm" onclick={shareToTelegram}>
							<Send size={14} class="mr-1" />
							В Telegram
						</Button>
					</span>
				</div>
			</div>
		</div>
	{:else}
		<div class="error-box">Не удалось получить данные для подключения</div>
	{/if}

	{#snippet actions()}
		<Button variant="secondary" onclick={onclose}>Закрыть</Button>
	{/snippet}
</Modal>

<style>
	.loading-box, .error-box {
		padding: 2rem;
		text-align: center;
		color: var(--color-text-muted);
		font-size: 14px;
	}

	.share-container {
		display: flex;
		gap: 1.5rem;
		align-items: flex-start;
		min-height: 280px;
	}

	.qr-col {
		display: flex;
		flex-direction: column;
		align-items: center;
		width: 220px;
		flex-shrink: 0;
	}

	.qr-wrapper {
		background: #ffffff;
		padding: 8px;
		border-radius: var(--radius);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.qr-img {
		width: 180px;
		height: 180px;
		display: block;
	}

	.qr-placeholder {
		width: 180px;
		height: 180px;
		background: var(--color-bg-secondary);
		border: 1px dashed var(--color-border);
		border-radius: var(--radius);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 12px;
		color: var(--color-text-muted);
	}

	.qr-hint {
		margin-top: 10px;
		font-size: 11px;
		color: var(--color-text-muted);
		text-align: center;
		line-height: 1.4;
	}

	.data-col {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--color-border);
		padding-bottom: 4px;
	}

	.tab {
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		padding: 6px 12px;
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--t-fast) ease;
	}

	.tab:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-hover);
	}

	.tab.active {
		color: var(--color-accent);
		background: var(--color-accent-tint);
	}

	.content-preview {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 10px;
		min-height: 160px;
		max-height: 220px;
		overflow-y: auto;
	}

	.url-input {
		width: 100%;
		background: transparent;
		border: none;
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-primary);
		word-break: break-all;
		outline: none;
	}

	.code-box {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 11px;
		line-height: 1.45;
		color: var(--color-text-primary);
		white-space: pre-wrap;
		word-break: break-word;
	}

	.actions-row {
		display: flex;
		gap: 8px;
	}

	.endpoint-picker {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin-bottom: 4px;
	}

	.picker-label {
		font-size: 11px;
		font-weight: 500;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.picker-hint {
		font-size: 11px;
		color: var(--color-warning, #f59e0b);
		line-height: 1.4;
		padding: 2px 0;
	}

	.picker-select {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 6px 10px;
		font-size: 12px;
		color: var(--color-text-primary);
		outline: none;
		cursor: pointer;
	}
	.picker-select:focus {
		border-color: var(--color-accent);
	}

	.tg-share-wrap :global(button) {
		color: #229ED9 !important;
		border-color: rgba(34, 158, 217, 0.4) !important;
	}
	.tg-share-wrap :global(button:hover) {
		background: rgba(34, 158, 217, 0.1) !important;
		border-color: #229ED9 !important;
	}

	.font-mono {
		font-family: var(--font-mono);
	}

	@media (max-width: 640px) {
		.share-container {
			flex-direction: column;
			align-items: stretch;
		}
		.qr-col {
			width: 100%;
		}
	}
</style>
