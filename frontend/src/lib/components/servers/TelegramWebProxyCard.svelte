<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { Button, Modal } from '$lib/components/ui';
	import TelegramProxyWizard from './telegram-wizard/TelegramProxyWizard.svelte';
	import type { TgWebProxyConfig, TgWebProxyStatus, TgWebProxyRevealData } from '$lib/types';
	import QRCode from 'qrcode';
	import {
		Power,
		RotateCw,
		Copy,
		QrCode,
		ExternalLink,
		Settings,
		KeyRound,
		Radio,
		Send,
		ShieldCheck,
		RefreshCw,
		Smartphone,
		Globe,
		AlertTriangle,
		CheckCircle2,
		Trash2,
		Sparkles,
		Eye,
		Activity,
	} from 'lucide-svelte';

	let status = $state<TgWebProxyStatus | null>(null);
	let config = $state<TgWebProxyConfig | null>(null);
	let loading = $state(true);
	let actionLoading = $state(false);

	// Tabs for main view and reveal modal
	let shareTab = $state<'webproxy' | 'mtproxy'>('webproxy');

	// Reveal modal state
	let revealOpen = $state(false);
	let revealLoading = $state(false);
	let revealData = $state<TgWebProxyRevealData | null>(null);
	let revealQrDataUrl = $state('');
	let qrGenerating = $state(false);

	// Settings modal
	let settingsOpen = $state(false);
	let editConfig = $state<Partial<TgWebProxyConfig>>({});
	let saveConfigLoading = $state(false);

	// Confirmation modals
	let rotateConfirmOpen = $state(false);
	let rotateSecretLoading = $state(false);

	let revokeConfirmOpen = $state(false);
	let revokeLoading = $state(false);

	let clearCacheConfirmOpen = $state(false);
	let clearCacheLoading = $state(false);

	let wizardOpen = $state(false);

	onMount(() => {
		void loadData();
		const interval = setInterval(loadStatus, 5000);
		return () => clearInterval(interval);
	});

	async function loadData() {
		loading = true;
		try {
			const [s, c] = await Promise.all([
				api.getTgWebProxyStatus(),
				api.getTgWebProxyConfig(),
			]);
			status = s;
			config = c;
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка загрузки Telegram Proxy');
		} finally {
			loading = false;
		}
	}

	async function loadStatus() {
		try {
			status = await api.getTgWebProxyStatus();
		} catch {
			// ignore polling errors
		}
	}

	async function openRevealModal() {
		revealLoading = true;
		revealOpen = true;
		try {
			revealData = await api.revealTgWebProxySecret();
			await updateRevealQR();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка получения секретных ключей');
			closeRevealModal();
		} finally {
			revealLoading = false;
		}
	}

	function closeRevealModal() {
		revealOpen = false;
		revealData = null;
		revealQrDataUrl = '';
	}

	async function updateRevealQR() {
		if (!revealData) return;
		const url = shareTab === 'webproxy' ? (revealData.tg_url || revealData.bridge_url) : revealData.mtproxy_url;
		if (!url) {
			revealQrDataUrl = '';
			return;
		}
		qrGenerating = true;
		try {
			revealQrDataUrl = await QRCode.toDataURL(url, {
				width: 300,
				margin: 2,
				errorCorrectionLevel: 'L',
				color: { dark: '#000000', light: '#ffffff' },
			});
		} catch {
			revealQrDataUrl = '';
		} finally {
			qrGenerating = false;
		}
	}

	function setShareTab(tab: 'webproxy' | 'mtproxy') {
		shareTab = tab;
		if (revealOpen && revealData) {
			void updateRevealQR();
		}
	}

	async function togglePower() {
		actionLoading = true;
		try {
			const nextAction = status?.running ? 'stop' : 'start';
			status = await api.tgWebProxyAction(nextAction);
			notifications.success(status?.running ? 'Telegram Proxy запущен' : 'Telegram Proxy остановлен');
			if (config && status) config.enabled = status.running;
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка управления сервисом');
		} finally {
			actionLoading = false;
		}
	}

	async function restartServer() {
		actionLoading = true;
		try {
			status = await api.tgWebProxyAction('restart');
			notifications.success('Воркеры Telegram Proxy перезапущены');
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка перезапуска');
		} finally {
			actionLoading = false;
		}
	}

	function openSettingsModal() {
		editConfig = config ? { ...config } : {};
		settingsOpen = true;
	}

	async function saveSettings() {
		saveConfigLoading = true;
		try {
			config = await api.updateTgWebProxyConfig(editConfig);
			notifications.success('Настройки сохранены');
			settingsOpen = false;
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка сохранения настроек');
		} finally {
			saveConfigLoading = false;
		}
	}

	async function executeRotateSecret() {
		rotateSecretLoading = true;
		try {
			status = await api.tgWebProxyAction('rotate_secret');
			rotateConfirmOpen = false;
			notifications.success('Новый ключ сгенерирован. Старый ключ активен в переходном периоде (7 дней).');
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка ротации ключа');
		} finally {
			rotateSecretLoading = false;
		}
	}

	async function executeRevokeLegacy() {
		revokeLoading = true;
		try {
			status = await api.tgWebProxyAction('revoke_legacy');
			revokeConfirmOpen = false;
			notifications.success('Старый ключ успешно отозван. Подключения с ним больше не принимаются.');
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка отзыва ключа');
		} finally {
			revokeLoading = false;
		}
	}

	async function executeClearScannerCache() {
		clearCacheLoading = true;
		try {
			status = await api.tgWebProxyAction('clear_scanner_cache');
			clearCacheConfirmOpen = false;
			notifications.success('Кэш сетевых сканеров очищен. telemt перезапущен.');
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка очистки кэша');
		} finally {
			clearCacheLoading = false;
		}
	}

	function formatDate(isoStr?: string): string {
		if (!isoStr) return '';
		try {
			const d = new Date(isoStr);
			return d.toLocaleString('ru-RU', {
				day: 'numeric',
				month: 'short',
				hour: '2-digit',
				minute: '2-digit',
			});
		} catch {
			return isoStr;
		}
	}

	async function copy(text: string, label: string) {
		if (await copyToClipboard(text)) {
			notifications.success(`${label} скопирован`);
		}
	}
