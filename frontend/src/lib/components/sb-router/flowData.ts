import type {
	SingboxRouterRule,
	SingboxRouterDNSServer,
	SingboxRouterDNSGlobals,
	SingboxRouterSettings,
	SingboxRouterWANInterface,
} from '$lib/types';
import type { OutboundGroup } from '$lib/components/routing/singboxRouter/outboundOptions';
import { isSystemRule } from './adapters';

export interface RoutingSummary {
  /** Подпись выхода по умолчанию: «Напрямую» если route.final = direct, иначе тег. */
  defaultLabel: string;
  /** DNS для ветки по умолчанию: server final-сервера, иначе «системный». */
  defaultDnsLabel: string;
  /** Уникальные теги туннельных outbound'ов, используемых правилами (в порядке появления). */
  tunnels: string[];
  /** Кол-во туннелируемых правил. */
  tunneledRuleCount: number;
  /** Кол-во правил, идущих мимо туннеля (direct / final). */
  bypassRuleCount: number;
  /** DNS туннельной ветки: server первого detour-сервера, иначе null. */
  tunnelDnsLabel: string | null;
}

function isTunneled(r: SingboxRouterRule): boolean {
  return !!r.outbound && r.outbound !== 'direct' && r.action !== 'reject';
}

function isBypassTunnel(r: SingboxRouterRule): boolean {
  if (r.action === 'reject' || isSystemRule(r)) return false;
  return !isTunneled(r);
}

function dnsLabelByTag(servers: SingboxRouterDNSServer[], tag: string): string | null {
  const s = servers.find((x) => x.tag === tag);
  if (!s) return null;
  return s.server || s.tag;
}

function outboundLabelByTag(groups: OutboundGroup[] | undefined, tag: string): string {
  for (const group of groups ?? []) {
    const item = group.items?.find((x) => x.value === tag);
    if (item) return item.label;
  }
  return tag;
}

export function formatWanInterfaceLabel(
	iface: Pick<SingboxRouterWANInterface, 'name' | 'label'>,
): string {
	const label = iface.label?.trim() || iface.name;
	return `${label} (${iface.name})`;
}

/** Подпись WAN для ветки «напрямую»: выбранный или авто-определяемый интерфейс провайдера. */
export function resolveDefaultWanLabel(
	settings: Pick<SingboxRouterSettings, 'wanAutoDetect' | 'wanInterface'> | null | undefined,
	wanInterfaces: SingboxRouterWANInterface[],
	routeFinal = 'direct',
): string | null {
	if (routeFinal && routeFinal !== 'direct') return null;
	if (!settings) return null;

	if (!settings.wanAutoDetect) {
		const name = settings.wanInterface?.trim();
		if (!name) return null;
		const iface = wanInterfaces.find((i) => i.name === name);
		return iface ? formatWanInterfaceLabel(iface) : formatWanInterfaceLabel({ name, label: name });
	}

	const sorted = [...wanInterfaces].sort((a, b) => a.priority - b.priority);
	const primary = sorted.find((i) => i.up) ?? sorted[0];
	return primary ? formatWanInterfaceLabel(primary) : null;
}

export function deriveRoutingSummary(
  rules: SingboxRouterRule[],
  routeFinal: string,
  dnsServers: SingboxRouterDNSServer[],
  dnsGlobals: SingboxRouterDNSGlobals,
  outboundOptions: OutboundGroup[] = [],
): RoutingSummary {
  const tunneled = rules.filter(isTunneled);
  const bypass = rules.filter(isBypassTunnel);
  const tunnels: string[] = [];
  for (const r of tunneled) {
    const tag = r.outbound as string;
    if (!tunnels.includes(tag)) tunnels.push(tag);
  }

  const defaultLabel = routeFinal && routeFinal !== 'direct' ? outboundLabelByTag(outboundOptions, routeFinal) : 'Напрямую';
  const defaultDnsLabel = dnsLabelByTag(dnsServers, dnsGlobals.final) ?? 'системный';

  const detourServer = dnsServers.find((s) => !!s.detour);
  const tunnelDnsLabel = detourServer ? (detourServer.server || detourServer.tag) : null;

  return {
    defaultLabel,
    defaultDnsLabel,
    tunnels: tunnels.map((tag) => outboundLabelByTag(outboundOptions, tag)),
    tunneledRuleCount: tunneled.length,
    bypassRuleCount: bypass.length,
    tunnelDnsLabel,
  };
}
