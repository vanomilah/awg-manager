<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import {
		Button,
		Badge,
		Dropdown,
		type DropdownOption,
		Modal,
	} from '$lib/components/ui';
	import {
		Play,
		Square,
		RefreshCw,
		CheckCircle,
		AlertTriangle,
		XCircle,
		Zap,
		Settings2,
		Trash2,
		Check,
		Sparkles,
		ChevronDown,
		ChevronRight,
		Search,
	} from 'lucide-svelte';
	import type {
		AdaptiveRoutingSettings,
		OperationalState,
		ResolvedEgress,
		EgressRef,
		LearnedDataResponse,
	} from '$lib/types/adaptiveRouting';
	import type { AccessPolicy, PolicyGlobalInterface, RoutingTunnel } from '$lib/types/routing';

	interface Props {
		policies?: AccessPolicy[];
		policyInterfaces?: PolicyGlobalInterface[];
		tunnels?: RoutingTunnel[];
	}

	let {
		policies = [],
		policyInterfaces = [],
		tunnels = [],
	}: Props = $props();

	function defaultSettings(): AdaptiveRoutingSettings {
		return {
			enabled: false,
			routingTableId: 105,
			fwmarkMask: '0x30000000',
			fwmarkTest: '0x10000000',
			fwmarkOk: '0x20000000',
			rulePriorityTest: 96,
			rulePriorityOk: 95,
			source: {
				type: 'all_lan',
			},
			primaryEgress: {
				kind: 'kernel-tunnel',
				resourceId: '',
				engine: 'system',
			},
			failurePolicy: 'direct',
			detection: {
				fastIntervalSeconds: 1,
				softIntervalSeconds: 1,
				judgeIntervalSeconds: 1,
				healthIntervalSeconds: 5,
				tcpSynRetries: 2,
				lateStallBytes: 1500,
			},
			persistence: {
				okTtlSeconds: 0,
				maxEntries: 4096,
				separateTcpUdp: true,
			},
			alwaysFileEnabled: true,
			neverFileEnabled: true,
			alwaysEntries: [],
			neverEntries: [],
		};
	}

	let loading = $state(true);
	let saving = $state(false);
	let actionBusy = $state(false);
	let testingEgress = $state(false);

	let status = $state<OperationalState | null>(null);
	let settings = $state<AdaptiveRoutingSettings>(defaultSettings());
	let egresses = $state<ResolvedEgress[]>([]);
	let learned = $state<LearnedDataResponse | null>(null);

	let initialSettingsJson = $state('');
	let settingsHydrated = $state(false);

	let testResult = $state<{ available: boolean; interface: string; reason?: string } | null>(null);

	let detectorExpanded = $state(false);
	let expertExpanded = $state(false);
	let showLearnedModal = $state(false);

	let alwaysText = $state('');
	let neverText = $state('');

	let pollTimer: ReturnType<typeof setInterval> | null = null;

	function egressKey(ref: EgressRef): string {
		if (!ref || !ref.resourceId) return '';
		return `${ref.engine}::${ref.kind}::${ref.resourceId}`;
	}

	function parseEgressKey(key: string): EgressRef {
		const parts = key.split('::');
		if (parts.length < 3) {
			return { engine: 'system', kind: 'kernel-tunnel', resourceId: '' };
		}
		return {
			engine: parts[0] as EgressRef['engine'],
			kind: parts[1] as EgressRef['kind'],
			resourceId: parts.slice(2).join('::'),
		};
	}

	let selectedEgressValue = $state('');

	function useServerSettings(next: AdaptiveRoutingSettings) {
		settings = structuredClone(next);
		selectedEgressValue = egressKey(next.primaryEgress);
		alwaysText = (next.alwaysEntries || []).join('\n');
		neverText = (next.neverEntries || []).join('\n');
		initialSettingsJson = JSON.stringify(next);
		settingsHydrated = true;
	}

	// Dirty state detection
	const isDirty = $derived.by(() => {
		if (!initialSettingsJson) return false;
		const currentSnapshot = {
			...settings,
			alwaysEntries: alwaysText.split('\n').map((s) => s.trim()).filter(Boolean),
			neverEntries: neverText.split('\n').map((s) => s.trim()).filter(Boolean),
		};
		return JSON.stringify(currentSnapshot) !== initialSettingsJson;
	});

	async function loadAll(initial = false) {
		if (initial) loading = true;
		try {
			const [statusRes, egressRes, learnedRes] = await Promise.all([
				api.getAdaptiveRoutingStatus(),
				api.getAdaptiveRoutingEgresses(),
				api.getAdaptiveRoutingLearned().catch(() => null),
			]);

			status = statusRes.state;
			egresses = egressRes.items || [];
			learned = learnedRes;

			// Polling refreshes runtime state and counters only. It must never
			// overwrite a user's unsaved form selections.
			if (!settingsHydrated) {
				useServerSettings(statusRes.settings);
			}
		} catch (e) {
			if (initial) {
				notifications.error(`Ошибка загрузки Susanin: ${(e as Error).message}`);
			}
		} finally {
			if (initial) loading = false;
		}
	}

	onMount(() => {
		void loadAll(true);
		pollTimer = setInterval(() => {
			void loadAll(false);
		}, 5000);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
	});

	function handleEgressChange(newVal: string) {
		selectedEgressValue = newVal;
		settings = { ...settings, primaryEgress: parseEgressKey(newVal) };
		testResult = null;
	}

	function selectSource(type: 'all_lan' | 'policy') {
		settings = {
			...settings,
			source: type === 'policy'
				? { type: 'policy', policyId: settings.source.policyId || policies[0]?.name || '' }
				: { type: 'all_lan' },
		};
	}

	function selectFailurePolicy(value: 'direct' | 'block') {
		settings = { ...settings, failurePolicy: value };
	}

	async function handleApply() {
		if (saving) return;
		saving = true;
		try {
			settings.alwaysEntries = alwaysText
				.split('\n')
				.map((s) => s.trim())
				.filter(Boolean);
			settings.neverEntries = neverText
				.split('\n')
				.map((s) => s.trim())
				.filter(Boolean);

			const res = await api.applyAdaptiveRouting(settings);
			status = res.state;
			useServerSettings(res.settings);
			notifications.success('Настройки Susanin успешно применены');
			void loadAll(false);
		} catch (e) {
			notifications.error(`Ошибка применения: ${(e as Error).message}`);
		} finally {
			saving = false;
		}
	}

	async function handleStart() {
		if (actionBusy) return;
		actionBusy = true;
		try {
			const res = await api.startAdaptiveRouting();
			status = res.state;
			notifications.success('Адаптивная маршрутизация Susanin запущена');
			void loadAll(false);
		} catch (e) {
			notifications.error(`Ошибка запуска: ${(e as Error).message}`);
		} finally {
			actionBusy = false;
		}
	}

	async function handleStop() {
		if (actionBusy) return;
		actionBusy = true;
		try {
			const res = await api.stopAdaptiveRouting();
			status = res.state;
			notifications.info('Адаптивная маршрутизация остановлена');
			void loadAll(false);
		} catch (e) {
			notifications.error(`Ошибка остановки: ${(e as Error).message}`);
		} finally {
			actionBusy = false;
		}
	}

	async function handleTestEgress() {
		if (!settings.primaryEgress.resourceId) {
			notifications.warning('Выберите выход для проверки');
			return;
		}
		testingEgress = true;
		testResult = null;
		try {
			const res = await api.testAdaptiveRoutingEgress(settings.primaryEgress);
			testResult = res;
			if (res.available) {
				notifications.success(`Выход доступен (интерфейс: ${res.interface})`);
			} else {
				notifications.warning(`Выход недоступен: ${res.reason || 'неизвестная ошибка'}`);
			}
		} catch (e) {
			notifications.error(`Ошибка проверки: ${(e as Error).message}`);
		} finally {
			testingEgress = false;
		}
	}

	const egressOptions = $derived.by(() => {
		const opts: DropdownOption[] = [
			{ value: '', label: '— Выберите выход для обхода —' },
		];

		const systemTunnels = egresses.filter((e) => e.ref.kind === 'kernel-tunnel');
		const mihomoGroups = egresses.filter((e) => e.ref.kind === 'mihomo-group');
		const mihomoProxies = egresses.filter((e) => e.ref.kind === 'mihomo-proxy');
		const mihomoSubscriptions = egresses.filter((e) => e.ref.kind === 'mihomo-subscription');
		const singboxOutbounds = egresses.filter((e) => e.ref.kind === 'singbox-outbound');

		if (mihomoGroups.length > 0) {
			for (const g of mihomoGroups) {
				opts.push({
					value: egressKey(g.ref),
					label: g.displayName,
					description: 'Прокси-группа Mihomo',
					group: 'Прокси-группы',
				});
			}
		}

		if (systemTunnels.length > 0) {
			for (const t of systemTunnels) {
				opts.push({
					value: egressKey(t.ref),
					label: t.displayName,
					description: `Туннель · ${t.interface}`,
					group: 'Туннели',
				});
			}
		}

		if (mihomoProxies.length > 0) {
			for (const p of mihomoProxies) {
				opts.push({
					value: egressKey(p.ref),
					label: p.displayName,
					description: 'Прокси-узел Mihomo',
					group: 'Прокси-узлы',
				});
			}
		}

		if (singboxOutbounds.length > 0) {
			for (const s of singboxOutbounds) {
				opts.push({
					value: egressKey(s.ref),
					label: s.displayName,
					description: 'Выход sing-box',
					group: 'Выходы sing-box',
				});
			}
		}

		for (const subscription of mihomoSubscriptions) {
			opts.push({
				value: egressKey(subscription.ref),
				label: subscription.displayName,
				description: 'Подписка Mihomo',
				group: 'Подписки',
			});
		}

		return opts;
	});

	const activeEgressItem = $derived(
		egresses.find((e) => egressKey(e.ref) === selectedEgressValue)
	);

	function applyDetectorPreset(preset: 'balanced' | 'aggressive' | 'soft') {
		if (preset === 'balanced') {
			settings.detection.fastIntervalSeconds = 1;
			settings.detection.softIntervalSeconds = 1;
			settings.detection.judgeIntervalSeconds = 1;
			settings.detection.healthIntervalSeconds = 5;
			settings.detection.tcpSynRetries = 2;
			settings.detection.lateStallBytes = 1500;
		} else if (preset === 'aggressive') {
			settings.detection.fastIntervalSeconds = 1;
			settings.detection.softIntervalSeconds = 1;
			settings.detection.judgeIntervalSeconds = 1;
			settings.detection.healthIntervalSeconds = 3;
			settings.detection.tcpSynRetries = 1;
			settings.detection.lateStallBytes = 1000;
		} else if (preset === 'soft') {
			settings.detection.fastIntervalSeconds = 2;
			settings.detection.softIntervalSeconds = 2;
			settings.detection.judgeIntervalSeconds = 2;
			settings.detection.healthIntervalSeconds = 8;
			settings.detection.tcpSynRetries = 3;
			settings.detection.lateStallBytes = 2500;
		}
		notifications.info(`Применён пресет детектора: ${preset === 'balanced' ? 'Сбалансированный' : preset === 'aggressive' ? 'Агрессивный' : 'Мягкий'}`);
	}
