<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import {
		stripFircEmojis,
		FIRC_TUNNEL_NODE_POOL,
		type FircTunnel,
		type FircTunnelState,
		type FircTunnelSource,
		type FircTunnelInbound,
		type FircTunnelPreview,
		type FircTunnelProbeRow,
		type FircTunnelSubUsage,
		type FircGroup,
	} from '$lib/types/firc';
	import { StatStrip, Button, Badge, Toggle, StatusDot, Modal } from '$lib/components/ui';
	import { SingboxTunnelCard } from '$lib/components/singbox';
	import { singboxTunnels, singboxStatus } from '$lib/stores/singbox';
	import { notifications } from '$lib/stores/notifications';
	import { formatByteRate } from '$lib/utils/format';
	import {
		Plus,
		Trash2,
		RefreshCw,
		RotateCw,
		Activity,
		ArrowUp,
		ArrowDown,
		Search,
		Link2,
		Server,
		Shield,
		Network,
		SlidersHorizontal,
		X,
	} from 'lucide-svelte';

	interface Props {
		dashboardMode?: boolean;
		showTrustTunnels?: boolean;
		onOpenSingboxDetail?: (tag: string) => void;
	}

	let {
		dashboardMode = false,
		showTrustTunnels = true,
		onOpenSingboxDetail,
	}: Props = $props();

	interface DisplayNode {
		key: string;
		name: string;
		source: string;
		state: 'active' | 'down' | 'reserve' | 'excluded' | 'missing' | 'skipped';
		skipReason?: string;
		v6?: boolean;
	}

	function formatSubBytes(bytes?: number): string {
		if (bytes === undefined || bytes < 0) return '—';
		if (bytes === 0) return '0 Б';
		const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
		let val = bytes;
		let idx = 0;
		while (val >= 1024 && idx < units.length - 1) {
			val /= 1024;
			idx++;
		}
		return `${val >= 10 || idx === 0 ? val.toFixed(0) : val.toFixed(1)} ${units[idx]}`;
	}

	function formatSubUsage(usage?: FircTunnelSubUsage): string {
		if (!usage) return '';
		const parts: string[] = [];
		if (usage.used !== undefined || usage.total !== undefined) {
			const usedStr = formatSubBytes(usage.used ?? 0);
			const totalStr = usage.total ? formatSubBytes(usage.total) : '∞';
			parts.push(`${usedStr} / ${totalStr}`);
		}
		if (usage.expire && usage.expire > 0) {
			const d = new Date(usage.expire * 1000);
			parts.push(`до ${d.toLocaleDateString('ru-RU')}`);
		}
		return parts.join(' · ');
	}

	const SUB_INTERVALS = [
		{ value: 0, label: 'Вручную' },
		{ value: 3600, label: 'Каждый час' },
		{ value: 21600, label: 'Каждые 6 часов' },
		{ value: 43200, label: 'Каждые 12 часов' },
		{ value: 86400, label: 'Раз в сутки' },
	];

	const FINGERPRINTS: { value: '' | 'chrome' | 'firefox'; label: string }[] = [
		{ value: '', label: 'По умолчанию (из ссылки)' },
		{ value: 'chrome', label: 'Chrome' },
		{ value: 'firefox', label: 'Firefox' },
	];

	let tunnels = $state<FircTunnel[]>([]);
	let baselineJson = $state('[]');
	let liveStates = $state<Record<string, FircTunnelState>>({});
	let previewMap = $state<Record<string, FircTunnelPreview>>({});
	let groups = $state<FircGroup[]>([]);

	let loading = $state(true);
	let saving = $state(false);
	let searchQuery = $state('');
	let selectedTunnelId = $state<string | null>(null);
	let activeSubTab = $state<Record<string, 'nodes' | 'sources' | 'failover' | 'inbounds'>>({});
	let nodeFilterMap = $state<Record<string, string>>({});
	let busyTunnelAction = $state<Record<string, string>>({});
	let probeResults = $state<Record<string, Record<string, FircTunnelProbeRow>>>({});

	// Source modal state
	let sourceModalOpen = $state(false);
	let sourceModalTunnelId = $state<string | null>(null);
	let sourceModalKind = $state<'subscription' | 'link'>('subscription');
	let sourceModalName = $state('');
	let sourceModalUrl = $state('');
	let sourceModalInterval = $state(21600);
	let sourceModalBusy = $state(false);

	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let unsubSbTunnels: (() => void) | undefined;
	let unsubSbStatus: (() => void) | undefined;

	const isDirty = $derived(JSON.stringify(tunnels) !== baselineJson);
	const sbTunnelsList = $derived($singboxTunnels.data ?? []);

	function randomId(): string {
		const bytes = new Uint8Array(4);
		if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
			crypto.getRandomValues(bytes);
		} else {
			for (let i = 0; i < 4; i++) bytes[i] = Math.floor(Math.random() * 256);
		}
		return Array.from(bytes)
			.map((b) => b.toString(16).padStart(2, '0'))
			.join('');
	}

	function nextDeviceName(list: FircTunnel[]): string {
		const used = new Set(list.map((t) => t.device));
		for (let i = 0; i < 64; i++) {
			const cand = `tunvless${i}`;
			if (!used.has(cand)) return cand;
		}
		return `tunvless${list.length}`;
	}

	function normalizeTunnel(t: Partial<FircTunnel>, idx: number): FircTunnel {
		return {
			id: t.id || `tunnel${idx + 1}`,
			device: t.device || `tunvless${idx}`,
			description: stripFircEmojis(t.description ?? ''),
			enable: t.enable ?? true,
			active: t.active ?? 1,
			by: t.by ?? 'connection',
			interval: t.interval ?? 60,
			silence: t.silence ?? 20,
			filter: t.filter ?? '',
			order: [...(t.order ?? [])],
			exclude: [...(t.exclude ?? [])],
			sources: (t.sources ?? []).map((s) => ({
				id: s.id || randomId(),
				kind: s.kind || (s.link ? 'link' : 'subscription'),
				name: stripFircEmojis(s.name ?? ''),
				url: s.url ?? '',
				link: s.link ?? '',
				interval: s.interval ?? 21600,
				headers: s.headers,
			})),
			uplink: {
				kind: t.uplink?.kind ?? 'auto',
				ref: t.uplink?.ref ?? '',
			},
			advanced: {
				ca: t.advanced?.ca ?? '',
				insecure: t.advanced?.insecure ?? false,
				timeout: t.advanced?.timeout ?? 8,
				kernelTcp: t.advanced?.kernelTcp ?? false,
				fingerprint: t.advanced?.fingerprint ?? '',
			},
			inbounds: (t.inbounds ?? []).map((inb) => ({
				port: inb.port ?? 1099,
				listen: inb.listen || '0.0.0.0',
				node: inb.node ?? '',
				user: inb.user ?? '',
				password: inb.password ?? '',
			})),
		};
	}

	function getTunnelNodes(tun: FircTunnel): DisplayNode[] {
		const live = liveStates[tun.id]?.nodes ?? [];
		const prev = previewMap[tun.id]?.nodes ?? [];
		const byKey = new Map<string, DisplayNode>();

		for (const ln of live) {
			byKey.set(ln.key, {
				key: ln.key,
				name: stripFircEmojis(ln.name || ln.key),
				source: stripFircEmojis(ln.source || ''),
				state: ln.state,
				skipReason: ln.skipReason,
				v6: ln.v6,
			});
		}

		for (const pn of prev) {
			const existing = byKey.get(pn.key);
			if (!existing) {
				byKey.set(pn.key, {
					key: pn.key,
					name: stripFircEmojis(pn.name || pn.key),
					source: stripFircEmojis(pn.source || ''),
					state: pn.excluded ? 'excluded' : pn.missing ? 'missing' : 'reserve',
					skipReason: pn.skipReason,
				});
			}
		}

		for (const key of tun.order ?? []) {
			if (!byKey.has(key)) {
				const rawName = key.includes(':') ? key.slice(key.indexOf(':') + 1) : key;
				let decoded = rawName;
				try {
					decoded = decodeURIComponent(rawName);
				} catch {
					// keep raw
				}
				byKey.set(key, {
					key,
					name: stripFircEmojis(decoded),
					source: key.includes(':') ? key.slice(0, key.indexOf(':')) : '',
					state: (tun.exclude ?? []).includes(key) ? 'excluded' : 'reserve',
				});
			}
		}

		const orderIdx = new Map((tun.order ?? []).map((k, i) => [k, i]));
		return Array.from(byKey.values()).sort((a, b) => {
			const ia = orderIdx.get(a.key);
			const ib = orderIdx.get(b.key);
			if (ia !== undefined && ib !== undefined) return ia - ib;
			if (ia !== undefined) return -1;
			if (ib !== undefined) return 1;
			return a.name.localeCompare(b.name);
		});
	}

	async function loadAll(silent = false) {
		if (!silent) loading = true;
		try {
			const [tList, stList, gList] = await Promise.all([
				api.fircGetTunnels().catch(() => null),
				api.fircGetTunnelsState().catch(() => null),
				api.fircGetGroups().catch(() => null),
			]);
			if (tList && !isDirty) {
				const norm = tList.map((t, i) => normalizeTunnel(t, i));
				tunnels = norm;
				baselineJson = JSON.stringify(norm);
			}
			if (stList) {
				const map: Record<string, FircTunnelState> = {};
				for (const s of stList) {
					map[s.id] = s;
				}
				liveStates = map;
			}
			if (gList) {
				groups = gList;
			}
		} catch (e: unknown) {
			if (!silent) {
				notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка загрузки туннелей FIRC');
			}
		} finally {
			if (!silent) loading = false;
		}
	}

	onMount(() => {
		loadAll();
		unsubSbStatus = singboxStatus.subscribe(() => {});
		unsubSbTunnels = singboxTunnels.subscribe(() => {});
		pollTimer = setInterval(async () => {
			try {
				const stList = await api.fircGetTunnelsState();
				if (stList) {
					const map: Record<string, FircTunnelState> = {};
					for (const s of stList) {
						map[s.id] = s;
					}
					liveStates = map;
				}
			} catch {
				// ignore background poll error
			}
		}, 2500);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
		unsubSbStatus?.();
		unsubSbTunnels?.();
	});

	async function saveAll(customList?: FircTunnel[], toastMsg = 'Конфигурация туннелей сохранена') {
		saving = true;
		const payload = customList ?? tunnels;
		try {
			const res = await api.fircSaveTunnels(payload);
			const norm = (res?.tunnels ?? payload).map((t, i) => normalizeTunnel(t, i));
			tunnels = norm;
			baselineJson = JSON.stringify(norm);
			notifications.success(toastMsg);
			await loadAll(true);
		} catch (e: unknown) {
			notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка сохранения туннелей FIRC');
		} finally {
			saving = false;
		}
	}

	function resetChanges() {
		try {
			tunnels = JSON.parse(baselineJson);
		} catch {
			loadAll();
		}
	}

	function createNewTunnel() {
		const idx = tunnels.length;
		const dev = nextDeviceName(tunnels);
		const id = `tunnel${idx + 1}_${randomId().slice(0, 4)}`;
		const newTun = normalizeTunnel(
			{
				id,
				device: dev,
				enable: true,
				description: `Туннель ${dev}`,
				sources: [],
				order: [],
				exclude: [],
				active: 1,
				by: 'connection',
				interval: 60,
				silence: 20,
				uplink: { kind: 'auto', ref: '' },
				advanced: { ca: '', kernelTcp: false, insecure: false, fingerprint: '', timeout: 8 },
				inbounds: [],
			},
			idx,
		);
		tunnels = [...tunnels, newTun];
		selectedTunnelId = id;
		activeSubTab = { ...activeSubTab, [id]: 'sources' };
	}

	async function deleteTunnel(tun: FircTunnel) {
		const usedIn = groups.filter((g) => g.interface === tun.device).map((g) => g.name || g.id);
		if (usedIn.length > 0) {
			notifications.warning(
				`Туннель ${tun.device} используется в группах: ${usedIn.join(', ')}. Сначала смените интерфейс в этих группах.`,
			);
			return;
		}
		if (selectedTunnelId === tun.id) selectedTunnelId = null;
		const next = tunnels.filter((t) => t.id !== tun.id);
		await saveAll(next, `Туннель ${tun.device} удалён`);
	}

	async function toggleTunnelEnable(tun: FircTunnel, enabled: boolean) {
		const next = tunnels.map((t) => (t.id === tun.id ? { ...t, enable: enabled } : t));
		tunnels = next;
		await saveAll(
			next,
			enabled ? `Туннель ${tun.device} включён` : `Туннель ${tun.device} отключён`,
		);
	}

	async function handleRefreshSubscriptions(tun: FircTunnel) {
		busyTunnelAction = { ...busyTunnelAction, [tun.id]: 'refresh' };
		try {
			if (isDirty) {
				await saveAll(tunnels, 'Изменения сохранены перед обновлением');
			}
			await api.fircRefreshTunnel(tun.id);
			const prev = await api.fircPreviewTunnel(tun).catch(() => null);
			if (prev) {
				previewMap = { ...previewMap, [tun.id]: prev };
			}
			await loadAll(true);
			notifications.success(`Источники туннеля ${tun.device} обновлены`);
		} catch (e: unknown) {
			notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка обновления подписки');
		} finally {
			const copy = { ...busyTunnelAction };
			delete copy[tun.id];
			busyTunnelAction = copy;
		}
	}

	async function handleRestartTunnel(tun: FircTunnel) {
		busyTunnelAction = { ...busyTunnelAction, [tun.id]: 'restart' };
		try {
			await api.fircRestartTunnel(tun.id);
			notifications.success(`Туннель ${tun.device} перезапущен`);
			await loadAll(true);
		} catch (e: unknown) {
			notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка перезапуска туннеля');
		} finally {
			const copy = { ...busyTunnelAction };
			delete copy[tun.id];
			busyTunnelAction = copy;
		}
	}

	async function handleProbeTunnel(tun: FircTunnel) {
		busyTunnelAction = { ...busyTunnelAction, [tun.id]: 'probe' };
		try {
			const rows = await api.fircProbeTunnel({ id: tun.id, tunnel: tun });
			const map: Record<string, FircTunnelProbeRow> = { ...(probeResults[tun.id] ?? {}) };
			for (const row of rows ?? []) {
				map[row.key] = row;
			}
			probeResults = { ...probeResults, [tun.id]: map };
			notifications.success(`Проверка задержки узлов ${tun.device} завершена`);
		} catch (e: unknown) {
			notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка проверки узлов');
		} finally {
			const copy = { ...busyTunnelAction };
			delete copy[tun.id];
			busyTunnelAction = copy;
		}
	}

	function sortNodesByLatency(tun: FircTunnel) {
		const nodes = getTunnelNodes(tun);
		const probes = probeResults[tun.id] ?? {};

		const score = (n: DisplayNode): number => {
			const p = probes[n.key];
			if (p && p.ok && (p.firstByteMs || p.handshakeMs)) {
				return p.firstByteMs ?? p.handshakeMs ?? 999999;
			}
			if (n.state === 'active') return 1;
			if (n.state === 'reserve') return 50000;
			return 999999;
		};

		const sortedKeys = [...nodes].sort((a, b) => score(a) - score(b)).map((n) => n.key);
		tunnels = tunnels.map((t) => (t.id === tun.id ? { ...t, order: sortedKeys } : t));
	}

	function moveNode(tun: FircTunnel, index: number, delta: -1 | 1) {
		const nodes = getTunnelNodes(tun);
		const target = index + delta;
		if (target < 0 || target >= nodes.length) return;
		const keys = nodes.map((n) => n.key);
		const [item] = keys.splice(index, 1);
		keys.splice(target, 0, item);
		tunnels = tunnels.map((t) => (t.id === tun.id ? { ...t, order: keys } : t));
	}

	function toggleNodeIncluded(tun: FircTunnel, nodeKey: string, included: boolean) {
		const excl = new Set(tun.exclude ?? []);
		if (included) excl.delete(nodeKey);
		else excl.add(nodeKey);
		tunnels = tunnels.map((t) =>
			t.id === tun.id ? { ...t, exclude: Array.from(excl) } : t,
		);
	}

	function openAddSourceModal(tun: FircTunnel, kind: 'subscription' | 'link') {
		sourceModalTunnelId = tun.id;
		sourceModalKind = kind;
		sourceModalName = '';
		sourceModalUrl = '';
		sourceModalInterval = 21600;
		sourceModalOpen = true;
	}

	async function submitAddSource() {
		if (!sourceModalTunnelId || !sourceModalUrl.trim()) return;
		const tun = tunnels.find((t) => t.id === sourceModalTunnelId);
		if (!tun) return;

		sourceModalBusy = true;
		try {
			const defaultName = stripFircEmojis(
				sourceModalName.trim() ||
					(sourceModalKind === 'subscription' ? 'Подписка' : 'Прямая ссылка'),
			);
			let addedSources: FircTunnelSource[] = [];
			try {
				const normRes = await api.fircNormalizeSource({
					input: sourceModalUrl.trim(),
					name: defaultName,
					interval: sourceModalInterval,
				});
				if (normRes?.sources?.length) {
					addedSources = normRes.sources.map((s) => ({
						...s,
						id: s.id || randomId(),
						name: stripFircEmojis(s.name || defaultName),
						interval:
							s.kind === 'subscription' ? (s.interval || sourceModalInterval) : undefined,
					}));
				}
			} catch (normErr: unknown) {
				const trimmedLower = sourceModalUrl.trim().toLowerCase();
				if (
					trimmedLower.startsWith('happ://crypt') ||
					trimmedLower.startsWith('fp://') ||
					trimmedLower.startsWith('finiteproto://') ||
					trimmedLower.includes('finiteproto')
				) {
					throw normErr;
				}
			}

			if (addedSources.length === 0) {
				const fallbackSource: FircTunnelSource =
					sourceModalKind === 'subscription'
						? {
								id: randomId(),
								kind: 'subscription',
								name: defaultName,
								url: sourceModalUrl.trim(),
								interval: sourceModalInterval,
							}
						: {
								id: randomId(),
								kind: 'link',
								name: defaultName,
								link: sourceModalUrl.trim(),
							};
				addedSources = [fallbackSource];
			}

			const existingIds = new Set(tun.sources.map((s) => s.id));
			const deduped = addedSources.map((s) =>
				existingIds.has(s.id) ? { ...s, id: randomId() } : s,
			);

			const candidate: FircTunnel = {
				...tun,
				sources: [...tun.sources, ...deduped],
			};

			const preview = await api.fircPreviewTunnel(candidate).catch(() => null);
			if (preview) {
				previewMap = { ...previewMap, [tun.id]: preview };
				if ((candidate.order?.length ?? 0) === 0 && preview.nodes?.length) {
					candidate.order = preview.nodes.map((n) => n.key);
				}
			}

			tunnels = tunnels.map((t) => (t.id === tun.id ? candidate : t));
			sourceModalOpen = false;
			activeSubTab = { ...activeSubTab, [tun.id]: 'nodes' };
			notifications.success(
				deduped.length > 1
					? `Добавлено источников/узлов: ${deduped.length}. Нажмите «Сохранить изменения» для применения.`
					: 'Источник добавлен. Нажмите «Сохранить изменения» для применения.',
			);
		} catch (e: unknown) {
			notifications.error((e instanceof Error ? e.message : undefined) ?? 'Не удалось разобрать источник');
		} finally {
			sourceModalBusy = false;
		}
	}

	function removeSource(tun: FircTunnel, sourceId: string) {
		const nextSources = tun.sources.filter((s) => s.id !== sourceId);
		const prefix = `${sourceId}:`;
		const nextOrder = (tun.order ?? []).filter((k) => !k.startsWith(prefix));
		const nextExclude = (tun.exclude ?? []).filter((k) => !k.startsWith(prefix));
		const updated: FircTunnel = {
			...tun,
			sources: nextSources,
			order: nextOrder,
			exclude: nextExclude,
		};
		tunnels = tunnels.map((t) => (t.id === tun.id ? updated : t));
	}

	function addInbound(tun: FircTunnel) {
		const usedPorts = new Set(tunnels.flatMap((t) => (t.inbounds ?? []).map((i) => i.port)));
		let port = 1099;
		while (usedPorts.has(port)) port++;
		const inb: FircTunnelInbound = {
			port,
			listen: '0.0.0.0',
			node: FIRC_TUNNEL_NODE_POOL,
			user: '',
			password: '',
		};
		tunnels = tunnels.map((t) =>
			t.id === tun.id ? { ...t, inbounds: [...(t.inbounds ?? []), inb] } : t,
		);
	}

	function removeInbound(tun: FircTunnel, idx: number) {
		tunnels = tunnels.map((t) =>
			t.id === tun.id
				? { ...t, inbounds: (t.inbounds ?? []).filter((_, i) => i !== idx) }
				: t,
		);
	}

	const filteredTunnels = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		if (!q) return tunnels;
		return tunnels.filter((t) => {
			if (t.device.toLowerCase().includes(q)) return true;
			if ((t.description ?? '').toLowerCase().includes(q)) return true;
			const nodes = getTunnelNodes(t);
			return nodes.some(
				(n) => n.name.toLowerCase().includes(q) || n.key.toLowerCase().includes(q),
			);
		});
	});

	const filteredTrustTunnels = $derived.by(() => {
		if (!showTrustTunnels) return [];
		const q = searchQuery.trim().toLowerCase();
		if (!q) return sbTunnelsList;
		return sbTunnelsList.filter(
			(t) =>
				t.tag.toLowerCase().includes(q) ||
				t.protocol.toLowerCase().includes(q) ||
				(t.server ?? '').toLowerCase().includes(q),
		);
	});

	const summaryStats = $derived.by(() => {
		const total = tunnels.length;
		const upCount = tunnels.filter((t) => liveStates[t.id]?.status === 'up').length;
		let totalNodes = 0;
		let activeNodes = 0;
		let rxBps = 0;
		let txBps = 0;
		let inboundPorts: string[] = [];

		for (const t of tunnels) {
			const nodes = getTunnelNodes(t);
			totalNodes += nodes.length;
			const st = liveStates[t.id];
			if (st) {
				rxBps += st.rxBps ?? 0;
				txBps += st.txBps ?? 0;
				activeNodes += (st.nodes ?? []).filter((n) => n.state === 'active').length;
			}
			for (const inb of t.inbounds ?? []) {
				inboundPorts.push(`:${inb.port}`);
			}
		}

		return [
			{
				value: `${upCount}/${total}`,
				label: 'Туннели FIRC',
				sub: total > 0 ? tunnels.map((t) => t.device).join(' · ') : 'Нет туннелей',
			},
			{
				value: `${activeNodes}/${totalNodes}`,
				label: 'Узлы VLESS',
				sub: activeNodes > 0 ? 'Отказоустойчивый пул' : 'Ожидание трафика',
			},
			{
				value: `↓ ${formatByteRate(rxBps)}`,
				label: 'Текущая скорость',
				sub: `↑ ${formatByteRate(txBps)}`,
			},
			{
				value: inboundPorts.length > 0 ? inboundPorts.join(', ') : '—',
				label: 'Прокси SOCKS5/HTTP',
				sub: inboundPorts.length > 0 ? 'Входящие порты активны' : 'Порты не заданы',
			},
		];
	});

	function getCardBorderState(tun: FircTunnel): 'ok' | 'slow' | 'fail' | 'stopped' {
		if (!tun.enable) return 'stopped';
		const st = liveStates[tun.id];
		if (!st) return 'slow';
		if (st.status === 'up') return 'ok';
		if (st.status === 'starting' || st.status === 'waiting' || st.status === 'backoff')
			return 'slow';
		if (st.status === 'no_node' || st.status === 'bad_config' || st.status === 'uplink_down')
			return 'fail';
		return 'stopped';
	}

	function getTunnelStatusDot(tun: FircTunnel): 'success' | 'warning' | 'error' | 'muted' {
		if (!tun.enable) return 'muted';
		const st = liveStates[tun.id];
		if (!st) return 'muted';
		if (st.status === 'up') return 'success';
		if (st.status === 'starting' || st.status === 'waiting' || st.status === 'backoff')
			return 'warning';
		if (st.status === 'no_node' || st.status === 'bad_config' || st.status === 'uplink_down')
			return 'error';
		return 'muted';
	}

	function getTunnelStateLabel(tun: FircTunnel): string {
		if (!tun.enable) return 'Отключён';
		const st = liveStates[tun.id];
		if (!st) return 'Ожидание';
		if (st.status === 'up') return 'Активен';
		if (st.status === 'starting') return 'Запуск';
		if (st.status === 'waiting') return 'Ожидание';
		if (st.status === 'backoff') return 'Переподключение';
		if (st.status === 'no_node') return 'Нет доступных узлов';
		if (st.status === 'uplink_down') return 'Нет интернета (WAN)';
		if (st.status === 'bad_config') return 'Ошибка конфигурации';
		return 'Остановлен';
	}

	function getActiveNodeLabel(tun: FircTunnel): string | null {
		const st = liveStates[tun.id];
		if (!st) return null;
		if (st.active && st.active.length > 0) {
			const first = stripFircEmojis(st.active[0]);
			return st.active.length > 1 ? `${first} (+${st.active.length - 1})` : first;
		}
		const act = (st.nodes ?? []).filter((n) => n.state === 'active');
		if (act.length === 0) return null;
		const name = stripFircEmojis(act[0].name || act[0].key);
		return act.length > 1 ? `${name} (+${act.length - 1})` : name;
	}

	function getBestProbeLatency(tun: FircTunnel): number | null {
		const map = probeResults[tun.id];
		if (!map) return null;
		let best: number | null = null;
		for (const r of Object.values(map)) {
			if (r.ok) {
				const ms = r.firstByteMs ?? r.handshakeMs ?? null;
				if (ms !== null && (best === null || ms < best)) {
					best = ms;
				}
			}
		}
		return best;
	}

	function getUsedInGroups(tun: FircTunnel): string[] {
		return groups.filter((g) => g.interface === tun.device).map((g) => g.name || g.id);
	}

	function toggleTunnelDetail(id: string) {
		selectedTunnelId = selectedTunnelId === id ? null : id;
	}

	const selectedTunnel = $derived(
		selectedTunnelId ? tunnels.find((t) => t.id === selectedTunnelId) ?? null : null,
	);
