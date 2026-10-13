<script lang="ts">
	import { untrack, onMount } from 'svelte';
	import { Badge, Button, Card, Input, Stat, StatStrip, Toggle } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { errText } from '$lib/utils/errorMessage';
	import { formatUptime } from '$lib/components/freeturn/uptime';
	import type { OpenFluxConfig, OpenFluxStatus } from '$lib/types';
	import type { ProxyInstanceRow } from './rows';
	import DetailSection from './DetailSection.svelte';
	import LastErrorBox from './LastErrorBox.svelte';
	import LinkBox from './LinkBox.svelte';
	import LogSection from './LogSection.svelte';
	import { Key, Layers } from 'lucide-svelte';

	interface Props {
		row: ProxyInstanceRow;
		status: OpenFluxStatus | null;
		config: OpenFluxConfig | null;
		routerClock?: string;
		saving?: boolean;
		busy?: boolean;
		onstart: () => void;
		onstop: () => void;
		onsave: (config: OpenFluxConfig) => Promise<OpenFluxConfig | null | void>;
		onreload: () => Promise<void> | void;
	}

	let {
		row,
		status,
		config,
		routerClock = '',
		saving = false,
		busy = false,
		onstart,
		onstop,
		onsave,
		onreload,
	}: Props = $props();

	function normalizeDraft(cfg: OpenFluxConfig | null): OpenFluxConfig | null {
		if (!cfg) return null;
		const c = JSON.parse(JSON.stringify(cfg));
		c.name = c.name ?? '';
		c.secret = c.secret ?? '';
		c.directHost = c.directHost ?? '';
		c.mailruUrl = c.mailruUrl ?? '';
		c.yandexUrl = c.yandexUrl ?? '';
		c.directListen = c.directListen ?? '0.0.0.0:8445';
		c.transports = c.transports ?? [];
		c.mode = c.mode ?? 'l4';
		c.transportMode = c.transportMode ?? 'session';
		c.codec = c.codec ?? 'batched';
		c.debug = c.debug ?? 0;
		c.openFirewall = c.openFirewall ?? false;
		return c;
	}

	let draft = $state<OpenFluxConfig | null>(
		untrack(() => normalizeDraft(config)),
	);
	let lastSyncedConfigJson = $state(
		untrack(() => JSON.stringify(normalizeDraft(config))),
	);
	let logs = $state('');
	let starting = $state(false);
	let stopping = $state(false);
	let restarting = $state(false);
	let localSaving = $state(false);

	const running = $derived(row.state === 'running');
	const uptimeValue = $derived(running ? formatUptime(row.startedAt) || '—' : '—');
	const normalizedConfig = $derived(normalizeDraft(config));
	const isDirty = $derived(
		draft && normalizedConfig
			? JSON.stringify(draft) !== JSON.stringify(normalizedConfig)
			: false,
	);

	$effect(() => {
		const norm = normalizedConfig;
		if (!norm) return;
		const normJson = JSON.stringify(norm);
		if (!draft) {
			draft = JSON.parse(normJson);
			lastSyncedConfigJson = normJson;
		} else if (normJson !== lastSyncedConfigJson) {
			const wasDirty = JSON.stringify(draft) !== lastSyncedConfigJson;
			lastSyncedConfigJson = normJson;
			if (!wasDirty) {
				draft = JSON.parse(normJson);
			} else {
				draft.enabled = norm.enabled;
			}
		}
	});

	onMount(async () => {
		await fetchLogs();
	});

	function isTransportEnabled(name: string): boolean {
		return !!draft?.transports?.includes(name);
	}

	function toggleTransport(name: string) {
		if (!draft) return;
		const current = draft.transports ?? [];
		if (current.includes(name)) {
			draft.transports = current.filter((t) => t !== name);
		} else {
			draft.transports = [...current, name];
		}
	}

	function getDirectPort(): number {
		if (!draft?.directListen) return 8445;
		const parts = draft.directListen.split(':');
		const p = parseInt(parts[parts.length - 1], 10);
		return isNaN(p) ? 8445 : p;
	}

	function setDirectPort(port: number) {
		if (!draft) return;
		draft.directListen = `0.0.0.0:${port}`;
	}

	function generateSecret() {
		if (!draft) return;
		const arr = new Uint8Array(16);
		crypto.getRandomValues(arr);
		draft.secret = Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	async function handleStart() {
		starting = true;
		try {
			const st = await api.openfluxStart();
			if (st?.config) {
				draft = normalizeDraft(st.config);
				lastSyncedConfigJson = JSON.stringify(draft);
			} else if (draft) {
				draft.enabled = true;
			}
			notifications.success('OpenFlux запущен');
			await onreload();
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
			const st = await api.openfluxStop();
			if (st?.config) {
				draft = normalizeDraft(st.config);
				lastSyncedConfigJson = JSON.stringify(draft);
			} else if (draft) {
				draft.enabled = false;
			}
			notifications.success('OpenFlux остановлен');
			await onreload();
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
			await api.openfluxRestart();
			notifications.success('OpenFlux перезапущен');
			await onreload();
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка перезапуска: ' + errText(e));
		} finally {
			restarting = false;
		}
	}

	async function handleSave() {
		if (!draft) return;
		localSaving = true;
		try {
			await onsave(draft);
			lastSyncedConfigJson = JSON.stringify(draft);
			notifications.success('Настройки сохранены');
			await onreload();
			await fetchLogs();
		} catch (e) {
			notifications.error('Ошибка сохранения: ' + errText(e));
		} finally {
			localSaving = false;
		}
	}

	function revert() {
		if (config) draft = normalizeDraft(config);
	}

	async function toggleAutostart(enabled: boolean) {
		if (!draft) return;
		draft.enabled = enabled;
		await handleSave();
	}

	async function fetchLogs() {
		try {
			const res = await api.openfluxLogs(150);
			logs = res.logs;
		} catch {
			if (status?.logTail) logs = status.logTail;
		}
	}
</script>

<Card padding="lg">
	<div class="head">
		<h2>{row.name}</h2>
		<Badge size="sm" variant="info">OpenFlux-сервер</Badge>
		<Badge size="sm" variant="default">{(draft?.mode ?? 'l4').toUpperCase()}</Badge>
		<div class="head-actions">
			<Toggle
				label="Автозапуск"
				checked={draft?.enabled ?? false}
				onchange={toggleAutostart}
			/>
			{#if running}
				<Button
					variant="secondary"
					size="sm"
					loading={stopping}
					disabled={busy || stopping || restarting}
					onclick={handleStop}
				>
					Остановить
				</Button>
				<Button
					variant="secondary"
					size="sm"
					loading={restarting}
					disabled={busy || stopping || restarting}
					onclick={handleRestart}
				>
					Перезапустить
				</Button>
			{:else}
				<Button
					variant="primary"
					size="sm"
					loading={starting}
					disabled={busy || starting || !row.binaryPresent}
					onclick={handleStart}
				>
					Запустить
				</Button>
			{/if}
		</div>
	</div>

	<StatStrip>
		<Stat value={running ? 'Запущен' : 'Остановлен'} label="Состояние" sub={uptimeValue} />
		<Stat
			value={status?.pid ? `PID ${status.pid}` : '—'}
			label="Процесс"
			sub={running ? 'активен' : 'не запущен'}
		/>
		<Stat
			value={(draft?.mode ?? 'l4').toUpperCase()}
			label="Режим"
			sub={draft?.transportMode === 'session' ? 'Мульти-сессия' : 'Одиночный'}
		/>
		<Stat
			value={`:${getDirectPort()}`}
			label="TCP порт"
			sub={isTransportEnabled('direct') ? 'прямой TCP' : 'выключен'}
		/>
	</StatStrip>

	<LastErrorBox text={running ? '' : (status?.error ?? '')} />

	{#if status?.link}
		<div class="link-card-box">
			<div class="card-section-title">
				<Layers size={18} />
				<span>Ссылка для подключения (qThroneMobile / OpenFlux)</span>
			</div>
			<p class="section-desc">
				Импортируйте ссылку в <strong>qThroneMobile</strong> на смартфоне или в десктопный клиент OpenFlux.
				Клиент автоматически согласует и будет использовать доступные транспорты (Mail.ru, Yandex Docs или прямой TCP).
			</p>
			<LinkBox link={status.link} title="OpenFlux V1 Link" />
		</div>
	{/if}

	{#if draft}
		<DetailSection title="Параметры и транспорты" hint="Настройки сервера OpenFlux">
			<div class="config-grid">
				<div class="config-row">
					<div class="config-label">
						<strong>Имя профиля</strong>
						<span class="subtext">Отображается в названии узла в клиенте</span>
					</div>
					<div class="config-ctrl">
						<Input bind:value={draft.name} placeholder="Keenetic" fullWidth />
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Режим работы (Mode)</strong>
						<span class="subtext">
							L4 — встроенный пользовательский TCP/UDP стек (gVisor). L3 — сетевой уровень TUN.
						</span>
					</div>
					<div class="config-ctrl select-ctrl">
						<select bind:value={draft.mode} class="form-select">
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
						<select bind:value={draft.transportMode} class="form-select">
							<option value="session">Мульти-транспортная сессия (--negotiate, рекомендуется)</option>
							<option value="single">Одиночный транспорт (Single)</option>
						</select>
					</div>
				</div>

				<div class="section-divider">
					<span>Транспорты</span>
				</div>

				<!-- Direct TCP -->
				<div class="transport-block">
					<div class="transport-header">
						<div class="transport-title-row">
							<input
								type="checkbox"
								id="of-transport-direct"
								checked={isTransportEnabled('direct')}
								onchange={() => toggleTransport('direct')}
							/>
							<label for="of-transport-direct"><strong>Прямой TCP транспорт (Direct TCP)</strong></label>
						</div>
						<span class="subtext">Прямое TCP соединение на открытый порт роутера</span>
					</div>

					{#if isTransportEnabled('direct')}
						<div class="transport-fields">
							<div class="field-item">
								<label for="of-direct-port">Порт прослушивания:</label>
								<input
									id="of-direct-port"
									type="number"
									class="form-input"
									value={getDirectPort()}
									oninput={(e) => setDirectPort(parseInt((e.target as HTMLInputElement).value, 10))}
								/>
							</div>
							<div class="field-item">
								<label for="of-direct-host">Внешний хост / домен (необязательно):</label>
								<input
									id="of-direct-host"
									type="text"
									class="form-input"
									placeholder="Автоопределение (KeenDNS / WAN IP)"
									bind:value={draft.directHost}
								/>
							</div>
							<div class="field-toggle-row">
								<label class="toggle-label">
									<input type="checkbox" bind:checked={draft.openFirewall} />
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
								id="of-transport-mailru"
								checked={isTransportEnabled('mailru')}
								onchange={() => toggleTransport('mailru')}
							/>
							<label for="of-transport-mailru"><strong>Mail.ru Документы (Mail.ru Docs)</strong></label>
						</div>
						<span class="subtext">Маскировка и передача туннелированного трафика через документ Mail.ru Docs</span>
					</div>

					{#if isTransportEnabled('mailru')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="of-mailru-url">URL документа Mail.ru:</label>
								<input
									id="of-mailru-url"
									type="text"
									class="form-input"
									placeholder="https://docs.mail.ru/document/..."
									bind:value={draft.mailruUrl}
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
								id="of-transport-yandex"
								checked={isTransportEnabled('yandex')}
								onchange={() => toggleTransport('yandex')}
							/>
							<label for="of-transport-yandex"><strong>Яндекс Документы (Yandex Docs)</strong></label>
						</div>
						<span class="subtext">Маскировка и передача трафика через облачный документ Яндекс Документов</span>
					</div>

					{#if isTransportEnabled('yandex')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="of-yandex-url">URL документа Яндекс:</label>
								<input
									id="of-yandex-url"
									type="text"
									class="form-input"
									placeholder="https://docs.yandex.ru/docs/view?url=..."
									bind:value={draft.yandexUrl}
								/>
							</div>
						</div>
					{/if}
				</div>

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
						<Input bind:value={draft.secret} placeholder="Секретный ключ..." fullWidth />
						<Button
							variant="secondary"
							onclick={generateSecret}
							title="Сгенерировать случайный 32-значный ключ"
						>
							{#snippet iconBefore()}<Key size={15} />{/snippet}
							Ключ
						</Button>
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Кодек пакетов (Codec)</strong>
						<span class="subtext">
							batched — пакетирование и сжатие трафика. legacy — покадровый режим.
						</span>
					</div>
					<div class="config-ctrl select-ctrl">
						<select bind:value={draft.codec} class="form-select">
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
						<select bind:value={draft.debug} class="form-select">
							<option value={0}>0 — Выключено</option>
							<option value={1}>1 — Базовая отладка (-d)</option>
							<option value={2}>2 — Подробная отладка (-dd)</option>
							<option value={3}>3 — Трассировка пакетов (-ddd)</option>
						</select>
					</div>
				</div>
			</div>

			<div class="save-actions">
				{#if isDirty}
					<Button variant="ghost" onclick={revert}>Отменить</Button>
				{/if}
				<Button
					variant="primary"
					loading={localSaving || saving}
					disabled={localSaving || saving}
					onclick={handleSave}
				>
					Сохранить настройки
				</Button>
			</div>
		</DetailSection>
	{/if}

	<LogSection log={logs || status?.logTail} {routerClock} hint="Журнал работы процесса OpenFlux" />
</Card>

<style>
	.head {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
		margin-bottom: 1rem;
	}

	.head h2 {
		margin: 0;
		font-size: 1.125rem;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.head-actions {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		margin-left: auto;
		flex-wrap: wrap;
	}

	.link-card-box {
		margin: 1.25rem 0;
		padding: 1rem;
		border-radius: var(--radius);
		border: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
	}

	.card-section-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.9375rem;
		font-weight: 600;
		color: var(--color-text-primary);
		margin-bottom: 0.35rem;
	}

	.section-desc {
		font-size: 0.8125rem;
		color: var(--color-text-secondary);
		margin-bottom: 0.75rem;
		line-height: 1.4;
	}

	.config-grid {
		display: flex;
		flex-direction: column;
		gap: 0.875rem;
	}

	.config-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0.5rem 0;
		border-bottom: 1px solid var(--color-border);
		gap: 1rem;
	}

	.config-label {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		flex: 1;
	}

	.subtext {
		font-size: 0.75rem;
		color: var(--color-text-muted);
		line-height: 1.35;
	}

	.config-ctrl {
		display: flex;
		align-items: center;
		min-width: 220px;
	}

	.select-ctrl {
		min-width: 300px;
	}

	.secret-ctrl {
		display: flex;
		gap: 0.5rem;
		min-width: 320px;
	}

	.form-select,
	.form-input {
		width: 100%;
		padding: 0.45rem 0.65rem;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-primary);
		font-size: 0.8125rem;
	}

	.form-select:focus,
	.form-input:focus {
		border-color: var(--color-accent);
		outline: none;
	}

	.section-divider {
		padding: 0.625rem 0 0.25rem;
		font-size: 0.75rem;
		font-weight: 600;
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		border-bottom: 1px solid var(--color-border);
	}

	.transport-block {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		padding: 0.75rem 0.875rem;
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

	.transport-header {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
	}

	.transport-title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
		color: var(--color-text-primary);
	}

	.transport-fields {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 0.625rem;
		margin-top: 0.25rem;
		padding-top: 0.625rem;
		border-top: 1px dashed var(--color-border);
	}

	.field-item {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		font-size: 0.75rem;
		color: var(--color-text-secondary);
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
		font-size: 0.75rem;
		cursor: pointer;
		color: var(--color-text-secondary);
	}

	.save-actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		margin-top: 1rem;
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
