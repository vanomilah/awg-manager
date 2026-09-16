<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { Button, Modal } from '$lib/components/ui';
	import type { XrayConfig, XrayStatus, XrayClient, XrayMigrationStatus } from '$lib/types';
	import XrayShareModal from './XrayShareModal.svelte';
	import XrayAutoTunnelModal from './XrayAutoTunnelModal.svelte';
	import XrayWizard from './xray-wizard/XrayWizard.svelte';
	import {
		Power,
		RotateCw,
		Plus,
		Trash2,
		Copy,
		QrCode,
		Link2,
		Router,
		Settings,
		ShieldCheck,
		ExternalLink,
		Check,
		AlertTriangle,
		Sparkles,
	} from 'lucide-svelte';

	let status = $state<XrayStatus | null>(null);
	let config = $state<XrayConfig | null>(null);
	let migrationStatus = $state<XrayMigrationStatus | null>(null);
	let conflictResolving = $state(false);
	let clients = $derived(status?.clients ?? config?.clients ?? []);
	let loading = $state(true);
	let actionLoading = $state(false);
	let wizardOpen = $state(false);

	// Client modals
	let shareOpen = $state(false);
	let autoTunnelOpen = $state(false);
	let activeClientId = $state('');
	let activeClientRemark = $state('');

	// Add client modal
	let addClientOpen = $state(false);
	let newClientRemark = $state('');
	let addClientLoading = $state(false);

	// Settings modal
	let settingsOpen = $state(false);
	let editConfig = $state<Partial<XrayConfig>>({});
	let saveConfigLoading = $state(false);

	onMount(() => {
		void loadData();
		const interval = setInterval(loadStatus, 5000);
		return () => clearInterval(interval);
	});

	async function loadData() {
		loading = true;
		try {
			const [s, c, m] = await Promise.all([
				api.getXrayServerStatus(),
				api.getXrayServerConfig(),
				api.getXrayMigrationStatus().catch(() => null),
			]);
			status = s;
			config = c;
			migrationStatus = m;
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка загрузки данных Xray');
		} finally {
			loading = false;
		}
	}

	async function handleResolveConflict(action: 'keep_new' | 'keep_legacy' | 'import_legacy_draft') {
		conflictResolving = true;
		try {
			const res = await api.resolveXrayConflict(action);
			if (res.success) {
				notifications.success(
					action === 'keep_new'
						? 'Выбран новый Xray сервер'
						: action === 'keep_legacy'
						? 'Сохранена конфигурация Legacy'
						: 'Настройки Legacy импортированы'
				);
				await loadData();
			}
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка разрешения конфликта');
		} finally {
			conflictResolving = false;
		}
	}

	async function loadStatus() {
		try {
			status = await api.getXrayServerStatus();
		} catch {
			// ignore polling error
		}
	}

	async function togglePower() {
		actionLoading = true;
		try {
			const nextAction = status?.running ? 'stop' : 'start';
			status = await api.xrayServerAction(nextAction);
			notifications.success(status?.running ? 'Xray сервер запущен' : 'Xray сервер остановлен');
			if (config && status) config.enabled = status.running;
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка управления Xray');
		} finally {
			actionLoading = false;
		}
	}

	async function restartServer() {
		actionLoading = true;
		try {
			status = await api.xrayServerAction('restart');
			notifications.success('Xray сервер перезапущен');
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка перезапуска Xray');
		} finally {
			actionLoading = false;
		}
	}

	async function handleAddClient() {
		if (!newClientRemark.trim()) {
			notifications.error('Введите имя или примечание для клиента');
			return;
		}
		addClientLoading = true;
		try {
			const client = await api.addXrayClient(newClientRemark.trim());
			notifications.success(`Клиент «${client.remark}» добавлен`);
			newClientRemark = '';
			addClientOpen = false;
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка добавления клиента');
		} finally {
			addClientLoading = false;
		}
	}

	async function handleDeleteClient(client: XrayClient) {
		if (!confirm(`Удалить клиента «${client.remark}»?`)) return;
		try {
			await api.deleteXrayClient(client.id);
			notifications.success(`Клиент «${client.remark}» удалён`);
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка удаления клиента');
		}
	}

	async function handleToggleClient(client: XrayClient) {
		try {
			await api.toggleXrayClient(client.id, !client.enabled);
			notifications.success(client.enabled ? `Клиент «${client.remark}» отключён` : `Клиент «${client.remark}» включён`);
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка переключения клиента');
		}
	}

	function openShare(client: XrayClient) {
		activeClientId = client.id;
		activeClientRemark = client.remark;
		shareOpen = true;
	}

	function openAutoTunnel(client: XrayClient) {
		activeClientId = client.id;
		activeClientRemark = client.remark;
		autoTunnelOpen = true;
	}

	function openSettingsModal() {
		editConfig = config ? { ...config } : {};
		settingsOpen = true;
	}

	async function saveSettings() {
		saveConfigLoading = true;
		try {
			config = await api.updateXrayServerConfig(editConfig);
			notifications.success('Настройки Xray сохранены');
			settingsOpen = false;
			await loadData();
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка сохранения настроек');
		} finally {
			saveConfigLoading = false;
		}
	}

	async function copy(text: string, label: string) {
		if (await copyToClipboard(text)) {
			notifications.success(`${label} скопирован`);
		}
	}
