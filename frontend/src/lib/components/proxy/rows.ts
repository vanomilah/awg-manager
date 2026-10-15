// Модель строки списка/детали страницы «Прокси» и её сборка из статуса и
// конфига. Список слева един для обоих протоколов: протокол и режим — признаки
// строки, а не уровень навигации (ia.md §1).

import type {
  FreeTurnConfig,
  FreeTurnProcessStatus,
  FreeTurnServerConfig,
  FreeTurnStatus,
  OpenFluxClientConfig,
  OpenFluxClientStatus,
  OpenFluxConfig,
  OpenFluxStatus,
  WdttConfig,
  WdttProcessStatus,
  WdttServerConfig,
  WdttStatus,
} from "$lib/types";

export type ProxyProtocol = "wdtt" | "freeturn" | "openflux";

export type ProxyRole = "client" | "server";

export type ProxyRunState = "running" | "error" | "stopped";

export interface ProxyInstanceRow {
  /** Ключ строки: id инстанса уникален только внутри протокола. */
  key: string;
  /** id инстанса для ручек API. */
  id: string;
  protocol: ProxyProtocol;
  role: ProxyRole;
  name: string;
  state: ProxyRunState;
  /** UI-имя флага Enabled бэкенда (оговорка LS-08). */
  autostart: boolean;
  pid?: number;
  startedAt?: string;
  /**
   * Наш живой процесс с унаследованным pid-файлом (LS-09). Готовое поле
   * бэкенда — на фронте НЕ вычисляется (спека §2.1).
   */
  orphanedPid: boolean;
  binaryPresent: boolean;
  /** Режим WDTT: connMode клиента / relayMode сервера. У FreeTurn режима нет. */
  mode?: "wg" | "raw";
  /**
   * Имя старого конфига, из которого инстанс перенёс посев. Пусто у
   * заведённых через UI — по нему деталь показывает бейдж «перенесено»,
   * объясняющий происхождение «дефолтных» инстансов.
   */
  seededFrom?: string;
  /**
   * Схема потока для карточки списка: три звена «откуда — через что — куда»,
   * уже словами. Собирается здесь, чтобы список не знал про роли и конфиги.
   */
  flow?: ProxyFlowStep[];
}

/** Звено схемы потока: подпись и техническая деталь под ней. */
export interface ProxyFlowStep {
  label: string;
  detail: string;
}

type ProcessStatus = WdttProcessStatus | FreeTurnProcessStatus;
type InstanceStatus = { id: string; name: string; status: ProcessStatus };

/**
 * «Включён, но не работает» (LS-06, RB-02) — процесс должен работать, но не работает.
 *
 * Судит `enabled` конфига, а не `lastError`: бэкенд снимает Enabled только на
 * явный стоп пользователя (`internal/wdtt/service.go:803`,
 * `internal/freeturn/service.go:702`), а по нему же поднимает клиента
 * супервизор — это и есть «должен работать». `lastError` живёт в памяти
 * процесса и переживает стоп упавшего инстанса (`process.Stop` его не
 * трогает), так что сам по себе состоянием не является.
 */
function runState(s: ProcessStatus, enabled: boolean): ProxyRunState {
  if (s.running) return "running";
  return enabled ? "error" : "stopped";
}

/**
 * Ключ строки списка. Единственный владелец формата: его же собирают
 * `rowKeyFromInstanceKey` и обработчики мастеров на странице прокси, а
 * разъехавшись, они молча перестают находить строку.
 */
export function rowKey(protocol: ProxyProtocol, role: ProxyRole, id: string): string {
  return `${protocol}:${role}:${id}`;
}

function toRow(
  protocol: ProxyProtocol,
  role: ProxyRole,
  inst: InstanceStatus,
  autostart: boolean,
  mode?: "wg" | "raw",
  seededFrom?: string,
  flow?: ProxyFlowStep[],
): ProxyInstanceRow {
  const s = inst.status;
  return {
    key: rowKey(protocol, role, inst.id),
    id: inst.id,
    protocol,
    role,
    name: inst.name,
    state: runState(s, autostart),
    autostart,
    pid: s.pid,
    startedAt: s.startedAt,
    orphanedPid: typeof s.orphanedPid === 'number' ? s.orphanedPid > 0 : !!s.orphanedPid,
    binaryPresent: s.binaryPresent,
    mode,
    seededFrom,
    flow,
  };
}