</script>

<div class="firc-tunnels-wrap">
	{#if !dashboardMode}
		<StatStrip stats={summaryStats} />

		<!-- Toolbar -->
		<div class="tunnels-toolbar">
			<div class="toolbar-left">
				<span class="tunnel-count">
					{tunnels.length} туннеля FIRC
					{#if showTrustTunnels && sbTunnelsList.length > 0}
						· {sbTunnelsList.length} TrustTunnel
					{/if}
				</span>
				<div class="search-box">
					<Search size={14} />
					<input
						type="text"
						placeholder="Поиск по туннелям, узлам или описаниям…"
						bind:value={searchQuery}
					/>
				</div>
			</div>

			<div class="toolbar-right">
				{#if isDirty}
					<Button variant="ghost" size="sm" onclick={resetChanges} disabled={saving}>
						Отменить
					</Button>
					<Button variant="primary" size="sm" loading={saving} onclick={() => saveAll()}>
						Сохранить изменения
					</Button>
				{/if}
				<Button variant="primary" size="sm" onclick={createNewTunnel}>
					{#snippet iconBefore()}<Plus size={14} />{/snippet}
					Добавить туннель
				</Button>
			</div>
		</div>
	{:else if isDirty}
		<div class="dashboard-save-bar">
			<span>Есть несохранённые изменения в туннелях FIRC</span>
			<div class="toolbar-right">
				<Button variant="ghost" size="sm" onclick={resetChanges} disabled={saving}>
					Отменить
				</Button>
				<Button variant="primary" size="sm" loading={saving} onclick={() => saveAll()}>
					Сохранить изменения
				</Button>
			</div>
		</div>
	{/if}

	{#if loading}
		<div class="empty-card">Загрузка туннелей FIRC…</div>
	{:else if filteredTunnels.length === 0}
		<div class="empty-card">
			<div class="empty-title">Туннели VLESS не найдены</div>
			<div class="empty-sub">
				Добавьте туннель с подпиской или прямой ссылкой vless:// для маршрутизации групп FIRC.
			</div>
			<Button variant="primary" size="sm" onclick={createNewTunnel}>
				{#snippet iconBefore()}<Plus size={14} />{/snippet}
				Создать первый туннель
			</Button>
		</div>
	{:else}
		<!-- Classic awg-manager Card Grid -->
		<div class="tunnel-grid tunnel-grid--compact">
			{#each filteredTunnels as tun (tun.id)}
				{@const live = liveStates[tun.id]}
				{@const nodes = getTunnelNodes(tun)}
				{@const borderState = getCardBorderState(tun)}
				{@const activeNodeName = getActiveNodeLabel(tun)}
				{@const usedGroups = getUsedInGroups(tun)}
				{@const busy = busyTunnelAction[tun.id]}
				{@const bestMs = getBestProbeLatency(tun)}
				{@const isSelected = selectedTunnelId === tun.id}
				{@const hasSubscription = tun.sources.some((s) => s.kind === 'subscription')}
				{@const availableCount = Math.max(0, nodes.length - (tun.exclude?.length ?? 0))}

				<div
					class="card view-compact firc-card"
					class:ok={borderState === 'ok'}
					class:slow={borderState === 'slow'}
					class:fail={borderState === 'fail'}
					class:stopped={borderState === 'stopped'}
					class:selected={isSelected}
				>
					<div class="tunnel-card-intro">
						<div class="title-row">
							<div class="title-left">
								<StatusDot
									variant={getTunnelStatusDot(tun)}
									size="sm"
									pulse={live?.status === 'up'}
								/>
								<button
									type="button"
									class="card-title-btn"
									onclick={() => toggleTunnelDetail(tun.id)}
									title="Открыть узлы и настройки туннеля"
								>
									{tun.description || tun.id}
								</button>
								<Badge variant="accent" size="sm">FIRC</Badge>
								<Badge variant="default" size="sm">
									{hasSubscription ? 'подписка' : 'ссылка'}
								</Badge>
							</div>

							<div class="title-right">
								<button
									type="button"
									class="ping-pill"
									class:ok={bestMs !== null && bestMs < 250}
									class:slow={bestMs !== null && bestMs >= 250}
									disabled={Boolean(busy) || nodes.length === 0}
									onclick={() => handleProbeTunnel(tun)}
									title="Проверить задержку всех узлов (Probe)"
								>
									{#if busy === 'probe'}
										<span>…</span>
									{:else if bestMs !== null}
										<span>{bestMs} ms</span>
									{:else}
										<Activity size={11} />
										<span>Ping</span>
									{/if}
								</button>

								<Toggle
									checked={tun.enable}
									size="sm"
									onchange={(v) => toggleTunnelEnable(tun, v)}
								/>
							</div>
						</div>

						<div class="iface">
							<span>{tun.device}</span>
							{#if (tun.inbounds?.length ?? 0) > 0}
								<span class="meta-dot">·</span>
								<span>SOCKS5 :{tun.inbounds.map((i) => i.port).join(', :')}</span>
							{/if}
							<span class="meta-dot">·</span>
							<span>{getTunnelStateLabel(tun)}</span>
						</div>

						<div class="badges">
							<span class="badge b-vless">VLESS</span>
							<span class="badge b-reality">Reality</span>
							<span class="badge b-mode">
								{tun.by === 'connection'
									? 'По соед.'
									: tun.by === 'site'
										? 'По сайту'
										: 'Сайт+Клиент'} · ×{tun.active || 1}
							</span>
							{#if usedGroups.length > 0}
								<span class="badge b-groups" title={usedGroups.join(', ')}>
									Групп: {usedGroups.length}
								</span>
							{/if}
						</div>
					</div>

					<div class="divider divider-dashed"></div>

					<div class="card-rows">
						<div class="row">
							<span class="item-label">Узлы</span>
							<span class="value">
								{availableCount}/{nodes.length} в пуле · источн.: {tun.sources.length}
							</span>
						</div>

						<div class="row">
							<span class="item-label">Активен</span>
							<span class="value active-node-val" title={activeNodeName ?? ''}>
								{activeNodeName || (tun.enable ? 'Ожидание трафика' : 'Отключён')}
							</span>
						</div>

						<div class="row">
							<span class="item-label">Маршруты</span>
							<span class="value groups-val" title={usedGroups.join(', ')}>
								{usedGroups.length > 0 ? usedGroups.join(', ') : 'Не назначен'}
							</span>
						</div>
					</div>

					<div class="actions actions--bar">
						<button
							type="button"
							class="card-act-btn primary-act"
							class:active={isSelected}
							onclick={() => toggleTunnelDetail(tun.id)}
						>
							<SlidersHorizontal size={12} />
							<span>{isSelected ? 'Скрыть' : 'Узлы и настройки'}</span>
						</button>

						<button
							type="button"
							class="card-act-btn"
							disabled={Boolean(busy)}
							onclick={() => handleRefreshSubscriptions(tun)}
							title="Обновить подписки"
						>
							<RefreshCw size={12} class={busy === 'refresh' ? 'spin' : ''} />
							<span>Обновить</span>
						</button>

						<button
							type="button"
							class="card-act-btn"
							disabled={Boolean(busy)}
							onclick={() => handleRestartTunnel(tun)}
							title="Перезапустить туннель"
						>
							<RotateCw size={12} class={busy === 'restart' ? 'spin' : ''} />
							<span>Рестарт</span>
						</button>

						<button
							type="button"
							class="card-act-btn danger-act"
							onclick={() => deleteTunnel(tun)}
							title="Удалить туннель"
						>
							<Trash2 size={12} />
						</button>
					</div>

					<div class="chart-section">
						<div class="chart-body">
							<div class="chart-head">
								<span>Пул узлов ({nodes.length})</span>
								<span class="stats">
									{nodes.filter((n) => n.state === 'active').length} акт. · {availableCount} рез.
								</span>
							</div>
							<div class="node-health-bar">
								{#if nodes.length === 0}
									<div class="nh-seg nh-empty" style="flex: 1"></div>
								{:else}
									{#each nodes.slice(0, 32) as n (n.key)}
										{@const isExcl = (tun.exclude ?? []).includes(n.key)}
										<div
											class="nh-seg"
											class:nh-active={!isExcl && n.state === 'active'}
											class:nh-reserve={!isExcl && n.state !== 'active' && n.state !== 'down'}
											class:nh-down={!isExcl && n.state === 'down'}
											class:nh-excl={isExcl}
											title={`${n.name}: ${isExcl ? 'Исключён' : n.state}`}
										></div>
									{/each}
								{/if}
							</div>

							<div class="chart-head traffic-head">
								<span>Скорость</span>
								<span class="stats">
									↓ {formatByteRate(live?.rxBps ?? 0)} · ↑ {formatByteRate(live?.txBps ?? 0)}
								</span>
							</div>
						</div>
					</div>
				</div>
			{/each}
		</div>

		<!-- Expanded 4-Tab Detail Panel for Selected FIRC Tunnel -->
		{#if selectedTunnel}
			{@const tun = selectedTunnel}
			{@const nodes = getTunnelNodes(tun)}
			{@const subTab = activeSubTab[tun.id] ?? 'nodes'}
			{@const busy = busyTunnelAction[tun.id]}

			<div class="tunnel-detail-panel">
				<div class="detail-panel-header">
					<div class="detail-header-left">
						<StatusDot variant={getTunnelStatusDot(tun)} size="md" />
						<Badge variant="accent" size="sm" mono>{tun.device}</Badge>
						<input
							class="desc-input"
							type="text"
							placeholder="Название или описание туннеля…"
							value={tun.description || tun.id}
							oninput={(e) => {
								const v = (e.currentTarget as HTMLInputElement).value;
								tunnels = tunnels.map((t) =>
									t.id === tun.id ? { ...t, description: v } : t,
								);
							}}
						/>
					</div>

					<div class="detail-header-right">
						{#if isDirty}
							<Button variant="ghost" size="sm" onclick={resetChanges} disabled={saving}>
								Отменить
							</Button>
							<Button variant="primary" size="sm" loading={saving} onclick={() => saveAll()}>
								Сохранить изменения
							</Button>
						{/if}
						<button
							type="button"
							class="route-action-btn"
							title="Закрыть панель настроек"
							onclick={() => (selectedTunnelId = null)}
						>
							<X size={15} />
						</button>
					</div>
				</div>

				<div class="tunnel-body">
					<div class="tunnel-subtabs">
						<button
							type="button"
							class="subtab-btn"
							class:active={subTab === 'nodes'}
							onclick={() => (activeSubTab = { ...activeSubTab, [tun.id]: 'nodes' })}
						>
							<Server size={13} />
							<span>Узлы ({nodes.length})</span>
						</button>
						<button
							type="button"
							class="subtab-btn"
							class:active={subTab === 'sources'}
							onclick={() => (activeSubTab = { ...activeSubTab, [tun.id]: 'sources' })}
						>
							<Link2 size={13} />
							<span>Подписки и ссылки ({tun.sources.length})</span>
						</button>
						<button
							type="button"
							class="subtab-btn"
							class:active={subTab === 'failover'}
							onclick={() => (activeSubTab = { ...activeSubTab, [tun.id]: 'failover' })}
						>
							<Shield size={13} />
							<span>Отказоустойчивость и сеть</span>
						</button>
						<button
							type="button"
							class="subtab-btn"
							class:active={subTab === 'inbounds'}
							onclick={() => (activeSubTab = { ...activeSubTab, [tun.id]: 'inbounds' })}
						>
							<Network size={13} />
							<span>Входящие порты ({tun.inbounds?.length ?? 0})</span>
						</button>
					</div>

					<!-- TAB 1: NODES -->
					{#if subTab === 'nodes'}
						{@const nodeQuery = (nodeFilterMap[tun.id] ?? '').trim().toLowerCase()}
						{@const tunProbes = probeResults[tun.id] ?? {}}

						<div class="subtab-panel">
							<div class="nodes-toolbar">
								<div class="search-box compact">
									<Search size={13} />
									<input
										type="text"
										placeholder="Фильтр узлов…"
										value={nodeFilterMap[tun.id] ?? ''}
										oninput={(e) =>
											(nodeFilterMap = {
												...nodeFilterMap,
												[tun.id]: (e.currentTarget as HTMLInputElement).value,
											})}
									/>
								</div>
								<div class="nodes-toolbar-actions">
									<Button
										variant="secondary"
										size="sm"
										loading={busy === 'probe'}
										onclick={() => handleProbeTunnel(tun)}
									>
										Проверить задержку
									</Button>
									<Button
										variant="secondary"
										size="sm"
										onclick={() => sortNodesByLatency(tun)}
									>
										Сортировать по пингу
									</Button>
									<Button
										variant="ghost"
										size="sm"
										onclick={() =>
											(tunnels = tunnels.map((t) =>
												t.id === tun.id ? { ...t, exclude: [] } : t,
											))}
									>
										Включить все
									</Button>
								</div>
							</div>

							{#if nodes.length === 0}
								<div class="sub-empty">
									Нет узлов. Перейдите во вкладку «Подписки и ссылки», чтобы добавить подписку или ссылку vless://.
								</div>
							{:else}
								<div class="nodes-table-wrap">
									<table class="nodes-table">
										<thead>
											<tr>
												<th style="width: 68px;">Приор.</th>
												<th>Узел</th>
												<th>Источник</th>
												<th>Статус</th>
												<th>Задержка</th>
												<th style="width: 70px; text-align: right;">В пуле</th>
											</tr>
										</thead>
										<tbody>
											{#each nodes as node, nIdx (node.key)}
												{#if !nodeQuery || node.name.toLowerCase().includes(nodeQuery) || node.key.toLowerCase().includes(nodeQuery)}
													{@const pr = tunProbes[node.key]}
													{@const isIncluded = !(tun.exclude ?? []).includes(node.key)}
													<tr class:excluded-row={!isIncluded}>
														<td>
															<div class="prio-cell">
																<span class="prio-num">{String(nIdx + 1).padStart(2, '0')}</span>
																<div class="prio-btns">
																	<button
																		type="button"
																		class="mini-icon-btn"
																		disabled={nIdx === 0}
																		onclick={() => moveNode(tun, nIdx, -1)}
																	>
																		<ArrowUp size={11} />
																	</button>
																	<button
																		type="button"
																		class="mini-icon-btn"
																		disabled={nIdx === nodes.length - 1}
																		onclick={() => moveNode(tun, nIdx, 1)}
																	>
																		<ArrowDown size={11} />
																	</button>
																</div>
															</div>
														</td>
														<td class="node-name-cell">
															{node.name}
															{#if node.v6}
																<Badge variant="info" size="xs" mono title="Узел поддерживает сквозной IPv6 в туннеле">IPv6</Badge>
															{/if}
														</td>
														<td class="mono-cell">{node.source || '—'}</td>
														<td>
															{#if !isIncluded}
																<Badge variant="muted" size="xs">Исключён</Badge>
															{:else if node.state === 'active'}
																<Badge variant="success" size="xs">Активен</Badge>
															{:else if node.state === 'reserve'}
																<Badge variant="info" size="xs">Резерв</Badge>
															{:else if node.state === 'down'}
																<Badge variant="error" size="xs" title={node.skipReason}>
																	Недоступен
																</Badge>
															{:else}
																<Badge variant="muted" size="xs">Ожидание</Badge>
															{/if}
														</td>
														<td class="mono-cell">
															{#if pr}
																{#if !pr.ok}
																	<span class="probe-err" title={pr.why}>Таймаут</span>
																{:else}
																	<span class="probe-ok">
																		{pr.firstByteMs ?? pr.handshakeMs ?? 0} мс
																	</span>
																{/if}
															{:else}
																<span class="muted-dash">—</span>
															{/if}
														</td>
														<td style="text-align: right;">
															<Toggle
																size="sm"
																checked={isIncluded}
																onchange={(v) => toggleNodeIncluded(tun, node.key, v)}
															/>
														</td>
													</tr>
												{/if}
											{/each}
										</tbody>
									</table>
								</div>
							{/if}
						</div>
					{/if}

					<!-- TAB 2: SOURCES -->
					{#if subTab === 'sources'}
						<div class="subtab-panel">
							<div class="sources-header">
								<span class="hint">
									Источники загружают серверы VLESS из URL подписок или одиночных ссылок vless://.
								</span>
								<div class="sources-actions">
									<Button
										variant="secondary"
										size="sm"
										onclick={() => openAddSourceModal(tun, 'subscription')}
									>
										{#snippet iconBefore()}<Plus size={13} />{/snippet}
										Добавить подписку
									</Button>
									<Button
										variant="secondary"
										size="sm"
										onclick={() => openAddSourceModal(tun, 'link')}
									>
										{#snippet iconBefore()}<Plus size={13} />{/snippet}
										Добавить ссылку vless://
									</Button>
								</div>
							</div>

							{#if tun.sources.length === 0}
								<div class="sub-empty">Источники ещё не добавлены.</div>
							{:else}
								<div class="sources-list">
									{#each tun.sources as src (src.id)}
										<div class="source-row">
											<div class="source-main">
												<div class="source-title-line">
													<Badge
														variant={src.kind === 'subscription' ? 'accent' : 'default'}
														size="xs"
													>
														{src.kind === 'subscription' ? 'Подписка' : 'Ссылка'}
													</Badge>
													<input
														class="inp compact-inp"
														type="text"
														value={src.name ?? ''}
														placeholder="Название источника"
														oninput={(e) => {
															const v = (e.currentTarget as HTMLInputElement).value;
															tunnels = tunnels.map((t) =>
																t.id === tun.id
																	? {
																			...t,
																			sources: t.sources.map((s) =>
																				s.id === src.id ? { ...s, name: v } : s,
																			),
																		}
																	: t,
															);
														}}
													/>
													{#if src.kind === 'subscription'}
														{@const subLive = liveStates[tun.id]?.subscriptions?.find((s) => s.name === src.name) ?? (liveStates[tun.id]?.subscriptions?.length === 1 ? liveStates[tun.id]?.subscriptions[0] : undefined)}
														{@const usageText = formatSubUsage(subLive?.usage ?? undefined)}
														{#if usageText}
															<Badge variant="default" size="xs" mono title="Использование трафика и срок подписки (Subscription-Userinfo)">
																{usageText}
															</Badge>
														{/if}
													{/if}
												</div>
												{#if src.kind === 'subscription'}
													<input
														class="inp mono compact-inp"
														type="text"
														value={src.url ?? ''}
														placeholder="https://..."
														oninput={(e) => {
															const v = (e.currentTarget as HTMLInputElement).value;
															tunnels = tunnels.map((t) =>
																t.id === tun.id
																	? {
																			...t,
																			sources: t.sources.map((s) =>
																				s.id === src.id ? { ...s, url: v } : s,
																			),
																		}
																	: t,
															);
														}}
													/>
												{:else}
													<input
														class="inp mono compact-inp"
														type="text"
														value={src.link ?? src.url ?? ''}
														placeholder="vless://..."
														oninput={(e) => {
															const v = (e.currentTarget as HTMLInputElement).value;
															tunnels = tunnels.map((t) =>
																t.id === tun.id
																	? {
																			...t,
																			sources: t.sources.map((s) =>
																				s.id === src.id ? { ...s, link: v } : s,
																			),
																		}
																	: t,
															);
														}}
													/>
												{/if}
											</div>

											<div class="source-side">
												{#if src.kind === 'subscription'}
													<select
														class="inp compact-select"
														value={src.interval ?? 21600}
														onchange={(e) => {
															const v = Number((e.currentTarget as HTMLSelectElement).value);
															tunnels = tunnels.map((t) =>
																t.id === tun.id
																	? {
																			...t,
																			sources: t.sources.map((s) =>
																				s.id === src.id ? { ...s, interval: v } : s,
																			),
																		}
																	: t,
															);
														}}
													>
														{#each SUB_INTERVALS as opt}
															<option value={opt.value}>{opt.label}</option>
														{/each}
													</select>
												{/if}
												<button
													type="button"
													class="route-action-btn danger"
													title="Удалить источник"
													onclick={() => removeSource(tun, src.id)}
												>
													<Trash2 size={14} />
												</button>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/if}

					<!-- TAB 3: FAILOVER & ADVANCED -->
					{#if subTab === 'failover'}
						<div class="subtab-panel">
							<div class="settings-grid-2">
								<div class="sub-box">
									<div class="sub-box-title">Отказоустойчивость и балансировка</div>

									<div class="field-grid-2">
										<div class="field">
											<label class="lbl" for={`fo-act-${tun.id}`}>Активных узлов одновременно</label>
											<input
												id={`fo-act-${tun.id}`}
												class="inp mono"
												type="number"
												min="1"
												max="8"
												value={tun.active ?? 1}
												oninput={(e) => {
													const v = Number((e.currentTarget as HTMLInputElement).value);
													tunnels = tunnels.map((t) =>
														t.id === tun.id ? { ...t, active: v } : t,
													);
												}}
											/>
										</div>

										<div class="field">
											<label class="lbl" for={`fo-mode-${tun.id}`}>Режим распределения</label>
											<select
												id={`fo-mode-${tun.id}`}
												class="inp"
												value={tun.by ?? 'connection'}
												onchange={(e) => {
													const v = (e.currentTarget as HTMLSelectElement).value as FircTunnel['by'];
													tunnels = tunnels.map((t) =>
														t.id === tun.id ? { ...t, by: v } : t,
													);
												}}
											>
												<option value="connection">По соединениям</option>
												<option value="site">По домену (сайту)</option>
												<option value="site-client">По сайту и клиенту</option>
											</select>
										</div>
									</div>

									<div class="field-grid-2">
										<div class="field">
											<label class="lbl" for={`fo-int-${tun.id}`}>Интервал проверки (с)</label>
											<input
												id={`fo-int-${tun.id}`}
												class="inp mono"
												type="number"
												min="5"
												max="300"
												value={tun.interval ?? 60}
												oninput={(e) => {
													const v = Number((e.currentTarget as HTMLInputElement).value);
													tunnels = tunnels.map((t) =>
														t.id === tun.id ? { ...t, interval: v } : t,
													);
												}}
											/>
										</div>

										<div class="field">
											<label class="lbl" for={`fo-sil-${tun.id}`}>Таймаут тишины (с)</label>
											<input
												id={`fo-sil-${tun.id}`}
												class="inp mono"
												type="number"
												min="5"
												max="300"
												value={tun.silence ?? 20}
												oninput={(e) => {
													const v = Number((e.currentTarget as HTMLInputElement).value);
													tunnels = tunnels.map((t) =>
														t.id === tun.id ? { ...t, silence: v } : t,
													);
												}}
											/>
										</div>
									</div>
								</div>

								<div class="sub-box">
									<div class="sub-box-title">Сетевой стек и TLS</div>

									<div class="field-grid-2">
										<div class="field">
											<label class="lbl" for={`adv-fp-${tun.id}`}>Отпечаток uTLS (Fingerprint)</label>
											<select
												id={`adv-fp-${tun.id}`}
												class="inp"
												value={tun.advanced?.fingerprint ?? ''}
												onchange={(e) => {
													const v = (e.currentTarget as HTMLSelectElement).value as '' | 'chrome' | 'firefox';
													tunnels = tunnels.map((t) =>
														t.id === tun.id
															? { ...t, advanced: { ...t.advanced, fingerprint: v } }
															: t,
													);
												}}
											>
												{#each FINGERPRINTS as fp}
													<option value={fp.value}>{fp.label}</option>
												{/each}
											</select>
										</div>

										<div class="field">
											<label class="lbl" for={`adv-to-${tun.id}`}>Таймаут подключения (с)</label>
											<input
												id={`adv-to-${tun.id}`}
												class="inp mono"
												type="number"
												min="2"
												max="60"
												value={tun.advanced?.timeout ?? 8}
												oninput={(e) => {
													const v = Number((e.currentTarget as HTMLInputElement).value);
													tunnels = tunnels.map((t) =>
														t.id === tun.id
															? { ...t, advanced: { ...t.advanced, timeout: v } }
															: t,
													);
												}}
											/>
										</div>
									</div>

									<div class="field-row">
										<div class="field-row-text">
											<span class="lbl">Ядерный стек TCP (Kernel TCP)</span>
											<span class="hint">Ускоряет обработку TCP-соединений на роутере</span>
										</div>
										<Toggle
											size="sm"
											checked={Boolean(tun.advanced?.kernelTcp)}
											onchange={(v) => {
												tunnels = tunnels.map((t) =>
													t.id === tun.id
														? { ...t, advanced: { ...t.advanced, kernelTcp: v } }
														: t,
												);
											}}
										/>
									</div>

									<div class="field-row">
										<div class="field-row-text">
											<span class="lbl">Пропускать проверку TLS-сертификата</span>
											<span class="hint">Разрешить самоподписанные сертификаты (allowInsecure)</span>
										</div>
										<Toggle
											size="sm"
											checked={Boolean(tun.advanced?.insecure)}
											onchange={(v) => {
												tunnels = tunnels.map((t) =>
													t.id === tun.id
														? { ...t, advanced: { ...t.advanced, insecure: v } }
														: t,
												);
											}}
										/>
									</div>
								</div>
							</div>
						</div>
					{/if}

					<!-- TAB 4: INBOUNDS (SOCKS5 / HTTP) -->
					{#if subTab === 'inbounds'}
						<div class="subtab-panel">
							<div class="sources-header">
								<span class="hint">
									Входящие порты позволяют подключаться к туннелю по протоколам SOCKS5 и HTTP (например, порт 1099 для локальных служб).
								</span>
								<Button variant="secondary" size="sm" onclick={() => addInbound(tun)}>
									{#snippet iconBefore()}<Plus size={13} />{/snippet}
									Добавить порт
								</Button>
							</div>

							{#if (tun.inbounds?.length ?? 0) === 0}
								<div class="sub-empty">Локальные прокси-порты не настроены.</div>
							{:else}
								<div class="sources-list">
									{#each tun.inbounds as inb, idx (idx)}
										<div class="inbound-row">
											<div class="field">
												<label class="lbl" for={`inb-listen-${tun.id}-${idx}`}>Адрес</label>
												<input
													id={`inb-listen-${tun.id}-${idx}`}
													class="inp mono compact-inp"
													type="text"
													value={inb.listen || '0.0.0.0'}
													oninput={(e) => {
														const v = (e.currentTarget as HTMLInputElement).value;
														tunnels = tunnels.map((t) =>
															t.id === tun.id
																? {
																		...t,
																		inbounds: t.inbounds.map((item, i) =>
																			i === idx ? { ...item, listen: v } : item,
																		),
																	}
																: t,
														);
													}}
												/>
											</div>
											<div class="field">
												<label class="lbl" for={`inb-port-${tun.id}-${idx}`}>Порт</label>
												<input
													id={`inb-port-${tun.id}-${idx}`}
													class="inp mono compact-inp"
													type="number"
													min="1"
													max="65535"
													value={inb.port}
													oninput={(e) => {
														const v = Number((e.currentTarget as HTMLInputElement).value);
														tunnels = tunnels.map((t) =>
															t.id === tun.id
																? {
																		...t,
																		inbounds: t.inbounds.map((item, i) =>
																			i === idx ? { ...item, port: v } : item,
																		),
																	}
																: t,
														);
													}}
												/>
											</div>
											<div class="field">
												<label class="lbl" for={`inb-node-${tun.id}-${idx}`}>Привязка к узлу</label>
												<select
													id={`inb-node-${tun.id}-${idx}`}
													class="inp compact-inp"
													value={inb.node ?? ''}
													onchange={(e) => {
														const v = (e.currentTarget as HTMLSelectElement).value;
														tunnels = tunnels.map((t) =>
															t.id === tun.id
																? {
																		...t,
																		inbounds: t.inbounds.map((item, i) =>
																			i === idx ? { ...item, node: v } : item,
																		),
																	}
																: t,
														);
													}}
												>
													<option value={FIRC_TUNNEL_NODE_POOL}>Весь пул отказоустойчивости (*)</option>
													{#if inb.node === ''}
														<option value="">Авто (весь пул)</option>
													{/if}
													{#each nodes as n (n.key)}
														<option value={n.key}>{n.name}</option>
													{/each}
												</select>
											</div>
											<div class="inbound-del">
												<button
													type="button"
													class="route-action-btn danger"
													title="Удалить порт"
													onclick={() => removeInbound(tun, idx)}
												>
													<Trash2 size={14} />
												</button>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/if}
				</div>
			</div>
		{/if}
	{/if}

	<!-- TrustTunnel / Sing-box tunnels section (visible right alongside FIRC tunnels!) -->
	{#if showTrustTunnels && filteredTrustTunnels.length > 0}
		<div class="trust-tunnels-section">
			<div class="section-divider-head">
				<span class="section-divider-title">TrustTunnel / Sing-box</span>
				<Badge variant="default" size="xs" mono>{filteredTrustTunnels.length}</Badge>
			</div>
			<div class="tunnel-grid tunnel-grid--compact">
				{#each filteredTrustTunnels as stTunnel, i (stTunnel.tag)}
					<SingboxTunnelCard
						tunnel={stTunnel}
						layout="compact"
						renderMode="compact"
						autoDelayCheckNonce={1}
						autoDelayCheckDelayMs={i * 180}
						ondetail={(tag) => onOpenSingboxDetail?.(tag)}
					/>
				{/each}
			</div>
		</div>
	{/if}
</div>

<Modal
	bind:open={sourceModalOpen}
	title={sourceModalKind === 'subscription' ? 'Добавить подписку VLESS' : 'Добавить ссылку vless://'}
	size="md"
	onclose={() => (sourceModalOpen = false)}
>
	<div class="modal-form">
		<div class="field">
			<label class="lbl" for="src-modal-name">Название</label>
			<input
				id="src-modal-name"
				class="inp"
				type="text"
				placeholder={sourceModalKind === 'subscription' ? 'Например: Основная подписка' : 'Например: Нидерланды XHTTP'}
				bind:value={sourceModalName}
			/>
		</div>

		<div class="field">
			<label class="lbl" for="src-modal-url">
				{sourceModalKind === 'subscription' ? 'URL подписки (https://...)' : 'Ссылка (vless://...)'}
			</label>
			<input
				id="src-modal-url"
				class="inp mono"
				type="text"
				placeholder={sourceModalKind === 'subscription' ? 'https://...' : 'vless://...'}
				bind:value={sourceModalUrl}
			/>
		</div>

		{#if sourceModalKind === 'subscription'}
			<div class="field">
				<label class="lbl" for="src-modal-int">Интервал автообновления</label>
				<select id="src-modal-int" class="inp" bind:value={sourceModalInterval}>
					{#each SUB_INTERVALS as opt}
						<option value={opt.value}>{opt.label}</option>
					{/each}
				</select>
			</div>
		{/if}
	</div>

	{#snippet actions()}
		<Button variant="secondary" size="sm" onclick={() => (sourceModalOpen = false)}>
			Отмена
		</Button>
		<Button
			variant="primary"
			size="sm"
			loading={sourceModalBusy}
			disabled={!sourceModalUrl.trim()}
			onclick={submitAddSource}
		>
			Добавить
		</Button>
	{/snippet}
</Modal>

<style>
	.firc-tunnels-wrap {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}

	.tunnels-toolbar,
	.dashboard-save-bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		flex-wrap: wrap;
	}

	.dashboard-save-bar {
		padding: 8px 12px;
		background: color-mix(in srgb, var(--color-accent) 10%, var(--color-bg-secondary));
		border: 1px solid color-mix(in srgb, var(--color-accent) 40%, var(--color-border));
		border-radius: var(--radius);
		font-size: 12px;
	}

	.tunnel-count {
		font-size: 0.8125rem;
		color: var(--color-text-muted);
	}

	.toolbar-left,
	.toolbar-right {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}

	.search-box {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 10px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: 7px;
		color: var(--color-text-secondary);
		min-width: 250px;
	}

	.search-box.compact {
		min-width: 200px;
		padding: 5px 9px;
		background: var(--color-bg-tertiary);
	}

	.search-box input {
		border: none;
		background: transparent;
		color: var(--color-text-primary);
		font-size: 12px;
		width: 100%;
		outline: none;
	}

	/* ── Classic awg-manager Compact Card Layout ── */
	.card.firc-card {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 12px 14px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		color: var(--color-text-primary);
		position: relative;
		transition:
			border-color var(--t-fast) ease,
			box-shadow var(--t-fast) ease;
	}

	.card.firc-card.ok {
		border-color: var(--color-success-border);
	}
	.card.firc-card.slow {
		border-color: var(--color-warning-border);
	}
	.card.firc-card.fail {
		border-color: var(--color-error-border);
	}
	.card.firc-card.stopped {
		border-color: var(--color-muted-border);
		opacity: 0.72;
	}
	.card.firc-card.selected {
		border-color: var(--color-accent);
		box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-accent) 40%, transparent);
	}

	.tunnel-card-intro {
		display: flex;
		flex-direction: column;
		gap: 5px;
	}

	.title-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-width: 0;
	}

	.title-left {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		flex: 1;
	}

	.title-right {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-shrink: 0;
	}

	.card-title-btn {
		margin: 0;
		padding: 0;
		border: none;
		background: none;
		font: inherit;
		font-size: var(--sbx-card-title, 0.9375rem);
		font-weight: 600;
		color: var(--color-text-primary);
		cursor: pointer;
		text-align: left;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.card-title-btn:hover {
		color: var(--color-accent);
	}

	.ping-pill {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 7px;
		border-radius: 999px;
		border: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		font-family: var(--font-mono);
		font-size: 10px;
		font-weight: 600;
		cursor: pointer;
		transition: all 0.15s;
	}

	.ping-pill:hover:not(:disabled) {
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.ping-pill.ok {
		background: rgba(16, 185, 129, 0.14);
		border-color: rgba(16, 185, 129, 0.35);
		color: #34d399;
	}

	.ping-pill.slow {
		background: rgba(245, 158, 11, 0.14);
		border-color: rgba(245, 158, 11, 0.35);
		color: #fbbf24;
	}

	.iface {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		color: var(--color-text-muted);
		font-size: var(--sbx-card-meta, 11px);
		font-family: var(--font-mono);
	}

	.meta-dot {
		margin: 0 0.35em;
		opacity: 0.75;
	}

	.badges {
		display: flex;
		gap: 5px;
		flex-wrap: wrap;
	}

	.badge {
		padding: 2px 8px;
		font-size: var(--sbx-card-badge, 10px);
		border-radius: 10px;
		font-weight: 500;
	}

	.b-vless {
		background: rgba(59, 130, 246, 0.15);
		color: #60a5fa;
	}
	.b-reality {
		background: rgba(236, 72, 153, 0.15);
		color: #f472b6;
	}
	.b-mode {
		background: rgba(139, 92, 246, 0.15);
		color: #a78bfa;
	}
	.b-groups {
		background: rgba(20, 184, 166, 0.16);
		color: #5eead4;
	}

	.divider-dashed {
		height: 0;
		border: none;
		border-top: 1px dashed var(--color-border);
		margin: 0;
	}

	.card-rows {
		display: flex;
		flex-direction: column;
		gap: 5px;
	}

	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}

	.row .item-label {
		color: var(--color-text-muted);
		font-size: var(--sbx-card-label, 10px);
		text-transform: uppercase;
		letter-spacing: 0.04em;
		width: 68px;
		flex-shrink: 0;
	}

	.row .value {
		font-size: var(--sbx-card-value, 11px);
		color: var(--color-text-secondary);
		font-family: var(--font-mono);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		min-width: 0;
	}

	.active-node-val {
		color: var(--color-text-primary) !important;
		font-weight: 500;
	}

	.actions--bar {
		display: flex;
		align-items: center;
		gap: 5px;
		flex-wrap: wrap;
		margin-top: auto;
	}

	.card-act-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 5px;
		padding: 5px 9px;
		border-radius: 6px;
		border: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		font-size: 11px;
		font-weight: 500;
		cursor: pointer;
		transition: all 0.15s;
	}

	.card-act-btn:hover:not(:disabled) {
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.card-act-btn.primary-act {
		flex: 1;
	}

	.card-act-btn.primary-act.active {
		background: color-mix(in srgb, var(--color-accent) 16%, var(--color-bg-tertiary));
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.card-act-btn.danger-act:hover:not(:disabled) {
		border-color: rgba(247, 118, 142, 0.5);
		color: #f7768e;
	}

	.card-act-btn:disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}

	.chart-section {
		margin: 2px -14px -12px;
		border-radius: 0 0 var(--radius) var(--radius);
		background: color-mix(in srgb, var(--color-bg-primary) 45%, var(--color-bg-secondary));
		border-top: 1px solid var(--color-border);
		overflow: hidden;
	}

	.chart-body {
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 5px;
	}

	.chart-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		color: var(--color-text-muted);
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.chart-head .stats {
		font-family: var(--font-mono);
		font-size: 11px;
		text-transform: none;
		letter-spacing: normal;
		color: var(--color-text-secondary);
	}

	.node-health-bar {
		display: flex;
		gap: 2px;
		height: 6px;
		border-radius: 3px;
		overflow: hidden;
	}

	.nh-seg {
		flex: 1;
		border-radius: 2px;
		background: rgba(148, 163, 184, 0.25);
	}

	.nh-seg.nh-active {
		background: #34d399;
	}
	.nh-seg.nh-reserve {
		background: rgba(56, 189, 248, 0.45);
	}
	.nh-seg.nh-down {
		background: #f87171;
	}
	.nh-seg.nh-excl {
		background: rgba(148, 163, 184, 0.12);
	}

	/* ── Full-Width Expanded Detail Panel ── */
	.tunnel-detail-panel {
		border: 1px solid var(--color-accent);
		border-radius: var(--radius);
		background: var(--color-bg-secondary);
		overflow: hidden;
	}

	.detail-panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 14px;
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		flex-wrap: wrap;
	}

	.detail-header-left,
	.detail-header-right {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.detail-header-left {
		flex: 1;
		min-width: 220px;
	}

	.desc-input {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: 6px;
		color: var(--color-text-primary);
		font-size: 13px;
		font-weight: 600;
		padding: 4px 10px;
		width: 100%;
		max-width: 340px;
	}

	.desc-input:focus {
		border-color: var(--color-accent);
		outline: none;
	}

	/* ── TrustTunnel Section Divider ── */
	.trust-tunnels-section {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin-top: 6px;
	}

	.section-divider-head {
		display: flex;
		align-items: center;
		gap: 8px;
		padding-top: 6px;
		border-top: 1px solid var(--color-border);
	}

	.section-divider-title {
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.route-action-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		border-radius: 6px;
		border: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all 0.15s;
	}

	.route-action-btn:hover:not(:disabled) {
		color: var(--color-text-primary);
		border-color: var(--color-accent);
	}

	.route-action-btn.danger:hover:not(:disabled) {
		color: #f7768e;
		border-color: rgba(247, 118, 142, 0.5);
	}

	.tunnel-body {
		padding: 12px 14px;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.tunnel-subtabs {
		display: flex;
		align-items: center;
		gap: 6px;
		border-bottom: 1px solid var(--color-border);
		padding-bottom: 8px;
		flex-wrap: wrap;
	}

	.subtab-btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 5px 10px;
		border-radius: 6px;
		border: 1px solid transparent;
		background: transparent;
		color: var(--color-text-secondary);
		font-size: 12px;
		font-weight: 500;
		cursor: pointer;
		transition: all 0.15s;
	}

	.subtab-btn:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}

	.subtab-btn.active {
		color: var(--color-accent);
		background: var(--color-bg-tertiary);
		border-color: var(--color-border);
		font-weight: 600;
	}

	.subtab-panel {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.nodes-toolbar,
	.sources-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		flex-wrap: wrap;
	}

	.nodes-toolbar-actions,
	.sources-actions {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
	}

	.nodes-table-wrap {
		border: 1px solid var(--color-border);
		border-radius: 8px;
		overflow-x: auto;
		max-height: 360px;
		overflow-y: auto;
		background: var(--color-bg-secondary);
	}

	.nodes-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 12px;
	}

	.nodes-table th {
		text-align: left;
		padding: 8px 10px;
		font-size: 11px;
		font-weight: 600;
		color: var(--color-text-secondary);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		position: sticky;
		top: 0;
		z-index: 1;
	}

	.nodes-table td {
		padding: 7px 10px;
		border-bottom: 1px solid rgba(255, 255, 255, 0.04);
		color: var(--color-text-primary);
	}

	.nodes-table tr.excluded-row {
		opacity: 0.5;
	}

	.prio-cell {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.prio-num {
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-secondary);
	}

	.prio-btns {
		display: flex;
		flex-direction: column;
		gap: 1px;
	}

	.mini-icon-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 16px;
		height: 14px;
		border: none;
		background: transparent;
		color: var(--color-text-secondary);
		cursor: pointer;
		padding: 0;
	}

	.mini-icon-btn:hover:not(:disabled) {
		color: var(--color-accent);
	}

	.mini-icon-btn:disabled {
		opacity: 0.3;
		cursor: default;
	}

	.node-name-cell {
		font-weight: 500;
	}

	.mono-cell {
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--color-text-secondary);
	}

	.probe-ok {
		color: var(--color-success, #9ece6a);
		font-weight: 600;
	}

	.probe-err {
		color: #f7768e;
		font-size: 11px;
	}

	.muted-dash {
		color: var(--color-text-secondary);
	}

	.sources-list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.source-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding: 10px 12px;
		border: 1px solid var(--color-border);
		border-radius: 8px;
		background: var(--color-bg-secondary);
		flex-wrap: wrap;
	}

	.source-main {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1;
		min-width: 240px;
	}

	.source-title-line {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.source-side {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.inbound-row {
		display: grid;
		grid-template-columns: 1fr 120px 1.5fr auto;
		gap: 10px;
		align-items: end;
		padding: 10px 12px;
		border: 1px solid var(--color-border);
		border-radius: 8px;
		background: var(--color-bg-secondary);
	}

	@media (max-width: 720px) {
		.inbound-row {
			grid-template-columns: 1fr;
		}
	}

	.settings-grid-2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	@media (max-width: 860px) {
		.settings-grid-2 {
			grid-template-columns: 1fr;
		}
	}

	.sub-box {
		padding: 12px 14px;
		border: 1px solid var(--color-border);
		border-radius: 8px;
		background: var(--color-bg-secondary);
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.sub-box-title {
		font-size: 11px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-secondary);
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.field-grid-2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
	}

	.field-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding-top: 6px;
		border-top: 1px solid rgba(255, 255, 255, 0.04);
	}

	.field-row-text {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.lbl {
		font-size: 12px;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.hint {
		font-size: 11px;
		color: var(--color-text-secondary);
	}

	.inp {
		padding: 6px 10px;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: 6px;
		color: var(--color-text-primary);
		font-size: 12px;
		width: 100%;
		box-sizing: border-box;
	}

	.inp:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.compact-inp {
		padding: 5px 8px;
	}

	.compact-select {
		width: auto;
		min-width: 140px;
	}

	.mono {
		font-family: var(--font-mono);
	}

	.empty-card,
	.sub-empty {
		padding: 28px;
		text-align: center;
		border: 1px solid var(--color-border);
		border-radius: 10px;
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
		font-size: 13px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
	}

	.empty-title {
		font-size: 14px;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.modal-form {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	:global(.spin) {
		animation: firc-spin 1s linear infinite;
	}

	@keyframes firc-spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