</script>

<div class="tg-card">
	<!-- Card Header -->
	<header class="card-header">
		<div class="header-left">
			<div class="title-row">
				<span class="led" class:led-running={status?.running} class:led-stopped={!status?.running}></span>
				<h2 class="title">Telegram Прокси (MTProto + Web)</h2>
				<span class="badge badge-{status?.running ? 'success' : 'neutral'}">
					{status?.running ? 'Работает' : 'Остановлен'}
				</span>
				<span class="proto-badge">MTProto :8443 + Web :8085</span>
			</div>
			<div class="subtitle">
				Единая система Telegram: прямое MTProto-подключение и веб-проксирование через совместимый CDN
			</div>
		</div>

		<div class="header-actions">
			<button
				type="button"
				class="icon-btn"
				title="Сбросить кэш сетевой диагностики"
				onclick={() => clearCacheConfirmOpen = true}
				disabled={actionLoading}
			>
				<Trash2 size={16} />
			</button>
			<button
				type="button"
				class="icon-btn"
				title="Перезапустить воркеры"
				onclick={restartServer}
				disabled={actionLoading}
			>
				<RotateCw size={16} class={actionLoading ? 'spin' : ''} />
			</button>
			<button
				type="button"
				class="icon-btn"
				title="Настройки портов и сети"
				onclick={openSettingsModal}
			>
				<Settings size={16} />
			</button>
			<Button
				variant="secondary"
				size="sm"
				onclick={() => wizardOpen = true}
				title="Пошаговый мастер настройки Telegram Proxy"
			>
				<Sparkles size={14} class="mr-1 text-primary-400" />
				Мастер настройки
			</Button>
			<Button
				variant={status?.running ? 'secondary' : 'primary'}
				size="sm"
				onclick={togglePower}
				disabled={actionLoading}
			>
				<Power size={14} class="mr-1" />
				{status?.running ? 'Остановить' : 'Запустить'}
			</Button>
		</div>
	</header>

	<!-- Status & Health Strip -->
	<section class="meta-strip">
		<div class="strip-item">
			<span class="strip-label">MTProxy Direct</span>
			<span class="strip-val">
				<span class="health-dot" class:online={status?.direct_online}></span>
				<span class="health-text">{status?.direct_online ? 'онлайн (:8443)' : 'выключен'}</span>
			</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Web Proxy (CDN)</span>
			<span class="strip-val">
				<span class="health-dot" class:online={status?.webproxy_online}></span>
				<span class="health-text">{status?.webproxy_online ? 'онлайн (:8085)' : 'выключен'}</span>
			</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Core Raw Backend</span>
			<span class="strip-val">
				<span class="health-dot" class:online={status?.raw_online}></span>
				<span class="health-text">{status?.raw_online ? 'онлайн (:2398)' : 'выключен'}</span>
			</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Egress ({config?.upstream_device || 'nwg1'})</span>
			<span class="strip-val">
				{#if status?.upstream_status === 'ok'}
					<span class="health-dot online"></span>
					<span class="health-text font-mono">норма</span>
				{:else if status?.upstream_status === 'degraded'}
					<span class="health-dot degraded"></span>
					<span class="health-text font-mono">деградация</span>
				{:else}
					<span class="health-dot stopped"></span>
					<span class="health-text font-mono">интерфейс down</span>
				{/if}
			</span>
		</div>
	</section>

	<!-- Grace-Period Alert Banner -->
	{#if status?.legacy_active}
		<div class="grace-alert">
			<div class="grace-alert-left">
				<AlertTriangle size={18} class="grace-icon" />
				<div class="grace-text">
					<span class="grace-title">Активен переходный период старого ключа (Grace Period)</span>
					<span class="grace-desc">
						Старые ссылки продолжают работать до <strong>{formatDate(status.legacy_expires_at)}</strong>.
						{#if status.legacy_expired_pending_reconcile}
							<span class="badge badge-warning ml-1">Срок истёк (ожидает очистки)</span>
						{/if}
					</span>
				</div>
			</div>
			<Button variant="secondary" size="sm" onclick={() => revokeConfirmOpen = true}>
				<ShieldCheck size={14} class="mr-1" />
				Отозвать старый ключ сейчас
			</Button>
		</div>
	{/if}

	<!-- Secret Key and Main Actions Strip -->
	<section class="actions-strip">
		<div class="key-display-box">
			<KeyRound size={15} class="key-icon" />
			<span class="key-label">Секретный ключ:</span>
			<code class="key-code font-mono">{status?.secret_masked || '••••••••'}</code>
		</div>

		<div class="actions-right">
			<Button variant="primary" size="sm" onclick={openRevealModal}>
				<Eye size={14} class="mr-1" />
				Показать ссылку и QR-код
			</Button>

			<Button variant="secondary" size="sm" onclick={() => rotateConfirmOpen = true}>
				<RefreshCw size={14} class="mr-1" />
				Ротация ключа
			</Button>
		</div>
	</section>

	<!-- Overview & Protocol Selector Tabs -->
	<div class="mode-tabs">
		<button
			type="button"
			class="mode-tab"
			class:active={shareTab === 'webproxy'}
			onclick={() => setShareTab('webproxy')}
		>
			<Globe size={15} class="mr-1.5" />
			<span>Web Proxy (CDN · WebSocket)</span>
			<span class="tab-badge tab-badge-rec">Рекомендуется для РФ</span>
		</button>
		<button
			type="button"
			class="mode-tab"
			class:active={shareTab === 'mtproxy'}
			onclick={() => setShareTab('mtproxy')}
		>
			<Smartphone size={15} class="mr-1.5" />
			<span>Прямой MTProxy (:8443)</span>
			<span class="tab-badge">Fake-TLS</span>
		</button>
	</div>

	<!-- Protocol Overview Info Box -->
	<div class="info-container">
		{#if shareTab === 'webproxy'}
			<div class="info-pane">
				<div class="info-header">
					<span class="info-badge">WebSocket over CDN</span>
					<span class="font-mono text-xs text-muted">Edge: {config?.public_hostname ? `${config.public_hostname}:443` : 'Не настроен'}</span>
				</div>
				<p class="info-p">
					Подключение через совместимую сеть доставки контента по HTTPS/WSS. Публичные параметры origin настраиваются отдельно и не требуются клиентскому приложению.
				</p>
			</div>
		{:else}
			<div class="info-pane">
				<div class="info-header">
					<span class="info-badge">Direct Fake-TLS</span>
					<span class="font-mono text-xs text-muted">Сервер: {config?.direct_host ? `${config.direct_host}:${config?.direct_port || 8443}` : 'Не настроен'}</span>
				</div>
				<p class="info-p">
					Прямое подключение к роутеру по протоколу Telegram MTProto с Fake-TLS и доменом SNI <code>{config?.tls_domain || 'example.com'}</code>. Подходит для клиентов без поддержки CDN-подключения.
				</p>
			</div>
		{/if}
	</div>
</div>

<!-- Modal: Reveal Link & QR -->
<Modal bind:open={revealOpen} title="Подключение к Telegram Прокси" onclose={closeRevealModal} size="lg">
	{#if revealLoading}
		<div class="reveal-loading">
			<RefreshCw size={24} class="spin mb-2" />
			<span>Загрузка защищённых параметров...</span>
		</div>
	{:else if revealData}
		<div class="reveal-content">
			<!-- Mode switch inside modal -->
			<div class="reveal-tabs">
				<button
					type="button"
					class="reveal-tab"
					class:active={shareTab === 'webproxy'}
					onclick={() => setShareTab('webproxy')}
				>
					<Globe size={14} class="mr-1" />
					Web Proxy (CDN)
				</button>
				<button
					type="button"
					class="reveal-tab"
					class:active={shareTab === 'mtproxy'}
					onclick={() => setShareTab('mtproxy')}
				>
					<Smartphone size={14} class="mr-1" />
					Прямой MTProxy (:8443)
				</button>
			</div>

			<div class="reveal-body">
				<!-- QR Code Column -->
				<div class="reveal-qr-col">
					{#if qrGenerating}
						<div class="qr-placeholder">Генерация QR...</div>
					{:else if revealQrDataUrl}
						<div class="qr-wrapper">
							<img src={revealQrDataUrl} alt="QR код для Telegram" class="qr-img" />
						</div>
						<div class="qr-hint">
							{#if shareTab === 'webproxy'}
								Отсканируйте камерой смартфона — подключение через CDN (:443)
							{:else}
								Прямое подключение к роутеру (требует открытого порта :8443)
							{/if}
						</div>
					{/if}
				</div>

				<!-- Links Column -->
				<div class="reveal-links-col">
					{#if shareTab === 'webproxy'}
						<!-- Web Proxy Share -->
						<div class="link-block">
							<div class="link-header">
								<span class="link-label">Ссылка для Telegram в 1 клик (tg://webproxy):</span>
							</div>
							<div class="input-with-actions">
								<input type="text" readonly value={revealData.tg_url} class="link-input font-mono" />
								<Button variant="primary" size="sm" onclick={() => copy(revealData?.tg_url || '', 'tg://webproxy ссылка')}>
									<Copy size={14} class="mr-1" />
									Копировать
								</Button>
								<a href={revealData.tg_url} class="btn-open-tg">
									<Send size={14} class="mr-1" />
									Открыть
								</a>
							</div>
						</div>

						<div class="link-block">
							<div class="link-header">
								<span class="link-label">Веб-ссылка (https://t.me/webproxy):</span>
							</div>
							<div class="input-with-actions">
								<input type="text" readonly value={revealData.tme_url} class="link-input font-mono" />
								<Button variant="secondary" size="sm" onclick={() => copy(revealData?.tme_url || '', 't.me ссылка')}>
									<Copy size={14} class="mr-1" />
									Копировать
								</Button>
							</div>
						</div>

						<div class="link-block">
							<div class="link-header">
								<span class="link-label">HTTP Bridge ссылка (WebSocket over CDN):</span>
							</div>
							<div class="input-with-actions">
								<input type="text" readonly value={revealData.bridge_url} class="link-input font-mono" />
								<Button variant="secondary" size="sm" onclick={() => copy(revealData?.bridge_url || '', 'HTTP Bridge URL')}>
									<Copy size={14} class="mr-1" />
									Копировать
								</Button>
							</div>
						</div>
					{:else}
						<!-- Direct MTProxy Share -->
						<div class="link-block">
							<div class="link-header">
								<span class="link-label">Прямая ссылка для официального Telegram (в 1 клик):</span>
							</div>
							<div class="input-with-actions">
								<input type="text" readonly value={revealData.mtproxy_url} class="link-input font-mono" />
								<Button variant="primary" size="sm" onclick={() => copy(revealData?.mtproxy_url || '', 'MTProxy ссылка')}>
									<Copy size={14} class="mr-1" />
									Копировать
								</Button>
								<a href={revealData.mtproxy_url} class="btn-open-tg">
									<Send size={14} class="mr-1" />
									Открыть
								</a>
							</div>
						</div>

						<div class="link-block">
							<div class="link-header">
								<span class="link-label">Веб-ссылка (t.me):</span>
							</div>
							<div class="input-with-actions">
								<input type="text" readonly value={revealData.mtproxy_tme_url} class="link-input font-mono" />
								<Button variant="secondary" size="sm" onclick={() => copy(revealData?.mtproxy_tme_url || '', 't.me ссылка')}>
									<Copy size={14} class="mr-1" />
									Копировать
								</Button>
							</div>
						</div>

						<!-- Manual MTProxy Parameters -->
						<div class="manual-box">
							<div class="manual-grid">
								<div class="manual-item">
									<span class="m-label">Сервер:</span>
									<div class="m-val-row">
										<code class="m-code">{revealData.direct_host}</code>
										<button type="button" class="copy-mini-btn" onclick={() => copy(revealData?.direct_host || '', 'Сервер')}>
											<Copy size={12} />
										</button>
									</div>
								</div>
								<div class="manual-item">
									<span class="m-label">Порт:</span>
									<div class="m-val-row">
										<code class="m-code">{revealData.direct_port}</code>
										<button type="button" class="copy-mini-btn" onclick={() => copy(String(revealData?.direct_port || 8443), 'Порт')}>
											<Copy size={12} />
										</button>
									</div>
								</div>
								<div class="manual-item manual-item-wide">
									<span class="m-label">Секрет Fake-TLS ({revealData.tls_domain}):</span>
									<div class="m-val-row">
										<code class="m-code font-mono text-xs">{revealData.mtproxy_secret}</code>
										<button type="button" class="copy-mini-btn" onclick={() => copy(revealData?.mtproxy_secret || '', 'Секрет Fake-TLS')}>
											<Copy size={12} />
										</button>
									</div>
								</div>
							</div>
						</div>
					{/if}
				</div>
			</div>
		</div>
	{/if}

	{#snippet actions()}
		<Button variant="secondary" onclick={closeRevealModal}>Закрыть</Button>
	{/snippet}
</Modal>

<!-- Modal: Rotate Secret Confirmation -->
<Modal bind:open={rotateConfirmOpen} title="Ротация ключа Telegram Proxy" onclose={() => rotateConfirmOpen = false} size="sm">
	<div class="confirm-content">
		<AlertTriangle size={32} class="confirm-icon text-warning mb-3" />
		<p class="confirm-p">
			Будет сгенерирован новый 16-байтный секретный ключ.
		</p>
		<p class="confirm-note">
			<strong>Grace-период:</strong> текущий ключ останется активным в течение 7 дней, чтобы ваши устройства успели обновить настройки. Во время применения произойдёт кратковременный перезапуск процессов (~0.5 сек).
		</p>
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => rotateConfirmOpen = false} disabled={rotateSecretLoading}>Отмена</Button>
		<Button variant="primary" onclick={executeRotateSecret} disabled={rotateSecretLoading}>
			<RefreshCw size={14} class={rotateSecretLoading ? 'spin mr-1' : 'mr-1'} />
			{rotateSecretLoading ? 'Ротация...' : 'Подтвердить ротацию'}
		</Button>
	{/snippet}
</Modal>

<!-- Modal: Revoke Legacy Secret Confirmation -->
<Modal bind:open={revokeConfirmOpen} title="Немедленный отзыв старого ключа" onclose={() => revokeConfirmOpen = false} size="sm">
	<div class="confirm-content">
		<AlertTriangle size={32} class="confirm-icon text-danger mb-3" />
		<p class="confirm-p">
			Вы действительно хотите <strong>немедленно аннулировать старый ключ</strong>?
		</p>
		<p class="confirm-note">
			Все клиенты, использующие прежние ссылки или старый секрет, мгновенно потеряют доступ.
		</p>
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => revokeConfirmOpen = false} disabled={revokeLoading}>Отмена</Button>
		<Button variant="danger" onclick={executeRevokeLegacy} disabled={revokeLoading}>
			<Trash2 size={14} class="mr-1" />
			{revokeLoading ? 'Отзыв...' : 'Отозвать немедленно'}
		</Button>
	{/snippet}
</Modal>

<!-- Modal: Clear Scanner Cache Confirmation -->
<Modal bind:open={clearCacheConfirmOpen} title="Очистка кэша сетевых сканеров" onclose={() => clearCacheConfirmOpen = false} size="sm">
	<div class="confirm-content">
		<ShieldCheck size={32} class="confirm-icon text-primary mb-3" />
		<p class="confirm-p">
			Будет удалён кэш сетевой диагностики <code>/tmp/cache/beobachten.txt</code> и перезапущен демон <code>telemt</code>.
		</p>
		<p class="confirm-note">
			Это полезно, если сетевые сканеры провайдера временно внесли порты или IP в чёрный список.
		</p>
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => clearCacheConfirmOpen = false} disabled={clearCacheLoading}>Отмена</Button>
		<Button variant="primary" onclick={executeClearScannerCache} disabled={clearCacheLoading}>
			{clearCacheLoading ? 'Очистка...' : 'Очистить кэш'}
		</Button>
	{/snippet}
</Modal>

<!-- Modal: Settings -->
<Modal bind:open={settingsOpen} title="Параметры Telegram Proxy" onclose={() => settingsOpen = false} size="md">
	<div class="settings-form">
		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Прямой хост MTProxy (для телефонов):</span>
				<input type="text" bind:value={editConfig.direct_host} class="text-input" placeholder="router.example.com" />
				<span class="field-hint">Домен KeenDNS/DDNS или белый IP роутера</span>
			</label>
			<label class="form-field">
				<span class="field-label">Порт MTProxy (telemt):</span>
				<input type="number" bind:value={editConfig.direct_port} class="text-input" placeholder="8443" />
				<span class="field-hint">Порт прямого Fake-TLS</span>
			</label>
		</div>

		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Домен SNI для Fake-TLS:</span>
				<input type="text" bind:value={editConfig.tls_domain} class="text-input" placeholder="example.com" />
				<span class="field-hint">Доступный домен с поддержкой TLS</span>
			</label>
			<label class="form-field">
				<span class="field-label">Интерфейс выхода (Egress):</span>
				<input type="text" bind:value={editConfig.upstream_device} class="text-input" placeholder="nwg1" />
				<span class="field-hint">Сетевой интерфейс выхода (nwg1, ISP и т.д.)</span>
			</label>
		</div>

		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Публичный хост CDN (Web Proxy):</span>
				<input type="text" bind:value={editConfig.public_hostname} class="text-input" />
			</label>
			<label class="form-field">
				<span class="field-label">Режим транспорта:</span>
				<select bind:value={editConfig.carrier_mode} class="select-input">
					<option value="get">get (CDN Friendly · GET-запросы)</option>
					<option value="websocket">websocket</option>
					<option value="https">https</option>
				</select>
			</label>
		</div>

		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Порт tproxy-server:</span>
				<input type="number" bind:value={editConfig.listen_port} class="text-input" />
			</label>
			<label class="form-field">
				<span class="field-label">Админ-порт метрик:</span>
				<input type="number" bind:value={editConfig.admin_port} class="text-input" />
			</label>
		</div>

		<div class="form-field">
			<span class="field-label">Бэкенд MTProxy:</span>
			<input type="text" bind:value={editConfig.backend} class="text-input" />
			<span class="field-hint">Адрес локального сырого telemt (по умолчанию 127.0.0.1:2398)</span>
		</div>
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => settingsOpen = false}>Отмена</Button>
		<Button variant="primary" onclick={saveSettings} disabled={saveConfigLoading}>
			{saveConfigLoading ? 'Сохранение...' : 'Сохранить'}
		</Button>
	{/snippet}
</Modal>

<TelegramProxyWizard
	bind:open={wizardOpen}
	onclose={() => (wizardOpen = false)}
	onapplied={loadData}
/>

<style>
	.tg-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		padding: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	.card-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
		flex-wrap: wrap;
	}

	.header-left {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.title-row {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}

	.title {
		font-size: 16px;
		font-weight: 600;
		color: var(--color-text-primary);
		margin: 0;
	}

	.subtitle {
		font-size: 12px;
		color: var(--color-text-muted);
	}

	.proto-badge {
		font-size: 11px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
	}

	.badge {
		font-size: 11px;
		padding: 2px 8px;
		border-radius: var(--radius-sm);
		font-weight: 500;
	}

	.badge-success {
		background: rgba(34, 197, 94, 0.15);
		color: #22c55e;
	}

	.badge-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.badge-warning {
		background: rgba(234, 179, 8, 0.15);
		color: #eab308;
	}

	.led {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.led-running {
		background: #22c55e;
		box-shadow: 0 0 6px #22c55e;
	}

	.led-stopped {
		background: var(--color-text-muted);
		opacity: 0.5;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.icon-btn {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
		padding: 6px;
		border-radius: var(--radius-sm);
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: color 0.15s, border-color 0.15s;
	}

	.icon-btn:hover {
		color: var(--color-text-primary);
		border-color: var(--color-border-hover);
	}

	.spin {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	.meta-strip {
		display: flex;
		align-items: center;
		gap: 1rem;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 0.65rem 1rem;
		flex-wrap: wrap;
	}

	.strip-item {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.strip-label {
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.strip-val {
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-primary);
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.strip-divider {
		width: 1px;
		height: 24px;
		background: var(--color-border);
	}

	.health-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--color-text-muted);
	}

	.health-dot.online {
		background: #22c55e;
		box-shadow: 0 0 4px #22c55e;
	}

	.health-dot.degraded {
		background: #eab308;
		box-shadow: 0 0 4px #eab308;
	}

	.health-dot.stopped {
		background: #ef4444;
	}

	.health-text {
		font-size: 12px;
	}

	.grace-alert {
		background: rgba(234, 179, 8, 0.1);
		border: 1px solid rgba(234, 179, 8, 0.3);
		border-radius: var(--radius-sm);
		padding: 0.75rem 1rem;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		flex-wrap: wrap;
	}

	.grace-alert-left {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	:global(.grace-icon) {
		color: #eab308;
		flex-shrink: 0;
	}

	.grace-text {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.grace-title {
		font-size: 13px;
		font-weight: 600;
		color: #eab308;
	}

	.grace-desc {
		font-size: 12px;
		color: var(--color-text-secondary);
	}

	.actions-strip {
		display: flex;
		align-items: center;
		justify-content: space-between;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 0.75rem 1rem;
		gap: 1rem;
		flex-wrap: wrap;
	}

	.key-display-box {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	:global(.key-icon) {
		color: var(--color-text-muted);
	}

	.key-label {
		font-size: 12px;
		color: var(--color-text-muted);
	}

	.key-code {
		font-size: 12px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		padding: 2px 8px;
		border-radius: var(--radius-sm);
		color: var(--color-text-primary);
	}

	.actions-right {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.mode-tabs {
		display: flex;
		gap: 6px;
		border-bottom: 1px solid var(--color-border);
		padding-bottom: 8px;
	}

	.mode-tab {
		display: flex;
		align-items: center;
		padding: 6px 12px;
		border-radius: var(--radius-sm);
		border: 1px solid transparent;
		background: transparent;
		color: var(--color-text-secondary);
		font-size: 13px;
		cursor: pointer;
		transition: all 0.15s;
	}

	.mode-tab:hover {
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
	}

	.mode-tab.active {
		background: var(--color-bg-primary);
		border-color: var(--color-border);
		color: var(--color-text-primary);
		font-weight: 500;
	}

	.tab-badge {
		font-size: 10px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		padding: 1px 5px;
		border-radius: 4px;
		margin-left: 8px;
		color: var(--color-text-muted);
	}

	.tab-badge-rec {
		background: rgba(34, 197, 94, 0.15);
		border-color: rgba(34, 197, 94, 0.3);
		color: #22c55e;
	}

	.info-container {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 1rem;
	}

	.info-pane {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	.info-header {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.info-badge {
		font-size: 11px;
		font-weight: 600;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		padding: 2px 6px;
		border-radius: 4px;
		color: var(--color-text-primary);
	}

	.info-p {
		font-size: 12px;
		line-height: 1.5;
		color: var(--color-text-secondary);
		margin: 0;
	}

	/* Reveal Modal Styles */
	.reveal-loading {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: 2.5rem;
		color: var(--color-text-muted);
	}

	.reveal-content {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.reveal-tabs {
		display: flex;
		gap: 8px;
		border-bottom: 1px solid var(--color-border);
		padding-bottom: 8px;
	}

	.reveal-tab {
		display: flex;
		align-items: center;
		padding: 6px 12px;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
		background: var(--color-bg-primary);
		color: var(--color-text-secondary);
		font-size: 12px;
		cursor: pointer;
	}

	.reveal-tab.active {
		background: var(--color-primary, #3b82f6);
		color: white;
		border-color: var(--color-primary, #3b82f6);
		font-weight: 500;
	}

	.reveal-body {
		display: grid;
		grid-template-columns: 240px 1fr;
		gap: 1.25rem;
		align-items: start;
	}

	@media (max-width: 640px) {
		.reveal-body {
			grid-template-columns: 1fr;
		}
	}

	.reveal-qr-col {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
	}

	.qr-wrapper {
		background: white;
		padding: 8px;
		border-radius: var(--radius-sm);
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
	}

	.qr-img {
		width: 220px;
		height: 220px;
		display: block;
	}

	.qr-placeholder {
		width: 220px;
		height: 220px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--color-bg-secondary);
		border: 1px dashed var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		font-size: 12px;
	}

	.qr-hint {
		font-size: 11px;
		color: var(--color-text-muted);
		text-align: center;
		line-height: 1.4;
	}

	.reveal-links-col {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.link-block {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.link-header {
		display: flex;
		align-items: center;
	}

	.link-label {
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-secondary);
	}

	.input-with-actions {
		display: flex;
		gap: 6px;
	}

	.link-input {
		flex: 1;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 5px 8px;
		font-size: 12px;
		color: var(--color-text-primary);
		outline: none;
	}

	.btn-open-tg {
		display: inline-flex;
		align-items: center;
		background: #229ed9;
		color: white;
		border-radius: var(--radius-sm);
		padding: 0 10px;
		font-size: 12px;
		font-weight: 500;
		text-decoration: none;
		white-space: nowrap;
		transition: opacity 0.15s;
	}

	.btn-open-tg:hover {
		opacity: 0.9;
	}

	.manual-box {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 0.75rem;
	}

	.manual-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
	}

	.manual-item-wide {
		grid-column: span 2;
	}

	.m-label {
		font-size: 11px;
		color: var(--color-text-muted);
		display: block;
		margin-bottom: 2px;
	}

	.m-val-row {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.m-code {
		font-size: 12px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		flex: 1;
		word-break: break-all;
	}

	.copy-mini-btn {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-muted);
		padding: 3px 5px;
		border-radius: var(--radius-sm);
		cursor: pointer;
	}

	.copy-mini-btn:hover {
		color: var(--color-text-primary);
	}

	/* Settings Form */
	.settings-form {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.form-row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	.form-field {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.field-label {
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.field-hint {
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.text-input, .select-input {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 6px 10px;
		font-size: 13px;
		color: var(--color-text-primary);
	}

	/* Confirmation Modals */
	.confirm-content {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		padding: 0.5rem 0;
	}

	:global(.confirm-icon) {
		margin-bottom: 0.5rem;
	}

	.confirm-p {
		font-size: 13px;
		color: var(--color-text-primary);
		margin: 0 0 8px 0;
	}

	.confirm-note {
		font-size: 12px;
		color: var(--color-text-secondary);
		line-height: 1.4;
		margin: 0;
		background: var(--color-bg-primary);
		padding: 8px 12px;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
	}

	.text-warning {
		color: #eab308;
	}

	.text-danger {
		color: #ef4444;
	}

	.text-primary {
		color: #3b82f6;
	}

	.mr-1 { margin-right: 4px; }
	.mr-1\.5 { margin-right: 6px; }
	.ml-1 { margin-left: 4px; }
	.mb-2 { margin-bottom: 8px; }
	.mb-3 { margin-bottom: 12px; }
</style>