/**
 * Вид записи бэкенда → протокол и роль строки. Форматы ключей разошлись:
 * бэкенд слил протокол с ролью через дефис (`Record.Key()` —
 * `internal/proxyrt/instancestore/record.go:110`), у строки это отдельные
 * звенья. Набор закрыт — `instancestore.AllKinds`.
 */
const INSTANCE_KINDS: Record<string, { protocol: ProxyProtocol; role: ProxyRole }> = {
	'wdtt-client': { protocol: 'wdtt', role: 'client' },
	'wdtt-server': { protocol: 'wdtt', role: 'server' },
	'freeturn-client': { protocol: 'freeturn', role: 'client' },
	'freeturn-server': { protocol: 'freeturn', role: 'server' },
	'openflux-client': { protocol: 'openflux', role: 'client' },
	'openflux-server': { protocol: 'openflux', role: 'server' },
};

/**
 * Ключ строки списка по ключу инстанса бэкенда (`kind:id`) — им адресует
 * глубокая ссылка с карточки туннеля (`ProxyOwnedBadge`). Роль отдаём
 * вместе с ключом: по ней страница выбирает вкладку. null — вид роли
 * неизвестен или id пуст.
 */
export function rowKeyFromInstanceKey(
	instanceKey: string,
): { key: string; role: ProxyRole } | null {
	const sep = instanceKey.indexOf(':');
	if (sep < 0) return null;
	const kind = INSTANCE_KINDS[instanceKey.slice(0, sep)];
	const id = instanceKey.slice(sep + 1);
	if (!kind || !id) return null;
	return { key: rowKey(kind.protocol, kind.role, id), role: kind.role };
}

/** Порт из адреса `host:port`; пусто — адреса нет или он без порта. */
function portOf(addr?: string): string {
	const p = (addr ?? '').trim().split(':').pop() ?? '';
	return /^\d+$/.test(p) ? `:${p}` : '';
}

/** Хост из адреса, без порта: в узкой карточке порт сервера роли не играет. */
function hostOf(addr?: string): string {
	const a = (addr ?? '').trim();
	if (!a) return '—';
	const idx = a.lastIndexOf(':');
	return idx > 0 ? a.slice(0, idx) : a;
}

/** Поток клиента: роутер → сервер → интернет. */
function clientFlow(listen?: string, peer?: string): ProxyFlowStep[] {
	return [
		{ label: 'Этот роутер', detail: portOf(listen) || 'локальный порт' },
		{ label: hostOf(peer), detail: portOf(peer) || 'адрес сервера' },
		{ label: 'Интернет', detail: 'через выход' },
	];
}

/** Поток OpenFlux-клиента: роутер (OpkgTun / SOCKS5) → сервер OpenFlux → интернет. */
function openfluxClientFlow(c?: OpenFluxClientConfig, st?: OpenFluxClientStatus): ProxyFlowStep[] {
	const transports = c?.transports ?? ['direct'];
	const transportLabels = transports.map((t) => {
		if (t === 'webdav') return 'WebDAV';
		if (t === 'mailru') return 'Mail.ru';
		if (t === 'yandex') return 'Яндекс';
		if (t === 'direct') return 'Direct';
		if (t === 'cupsonline') return 'Cups';
		if (t === 'telemost') return 'Telemost';
		return t;
	});
	const isTun = (c?.inboundMode ?? st?.inboundMode ?? 'socks5') === 'tun';
	const rawName = st?.ndmsIface || c?.ndmsIface || '';
	const ifaceName = (rawName && rawName !== 'OpkgTun0' && rawName !== 'OpkgTun1') ? rawName : 'OpkgTun (авто)';
	const routerDetail = isTun
		? ifaceName
		: `SOCKS5 ${portOf(c?.socks5Listen || ':1085') || ':1085'}`;
	const peerLabel = c?.directDial ? hostOf(c.directDial) : 'OpenFlux';
	const peerDetail =
		transportLabels.length > 0
			? transportLabels.join(' / ')
			: portOf(c?.directDial) || 'мульти-транспорт';
	return [
		{ label: 'Этот роутер', detail: routerDetail },
		{ label: peerLabel, detail: peerDetail },
		{ label: 'Интернет', detail: 'через выход' },
	];
}