</script>

<div class="xray-card">
	<!-- Top Bar / Header -->
	<header class="card-header">
		<div class="header-left">
			<div class="title-row">
				<span class="led" class:led-running={status?.running} class:led-stopped={!status?.running}></span>
				<h2 class="title">Xray VLESS · CDN</h2>
				<span class="badge badge-{status?.running ? 'success' : 'neutral'}">
					{status?.running ? 'Работает' : 'Остановлен'}
				</span>
				<span class="version-badge">Xray {status?.version || '26.7.28'}</span>
			</div>
			<div class="subtitle">
				VLESS-туннель через совместимый CDN с транспортом XHTTP
			</div>
		</div>

		<div class="header-actions">
			<button
				type="button"
				class="icon-btn"
				title="Перезапустить Xray"
				onclick={restartServer}
				disabled={actionLoading}
			>
				<RotateCw size={16} class={actionLoading ? 'spin' : ''} />
			</button>
			<button
				type="button"
				class="icon-btn"
				title="Настройки параметров CDN и портов"
				onclick={openSettingsModal}
			>
				<Settings size={16} />
			</button>
			<Button
				variant="secondary"
				size="sm"
				onclick={() => (wizardOpen = true)}
				title="Пошаговый мастер настройки Xray VLESS"
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

	{#if status?.recovery_required}
		<div class="alert-banner warning">
			<AlertTriangle size={18} class="banner-icon text-amber-500" />
			<div class="banner-content">
				<div class="banner-title">Требуется восстановление конфигурации</div>
				<div class="banner-desc">{status.recovery_reason || 'Файл настроек повреждён. Исходный файл не был изменён.'}</div>
			</div>
		</div>
	{/if}

	{#if migrationStatus?.discovery?.found && migrationStatus?.discovery?.topology === 'C'}
		<div class="alert-banner error">
			<AlertTriangle size={18} class="banner-icon text-red-500" />
			<div class="banner-content">
				<div class="banner-title">Конфликт конфигурации Legacy Xray</div>
				<div class="banner-desc">{migrationStatus.discovery.conflict_reason || 'Обнаружена неподдерживаемая или неоднозначная конфигурация.'}</div>
				<div class="banner-actions">
					<Button variant="secondary" size="sm" disabled={conflictResolving} onclick={() => handleResolveConflict('keep_new')}>
						Использовать новый Xray
					</Button>
					<Button variant="secondary" size="sm" disabled={conflictResolving} onclick={() => handleResolveConflict('keep_legacy')}>
						Оставить Legacy (S99xray-cdn)
					</Button>
				</div>
			</div>
		</div>
	{:else if migrationStatus?.discovery?.found && migrationStatus?.decision?.active_generation === 'legacy'}
		<div class="alert-banner info">
			<AlertTriangle size={18} class="banner-icon text-blue-500" />
			<div class="banner-content">
				<div class="banner-title">Активна Legacy конфигурация (S99xray-cdn)</div>
				<div class="banner-desc">Сервер управляется скриптом init.d. Вы можете переключиться на управляемый Xray или импортировать клиентов.</div>
				<div class="banner-actions">
					<Button variant="primary" size="sm" disabled={conflictResolving} onclick={() => handleResolveConflict('keep_new')}>
						Переключить на новый Xray
					</Button>
					<Button variant="secondary" size="sm" disabled={conflictResolving} onclick={() => handleResolveConflict('import_legacy_draft')}>
						Импортировать клиентов
					</Button>
				</div>
			</div>
		</div>
	{/if}

	<!-- CDN & Architecture Parameters Strip -->
	<section class="cdn-strip">
		<div class="strip-item">
			<span class="strip-label">CDN Домен</span>
			<span class="strip-val font-mono">{config?.public_domain ? `${config.public_domain}:443` : 'Не настроен'}</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Транспорт</span>
			<span class="strip-val">XHTTP · {config?.mode || 'packet-up'} (GET)</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Входящий путь</span>
			<span class="strip-val font-mono">{config?.path || '/cdn-bridge/'}</span>
		</div>
		<div class="strip-divider"></div>
		<div class="strip-item">
			<span class="strip-label">Диспетчер Origin</span>
			<span class="strip-val font-mono">Port {config?.dispatcher_port || 9009} &rarr; {config?.listen_port || 9008}</span>
		</div>
	</section>

	<!-- Clients Section -->
	<section class="clients-section">
		<div class="section-header">
			<div class="section-title">
				<span>Клиенты / Пиры</span>
				<span class="counter">({clients.length})</span>
			</div>
			<Button variant="primary" size="sm" onclick={() => addClientOpen = true}>
				<Plus size={14} class="mr-1" />
				Добавить клиента
			</Button>
		</div>

		{#if clients.length === 0}
			<div class="empty-clients">
				<span>Нет добавленных клиентов. Создайте клиента для подключения дачного роутера или смартфона.</span>
				<Button variant="secondary" size="sm" onclick={() => (wizardOpen = true)}>
					<Sparkles size={14} class="mr-1 text-primary-400" />
					Запустить мастер настройки
				</Button>
			</div>
		{:else}
			<div class="clients-list">
				{#each clients as client (client.id)}
					<div class="client-item" class:disabled={!client.enabled}>
						<div class="client-info">
							<div class="client-name-row">
								<span class="client-name">{client.remark || 'Без имени'}</span>
								<span class="client-status-badge" class:active={client.enabled}>
									{client.enabled ? 'Активен' : 'Отключён'}
								</span>
							</div>
							<div class="client-uuid-row">
								<span class="uuid-label">UUID:</span>
								<code class="uuid-code">{client.id}</code>
								<button
									type="button"
									class="copy-mini-btn"
									title="Копировать UUID"
									onclick={() => copy(client.id, 'UUID')}
								>
									<Copy size={12} />
								</button>
							</div>
						</div>

						<div class="client-actions">
							<Button variant="primary" size="sm" onclick={() => openShare(client)}>
								<QrCode size={14} class="mr-1" />
								Ссылка и QR
							</Button>
							<Button variant="secondary" size="sm" onclick={() => openAutoTunnel(client)}>
								<Router size={14} class="mr-1" />
								Создать туннель
							</Button>
							<button
								type="button"
								class="toggle-switch-btn"
								class:on={client.enabled}
								title={client.enabled ? 'Отключить клиента' : 'Включить клиента'}
								onclick={() => handleToggleClient(client)}
							>
								<div class="toggle-indicator"></div>
							</button>
							<button
								type="button"
								class="delete-btn"
								title="Удалить клиента"
								onclick={() => handleDeleteClient(client)}
							>
								<Trash2 size={15} />
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>
</div>

<!-- Modal: Add Client -->
<Modal bind:open={addClientOpen} title="Новый клиент Xray VLESS" onclose={() => addClientOpen = false} size="sm">
	<form onsubmit={(e) => { e.preventDefault(); void handleAddClient(); }} class="add-client-form">
		<label class="form-field">
			<span class="field-label">Имя / Назначение клиента:</span>
			<input
				type="text"
				bind:value={newClientRemark}
				placeholder="например, Дача, Телефон (Happ), Ноутбук"
				class="text-input"
				required
			/>
		</label>
		<p class="field-hint">
			Уникальный UUID для клиента будет сгенерирован автоматически в формате V4.
		</p>
	</form>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => addClientOpen = false}>Отмена</Button>
		<Button variant="primary" onclick={handleAddClient} disabled={addClientLoading}>
			{addClientLoading ? 'Создание...' : 'Создать'}
		</Button>
	{/snippet}
</Modal>

<!-- Modal: Settings -->
<Modal bind:open={settingsOpen} title="Параметры Xray Сервера" onclose={() => settingsOpen = false} size="md">
	<div class="settings-form">
		<div class="form-row">
			<label class="form-field">
				<span class="field-label">CDN Домен:</span>
				<input type="text" bind:value={editConfig.public_domain} class="text-input" />
			</label>
			<label class="form-field">
				<span class="field-label">Порт CDN Edge:</span>
				<input type="number" bind:value={editConfig.public_port} class="text-input" />
			</label>
		</div>

		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Входящий путь XHTTP:</span>
				<input type="text" bind:value={editConfig.path} class="text-input" />
			</label>
			<label class="form-field">
				<span class="field-label">Режим XHTTP:</span>
				<select bind:value={editConfig.mode} class="select-input">
					<option value="packet-up">packet-up (GET-совместимый режим)</option>
					<option value="stream-up">stream-up</option>
					<option value="auto">auto</option>
				</select>
			</label>
		</div>

		<div class="form-row">
			<label class="form-field">
				<span class="field-label">Порт Xray (Backend):</span>
				<input type="number" bind:value={editConfig.listen_port} class="text-input" />
			</label>
			<label class="form-field">
				<span class="field-label">Порт Диспетчера (Origin):</span>
				<input type="number" bind:value={editConfig.dispatcher_port} class="text-input" />
			</label>
		</div>

		<div class="form-field">
			<span class="field-label">Порт Socks выхода (для политик AWGM):</span>
			<input type="number" bind:value={editConfig.outbound_socks_port} placeholder="0 — прямой выход в интернет, 1099 — через локальный прокси" class="text-input" />
			<span class="field-hint">0 для прямого выхода в интернет; укажите локальный Socks5 порт (например 1099), если хотите пускать клиентов через другие туннели AWGM.</span>
		</div>
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={() => settingsOpen = false}>Отмена</Button>
		<Button variant="primary" onclick={saveSettings} disabled={saveConfigLoading}>
			{saveConfigLoading ? 'Сохранение...' : 'Сохранить'}
		</Button>
	{/snippet}
</Modal>

<!-- Modals for Client Actions -->
<XrayShareModal
	bind:open={shareOpen}
	clientId={activeClientId}
	clientRemark={activeClientRemark}
	onclose={() => shareOpen = false}
/>

<XrayAutoTunnelModal
	bind:open={autoTunnelOpen}
	clientId={activeClientId}
	clientRemark={activeClientRemark}
	onclose={() => autoTunnelOpen = false}
/>

<XrayWizard
	bind:open={wizardOpen}
	onclose={() => (wizardOpen = false)}
	onapplied={loadData}
/>

<style>
	.xray-card {
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

	.version-badge {
		font-size: 11px;
		font-family: var(--font-mono);
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
		background: var(--color-bg-hover);
		color: var(--color-text-muted);
	}

	.led {
		width: 10px;
		height: 10px;
		border-radius: 50%;
	}
	.led-running {
		background: var(--color-success);
		box-shadow: 0 0 8px var(--color-success);
	}
	.led-stopped {
		background: var(--color-text-muted);
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.icon-btn {
		width: 32px;
		height: 32px;
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: all var(--t-fast) ease;
	}
	.icon-btn:hover:not(:disabled) {
		color: var(--color-text-primary);
		background: var(--color-bg-hover);
		border-color: var(--color-accent);
	}
	.icon-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.spin {
		animation: spin 1s linear infinite;
	}
	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	/* CDN Strip */
	.cdn-strip {
		display: flex;
		align-items: center;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 10px 14px;
		flex-wrap: wrap;
		gap: 12px;
	}

	.strip-item {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.strip-label {
		font-size: 10px;
		text-transform: uppercase;
		color: var(--color-text-muted);
		letter-spacing: 0.04em;
	}

	.strip-val {
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.strip-divider {
		width: 1px;
		height: 24px;
		background: var(--color-border);
	}

	/* Clients Section */
	.clients-section {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.section-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.section-title {
		font-size: 13px;
		font-weight: 600;
		color: var(--color-text-primary);
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.counter {
		color: var(--color-text-muted);
		font-size: 12px;
	}

	.empty-clients {
		padding: 2rem;
		text-align: center;
		background: var(--color-bg-primary);
		border: 1px dashed var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		font-size: 13px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 12px;
	}

	.clients-list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.client-item {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 10px 14px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		gap: 12px;
		flex-wrap: wrap;
		transition: opacity var(--t-fast) ease;
	}

	.client-item.disabled {
		opacity: 0.6;
	}

	.client-info {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 200px;
	}

	.client-name-row {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.client-name {
		font-size: 14px;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.client-status-badge {
		font-size: 10px;
		padding: 1px 6px;
		border-radius: 4px;
		background: var(--color-bg-hover);
		color: var(--color-text-muted);
	}
	.client-status-badge.active {
		background: rgba(34, 197, 94, 0.15);
		color: #22c55e;
	}

	.client-uuid-row {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.uuid-label {
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.uuid-code {
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.copy-mini-btn {
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		padding: 2px;
		display: inline-flex;
		align-items: center;
	}
	.copy-mini-btn:hover {
		color: var(--color-accent);
	}

	.client-actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.toggle-switch-btn {
		width: 36px;
		height: 20px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: 10px;
		cursor: pointer;
		position: relative;
		padding: 2px;
		transition: background var(--t-fast) ease, border-color var(--t-fast) ease;
	}
	.toggle-switch-btn.on {
		background: var(--color-accent);
		border-color: var(--color-accent);
	}

	.toggle-indicator {
		width: 14px;
		height: 14px;
		background: #ffffff;
		border-radius: 50%;
		transition: transform var(--t-fast) ease;
	}
	.toggle-switch-btn.on .toggle-indicator {
		transform: translateX(16px);
	}

	.delete-btn {
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		padding: 6px;
		border-radius: var(--radius-sm);
		display: inline-flex;
		align-items: center;
		transition: color var(--t-fast) ease;
	}
	.delete-btn:hover {
		color: var(--color-error);
	}

	/* Forms */
	.add-client-form, .settings-form {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.form-row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
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

	.text-input, .select-input {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 8px 10px;
		font-size: 13px;
		color: var(--color-text-primary);
		outline: none;
	}
	.text-input:focus, .select-input:focus {
		border-color: var(--color-accent);
	}

	.field-hint {
		font-size: 11px;
		color: var(--color-text-muted);
		margin: 0;
	}

	.alert-banner {
		display: flex;
		align-items: flex-start;
		gap: 12px;
		padding: 12px 16px;
		border-radius: var(--radius-md);
		margin-bottom: 16px;
		font-size: 13px;
	}
	.alert-banner.warning {
		background: rgba(245, 158, 11, 0.1);
		border: 1px solid rgba(245, 158, 11, 0.3);
		color: var(--color-text-primary);
	}
	.alert-banner.error {
		background: rgba(239, 68, 68, 0.1);
		border: 1px solid rgba(239, 68, 68, 0.3);
		color: var(--color-text-primary);
	}
	.alert-banner.info {
		background: rgba(59, 130, 246, 0.1);
		border: 1px solid rgba(59, 130, 246, 0.3);
		color: var(--color-text-primary);
	}
	.banner-content {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1;
	}
	.banner-title {
		font-weight: 600;
	}
	.banner-desc {
		color: var(--color-text-muted);
		font-size: 12px;
		line-height: 1.4;
	}
	.banner-actions {
		display: flex;
		gap: 8px;
		margin-top: 4px;
	}

	.font-mono {
		font-family: var(--font-mono);
	}

	.mr-1 {
		margin-right: 4px;
	}

	@media (max-width: 640px) {
		.form-row {
			grid-template-columns: 1fr;
		}
		.cdn-strip {
			flex-direction: column;
			align-items: flex-start;
		}
		.strip-divider {
			display: none;
		}
	}
</style>
