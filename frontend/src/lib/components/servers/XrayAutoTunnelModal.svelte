<script lang="ts">
	import { Modal, Button } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import type { XrayShareLinks } from '$lib/types';
	import { Copy, Router, Network, ArrowRight } from 'lucide-svelte';

	interface Props {
		open: boolean;
		clientId: string;
		clientRemark: string;
		onclose: () => void;
	}

	let { open = $bindable(false), clientId, clientRemark, onclose }: Props = $props();

	let links = $state<XrayShareLinks | null>(null);
	let loading = $state(false);
	let targetType = $state<'dacha' | 'local'>('dacha');

	$effect(() => {
		if (open && clientId) {
			void loadLinks();
		} else {
			links = null;
		}
	});

	async function loadLinks() {
		loading = true;
		try {
			links = await api.getXrayClientLinks(clientId);
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка получения данных');
		} finally {
			loading = false;
		}
	}

	async function copyLink() {
		if (!links?.vless_url) return;
		if (await copyToClipboard(links.vless_url)) {
			notifications.success('VLESS ссылка для туннеля скопирована');
		}
	}
</script>

<Modal bind:open title={`Создать туннель: ${clientRemark || 'Клиент'}`} {onclose} size="md">
	{#if loading}
		<div class="loading-box">Загрузка данных...</div>
	{:else if links}
		<div class="modal-body">
			<div class="target-selector">
				<button
					type="button"
					class="target-card"
					class:active={targetType === 'dacha'}
					onclick={() => targetType = 'dacha'}
				>
					<div class="target-icon">
						<Router size={20} />
					</div>
					<div class="target-info">
						<div class="target-title">Удалённый роутер (Дача)</div>
						<div class="target-desc">Подключение одного роутера к другому через совместимый CDN</div>
					</div>
				</button>

				<button
					type="button"
					class="target-card"
					class:active={targetType === 'local'}
					onclick={() => targetType = 'local'}
				>
					<div class="target-icon">
						<Network size={20} />
					</div>
					<div class="target-info">
						<div class="target-title">Локальный тест на этом роутере</div>
						<div class="target-desc">Исходящий мост в Mihomo / Sing-box для проверки маршрутизации через CDN</div>
					</div>
				</button>
			</div>

			{#if targetType === 'dacha'}
				<div class="guide-box">
					<div class="guide-step">
						<span class="step-num">1</span>
						<span>Скопируйте VLESS ссылку моста через CDN:</span>
					</div>
					<div class="copy-box">
						<input type="text" readonly value={links.vless_url} class="vless-input" />
						<Button variant="primary" size="sm" onclick={copyLink}>
							<Copy size={14} class="mr-1" />
							Копировать
						</Button>
					</div>

					<div class="guide-step mt-3">
						<span class="step-num">2</span>
						<span>На дачном роутере откройте веб-интерфейс AWG-Manager:</span>
					</div>
					<div class="guide-instruction">
						Перейдите в <strong>«Туннели»</strong> &rarr; нажмите <strong>«+ Добавить туннель»</strong> &rarr; выберите <strong>«Импорт по ссылке»</strong> и вставьте скопированный VLESS URL.
					</div>

					<div class="note-box">
						Подключение использует указанный в ссылке CDN-адрес и транспорт XHTTP. Доступные режимы запросов зависят от возможностей выбранного CDN.
					</div>
				</div>
			{:else}
				<div class="guide-box">
					<div class="guide-instruction">
						Для добавления туннеля в активный движок роутера (Mihomo или Sing-box):
					</div>
					<div class="copy-box">
						<input type="text" readonly value={links.vless_url} class="vless-input" />
						<Button variant="primary" size="sm" onclick={copyLink}>
							<Copy size={14} class="mr-1" />
							Копировать
						</Button>
					</div>
					<div class="guide-instruction mt-2">
						Перейдите во вкладку <strong>«Подписки»</strong> или <strong>«Туннели»</strong> и добавьте узел через импорт VLESS.
					</div>
				</div>
			{/if}
		</div>
	{:else}
		<div class="error-box">Не удалось подготовить туннель</div>
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

	.modal-body {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.target-selector {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.target-card {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
		text-align: left;
		transition: all var(--t-fast) ease;
	}

	.target-card:hover {
		background: var(--color-bg-hover);
		border-color: var(--color-accent-border);
	}

	.target-card.active {
		border-color: var(--color-accent);
		background: var(--color-accent-tint);
	}

	.target-icon {
		color: var(--color-accent);
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.target-title {
		font-size: 13px;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.target-desc {
		font-size: 11px;
		color: var(--color-text-muted);
		margin-top: 2px;
	}

	.guide-box {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		padding: 14px;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.guide-step {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.step-num {
		width: 20px;
		height: 20px;
		border-radius: 50%;
		background: var(--color-accent);
		color: #ffffff;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		font-size: 11px;
		font-weight: bold;
		flex-shrink: 0;
	}

	.copy-box {
		display: flex;
		gap: 8px;
		align-items: center;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 6px 8px;
	}

	.vless-input {
		flex: 1;
		min-width: 0;
		background: transparent;
		border: none;
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-primary);
		outline: none;
	}

	.guide-instruction {
		font-size: 12px;
		color: var(--color-text-muted);
		line-height: 1.5;
	}

	.note-box {
		margin-top: 8px;
		padding: 8px 10px;
		background: var(--color-bg-secondary);
		border-left: 3px solid var(--color-accent);
		border-radius: var(--radius-sm);
		font-size: 11px;
		color: var(--color-text-muted);
		line-height: 1.4;
	}

	.mt-3 {
		margin-top: 0.75rem;
	}

	.mt-2 {
		margin-top: 0.5rem;
	}
</style>