</script>

<div class="susanin-page space-y-4">
	<!-- 1. Компактная Status Bar -->
	<div class="susanin-status bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-3.5 flex flex-col md:flex-row md:items-center justify-between gap-3 shadow-sm">
		<div class="flex items-center gap-3 flex-wrap">
			<div class="flex items-center gap-2">
				<span class="w-2.5 h-2.5 rounded-full {status?.status === 'running' ? 'bg-[var(--color-success)] animate-pulse' : status?.status === 'degraded' ? 'bg-[var(--color-warning)]' : 'bg-gray-500'}"></span>
				<span class="font-semibold text-sm text-[var(--color-text-primary)]">
					{status?.status === 'running' ? 'Susanin работает' : status?.status === 'degraded' ? 'Susanin деградирован' : 'Susanin остановлен'}
				</span>
			</div>

			<Badge variant={status?.routingOwner === 'susanin' ? 'success' : 'muted'} size="sm">
				Владелец сети: {status?.routingOwner === 'susanin' ? 'Susanin' : status?.routingOwner || 'Нет'}
			</Badge>

			{#if activeEgressItem}
				<div class="flex items-center gap-1 text-xs text-[var(--color-text-secondary)] bg-[var(--color-bg-tertiary)] px-2.5 py-1 rounded-md border border-[var(--color-border)]">
					<span class="text-[var(--color-text-muted)]">Выход:</span>
					<span class="font-medium text-[var(--color-text-primary)] truncate max-w-[200px]">{activeEgressItem.displayName}</span>
				</div>
			{:else}
				<span class="text-xs text-[var(--color-text-muted)]">Выход не выбран</span>
			{/if}

			<Badge variant={settings.failurePolicy === 'direct' ? 'info' : 'warning'} size="sm">
				{settings.failurePolicy === 'direct' ? 'Direct Fail-Open' : 'Block (Kill-Switch)'}
			</Badge>

			{#if isDirty}
				<span class="text-xs text-[var(--color-warning)] flex items-center gap-1 font-medium">
					<AlertTriangle class="w-3.5 h-3.5" />
					Есть несохранённые изменения
				</span>
			{/if}
		</div>

		<div class="flex items-center gap-2 shrink-0 self-end md:self-auto">
			{#if isDirty}
				<Button
					variant="primary"
					size="sm"
					loading={saving}
					onclick={handleApply}
				>
					<Check class="w-4 h-4 mr-1" />
					Применить
				</Button>
			{/if}

			{#if status?.status === 'running'}
				<Button
					variant="secondary"
					size="sm"
					disabled={actionBusy}
					onclick={handleStop}
				>
					<Square class="w-4 h-4 mr-1" />
					Остановить
				</Button>
			{:else}
				<Button
					variant="primary"
					size="sm"
					disabled={actionBusy || !settings.primaryEgress.resourceId}
					title={!settings.primaryEgress.resourceId ? 'Сначала выберите выход' : 'Запустить адаптивную маршрутизацию'}
					onclick={handleStart}
				>
					<Play class="w-4 h-4 mr-1" />
					Запустить
				</Button>
			{/if}
		</div>
	</div>

	<!-- 2. Основная конфигурация в 3 карточки (Responsive: 1 col on mobile, 2-3 on tablet/desktop) -->
	<div class="susanin-steps grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 items-stretch">
		<!-- Карточка 1: Источник трафика -->
		<div class="susanin-step bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-4 flex flex-col justify-between">
			<div>
				<div class="flex items-center gap-2 mb-3">
					<div class="w-7 h-7 rounded-lg bg-[var(--color-bg-tertiary)] flex items-center justify-center text-[var(--color-accent)] font-semibold text-xs border border-[var(--color-border)]">
						1
					</div>
					<div>
						<h3 class="font-semibold text-sm text-[var(--color-text-primary)]">Источник трафика</h3>
						<p class="text-xs text-[var(--color-text-muted)]">Какие устройства маршрутизировать</p>
					</div>
				</div>

				<div class="space-y-2 mt-2">
					<label class="flex items-start gap-2.5 p-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-tertiary)] cursor-pointer hover:border-[var(--color-border-hover)] transition-colors">
						<input
							type="radio"
							name="sourceType"
							value="all_lan"
							checked={settings.source.type === 'all_lan'}
							onchange={() => selectSource('all_lan')}
							class="mt-0.5 text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						<div>
							<div class="text-xs font-medium text-[var(--color-text-primary)]">Все устройства домашней сети</div>
							<div class="text-[11px] text-[var(--color-text-muted)]">Адаптивный обход действует для всего LAN-трафика</div>
						</div>
					</label>

					<label class="flex items-start gap-2.5 p-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-tertiary)] cursor-pointer hover:border-[var(--color-border-hover)] transition-colors">
						<input
							type="radio"
							name="sourceType"
							value="policy"
							checked={settings.source.type === 'policy'}
							onchange={() => selectSource('policy')}
							class="mt-0.5 text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						<div>
							<div class="text-xs font-medium text-[var(--color-text-primary)]">Выбранная политика Keenetic</div>
							<div class="text-[11px] text-[var(--color-text-muted)]">Маршрутизировать только устройства выбранной политики</div>
						</div>
					</label>

					{#if settings.source.type === 'policy'}
						<div class="p-2 bg-[var(--color-bg-primary)] rounded-lg border border-[var(--color-border)] mt-2">
							<label for="susanin-policy-select" class="block text-[11px] text-[var(--color-text-muted)] mb-1">Политика маршрутизации</label>
							<select
								id="susanin-policy-select"
								value={settings.source.policyId || ''}
								onchange={(e) => {
									settings = { ...settings, source: { ...settings.source, policyId: e.currentTarget.value } };
								}}
								class="w-full p-1.5 text-xs bg-[var(--color-bg-secondary)] border border-[var(--color-border)] rounded text-[var(--color-text-primary)] focus:outline-none focus:border-[var(--color-accent)]"
							>
								{#each policies as pol}
									<option value={pol.name}>{pol.name} ({pol.description || 'Политика'})</option>
								{/each}
							</select>
						</div>
					{/if}
				</div>
			</div>

			<div class="mt-4 pt-3 border-t border-[var(--color-border)] text-[11px] text-[var(--color-text-muted)]">
				Susanin слушает соединения и точечно направляет сбои через выход.
			</div>
		</div>

		<!-- Карточка 2: Выход в интернет -->
		<div class="susanin-step bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-4 flex flex-col justify-between">
			<div>
				<div class="flex items-center gap-2 mb-3">
					<div class="w-7 h-7 rounded-lg bg-[var(--color-bg-tertiary)] flex items-center justify-center text-[var(--color-accent)] font-semibold text-xs border border-[var(--color-border)]">
						2
					</div>
					<div>
						<h3 class="font-semibold text-sm text-[var(--color-text-primary)]">Выход для обхода</h3>
						<p class="text-xs text-[var(--color-text-muted)]">Куда отправлять заблокированные сайты</p>
					</div>
				</div>

				<div class="space-y-3">
					<Dropdown
						options={egressOptions}
						value={selectedEgressValue}
						onchange={handleEgressChange}
					/>

					{#if activeEgressItem}
						<div class="bg-[var(--color-bg-tertiary)] p-2.5 rounded-lg border border-[var(--color-border)] space-y-1.5">
							<div
								class="text-sm font-medium text-[var(--color-text-primary)] break-words"
								title={activeEgressItem.displayName}
							>
								{activeEgressItem.displayName}
							</div>
							<div class="flex items-center justify-between text-xs">
								<span class="text-[var(--color-text-muted)]">Интерфейс:</span>
								<span class="font-mono text-[var(--color-text-primary)] font-medium">{activeEgressItem.interface}</span>
							</div>
							<div class="flex items-center justify-between text-xs">
								<span class="text-[var(--color-text-muted)]">Исполнитель:</span>
								<Badge variant="muted" size="sm">{activeEgressItem.ref.engine.toUpperCase()}</Badge>
							</div>
							<div class="flex items-center justify-between text-xs">
								<span class="text-[var(--color-text-muted)]">Тип:</span>
								<span class="text-[var(--color-text-secondary)]">{activeEgressItem.ref.kind}</span>
							</div>
						</div>
					{/if}

					<div class="flex items-center gap-2">
						<Button
							variant="secondary"
							size="sm"
							loading={testingEgress}
							disabled={!settings.primaryEgress.resourceId}
							onclick={handleTestEgress}
						>
							<Sparkles class="w-3.5 h-3.5 mr-1" />
							Проверить доступность
						</Button>
						{#if testResult}
							<Badge variant={testResult.available ? 'success' : 'error'} size="sm">
								{testResult.available ? 'Доступен' : 'Недоступен'}
							</Badge>
						{/if}
					</div>
				</div>
			</div>

			<div class="mt-4 pt-3 border-t border-[var(--color-border)] text-[11px] text-[var(--color-text-muted)]">
				Трафик подается через интерфейс <code class="font-mono">awgsus0</code> без перехвата основного интернета.
			</div>
		</div>

		<!-- Карточка 3: Поведение при сбое -->
		<div class="susanin-step bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-4 flex flex-col justify-between">
			<div>
				<div class="flex items-center gap-2 mb-3">
					<div class="w-7 h-7 rounded-lg bg-[var(--color-bg-tertiary)] flex items-center justify-center text-[var(--color-accent)] font-semibold text-xs border border-[var(--color-border)]">
						3
					</div>
					<div>
						<h3 class="font-semibold text-sm text-[var(--color-text-primary)]">Поведение при сбое</h3>
						<p class="text-xs text-[var(--color-text-muted)]">Что делать, если выход недоступен</p>
					</div>
				</div>

				<div class="space-y-2 mt-2">
					<label class="flex items-start gap-2.5 p-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-tertiary)] cursor-pointer hover:border-[var(--color-border-hover)] transition-colors">
						<input
							type="radio"
							name="failurePolicy"
							value="direct"
							checked={settings.failurePolicy === 'direct'}
							onchange={() => selectFailurePolicy('direct')}
							class="mt-0.5 text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						<div>
							<div class="text-xs font-medium text-[var(--color-text-primary)]">Direct Fail-Open (Рекомендуется)</div>
							<div class="text-[11px] text-[var(--color-text-muted)]">При падении прокси весь трафик возвращается напрямую в интернет, связь не рвется</div>
						</div>
					</label>

					<label class="flex items-start gap-2.5 p-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-tertiary)] cursor-pointer hover:border-[var(--color-border-hover)] transition-colors">
						<input
							type="radio"
							name="failurePolicy"
							value="block"
							checked={settings.failurePolicy === 'block'}
							onchange={() => selectFailurePolicy('block')}
							class="mt-0.5 text-[var(--color-accent)] focus:ring-[var(--color-accent)]"
						/>
						<div>
							<div class="text-xs font-medium text-[var(--color-text-primary)]">Блокировать (Kill-Switch)</div>
							<div class="text-[11px] text-[var(--color-text-muted)]">Запрещать доступ в сеть при недоступности выхода (для строгой анонимности)</div>
						</div>
					</label>
				</div>
			</div>

			<div class="mt-4 pt-3 border-t border-[var(--color-border)] text-[11px] text-[var(--color-text-muted)]">
				Политика Fail-Open защищает от потери связи для банков, Госуслуг и мессенджеров.
			</div>
		</div>
	</div>

	<!-- 3. Секция статистики обучения и пресет детектора -->
	<div class="susanin-learning bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-4 shadow-sm">
		<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
			<div>
				<h3 class="font-semibold text-sm text-[var(--color-text-primary)] flex items-center gap-2">
					<Zap class="w-4 h-4 text-[var(--color-warning)]" />
					Статистика адаптивного обучения
				</h3>
				<p class="text-xs text-[var(--color-text-muted)]">
					Направления, которые Susanin распознал и запомнил для обхода
				</p>
			</div>

			<div class="flex items-center gap-2">
				<Button
					variant="secondary"
					size="sm"
					onclick={() => (showLearnedModal = true)}
				>
					<Search class="w-3.5 h-3.5 mr-1" />
					Посмотреть адреса
				</Button>
				<Button
					variant="secondary"
					size="sm"
					onclick={() => (detectorExpanded = !detectorExpanded)}
				>
					<Settings2 class="w-3.5 h-3.5 mr-1" />
					{detectorExpanded ? 'Скрыть детектор' : 'Настроить детектор'}
				</Button>
			</div>
		</div>

		<!-- Метрики обучения -->
		<div class="learning-grid grid grid-cols-2 sm:grid-cols-4 gap-3">
			<div class="bg-[var(--color-bg-tertiary)] p-3 rounded-lg border border-[var(--color-border)]">
				<div class="text-xs text-[var(--color-text-muted)]">Изучено TCP (ОК)</div>
				<div class="text-xl font-bold font-mono text-[var(--color-success)] mt-0.5">
					{status?.learnedTcpCount ?? 0}
				</div>
				<div class="text-[10px] text-[var(--color-text-muted)] mt-1">Через прокси-выход</div>
			</div>

			<div class="bg-[var(--color-bg-tertiary)] p-3 rounded-lg border border-[var(--color-border)]">
				<div class="text-xs text-[var(--color-text-muted)]">Изучено UDP (ОК)</div>
				<div class="text-xl font-bold font-mono text-[var(--color-info,#7dcfff)] mt-0.5">
					{status?.learnedUdpCount ?? 0}
				</div>
				<div class="text-[10px] text-[var(--color-text-muted)] mt-1">QUIC / Голосовые звонки</div>
			</div>

			<div class="bg-[var(--color-bg-tertiary)] p-3 rounded-lg border border-[var(--color-border)]">
				<div class="text-xs text-[var(--color-text-muted)]">На проверке (Testing)</div>
				<div class="text-xl font-bold font-mono text-[var(--color-warning)] mt-0.5">
					{(status?.testingTcpCount ?? 0) + (status?.testingUdpCount ?? 0)}
				</div>
				<div class="text-[10px] text-[var(--color-text-muted)] mt-1">Тестовая проба</div>
			</div>

			<div class="bg-[var(--color-bg-tertiary)] p-3 rounded-lg border border-[var(--color-border)]">
				<div class="text-xs text-[var(--color-text-muted)]">Списки исключений</div>
				<div class="text-xl font-bold font-mono text-[var(--color-text-primary)] mt-0.5">
					{(settings.alwaysEntries || []).length} / {(settings.neverEntries || []).length}
				</div>
				<div class="text-[10px] text-[var(--color-text-muted)] mt-1">Всегда / Напрямую</div>
			</div>
		</div>

		<!-- Раскрывающийся блок детектора -->
		{#if detectorExpanded}
			<div class="mt-4 pt-4 border-t border-[var(--color-border)] space-y-3">
				<div class="flex items-center justify-between flex-wrap gap-2">
					<div class="text-xs font-semibold text-[var(--color-text-primary)]">
						Параметры детектора сбоев соединений
					</div>
					<div class="flex items-center gap-1.5">
						<span class="text-xs text-[var(--color-text-muted)] mr-1">Готовые пресеты:</span>
						<button
							class="px-2 py-0.5 text-xs rounded border border-[var(--color-border)] hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] transition-colors"
							onclick={() => applyDetectorPreset('balanced')}
						>
							Сбалансированный
						</button>
						<button
							class="px-2 py-0.5 text-xs rounded border border-[var(--color-border)] hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] transition-colors"
							onclick={() => applyDetectorPreset('aggressive')}
						>
							Агрессивный
						</button>
						<button
							class="px-2 py-0.5 text-xs rounded border border-[var(--color-border)] hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] transition-colors"
							onclick={() => applyDetectorPreset('soft')}
						>
							Мягкий
						</button>
					</div>
				</div>

				<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
					<div>
						<label for="susanin-tcp-syn-retries" class="block text-xs text-[var(--color-text-muted)] mb-1">Повторов SYN до переключения</label>
						<input
							id="susanin-tcp-syn-retries"
							type="number"
							bind:value={settings.detection.tcpSynRetries}
							min={1}
							max={5}
							class="w-full p-2 text-xs bg-[var(--color-bg-primary)] border border-[var(--color-border)] rounded-md text-[var(--color-text-primary)]"
						/>
					</div>
					<div>
						<label for="susanin-health-interval" class="block text-xs text-[var(--color-text-muted)] mb-1">Интервал проверки здоровья (с)</label>
						<input
							id="susanin-health-interval"
							type="number"
							bind:value={settings.detection.healthIntervalSeconds}
							min={1}
							max={60}
							class="w-full p-2 text-xs bg-[var(--color-bg-primary)] border border-[var(--color-border)] rounded-md text-[var(--color-text-primary)]"
						/>
					</div>
					<div>
						<label for="susanin-late-stall-bytes" class="block text-xs text-[var(--color-text-muted)] mb-1">Порог подвисания (байт)</label>
						<input
							id="susanin-late-stall-bytes"
							type="number"
							bind:value={settings.detection.lateStallBytes}
							min={500}
							max={10000}
							class="w-full p-2 text-xs bg-[var(--color-bg-primary)] border border-[var(--color-border)] rounded-md text-[var(--color-text-primary)]"
						/>
					</div>
				</div>
			</div>
		{/if}
	</div>

	<!-- 4. Раскрывающийся раздел «Экспертные настройки (Accordion)» -->
	<div class="bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] overflow-hidden shadow-sm">
		<button
			type="button"
			class="w-full p-4 flex items-center justify-between text-left hover:bg-[var(--color-bg-tertiary)] transition-colors"
			onclick={() => (expertExpanded = !expertExpanded)}
		>
			<div class="flex items-center gap-2">
				{#if expertExpanded}
					<ChevronDown class="w-4 h-4 text-[var(--color-text-muted)]" />
				{:else}
					<ChevronRight class="w-4 h-4 text-[var(--color-text-muted)]" />
				{/if}
				<span class="font-semibold text-sm text-[var(--color-text-primary)]">
					Расширенные сетевые параметры (Expert)
				</span>
				<Badge variant="muted" size="sm">Списки и TTL кэша</Badge>
			</div>
			<span class="text-xs text-[var(--color-text-muted)]">
				{expertExpanded ? 'Свернуть' : 'Развернуть'}
			</span>
		</button>

		{#if expertExpanded}
			<div class="p-4 pt-1 border-t border-[var(--color-border)] space-y-4">
				<!-- Списки Always / Never -->
				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div>
						<label for="susanin-always-text" class="block text-xs font-semibold text-[var(--color-text-primary)] mb-1">
							Всегда через прокси-выход (Always)
						</label>
						<p class="text-[11px] text-[var(--color-text-muted)] mb-1.5">
							Домены и IP, по одному на строку (направляются через выход безусловно)
						</p>
						<textarea
							id="susanin-always-text"
							bind:value={alwaysText}
							rows={4}
							placeholder="example.com&#10;api.service.io&#10;198.51.100.0/24"
							class="w-full p-2.5 text-xs font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent)]"
						></textarea>
					</div>

					<div>
						<label for="susanin-never-text" class="block text-xs font-semibold text-[var(--color-text-primary)] mb-1">
							Всегда напрямую в интернет (Never)
						</label>
						<p class="text-[11px] text-[var(--color-text-muted)] mb-1.5">
							Домены и IP банков, госсервисов, локальных сетей (никогда не пойдут через прокси)
						</p>
						<textarea
							id="susanin-never-text"
							bind:value={neverText}
							rows={4}
							placeholder="gosuslugi.ru&#10;sberbank.ru&#10;192.168.0.0/16"
							class="w-full p-2.5 text-xs font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent)]"
						></textarea>
					</div>
				</div>

			</div>
		{/if}
	</div>
</div>

<style>
	.susanin-page { display: flex; width: 100%; flex-direction: column; gap: 18px; }
	.susanin-status {
		display: flex; align-items: center; justify-content: space-between; gap: 16px;
		padding: 16px 18px; border: 1px solid var(--color-border); border-radius: 12px;
		background: var(--color-bg-secondary); box-shadow: 0 1px 2px rgb(15 23 42 / 5%);
	}
	.susanin-steps { display: grid; grid-template-columns: minmax(0, 1fr) minmax(360px, 1.35fr) minmax(0, 1fr); gap: 18px; align-items: stretch; }
	.susanin-step {
		display: flex; min-width: 0; min-height: 300px; flex-direction: column; justify-content: space-between;
		padding: 20px; border: 1px solid var(--color-border); border-radius: 12px;
		background: var(--color-bg-secondary);
	}
	.susanin-step :global(label) { padding: 12px; }
	.susanin-learning {
		padding: 20px; border: 1px solid var(--color-border); border-radius: 12px;
		background: var(--color-bg-secondary); box-shadow: 0 1px 2px rgb(15 23 42 / 5%);
	}
	.learning-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
	.learning-grid > div { min-height: 92px; padding: 14px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-bg-tertiary); }
	@media (max-width: 1050px) {
		.susanin-steps { grid-template-columns: 1fr 1fr; }
		.susanin-step:last-child { grid-column: 1 / -1; min-height: 0; }
	}
	@media (max-width: 720px) {
		.susanin-status { align-items: stretch; flex-direction: column; }
		.susanin-steps, .learning-grid { grid-template-columns: 1fr; }
		.susanin-step, .susanin-step:last-child { grid-column: auto; min-height: 0; padding: 16px; }
		.susanin-learning { padding: 16px; }
	}
</style>

<!-- Модальное окно просмотра изученных адресов -->
{#if showLearnedModal}
	<Modal
		open={showLearnedModal}
		title="Изученные направления Susanin"
		onclose={() => (showLearnedModal = false)}
	>
		<div class="p-4 space-y-4 max-h-[80vh] overflow-y-auto">
			<div class="flex items-center justify-between">
				<p class="text-xs text-[var(--color-text-muted)]">
					Список хостов, которые направляются через выбранный прокси-выход
				</p>
				<Button
					variant="danger"
					size="sm"
					onclick={async () => {
						try {
							await api.clearAdaptiveRoutingCache();
							notifications.success('Кэш обучения очищен');
							void loadAll(false);
						} catch (e) {
							notifications.error(`Ошибка очистки: ${(e as Error).message}`);
						}
					}}
				>
					<Trash2 class="w-3.5 h-3.5 mr-1" />
					Очистить кэш
				</Button>
			</div>

			<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
				<div class="bg-[var(--color-bg-secondary)] p-3 rounded-lg border border-[var(--color-border)]">
					<div class="font-semibold text-xs text-[var(--color-text-primary)] mb-2 flex items-center justify-between">
						<span>Изученные адреса (ОК)</span>
						<Badge variant="success" size="sm">{(learned?.always || []).length} записей</Badge>
					</div>
					<div class="space-y-1 max-h-48 overflow-y-auto text-xs font-mono text-[var(--color-text-secondary)]">
						{#each learned?.always || [] as entry}
							<div class="p-1 rounded bg-[var(--color-bg-tertiary)] truncate">
								{entry}
							</div>
						{:else}
							<div class="text-[var(--color-text-muted)] italic text-center py-4">
								Список пока пуст
							</div>
						{/each}
					</div>
				</div>

				<div class="bg-[var(--color-bg-secondary)] p-3 rounded-lg border border-[var(--color-border)]">
					<div class="font-semibold text-xs text-[var(--color-text-primary)] mb-2 flex items-center justify-between">
						<span>Прямой доступ (Never)</span>
						<Badge variant="muted" size="sm">{(learned?.never || []).length} записей</Badge>
					</div>
					<div class="space-y-1 max-h-48 overflow-y-auto text-xs font-mono text-[var(--color-text-secondary)]">
						{#each learned?.never || [] as entry}
							<div class="p-1 rounded bg-[var(--color-bg-tertiary)] truncate">
								{entry}
							</div>
						{:else}
							<div class="text-[var(--color-text-muted)] italic text-center py-4">
								Список пока пуст
							</div>
						{/each}
					</div>
				</div>
			</div>

			<div class="flex justify-end pt-2">
				<Button variant="secondary" onclick={() => (showLearnedModal = false)}>
					Закрыть
				</Button>
			</div>
		</div>
	</Modal>
{/if}