/** Поток WDTT-сервера: абоненты → роутер → интернет. */
function wdttServerFlow(c?: WdttServerConfig, st?: WdttProcessStatus): ProxyFlowStep[] {
	// NDMS-имена берём из СТАТУСА: конфиг несёт только WG-половину, а raw-имя
	// (`rawNdmsIface`) живёт в наблюдении процесса.
	const ifaces = [st?.ndmsIface ?? c?.ndmsIface, st?.rawNdmsIface].filter(Boolean).join(' · ');
	const nat = c?.natMode === 'none' ? 'без NAT' : c?.natMode === 'internet-only' ? 'NAT: интернет' : 'NAT: полный';
	return [
		{ label: 'Абоненты', detail: portOf(c?.listen) || 'порт раздачи' },
		{ label: 'Этот роутер', detail: ifaces || 'интерфейсы не выделены' },
		{ label: 'Интернет', detail: nat },
	];
}

/** Поток FreeTurn-сервера: клиенты → роутер → бэкенд. */
function ftServerFlow(c?: FreeTurnServerConfig): ProxyFlowStep[] {
	return [
		{ label: 'Клиенты', detail: portOf(c?.listen) || 'порт раздачи' },
		{ label: 'Этот роутер', detail: c?.mode === 'tcp' ? 'TCP' : 'UDP' },
		{ label: c?.connect ? hostOf(c.connect) : 'Бэкенд', detail: c?.connect ? 'WG-сервер роутера' : 'не задан' },
	];
}

/** Поток OpenFlux-сервера: клиенты → этот роутер → интернет. */
function openfluxServerFlow(c?: OpenFluxConfig, st?: OpenFluxStatus): ProxyFlowStep[] {
	const transports = c?.transports ?? ['direct'];
	const transportLabels = transports.map((t) => {
		if (t === 'webdav') return 'WebDAV';
		if (t === 'mailru') return 'Mail.ru';
		if (t === 'yandex') return 'Яндекс';
		if (t === 'direct') return 'Direct';
		return t;
	});
	const clientDetail = transportLabels.length > 0 ? `qThrone / ${transportLabels.join(' / ')}` : 'qThrone';
	const port = portOf(c?.directListen || ':8445') || ':8445';
	const modeStr = (c?.mode ?? 'l4').toUpperCase();
	const egress = c?.egressInterface?.trim() ? ` → ${c.egressInterface.trim()}` : '';
	const routerDetail = `${modeStr} · ${port}${egress}`;

	const wan = c?.natStaticWan?.trim() || (c?.natStaticWans && c.natStaticWans.length > 0 ? c.natStaticWans.join(', ') : '');
	let nat = c?.natMode === 'none' ? 'без NAT' : c?.natMode === 'internet-only' ? (wan ? `NAT: ${wan}` : 'NAT: интернет') : 'NAT: полный';
	if (c?.policy && c.policy !== 'none') {
		nat += ` · ${c.policy}`;
	}

	return [
		{ label: 'Клиенты', detail: clientDetail },
		{ label: 'Этот роутер', detail: routerDetail },
		{ label: 'Интернет', detail: nat },
	];
}

export interface ProxySources {
  wdttStatus: WdttStatus | null;
  wdttConfig: WdttConfig | null;
  ftStatus: FreeTurnStatus | null;
  ftConfig: FreeTurnConfig | null;
  openfluxStatus?: OpenFluxStatus | null;
  openfluxConfig?: OpenFluxConfig | null;
  openfluxClientStatus?: OpenFluxClientStatus | null;
  openfluxClientConfig?: OpenFluxClientConfig | null;
}

