<script lang="ts">
	import { Modal, Button } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import type { XrayShareLinks } from '$lib/types';
	import QRCode from 'qrcode';
	import { Copy, Download } from 'lucide-svelte';

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

	$effect(() => {
		if (open && clientId) {
			void loadLinks();
		} else {
			links = null;
			qrDataUrl = '';
		}
	});

	async function loadLinks() {
		loading = true;
		try {
			links = await api.getXrayClientLinks(clientId);
			if (links?.vless_url) {
				generateQR(links.vless_url);
			}
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка получения ссылок');
		} finally {
			loading = false;
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
				label = 'Mihomo YAML';
				break;
		}
		if (text && (await copyToClipboard(text))) {
			notifications.success(`${label} скопирована`);
		}
	}

	function downloadConfig() {
		if (!links) return;
		let content = '';
		let filename = '';
		let type = 'application/json';

		switch (activeTab) {
			case 'vless':
				content = links.vless_url;
				filename = `xray-${clientRemark || 'client'}.txt`;
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
				filename = `mihomo-proxy-${clientRemark || 'client'}.yaml`;
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
						Mihomo
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
