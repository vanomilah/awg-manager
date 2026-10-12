<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { errText } from '$lib/utils/errorMessage';
	import { createSelfReschedulingPoll } from '$lib/utils/selfReschedulingPoll';
	import { Card, Button, Badge, Toggle, Input } from '$lib/components/ui';
	import LinkBox from '$lib/components/proxy/LinkBox.svelte';
	import type { OpenFluxConfig, OpenFluxStatus } from '$lib/types';
	import {
		Play,
		Square,
		RotateCw,
		Download,
		Key,
		ShieldCheck,
		FileText,
		Layers,
		Terminal,
		RefreshCw,
		Copy
	} from 'lucide-svelte';
	import { copyToClipboard } from '$lib/utils/clipboard';

	let status = $state<OpenFluxStatus | null>(null);
	let config = $state<OpenFluxConfig | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let installing = $state(false);
	let starting = $state(false);
	let stopping = $state(false);
	let restarting = $state(false);
	let logs = $state('');
	let logsLoading = $state(false);

	// Poll status every 3 seconds
	const statusPoll = createSelfReschedulingPoll(async () => {
		await fetchStatus();
	}, 3000);

	async function fetchStatus() {
		try {
			const st = await api.openfluxStatus();
			status = st;
			if (st.config && !config) {
				config = JSON.parse(JSON.stringify(st.config));
			}
			if (st.logTail) {
				logs = st.logTail;
			}
		} catch (e) {
			console.error('Failed to fetch openflux status:', e);
		}
	}

	async function fetchLogs() {
		logsLoading = true;
		try {
			const res = await api.openfluxLogs(150);
			logs = res.logs;
		} catch (e) {
			notifications.error('Не удалось загрузить логи: ' + errText(e));
		} finally {
			logsLoading = false;
		}
	}

	onMount(async () => {
		try {
			await fetchStatus();
			await fetchLogs();
		} finally {
			loading = false;
		}
		statusPoll.start();
	});

	onDestroy(() => {
		statusPoll.stop();
	});

	async function handleInstall() {
		installing = true;
		try {
			notifications.info('Запуск загрузки и установки OpenFlux...');
			status = await api.openfluxInstall();
			notifications.success('OpenFlux успешно установлен');
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка установки: ' + errText(e));
		} finally {
			installing = false;
		}
	}

	async function handleStart() {
		starting = true;
		try {
			status = await api.openfluxStart();
			notifications.success('OpenFlux запущен');
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка запуска: ' + errText(e));
		} finally {
			starting = false;
		}
	}

	async function handleStop() {
		stopping = true;
		try {
			status = await api.openfluxStop();
			notifications.success('OpenFlux остановлен');
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка остановки: ' + errText(e));
		} finally {
			stopping = false;
		}
	}

	async function handleRestart() {
		restarting = true;
		try {
			status = await api.openfluxRestart();
			notifications.success('OpenFlux перезапущен');
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка перезапуска: ' + errText(e));
		} finally {
			restarting = false;
		}
	}

	async function handleSaveConfig() {
		if (!config) return;
		saving = true;
		try {
			status = await api.openfluxSaveConfig(config);
			notifications.success('Настройки OpenFlux сохранены');
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка сохранения: ' + errText(e));
		} finally {
			saving = false;
		}
	}

	function generateSecret() {
		if (!config) return;
		// 32-character hexadecimal string
		const arr = new Uint8Array(16);
		crypto.getRandomValues(arr);
		config.secret = Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	function isTransportEnabled(name: string): boolean {
		return !!config?.transports.includes(name);
	}

	function toggleTransport(name: string) {
		if (!config) return;
		if (config.transports.includes(name)) {
			config.transports = config.transports.filter((t) => t !== name);
		} else {
			config.transports = [...config.transports, name];
		}
	}

	function getDirectPort(): number {
		if (!config?.directListen) return 8445;
		const parts = config.directListen.split(':');
		const p = parseInt(parts[parts.length - 1], 10);
		return isNaN(p) ? 8445 : p;
	}

	function setDirectPort(port: number) {
		if (!config) return;
		config.directListen = `0.0.0.0:${port}`;
	}

	function formatUptime(uptimeS?: number): string {
		if (!uptimeS || uptimeS <= 0) return '0с';
		const d = Math.floor(uptimeS / 86400);
		const h = Math.floor((uptimeS % 86400) / 3600);
		const m = Math.floor((uptimeS % 3600) / 60);
		const s = uptimeS % 60;
		if (d > 0) return `${d}д ${h}ч ${m}м`;
		if (h > 0) return `${h}ч ${m}м ${s}с`;
		if (m > 0) return `${m}м ${s}с`;
		return `${s}с`;
	}
</script>

<div class="openflux-container">
	{#if loading}
		<Card>
			<div class="loading-state">Загрузка состояния OpenFlux...</div>
		</Card>
	{:else}
		<!-- Status and Management Banner -->
		<Card>
			<div class="status-header">
				<div class="status-left">
					<div class="service-title-row">
						<span class="service-title">OpenFlux Exit Node</span>
						{#if status?.archSupported === false}
							<Badge variant="error">Архитектура {status.arch} не поддерживается</Badge>
						{:else if !status?.installed}
							<Badge variant="warning">Не установлен</Badge>
						{:else if status?.running}
							<Badge variant="success">Работает (PID: {status.pid})</Badge>
						{:else}
							<Badge variant="muted">Остановлен</Badge>
						{/if}
						{#if status?.version}
							<span class="version-label">v{status.version}</span>
						{/if}
					</div>

					<div class="status-details">
						<span class="detail-item">
							Режим: <strong>{config?.mode?.toUpperCase() ?? 'L4'} ({config?.transportMode === 'session' ? 'Мульти-сессия' : 'Одиночный'})</strong>
						</span>
						{#if status?.running && status?.uptimeS}
							<span class="detail-item">
								Аптайм: <strong>{formatUptime(status.uptimeS)}</strong>
							</span>
						{/if}
						<span class="detail-item">
							Транспорты:
							<strong>
								{config?.transports && config.transports.length > 0 ? config.transports.join(', ') : 'нет'}
							</strong>
						</span>
					</div>

					{#if status?.error}
						<div class="status-error">
							{status.error}
						</div>
					{/if}
				</div>

				<div class="status-actions">
					{#if !status?.installed}
						<Button
							variant="primary"
							loading={installing}
							disabled={installing || status?.archSupported === false}
							onclick={handleInstall}
						>
							{#snippet iconBefore()}
								<Download size={16} />
							{/snippet}
							Установить
						</Button>
					{:else}
						{#if status.running}
							<Button variant="danger" loading={stopping} disabled={stopping} onclick={handleStop}>
								{#snippet iconBefore()}
									<Square size={16} />
								{/snippet}
								Остановить
							</Button>
							<Button variant="secondary" loading={restarting} disabled={restarting} onclick={handleRestart}>
								{#snippet iconBefore()}
									<RotateCw size={16} />
								{/snippet}
								Перезапустить
							</Button>
						{:else}
							<Button variant="primary" loading={starting} disabled={starting} onclick={handleStart}>
								{#snippet iconBefore()}
									<Play size={16} />
								{/snippet}
								Запустить
							</Button>
						{/if}
						<Button variant="ghost" loading={installing} disabled={installing} onclick={handleInstall} title="Переустановить бинарник">
							{#snippet iconBefore()}
								<Download size={16} />
							{/snippet}
							Обновить
						</Button>
					{/if}
				</div>
			</div>
		</Card>

		<!-- Share Link Card -->
		{#if status?.link}
			<Card>
				<div class="card-section-title">
					<Layers size={18} />
					<span>Ссылка для подключения (qThroneMobile / OpenFlux)</span>
				</div>
				<p class="section-desc">
					Импортируйте ссылку в <strong>qThroneMobile</strong> на смартфоне или в десктопный клиент OpenFlux.
					Клиент автоматически согласует и будет использовать доступные транспорты (Mail.ru, Yandex Docs или прямой TCP).
				</p>
				<LinkBox link={status.link} title="OpenFlux V1 Link" />
			</Card>
		{/if}

		<!-- Configuration Card -->
		{#if config}
			<Card>
				<div class="card-section-title">
					<ShieldCheck size={18} />
					<span>Параметры сервера OpenFlux</span>
				</div>

				<div class="config-grid">
					<!-- General Settings -->
					<div class="config-row">
						<div class="config-label">
							<strong>Автозапуск сервиса</strong>
							<span class="subtext">Запускать OpenFlux автоматически при старте роутера</span>
						</div>
						<div class="config-ctrl">
							<Toggle
								bind:checked={config.enabled}
								onchange={(v) => {
									if (config) config.enabled = v;
								}}
							/>
						</div>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Имя профиля</strong>
							<span class="subtext">Отображается в названии узла в клиенте</span>
						</div>
						<div class="config-ctrl">
							<Input bind:value={config.name} placeholder="Keenetic" />
						</div>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Режим работы (Mode)</strong>
							<span class="subtext">
								L4 — встроенный пользовательский TCP/UDP стек (gVisor), без системного TUN.
								L3 — сетевой уровень TUN (требует прав ядра).
							</span>
						</div>
						<div class="config-ctrl select-ctrl">
							<select bind:value={config.mode} class="form-select">
								<option value="l4">L4 (Пользовательский стрим-прокси, рекомендуется)</option>
								<option value="l3">L3 (TUN сетевой уровень)</option>
							</select>
						</div>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Сессионный режим (Transport Mode)</strong>
							<span class="subtext">
								Мульти-сессия (--negotiate) согласует и чередует транспорты между клиентом и сервером.
							</span>
						</div>
						<div class="config-ctrl select-ctrl">
							<select bind:value={config.transportMode} class="form-select">
								<option value="session">Мульти-транспортная сессия (--negotiate, рекомендуется)</option>
								<option value="single">Одиночный транспорт (Single)</option>
							</select>
						</div>
					</div>

					<!-- Transports Section -->
					<div class="section-divider">
						<span>Транспорты</span>
					</div>

					<!-- Direct TCP -->
					<div class="transport-block">
						<div class="transport-header">
							<div class="transport-title-row">
								<input
									type="checkbox"
									id="transport-direct"
									checked={isTransportEnabled('direct')}
									onchange={() => toggleTransport('direct')}
								/>
								<label for="transport-direct"><strong>Прямой TCP транспорт (Direct TCP)</strong></label>
							</div>
							<span class="subtext">Прямое TCP соединение на открытый порт роутера</span>
						</div>

						{#if isTransportEnabled('direct')}
							<div class="transport-fields">
								<div class="field-item">
									<label for="direct-port">Порт прослушивания:</label>
									<input
										id="direct-port"
										type="number"
										class="form-input"
										value={getDirectPort()}
										oninput={(e) => setDirectPort(parseInt((e.target as HTMLInputElement).value, 10))}
									/>
								</div>
								<div class="field-item">
									<label for="direct-host">Внешний хост / домен (необязательно):</label>
									<input
										id="direct-host"
										type="text"
										class="form-input"
										placeholder="Автоопределение (KeenDNS / WAN IP)"
										bind:value={config.directHost}
									/>
								</div>
								<div class="field-toggle-row">
									<label class="toggle-label">
										<input type="checkbox" bind:checked={config.openFirewall} />
										<span>Открыть порт в файрволе Keenetic (WAN INPUT)</span>
									</label>
								</div>
							</div>
						{/if}
					</div>

					<!-- Mail.ru Docs -->
					<div class="transport-block">
						<div class="transport-header">
							<div class="transport-title-row">
								<input
									type="checkbox"
									id="transport-mailru"
									checked={isTransportEnabled('mailru')}
									onchange={() => toggleTransport('mailru')}
								/>
								<label for="transport-mailru"><strong>Mail.ru Документы (Mail.ru Docs)</strong></label>
							</div>
							<span class="subtext">Маскировка и передача туннелированного трафика через документ Mail.ru Docs</span>
						</div>

						{#if isTransportEnabled('mailru')}
							<div class="transport-fields">
								<div class="field-item full-width">
									<label for="mailru-url">URL документа Mail.ru:</label>
									<input
										id="mailru-url"
										type="text"
										class="form-input"
										placeholder="https://docs.mail.ru/document/..."
										bind:value={config.mailruUrl}
									/>
								</div>
							</div>
						{/if}
					</div>

					<!-- Yandex Docs -->
					<div class="transport-block">
						<div class="transport-header">
							<div class="transport-title-row">
								<input
									type="checkbox"
									id="transport-yandex"
									checked={isTransportEnabled('yandex')}
									onchange={() => toggleTransport('yandex')}
								/>
								<label for="transport-yandex"><strong>Яндекс Документы (Yandex Docs)</strong></label>
							</div>
							<span class="subtext">Маскировка и передача трафика через облачный документ Яндекс Документов</span>
						</div>

						{#if isTransportEnabled('yandex')}
							<div class="transport-fields">
								<div class="field-item full-width">
									<label for="yandex-url">URL документа Яндекс:</label>
									<input
										id="yandex-url"
										type="text"
										class="form-input"
										placeholder="https://docs.yandex.ru/docs/view?url=..."
										bind:value={config.yandexUrl}
									/>
								</div>
							</div>
						{/if}
					</div>

					<!-- Security & Codec Section -->
					<div class="section-divider">
						<span>Безопасность и кодек</span>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Общий секретный ключ (Secret Key)</strong>
							<span class="subtext">
								Ключ шифрования AES-256-GCM. Минимум 16 символов. Должен совпадать у клиента и сервера.
							</span>
						</div>
						<div class="config-ctrl secret-ctrl">
							<Input bind:value={config.secret} placeholder="Секретный ключ..." />
							<Button variant="secondary" onclick={generateSecret} title="Сгенерировать случайный 32-значный ключ">
								{#snippet iconBefore()}
									<Key size={15} />
								{/snippet}
								Ключ
							</Button>
						</div>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Кодек пакетов (Codec)</strong>
							<span class="subtext">
								batched — пакетирование и сжатие трафика (по умолчанию). legacy — покадровый режим.
							</span>
						</div>
						<div class="config-ctrl select-ctrl">
							<select bind:value={config.codec} class="form-select">
								<option value="batched">batched (Пакетирование, рекомендуется)</option>
								<option value="legacy">legacy (Покадровый)</option>
							</select>
						</div>
					</div>

					<div class="config-row">
						<div class="config-label">
							<strong>Уровень отладки (Debug log)</strong>
							<span class="subtext">Вывод подробной информации о сессиях в лог</span>
						</div>
						<div class="config-ctrl select-ctrl">
							<select bind:value={config.debug} class="form-select">
								<option value={0}>0 — Выключено</option>
								<option value={1}>1 — Базовая отладка (-d)</option>
								<option value={2}>2 — Подробная отладка (-dd)</option>
								<option value={3}>3 — Трассировка пакетов (-ddd)</option>
							</select>
						</div>
					</div>
				</div>

				<div class="save-actions">
					<Button variant="primary" loading={saving} disabled={saving} onclick={handleSaveConfig}>
						Сохранить настройки
					</Button>
				</div>
			</Card>
		{/if}

		<!-- Log Output Card -->
		<Card>
			<div class="logs-header">
				<div class="card-section-title">
					<Terminal size={18} />
					<span>Журнал работы OpenFlux</span>
				</div>
				<div class="logs-actions">
					<Button
						variant="ghost"
						size="sm"
						loading={logsLoading}
						onclick={fetchLogs}
						title="Обновить журнал"
					>
						{#snippet iconBefore()}
							<RefreshCw size={14} />
						{/snippet}
						Обновить
					</Button>
					<Button
						variant="ghost"
						size="sm"
						disabled={!logs}
						onclick={() => {
							if (logs) {
								copyToClipboard(logs);
								notifications.success('Логи скопированы');
							}
						}}
						title="Скопировать журнал"
					>
						{#snippet iconBefore()}
							<Copy size={14} />
						{/snippet}
						Копировать
					</Button>
				</div>
			</div>

			<pre class="log-viewer">{logs || 'Журнал пуст'}</pre>
		</Card>
	{/if}
</div>

<style>
	.openflux-container {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		margin-top: 0.5rem;
	}

	.loading-state {
		padding: 2rem;
		text-align: center;
		color: var(--color-text-muted);
	}

	.status-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		flex-wrap: wrap;
		gap: 1.5rem;
	}

	.status-left {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		min-width: 250px;
	}

	.service-title-row {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		flex-wrap: wrap;
	}

	.service-title {
		font-size: 1.125rem;
		font-weight: 600;
		color: var(--color-text-heading);
	}

	.version-label {
		font-size: 0.8125rem;
		color: var(--color-text-muted);
		font-family: var(--font-mono);
	}

	.status-details {
		display: flex;
		gap: 1.25rem;
		flex-wrap: wrap;
		font-size: 0.875rem;
		color: var(--color-text-muted);
		margin-top: 0.25rem;
	}

	.detail-item strong {
		color: var(--color-text-body);
	}

	.status-error {
		color: var(--color-error);
		font-size: 0.875rem;
		margin-top: 0.25rem;
	}

	.status-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.card-section-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 1rem;
		font-weight: 600;
		color: var(--color-text-heading);
		margin-bottom: 0.5rem;
	}

	.section-desc {
		font-size: 0.875rem;
		color: var(--color-text-muted);
		margin-bottom: 1rem;
		line-height: 1.4;
	}

	.config-grid {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.config-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0.75rem 0;
		border-bottom: 1px solid var(--color-border);
		gap: 1rem;
	}

	.config-label {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		flex: 1;
	}

	.subtext {
		font-size: 0.8125rem;
		color: var(--color-text-muted);
		line-height: 1.35;
	}

	.config-ctrl {
		display: flex;
		align-items: center;
		min-width: 200px;
	}

	.select-ctrl {
		min-width: 320px;
	}

	.secret-ctrl {
		display: flex;
		gap: 0.5rem;
		min-width: 340px;
	}

	.form-select {
		width: 100%;
		padding: 0.5rem 0.75rem;
		background: var(--color-bg-input, var(--color-surface));
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md, 6px);
		color: var(--color-text-body);
		font-size: 0.875rem;
	}

	.form-input {
		width: 100%;
		padding: 0.5rem 0.75rem;
		background: var(--color-bg-input, var(--color-surface));
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md, 6px);
		color: var(--color-text-body);
		font-size: 0.875rem;
	}

	.section-divider {
		padding: 0.75rem 0 0.25rem;
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--color-text-heading);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		border-bottom: 1px solid var(--color-border);
	}

	.transport-block {
		background: var(--color-bg-card-alt, rgba(255, 255, 255, 0.02));
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md, 8px);
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.transport-header {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.transport-title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.9375rem;
	}

	.transport-fields {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 0.75rem;
		margin-top: 0.25rem;
		padding-top: 0.75rem;
		border-top: 1px dashed var(--color-border);
	}

	.field-item {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
		font-size: 0.8125rem;
		color: var(--color-text-muted);
	}

	.field-item.full-width {
		grid-column: 1 / -1;
	}

	.field-toggle-row {
		grid-column: 1 / -1;
		margin-top: 0.25rem;
	}

	.toggle-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8125rem;
		cursor: pointer;
		color: var(--color-text-body);
	}

	.save-actions {
		display: flex;
		justify-content: flex-end;
		margin-top: 1.25rem;
	}

	.logs-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.75rem;
	}

	.logs-actions {
		display: flex;
		gap: 0.5rem;
	}

	.log-viewer {
		background: #0d1117;
		color: #c9d1d9;
		padding: 1rem;
		border-radius: 6px;
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;
		line-height: 1.45;
		max-height: 350px;
		overflow-y: auto;
		white-space: pre-wrap;
		word-break: break-all;
		margin: 0;
	}

	@media (max-width: 768px) {
		.config-row {
			flex-direction: column;
			align-items: flex-start;
			gap: 0.5rem;
		}

		.config-ctrl,
		.select-ctrl,
		.secret-ctrl {
			width: 100%;
			min-width: 0;
		}
	}
</style>
