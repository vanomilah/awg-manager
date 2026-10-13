<script lang="ts">
	import { untrack, onMount } from 'svelte';
	import { Badge, Button, Card, Input, Stat, StatStrip, Toggle } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { errText } from '$lib/utils/errorMessage';
	import { formatUptime } from '$lib/components/freeturn/uptime';
	import type {
		MihomoNativeProxy,
		OpenFluxClientConfig,
		OpenFluxClientStatus,
	} from '$lib/types';
	import type { ProxyInstanceRow } from './rows';
	import DetailSection from './DetailSection.svelte';
	import LastErrorBox from './LastErrorBox.svelte';
	import LinkBox from './LinkBox.svelte';
	import LogSection from './LogSection.svelte';
	import { ExternalLink, Import, Layers, Network, Play } from 'lucide-svelte';

	interface Props {
		row: ProxyInstanceRow;
		status: OpenFluxClientStatus | null;
		config: OpenFluxClientConfig | null;
		routerClock?: string;
		saving?: boolean;
		busy?: boolean;
		onstart: () => void;
		onstop: () => void;
		onsave: (config: OpenFluxClientConfig) => Promise<OpenFluxClientConfig | null | void>;
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

	function normalizeDraft(cfg: OpenFluxClientConfig | null): OpenFluxClientConfig | null {
		if (!cfg) return null;
		const c = JSON.parse(JSON.stringify(cfg));
		c.enabled = !!c.enabled;
		c.name = c.name ?? 'OpenFlux Выход';
		c.secret = c.secret ?? '';
		c.context = c.context ?? '';
		c.socks5Listen = c.socks5Listen ?? '127.0.0.1:1085';
		c.httpProxyListen = c.httpProxyListen ?? '';
		c.directDial = c.directDial ?? '';
		c.mailruUrl = c.mailruUrl ?? '';
		c.yandexUrl = c.yandexUrl ?? '';
		c.cupsonlineUrl = c.cupsonlineUrl ?? '';
		c.telemostUrl = c.telemostUrl ?? '';
		c.transports = c.transports ?? ['direct'];
		c.transportMode = c.transportMode ?? 'session';
		c.codec = c.codec ?? 'batched';
		c.debug = c.debug ?? 0;
		c.autoReconnect = c.autoReconnect ?? true;
		return c;
	}

	let draft = $state<OpenFluxClientConfig | null>(
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

	let importLink = $state('');
	let parsingLink = $state(false);
	let applyingLink = $state(false);

	let mihomoProxies = $state<MihomoNativeProxy[]>([]);
	let addingToMihomo = $state(false);

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
		await Promise.all([fetchLogs(), fetchMihomoProxies()]);
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

	function getSocks5Port(): number {
		const addr = draft?.socks5Listen || status?.config?.socks5Listen || '127.0.0.1:1085';
		const parts = addr.split(':');
		const p = parseInt(parts[parts.length - 1], 10);
		return isNaN(p) || p <= 0 ? 1085 : p;
	}

	const socks5Uri = $derived.by(() => {
		const port = getSocks5Port();
		const name = encodeURIComponent(
			draft?.name?.trim() || status?.config?.name?.trim() || 'OpenFlux',
		);
		return `socks5://127.0.0.1:${port}#${name}`;
	});

	const matchedMihomoProxy = $derived.by(() => {
		const port = getSocks5Port();
		return (
			mihomoProxies.find((p) => {
				if (p.protocol !== 'socks5') return false;
				const srv = String(
					p.nativeConfig?.server ?? (p as Record<string, unknown>).server ?? '',
				);
				const prt = Number(
					p.nativeConfig?.port ?? (p as Record<string, unknown>).port ?? 0,
				);
				if ((srv === '127.0.0.1' || srv === 'localhost') && prt === port) {
					return true;
				}
				if (p.rawUri && p.rawUri.includes(`127.0.0.1:${port}`)) {
					return true;
				}
				return false;
			}) ?? null
		);
	});

	const activeTransportsSummary = $derived.by(() => {
		const tr = draft?.transports ?? [];
		if (tr.length === 0) return 'не выбраны';
		return tr
			.map((t) => {
				if (t === 'direct') return 'Direct';
				if (t === 'mailru') return 'Mail.ru';
				if (t === 'yandex') return 'Яндекс';
				if (t === 'cupsonline') return 'Cups';
				if (t === 'telemost') return 'Telemost';
				return t;
			})
			.join(' / ');
	});

	async function fetchMihomoProxies() {
		if (typeof api.mihomoNativeProxies !== 'function') return;
		try {
			mihomoProxies = await api.mihomoNativeProxies();
		} catch {
			/* Mihomo native catalog is optional */
		}
	}

	async function handleAddToMihomo() {
		addingToMihomo = true;
		try {
			const res = await api.mihomoNativeCreateProxy(socks5Uri, 'mihomo');
			mihomoProxies = res.items ?? [];
			notifications.success('Узел OpenFlux добавлен в Mihomo');
		} catch (e) {
			notifications.error('Ошибка добавления в Mihomo: ' + errText(e));
		} finally {
			addingToMihomo = false;
		}
	}

	async function handleParseLink(applyAndStart = false) {
		const raw = importLink.trim();
		if (!raw) {
			notifications.error('Вставьте ссылку openflux://v1/...');
			return;
		}
		if (applyAndStart) applyingLink = true;
		else parsingLink = true;

		try {
			const res = await api.openfluxParseLink(raw);
			const nextCfg = normalizeDraft({
				...res.config,
				enabled: applyAndStart ? true : (draft?.enabled ?? res.config.enabled),
				socks5Listen: draft?.socks5Listen || res.config.socks5Listen || '127.0.0.1:1085',
				httpProxyListen: draft?.httpProxyListen ?? res.config.httpProxyListen ?? '',
				autoReconnect: draft?.autoReconnect ?? true,
			});
			draft = nextCfg;
			if (applyAndStart && nextCfg) {
				const saved = await onsave(nextCfg);
				if (saved) {
					draft = normalizeDraft(saved);
					lastSyncedConfigJson = JSON.stringify(draft);
				}
				const st = await api.openfluxClientStart();
				if (st?.client?.config) {
					draft = normalizeDraft(st.client.config);
					lastSyncedConfigJson = JSON.stringify(draft);
				}
				importLink = '';
				notifications.success('Ссылка импортирована, клиент OpenFlux запущен');
				await onreload();
				await fetchLogs();
			} else {
				notifications.success('Ссылка разобрана — проверьте параметры и нажмите «Сохранить»');
			}
		} catch (e) {
			notifications.error('Ошибка разбора ссылки: ' + errText(e));
		} finally {
			parsingLink = false;
			applyingLink = false;
		}
	}

	async function handleStart() {
		starting = true;
		try {
			if (isDirty && draft) {
				const saved = await onsave(draft);
				if (saved) {
					draft = normalizeDraft(saved);
					lastSyncedConfigJson = JSON.stringify(draft);
				}
			}
			const st = await api.openfluxClientStart();
			if (st?.client?.config) {
				draft = normalizeDraft(st.client.config);
				lastSyncedConfigJson = JSON.stringify(draft);
			} else if (draft) {
				draft.enabled = true;
			}
			notifications.success('OpenFlux-клиент запущен');
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
			const st = await api.openfluxClientStop();
			if (st?.client?.config) {
				draft = normalizeDraft(st.client.config);
				lastSyncedConfigJson = JSON.stringify(draft);
			} else if (draft) {
				draft.enabled = false;
			}
			notifications.success('OpenFlux-клиент остановлен');
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
			if (isDirty && draft) {
				const saved = await onsave({ ...draft, enabled: true });
				if (saved) {
					draft = normalizeDraft(saved);
					lastSyncedConfigJson = JSON.stringify(draft);
				}
			} else {
				const st = await api.openfluxClientRestart();
				if (st?.client?.config) {
					draft = normalizeDraft(st.client.config);
					lastSyncedConfigJson = JSON.stringify(draft);
				}
			}
			notifications.success('OpenFlux-клиент перезапущен');
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
			const res = await onsave(draft);
			if (res) {
				draft = normalizeDraft(res);
				lastSyncedConfigJson = JSON.stringify(draft);
			}
			notifications.success('Настройки клиента OpenFlux сохранены');
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
		if (typeof api.openfluxClientLogs !== 'function') return;
		try {
			const res = await api.openfluxClientLogs(150);
			logs = res.logs;
		} catch {
			if (status?.logTail) logs = status.logTail;
		}
	}
</script>

<Card padding="lg">
	<div class="head">
		<h2>{row.name}</h2>
		<Badge size="sm" variant="info">OpenFlux-клиент</Badge>
		<Badge size="sm" variant="default">SOCKS5 :{getSocks5Port()}</Badge>
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
			value={`:${getSocks5Port()}`}
			label="SOCKS5 порт"
			sub={draft?.httpProxyListen ? `HTTP ${draft.httpProxyListen}` : 'локальный прокси'}
		/>
		<Stat
			value={draft?.transportMode === 'session' ? 'Сессия' : 'Single'}
			label="Транспорты"
			sub={activeTransportsSummary}
		/>
	</StatStrip>

	<LastErrorBox text={running ? '' : (status?.error ?? '')} />

	<!-- Импорт ссылки openflux://v1/... -->
	<div class="info-card-box">
		<div class="card-section-title">
			<Import size={18} />
			<span>Быстрый импорт ссылки подключения (openflux://v1/...)</span>
		</div>
		<p class="section-desc">
			Вставьте ссылку <code>openflux://v1/...</code> с вашего зарубежного сервера или другого роутера Keenetic,
			чтобы автоматически заполнить ключ шифрования и все транспорты (Direct TCP, Mail.ru Docs, Яндекс Документы).
		</p>
		<div class="import-row">
			<div class="import-input">
				<Input
					bind:value={importLink}
					placeholder="openflux://v1/..."
					ariaLabel="Ссылка openflux://v1/..."
					fullWidth
				/>
			</div>
			<div class="import-actions">
				<Button
					variant="secondary"
					size="sm"
					loading={parsingLink}
					disabled={parsingLink || applyingLink || !importLink.trim()}
					onclick={() => handleParseLink(false)}
				>
					Разобрать ссылку
				</Button>
				<Button
					variant="primary"
					size="sm"
					loading={applyingLink}
					disabled={parsingLink || applyingLink || !importLink.trim() || !row.binaryPresent}
					onclick={() => handleParseLink(true)}
				>
					{#snippet iconBefore()}<Play size={14} />{/snippet}
					Применить и запустить
				</Button>
			</div>
		</div>
	</div>

	<!-- Подключение к маршрутизации роутера (Mihomo / SOCKS5) -->
	<div class="info-card-box">
		<div class="card-section-title">
			<Network size={18} />
			<span>Использование в маршрутизации роутера (Mihomo / SOCKS5)</span>
		</div>
		<p class="section-desc">
			Клиент OpenFlux поднимает локальный <strong>SOCKS5 прокси</strong> на
			<code>{draft?.socks5Listen || '127.0.0.1:1085'}</code>
			{#if draft?.httpProxyListen}
				и <strong>HTTP прокси</strong> на <code>{draft.httpProxyListen}</code>
			{/if}.
			Вы можете одним нажатием зарегистрировать его как прокси-узел в <strong>Mihomo</strong> для маршрутизации по доменам, спискам и политикам Keenetic.
		</p>
		<LinkBox link={socks5Uri} title="Локальный SOCKS5 URI для Mihomo" />
		<div class="mihomo-bar">
			{#if matchedMihomoProxy}
				<div class="mihomo-connected">
					<Badge size="sm" variant="success">Подключен в Mihomo: {matchedMihomoProxy.name}</Badge>
					{#if matchedMihomoProxy.bridge?.proxyInterface}
						<Badge size="sm" variant="default">Мост: {matchedMihomoProxy.bridge.proxyInterface}</Badge>
					{/if}
				</div>
			{:else}
				<Button
					variant="secondary"
					size="sm"
					loading={addingToMihomo}
					disabled={addingToMihomo}
					onclick={handleAddToMihomo}
				>
					{#snippet iconBefore()}<Layers size={14} />{/snippet}
					Добавить прокси-узел в Mihomo
				</Button>
			{/if}
			<Button variant="ghost" size="sm" href="/routing">
				Маршрутизация
				{#snippet iconAfter()}<ExternalLink size={12} />{/snippet}
			</Button>
		</div>
	</div>

	{#if draft}
		<DetailSection title="Параметры и транспорты клиента" hint="Настройки исходящего подключения OpenFlux">
			<div class="config-grid">
				<div class="config-row">
					<div class="config-label">
						<strong>Имя клиента</strong>
						<span class="subtext">Отображается в списке инстансов выхода и в Mihomo</span>
					</div>
					<div class="config-ctrl">
						<Input bind:value={draft.name} placeholder="OpenFlux Client" fullWidth />
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Локальный SOCKS5 прокси (--socks5)</strong>
						<span class="subtext">Адрес и порт, на котором клиент принимает трафик от Mihomo или локальных приложений</span>
					</div>
					<div class="config-ctrl">
						<Input bind:value={draft.socks5Listen} placeholder="127.0.0.1:1085" fullWidth />
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Локальный HTTP CONNECT прокси (--http-proxy)</strong>
						<span class="subtext">Дополнительный HTTP прокси порт (оставьте пустым для отключения)</span>
					</div>
					<div class="config-ctrl">
						<Input bind:value={draft.httpProxyListen} placeholder="127.0.0.1:1086" fullWidth />
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Автопереподключение при обрыве</strong>
						<span class="subtext">Автоматически перезапускать клиент через 5 секунд при обрыве сессии или таймауте рукопожатия</span>
					</div>
					<div class="config-ctrl">
						<Toggle
							label="Включено"
							checked={draft.autoReconnect ?? true}
							onchange={(v) => {
								if (draft) draft.autoReconnect = v;
							}}
						/>
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Сессионный режим (Transport Mode)</strong>
						<span class="subtext">
							Мульти-сессия (--negotiate) согласует и чередует доступные транспорты между клиентом и сервером.
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
					<span>Транспорты подключения к серверу</span>
				</div>

				<!-- Direct TCP -->
				<div class="transport-block">
					<div class="transport-header">
						<div class="transport-title-row">
							<input
								type="checkbox"
								id="ofc-transport-direct"
								checked={isTransportEnabled('direct')}
								onchange={() => toggleTransport('direct')}
							/>
							<label for="ofc-transport-direct"><strong>Прямой TCP транспорт (Direct TCP)</strong></label>
						</div>
						<span class="subtext">Прямое зашифрованное TCP соединение до удалённого сервера OpenFlux</span>
					</div>

					{#if isTransportEnabled('direct')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="ofc-direct-dial">Адрес сервера (IP:порт или домен:порт):</label>
								<input
									id="ofc-direct-dial"
									type="text"
									class="form-input"
									placeholder="203.0.113.10:8445"
									bind:value={draft.directDial}
								/>
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
								id="ofc-transport-mailru"
								checked={isTransportEnabled('mailru')}
								onchange={() => toggleTransport('mailru')}
							/>
							<label for="ofc-transport-mailru"><strong>Mail.ru Документы (Mail.ru Docs)</strong></label>
						</div>
						<span class="subtext">Передача трафика через общий редактируемый документ Mail.ru Docs (обход белых списков)</span>
					</div>

					{#if isTransportEnabled('mailru')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="ofc-mailru-url">URL документа Mail.ru:</label>
								<input
									id="ofc-mailru-url"
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
								id="ofc-transport-yandex"
								checked={isTransportEnabled('yandex')}
								onchange={() => toggleTransport('yandex')}
							/>
							<label for="ofc-transport-yandex"><strong>Яндекс Документы (Yandex Docs)</strong></label>
						</div>
						<span class="subtext">Передача трафика через облачный документ Яндекс Документов</span>
					</div>

					{#if isTransportEnabled('yandex')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="ofc-yandex-url">URL документа Яндекс:</label>
								<input
									id="ofc-yandex-url"
									type="text"
									class="form-input"
									placeholder="https://docs.yandex.ru/docs/view?url=..."
									bind:value={draft.yandexUrl}
								/>
							</div>
						</div>
					{/if}
				</div>

				<!-- CupsOnline -->
				<div class="transport-block">
					<div class="transport-header">
						<div class="transport-title-row">
							<input
								type="checkbox"
								id="ofc-transport-cupsonline"
								checked={isTransportEnabled('cupsonline')}
								onchange={() => toggleTransport('cupsonline')}
							/>
							<label for="ofc-transport-cupsonline"><strong>CupsOnline</strong></label>
						</div>
						<span class="subtext">Передача трафика через комнату CupsOnline</span>
					</div>

					{#if isTransportEnabled('cupsonline')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="ofc-cupsonline-url">URL комнаты CupsOnline:</label>
								<input
									id="ofc-cupsonline-url"
									type="text"
									class="form-input"
									placeholder="https://..."
									bind:value={draft.cupsonlineUrl}
								/>
							</div>
						</div>
					{/if}
				</div>

				<!-- Yandex Telemost -->
				<div class="transport-block">
					<div class="transport-header">
						<div class="transport-title-row">
							<input
								type="checkbox"
								id="ofc-transport-telemost"
								checked={isTransportEnabled('telemost')}
								onchange={() => toggleTransport('telemost')}
							/>
							<label for="ofc-transport-telemost"><strong>Яндекс Телемост (Telemost)</strong></label>
						</div>
						<span class="subtext">Передача трафика через встречу Яндекс Телемост</span>
					</div>

					{#if isTransportEnabled('telemost')}
						<div class="transport-fields">
							<div class="field-item full-width">
								<label for="ofc-telemost-url">URL встречи Телемост:</label>
								<input
									id="ofc-telemost-url"
									type="text"
									class="form-input"
									placeholder="https://telemost.yandex.ru/j/..."
									bind:value={draft.telemostUrl}
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
							Ключ шифрования AES-256-GCM (минимум 16 символов). Должен точно совпадать с ключом на сервере OpenFlux.
						</span>
					</div>
					<div class="config-ctrl secret-ctrl">
						<Input bind:value={draft.secret} placeholder="Секретный ключ сервера..." fullWidth />
					</div>
				</div>

				<div class="config-row">
					<div class="config-label">
						<strong>Контекст сессии (Session Context)</strong>
						<span class="subtext">
							Дополнительный тег деривации ключа (--session-context). Оставьте пустым, если не задан в ссылке.
						</span>
					</div>
					<div class="config-ctrl">
						<Input bind:value={draft.context} placeholder="Необязательно" fullWidth />
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
						<span class="subtext">Вывод подробной информации о сессиях в лог клиента</span>
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

	<LogSection log={logs || status?.logTail} {routerClock} hint="Журнал работы клиента OpenFlux" />
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

	.info-card-box {
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

	.import-row {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		flex-wrap: wrap;
	}

	.import-input {
		flex: 1;
		min-width: 240px;
	}

	.import-actions {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.mihomo-bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		margin-top: 0.75rem;
		flex-wrap: wrap;
	}

	.mihomo-connected {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
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