/** Вкладка «Выход»: клиенты всех протоколов (wdtt, freeturn, openflux). */
export function exitRows(src: ProxySources): ProxyInstanceRow[] {
  const rows: ProxyInstanceRow[] = [
    ...(src.wdttStatus?.clients ?? []).map((i) => {
      const inst = src.wdttConfig?.clients.find((x) => x.id === i.id);
      const c = inst?.config;
      return toRow(
        "wdtt",
        "client",
        i,
        c?.enabled === true,
        c?.connMode === "raw" ? "raw" : "wg",
        inst?.seededFrom,
        clientFlow(c?.listen, c?.peer),
      );
    }),
    ...(src.ftStatus?.clients ?? []).map((i) => {
      const inst = src.ftConfig?.clients.find((x) => x.id === i.id);
      return toRow(
        "freeturn",
        "client",
        i,
        inst?.config.enabled === true,
        undefined,
        inst?.seededFrom,
        clientFlow(inst?.config.listen, inst?.config.peer),
      );
    }),
  ];

  const ofClientSt = src.openfluxClientStatus ?? src.openfluxStatus?.client ?? null;
  const ofClientCfg = ofClientSt?.config ?? src.openfluxClientConfig ?? null;
  const isOfConfigured =
    ofClientSt?.running === true ||
    Boolean(
      ofClientSt?.config &&
        (ofClientSt.config.name?.trim() ||
          ofClientSt.config.directDial?.trim() ||
          (ofClientSt.config.transports && ofClientSt.config.transports.length > 0)),
    ) ||
    Boolean(
      !ofClientSt &&
        ofClientCfg &&
        (ofClientCfg.name?.trim() ||
          ofClientCfg.directDial?.trim() ||
          (ofClientCfg.transports && ofClientCfg.transports.length > 0)),
    );
  if (isOfConfigured) {
    const autostart = ofClientCfg?.enabled === true;
    const isRunning = ofClientSt?.running === true;
    const state: ProxyRunState = isRunning ? "running" : autostart ? "error" : "stopped";
    const startedAt =
      ofClientSt?.uptimeS && isRunning
        ? new Date(Date.now() - ofClientSt.uptimeS * 1000).toISOString()
        : undefined;

    rows.push({
      key: rowKey("openflux", "client", "default"),
      id: "default",
      protocol: "openflux",
      role: "client",
      name: ofClientCfg?.name || "OpenFlux Client",
      state,
      autostart,
      pid: ofClientSt?.pid,
      startedAt,
      orphanedPid: false,
      binaryPresent: (ofClientSt?.installed ?? src.openfluxStatus?.installed) === true,
      mode: (ofClientCfg?.inboundMode ?? 'socks5') === 'tun' ? 'raw' : undefined,
      flow: openfluxClientFlow(ofClientCfg, ofClientSt ?? undefined),
    });
  }

  return rows;
}

/** Вкладка «Раздача»: серверы всех протоколов (wdtt, freeturn, openflux). */
export function shareRows(src: ProxySources): ProxyInstanceRow[] {
  const rows: ProxyInstanceRow[] = [
    ...(src.wdttStatus?.servers ?? []).map((i) => {
      const inst = src.wdttConfig?.servers.find((x) => x.id === i.id);
      const c = inst?.config;
      return toRow(
        "wdtt",
        "server",
        i,
        c?.enabled === true,
        // Режима у сервера нет: обе половины работают всегда, а выбор WG/Raw
        // относится к выдаваемой ссылке (панель ссылки абоненту).
        undefined,
        inst?.seededFrom,
        wdttServerFlow(c, i.status),
      );
    }),
    ...(src.ftStatus?.servers ?? []).map((i) => {
      const inst = src.ftConfig?.servers.find((x) => x.id === i.id);
      return toRow(
        "freeturn",
        "server",
        i,
        inst?.config.enabled === true,
        undefined,
        inst?.seededFrom,
        ftServerFlow(inst?.config),
      );
    }),
  ];

  const ofCfg = src.openfluxConfig ?? src.openfluxStatus?.config;
  const ofSt = src.openfluxStatus;
  if (ofCfg || ofSt?.installed || ofSt?.running) {
    const autostart = ofCfg?.enabled === true;
    const isRunning = ofSt?.running === true;
    const state: ProxyRunState = isRunning ? "running" : autostart ? "error" : "stopped";
    const startedAt = ofSt?.uptimeS && isRunning
      ? new Date(Date.now() - ofSt.uptimeS * 1000).toISOString()
      : undefined;

    rows.push({
      key: rowKey("openflux", "server", "default"),
      id: "default",
      protocol: "openflux",
      role: "server",
      name: ofCfg?.name || "OpenFlux",
      state,
      autostart,
      pid: ofSt?.pid,
      startedAt,
      orphanedPid: false,
      binaryPresent: ofSt?.installed === true,
      mode: undefined,
      flow: openfluxServerFlow(ofCfg, ofSt ?? undefined),
    });
  }

  return rows;
}
