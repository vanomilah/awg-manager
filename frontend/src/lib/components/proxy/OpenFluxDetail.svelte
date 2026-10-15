<script lang="ts">
	import { untrack, onMount } from 'svelte';
	import {
		Badge,
		Button,
		Card,
		ChipMultiSelect,
		Dropdown,
		FieldHint,
		FormRow,
		Input,
		SegmentedControl,
		SideDrawer,
		Stat,
		StatStrip,
		Toggle,
	} from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { errText } from '$lib/utils/errorMessage';
	import { formatUptime } from '$lib/components/freeturn/uptime';
	import { ServerAccessPolicyDropdown } from '$lib/components/servers';
	import type {
		AccessPolicy,
		OpenFluxConfig,
		OpenFluxStatus,
		SingboxRouterSettings,
		TunnelListItem,
	} from '$lib/types';
	import type { ProxyInstanceRow } from './rows';
	import DetailSection from './DetailSection.svelte';
	import InstanceBadges from './InstanceBadges.svelte';
	import LastErrorBox from './LastErrorBox.svelte';
	import LinkBox from './LinkBox.svelte';
	import LogSection from './LogSection.svelte';
	import { natModeOptions, natModeLabel } from './shareConfig';
	import { ingressOn, nextIngressInterfaces, openfluxIngressRefs } from './shareIngress';
	import { effectiveStaticWan } from '$lib/api/proxyInstances';
	import SensitiveInput from '../proxy-panel/SensitiveInput.svelte';
	import { ExternalLink, Key, Layers } from 'lucide-svelte';

	interface Props {
		row: ProxyInstanceRow;
		status: OpenFluxStatus | null;
		config: OpenFluxConfig | null;
		routerClock?: string;
		policies?: AccessPolicy[];
		tunnels?: TunnelListItem[];
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
		policies = [],
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
		c.name = c.name ?? 'Keenetic';
		c.secret = c.secret ?? '';
		c.directHost = c.directHost ?? '';
		c.webdavUrl = c.webdavUrl ?? '';
		c.webdavUser = c.webdavUser ?? '';
		c.webdavPassword = c.webdavPassword ?? '';
		c.mailruUrl = c.mailruUrl ?? '';
		c.yandexUrl = c.yandexUrl ?? '';
		c.vyandexUrl = c.vyandexUrl ?? '';
		c.cupsonlineUrl = c.cupsonlineUrl ?? '';
		c.telemostUrl = c.telemostUrl ?? '';
		c.directListen = c.directListen ?? '0.0.0.0:8445';
		c.transports = c.transports ?? ['mailru', 'yandex', 'vyandex'];
		c.mode = c.mode ?? 'l4';
		c.transportMode = c.transportMode ?? 'session';
		c.codec = c.codec ?? 'batched';
		c.debug = c.debug ?? 0;
		c.openFirewall = c.openFirewall ?? true;
		c.natMode = c.natMode ?? 'full';
		c.natStaticWan = c.natStaticWan ?? '';
		c.natStaticWans = c.natStaticWans ?? [];
		c.policy = c.policy ?? '';
		c.lanSegments = c.lanSegments ?? [];
		c.exposeToPolicies = c.exposeToPolicies ?? false;
		c.egressInterface = c.egressInterface ?? '';
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

	let settingsOpen = $state(false);
	let lanOptions = $state<{ value: string; label: string }[]>([]);
	let wanOptions = $state<{ value: string; label: string }[]>([]);
	let ingress = $state(false);
	let activeEngine = $state<'sing-box' | 'mihomo'>('sing-box');
	const isMihomo = $derived(activeEngine === 'mihomo');
	const engineName = $derived(isMihomo ? 'Mihomo' : 'sing-box');
	let engineRunning = $state(true);
	let ingressForced = $state(false);
	let mutating = $state(false);

	const running = $derived(row.state === 'running');
	const uptimeValue = $derived(running ? formatUptime(row.startedAt) || '—' : '—');
	const normalizedConfig = $derived(normalizeDraft(config));
	const isDirty = $derived(
		draft && normalizedConfig
			? JSON.stringify(draft) !== JSON.stringify(normalizedConfig)
			: false,
	);

	const activeTransports = $derived(draft?.transports ?? []);
	const activeTransportsLabel = $derived(
		activeTransports.length === 0
			? 'Нет'
			: activeTransports
					.map((t) => {
						if (t === 'webdav') return 'WebDAV';
						if (t === 'direct') return 'Direct';
						if (t === 'mailru') return 'Mail.ru';
						if (t === 'yandex') return 'Яндекс';
						if (t === 'vyandex') return 'ВК/Яндекс';
						if (t === 'cupsonline') return 'Cups';
						if (t === 'telemost') return 'Telemost';
						return t;
					})
					.join(' / '),
	);
	const activeTransportsSub = $derived(
		activeTransports.length === 0
			? 'выключены'
			: `${activeTransports.length} ${activeTransports.length === 1 ? 'активен' : 'активно'}`,
	);

	const policyName = $derived(
		policies.find((p) => p.name === draft?.policy || p.id === draft?.policy)?.name || draft?.policy || '',
	);

	function allDevicesTProxy(s: SingboxRouterSettings): boolean {
		return (s.routingMode ?? 'tproxy') === 'tproxy' && s.deviceMode === 'all';
	}

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
		try {
			const segs = await api.listManagedLANSegments();
			lanOptions = segs.map((s) => ({ value: s.name, label: s.label || s.name }));
		} catch {
			/* сегменты вторичны */
		}
		try {
			const wans = await api.getWANInterfaces();
			wanOptions = wans.map((w) => ({ value: w.name, label: w.label || w.name }));
		} catch {
			/* список WAN вторичен */
		}
		try {
			const settings = await api.singboxRouterGetSettings();
			activeEngine = settings.routingEngine === 'mihomo' ? 'mihomo' : 'sing-box';
			ingress = ingressOn(settings.ingressInterfaces, openfluxIngressRefs(draft ?? undefined, status ?? undefined));
			ingressForced = allDevicesTProxy(settings);
		} catch {
			/* роутерные настройки вторичны */
		}
		try {
			if (activeEngine === 'mihomo') {
				engineRunning = (await api.mihomoStatus()).running;
			} else {
				engineRunning = (await api.singboxGetStatus()).running;
			}
		} catch {
			/* статус движка вторичен */
		}

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
		const safePort = Math.max(1, Math.min(65535, port || 8445));
		draft.directListen = `0.0.0.0:${safePort}`;
	}

	function generateSecret() {
		if (!draft) return;
		const arr = new Uint8Array(16);
		crypto.getRandomValues(arr);
		draft.secret = Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	async function toggleIngress(on: boolean) {
		if (!draft || mutating) return;
		mutating = true;
		try {
			const settings = await api.singboxRouterGetSettings();
			activeEngine = settings.routingEngine === 'mihomo' ? 'mihomo' : 'sing-box';
			ingressForced = allDevicesTProxy(settings);
			const refs = openfluxIngressRefs(draft, status ?? undefined);
			await api.singboxRouterPutSettings({
				...settings,
				ingressInterfaces: nextIngressInterfaces(settings.ingressInterfaces, refs, on),
			});
			ingress = on;
			notifications.success(
				on
					? `Маршрутизация трафика OpenFlux через ${engineName} включена`
					: `Маршрутизация трафика OpenFlux через ${engineName} выключена`,
			);
		} catch (e) {
			notifications.error('Ошибка изменения маршрутизации: ' + errText(e));
		} finally {
			mutating = false;
		}
	}

	let natWanBlocked = $state(false);

	function changeNat(val: string) {
		const mode = val as 'full' | 'internet-only' | 'none';
		if (mode === 'internet-only' && !effectiveStaticWan(draft ?? undefined).trim()) {
			natWanBlocked = true;
			if (draft) draft.natMode = 'internet-only';
			return;
		}
		natWanBlocked = false;
		if (draft) draft.natMode = mode;
	}

	async function handleStart() {
		if (onstart) {
			onstart();
			return;
		}
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
		if (onstop) {
			onstop();
			return;
		}
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
		if (draft.natMode === 'internet-only' && !effectiveStaticWan(draft).trim()) {
			notifications.error('Для режима NAT «Интернет» необходимо выбрать выход в интернет (WAN)');
			natWanBlocked = true;
			return;
		}
		localSaving = true;
		try {
			await onsave(draft);
			lastSyncedConfigJson = JSON.stringify(draft);
			notifications.success('Настройки сохранены');
			settingsOpen = false;
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
		settingsOpen = false;
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
		<InstanceBadges {row} />
		<div class="head-actions">
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
			<Button variant="ghost" size="sm" onclick={() => (settingsOpen = true)}>Настройки</Button>
		</div>
	</div>

	<StatStrip>
		<Stat value={running ? 'Запущен' : 'Остановлен'} label="Состояние" sub={uptimeValue} />
		<Stat
			value={draft?.transportMode === 'session' ? 'Мульти-сессия' : 'Одиночный'}
			label="Режим"
			sub={draft?.codec === 'batched' ? 'batched кодек' : 'legacy'}
		/>
		<Stat
			value={`:${getDirectPort()}`}
			label="Direct TCP"
			sub={isTransportEnabled('direct') ? 'прямой порт' : 'выключен'}
		/>
		<Stat
			value={activeTransportsLabel}
			label="Транспорты"
			sub={activeTransportsSub}
		/>
	</StatStrip>

	<LastErrorBox text={running ? '' : (status?.error ?? '')} />

	<!-- Секция: Куда идёт трафик -->
	<DetailSection title="Куда идёт трафик">
		<div class="line-row">
			<span class="line-label">Вход (абоненты):</span>
			{#if isTransportEnabled('direct')}
				<code>0.0.0.0:{getDirectPort()}</code>
			{:else}
				<code>Облачный транспорт</code>
			{/if}
			{#if isTransportEnabled('webdav')}
				<Badge size="sm" variant="default">WebDAV Cloud</Badge>
			{/if}
			{#if isTransportEnabled('mailru')}
				<Badge size="sm" variant="default">Mail.ru Docs</Badge>
			{/if}
			{#if isTransportEnabled('yandex')}
				<Badge size="sm" variant="default">Яндекс Docs</Badge>
			{/if}
			{#if isTransportEnabled('vyandex')}
				<Badge size="sm" variant="default">ВК/Яндекс</Badge>
			{/if}
			{#if isTransportEnabled('cupsonline')}
				<Badge size="sm" variant="default">Cups</Badge>
			{/if}
			{#if isTransportEnabled('telemost')}
				<Badge size="sm" variant="default">Telemost</Badge>
			{/if}
			<FieldHint
				text="Входящий трафик абонентов qThrone / OpenFlux через прямой TCP-порт или облачные документы."
				ariaLabel="Подсказка: приём абонентов"
			/>
		</div>

		<div class="line-row">
			<span class="line-label">Этот роутер:</span>
			<code>{(draft?.mode ?? 'l4').toUpperCase()}</code>
			<span class="line-sub">
				{draft?.mode === 'l3' ? 'L3 TUN / Packet forward' : 'L4 Stream proxy (gVisor)'}
			</span>
			<Badge size="sm" variant={draft?.mode !== 'l3' ? 'default' : (ingress ? 'success' : 'muted')}>
				{draft?.mode !== 'l3' ? 'Маршрутизация: прямой выход (L4)' : (ingress ? `Маршрутизация: ${engineName}` : 'Маршрутизация: напрямую')}
			</Badge>
			{#if draft?.egressInterface}
				<Badge size="sm" variant="default">Выход: {draft.egressInterface}</Badge>
			{/if}
			<FieldHint
				text={`Трафик абонентов направляется по правилам ${engineName} (FakeIP/TProxy) или выходит в интернет напрямую.`}
				ariaLabel="Подсказка: маршрутизация"
			/>
		</div>

		<div class="line-row">
			<span class="line-label">Выход в интернет:</span>
			<Badge size="sm" variant="accent">
				{natModeLabel(draft?.natMode)}
			</Badge>
			{#if draft?.natMode === 'internet-only' && effectiveStaticWan(draft ?? undefined)}
				<Badge size="sm" variant="default">WAN: {effectiveStaticWan(draft ?? undefined)}</Badge>
			{/if}
			{#if policyName && policyName !== 'none'}
				<Badge size="sm" variant="success">Политика: {policyName}</Badge>
			{:else}
				<Badge size="sm" variant="muted">Политика: по умолчанию</Badge>
			{/if}
			<Button variant="ghost" size="sm" href="/routing">
				Маршрутизация
				{#snippet iconAfter()}<ExternalLink size={12} />{/snippet}
			</Button>
		</div>
	</DetailSection>

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

	<LogSection log={logs || status?.logTail} {routerClock} hint="Журнал работы процесса OpenFlux" />
</Card>

<!-- Выдвижная панель «Настройки раздачи» -->
<SideDrawer open={settingsOpen} onClose={() => (settingsOpen = false)} title="Настройки раздачи">
	<div class="drawer-toggle">
		<div class="run-toggle">
			<Toggle
				label="Маршрутизация через системный маршрутизатор"
				hint={draft?.mode !== 'l3'
					? "OpenFlux работает в режиме L4 стрим-прокси (gVisor) без TUN-интерфейса. Трафик выходит напрямую через сетевой стек роутера."
					: !engineRunning
						? "Маршрутизатор не запущен — правило вступит в силу после его запуска"
						: ingressForced
							? "Режим устройств «все» — трафик абонентов перехватывается маршрутизатором"
							: ''}
				checked={draft?.mode !== 'l3' ? false : ingress}
				disabled={mutating || draft?.mode !== 'l3'}
				onchange={toggleIngress}
				spinner="before"
			/>
			<FieldHint
				text={draft?.mode !== 'l3'
					? "В режиме L4 стрим-прокси OpenFlux терминирует TCP-сессии в userspace и выходит в сеть напрямую. Перехват через системный маршрутизатор (Mihomo) и назначение политик доступа Keenetic действуют на сетевые интерфейсы (L3)."
					: ingressForced
						? "В маршрутизаторе выбран режим устройств «все»: под правила попадает весь транзитный трафик роутера, включая трафик абонентов, при любом положении этого тумблера."
						: "Трафик абонентов пойдёт по правилам системного маршрутизатора — тем же, что у устройств сети. Выключено — абоненты выходят напрямую, минуя правила."}
				ariaLabel="Подсказка: системная маршрутизация"
			/>
		</div>
	</div>

	{#if draft}
		<DetailSection title="Сеть">
			<div class="form">
				<FormRow
					label="Порт раздачи"
					for="of-direct-listen-port"
					hint="Главный внешний порт прямого TCP транспорта. Смена перезапустит сервер"
				>
					<div class="w-port">
						<Input
							id="of-direct-listen-port"
							type="number"
							value={String(getDirectPort())}
							onchange={(v) => setDirectPort(Number(v))}
							fullWidth
						/>
					</div>
				</FormRow>

				<FormRow label="Режим NAT">
					<SegmentedControl
						value={draft.natMode ?? 'full'}
						options={natModeOptions}
						ariaLabel="Режим NAT"
						disabled={busy}
						onchange={changeNat}
					/>
					{#if natWanBlocked}
						<span class="save-block">
							Сначала выберите выход в интернет (WAN) — без него режим «Интернет» не работает.
						</span>
					{/if}
				</FormRow>

				{#if draft.natMode === 'internet-only'}
					<FormRow label="Выход в интернет" hint="WAN-интерфейс для режима internet-only">
						<Dropdown
							value={effectiveStaticWan(draft)}
							options={[{ value: '', label: 'Не выбран' }, ...wanOptions]}
							placeholder={wanOptions.length ? 'Выберите WAN…' : 'WAN-интерфейсы не найдены'}
							disabled={!wanOptions.length || busy}
							onchange={(v) => {
								if (draft) {
									draft.natStaticWan = v;
									draft.natStaticWans = v ? [v] : [];
								}
								natWanBlocked = false;
							}}
							fullWidth
						/>
					</FormRow>
				{/if}

				<FormRow label="Доступ в LAN">
					<ChipMultiSelect
						values={draft.lanSegments ?? []}
						options={lanOptions}
						disabled={busy}
						onchange={(segs) => {
							if (draft) draft.lanSegments = segs;
						}}
					/>
				</FormRow>

				{#if draft.mode === 'l3'}
					<FormRow label="Политика доступа">
						<ServerAccessPolicyDropdown
							policy={draft.policy ?? 'none'}
							disabled={busy}
							onchange={(pol) => {
								if (draft) draft.policy = pol;
							}}
						/>
					</FormRow>
				{:else}
					<div class="setting-row">
						<div class="setting-copy">
							<span class="setting-title">Политика доступа</span>
							<span class="setting-description">
								В режиме L4 стрим-прокси OpenFlux выходит в сеть напрямую через стек роутера. Привязка к политикам доступа Keenetic доступна для L3-туннелей (WireGuard, WDTT).
							</span>
						</div>
					</div>
				{/if}
			</div>

			{#if draft.mode === 'l3'}
				<div class="toggle-row">
					<Toggle
						label="Использовать в политиках доступа"
						checked={draft.exposeToPolicies ?? false}
						disabled={busy}
						onchange={(v) => {
							if (draft) draft.exposeToPolicies = v;
						}}
					/>
					<FieldHint
						text="Выключено — интерфейсы остаются внутренними. Включено — роутер предлагает их в политиках доступа."
						ariaLabel="Подсказка: использовать в политиках доступа"
					/>
				</div>
			{/if}

			<div class="toggle-row">
				<Toggle
					label="Открыть порт в firewall Keenetic (WAN INPUT)"
					checked={draft.openFirewall !== false}
					disabled={busy}
					onchange={(v) => {
						if (draft) draft.openFirewall = v;
					}}
				/>
			</div>
		</DetailSection>

		<DetailSection title="Параметры OpenFlux и транспорты">
			<div class="form">
				<FormRow label="Имя профиля" hint="Отображается в клиенте при подключении">
					<Input bind:value={draft.name} placeholder="Keenetic" fullWidth />
				</FormRow>

				<FormRow label="Режим работы" hint="L4 — пользовательский стрим-прокси gVisor. L3 — TUN сетевой уровень">
					<select bind:value={draft.mode} class="form-select">
						<option value="l4">L4 (Стрим-прокси gVisor, рекомендуется)</option>
						<option value="l3">L3 (Пакетный TUN-уровень)</option>
					</select>
				</FormRow>

				<FormRow label="Транспортный режим" hint="Мульти-сессия согласовывает и чередует транспорты">
					<select bind:value={draft.transportMode} class="form-select">
						<option value="session">Мульти-сессия (--negotiate, рекомендуется)</option>
						<option value="single">Одиночный транспорт</option>
					</select>
				</FormRow>

				<FormRow label="Выходной интерфейс" hint="Интерфейс или локальный IP для исходящего трафика сервера (--local-ip)">
					<Dropdown
						value={draft.egressInterface ?? ''}
						options={[{ value: '', label: 'По умолчанию (авто)' }, ...wanOptions]}
						onchange={(v) => {
							if (draft) draft.egressInterface = v;
						}}
						fullWidth
					/>
				</FormRow>
			</div>

			<div class="transports-container">
				<!-- WebDAV Cloud (Mail.ru / Yandex.Disk) -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-webdav"
							checked={isTransportEnabled('webdav')}
							onchange={() => toggleTransport('webdav')}
						/>
						<label for="drawer-of-webdav"><strong>Облако Mail.ru / Яндекс.Диск (WebDAV — Высокая скорость)</strong></label>
					</div>
					{#if isTransportEnabled('webdav')}
						<div class="transport-inputs">
							<div class="transport-fields">
								<div class="field-item full-width">
									<label for="drawer-of-webdav-url">URL WebDAV:</label>
									<Input
										id="drawer-of-webdav-url"
										bind:value={draft.webdavUrl}
										placeholder="https://webdav.cloud.mail.ru/openflux"
										fullWidth
									/>
								</div>
								<div class="field-item">
									<label for="drawer-of-webdav-user">Логин (email):</label>
									<Input
										id="drawer-of-webdav-user"
										bind:value={draft.webdavUser}
										placeholder="user@mail.ru"
										fullWidth
									/>
								</div>
								<div class="field-item">
									<label for="drawer-of-webdav-pass">Пароль приложения:</label>
									<SensitiveInput
										bind:value={draft.webdavPassword}
										placeholder="Пароль внешнего приложения..."
									/>
								</div>
							</div>
						</div>
					{/if}
				</div>

				<!-- Mail.ru Docs -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-mailru"
							checked={isTransportEnabled('mailru')}
							onchange={() => toggleTransport('mailru')}
						/>
						<label for="drawer-of-mailru"><strong>Mail.ru Документы (Обход белых списков)</strong></label>
					</div>
					{#if isTransportEnabled('mailru')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.mailruUrl}
								placeholder="https://docs.mail.ru/document/..."
								fullWidth
							/>
						</div>
					{/if}
				</div>

				<!-- Yandex Docs -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-yandex"
							checked={isTransportEnabled('yandex')}
							onchange={() => toggleTransport('yandex')}
						/>
						<label for="drawer-of-yandex"><strong>Яндекс Документы (Yandex Docs)</strong></label>
					</div>
					{#if isTransportEnabled('yandex')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.yandexUrl}
								placeholder="https://docs.yandex.ru/docs/view?url=..."
								fullWidth
							/>
						</div>
					{/if}
				</div>

				<!-- Vyandex -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-vyandex"
							checked={isTransportEnabled('vyandex')}
							onchange={() => toggleTransport('vyandex')}
						/>
						<label for="drawer-of-vyandex"><strong>Vyandex (Voice/Docs)</strong></label>
					</div>
					{#if isTransportEnabled('vyandex')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.vyandexUrl}
								placeholder="https://docs.yandex.ru/docs/view?url=... (или пусто для использования Yandex URL)"
								fullWidth
							/>
						</div>
					{/if}
				</div>

				<!-- CupsOnline -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-cupsonline"
							checked={isTransportEnabled('cupsonline')}
							onchange={() => toggleTransport('cupsonline')}
						/>
						<label for="drawer-of-cupsonline"><strong>CupsOnline</strong></label>
					</div>
					{#if isTransportEnabled('cupsonline')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.cupsonlineUrl}
								placeholder="https://..."
								fullWidth
							/>
						</div>
					{/if}
				</div>

				<!-- Telemost -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-telemost"
							checked={isTransportEnabled('telemost')}
							onchange={() => toggleTransport('telemost')}
						/>
						<label for="drawer-of-telemost"><strong>Яндекс Телемост (Telemost)</strong></label>
					</div>
					{#if isTransportEnabled('telemost')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.telemostUrl}
								placeholder="https://telemost.yandex.ru/j/..."
								fullWidth
							/>
						</div>
					{/if}
				</div>

				<!-- Direct TCP -->
				<div class="transport-card">
					<div class="transport-title-row">
						<input
							type="checkbox"
							id="drawer-of-direct"
							checked={isTransportEnabled('direct')}
							onchange={() => toggleTransport('direct')}
						/>
						<label for="drawer-of-direct"><strong>Прямой TCP (Direct TCP, резервный)</strong></label>
					</div>
					{#if isTransportEnabled('direct')}
						<div class="transport-inputs">
							<Input
								bind:value={draft.directHost}
								placeholder="Внешний хост / KeenDNS (автоопределение)"
								fullWidth
							/>
						</div>
					{/if}
				</div>
			</div>

			<div class="form mt-3">
				<FormRow label="Секретный ключ" hint="AES-256-GCM ключ шифрования (мин. 16 символов)">
					<div class="secret-box">
						<Input bind:value={draft.secret} placeholder="Ключ шифрования..." fullWidth />
						<Button variant="secondary" size="sm" onclick={generateSecret} title="Сгенерировать случайный ключ">
							{#snippet iconBefore()}<Key size={14} />{/snippet}
							Ключ
						</Button>
					</div>
				</FormRow>

				<FormRow label="Кодек пакетов" hint="batched — пакетирование и сжатие трафика">
					<select bind:value={draft.codec} class="form-select">
						<option value="batched">batched (Рекомендуется)</option>
						<option value="legacy">legacy</option>
					</select>
				</FormRow>

				<FormRow label="Уровень отладки" hint="Детализация логов процесса">
					<select bind:value={draft.debug} class="form-select">
						<option value={0}>0 — Выключено</option>
						<option value={1}>1 — Базовая отладка (-d)</option>
						<option value={2}>2 — Подробная отладка (-dd)</option>
						<option value={3}>3 — Трассировка (-ddd)</option>
					</select>
				</FormRow>
			</div>

			<div class="btn-row">
				<Button
					variant="primary"
					loading={localSaving || saving}
					disabled={localSaving || saving}
					onclick={handleSave}
				>
					Сохранить
				</Button>
				<Button variant="ghost" onclick={revert}>Отменить</Button>
			</div>
		</DetailSection>
	{/if}
</SideDrawer>

<style>
	.head {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
		margin-bottom: 0.75rem;
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
		gap: 0.375rem;
		margin-left: auto;
		flex-wrap: wrap;
	}

	.line-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.45rem 0;
		border-bottom: 1px solid var(--color-border);
		flex-wrap: wrap;
		font-size: 0.8125rem;
	}

	.line-row:last-child {
		border-bottom: none;
	}

	.line-label {
		font-weight: 600;
		min-width: 140px;
		color: var(--color-text-secondary);
	}

	.line-sub {
		font-size: 0.75rem;
		color: var(--color-text-muted);
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

	.drawer-toggle {
		margin-bottom: 1rem;
	}

	.run-toggle {
		display: flex;
		align-items: center;
		gap: 0.375rem;
	}

	.form {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		--form-label-w: 140px;
		margin-bottom: 1rem;
	}

	.form :global(.form-row-control > *) {
		max-width: 420px;
	}

	.form :global(.form-row-control > [role='group']) {
		width: fit-content;
	}

	.w-port {
		width: 110px;
	}

	.toggle-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
		margin-top: 0.75rem;
	}

	.transports-container {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
		margin-top: 0.5rem;
	}

	.transport-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		padding: 0.625rem 0.75rem;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.transport-title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8125rem;
		color: var(--color-text-primary);
	}

	.transport-inputs {
		margin-top: 0.25rem;
	}

	.transport-fields {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: 0.5rem;
		margin-top: 0.25rem;
		padding-top: 0.5rem;
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

	.secret-box {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 100%;
	}

	.form-select {
		width: 100%;
		max-width: 420px;
		padding: 0.45rem 0.65rem;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-primary);
		font-size: 0.8125rem;
	}

	.form-select:focus {
		border-color: var(--color-accent);
		outline: none;
	}

	.btn-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-top: 1.25rem;
	}

	.mt-3 {
		margin-top: 0.75rem;
	}

	.save-block {
		display: block;
		margin-top: 0.35rem;
		font-size: 0.75rem;
		color: var(--color-warning, #f59e0b);
	}
</style>
