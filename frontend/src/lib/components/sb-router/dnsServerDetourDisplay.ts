import type {
	SingboxProxyGroup,
	SingboxRouterDNSServer,
	SingboxRouterOutbound,
	SingboxTunnel,
	Subscription,
} from '$lib/types';
import type { OutboundGroup } from '$lib/components/routing/singboxRouter/outboundOptions';
import {
	getDnsDirectLegacyDetour,
	normalizeDnsServerDetour,
} from '$lib/utils/dnsServerDetour';
import { resolveOutboundDisplay } from './adapters';
import type { OutboundDisplay } from './types';

/** Явный direct outbound в detour DNS-сервера. */
export function isDnsServerDirectDetour(detour?: string): boolean {
	return detour?.trim() === 'direct';
}

/** @deprecated use isDnsServerEmptyDetour from $lib/utils/dnsServerDetour */
export function isDnsServerViaRouteDetour(detour?: string): boolean {
	return normalizeDnsServerDetour(detour) === undefined;
}

const INVALID_DNS_DIRECT_TITLE =
	'Недопустимый detour на final DNS — будет убран при сохранении. Должно быть «Напрямую».';

/**
 * DNS server detour chip:
 * - empty / direct (non-dns-direct) → «Напрямую»
 * - dns-direct с legacy detour → фактическое значение, красный + !
 * - конкретный outbound → обычный мелкий бейдж цели
 */
export function dnsServerDetourDisplay(
	server: SingboxRouterDNSServer,
	outbounds: SingboxRouterOutbound[],
	outboundOptions: OutboundGroup[] = [],
	subscriptions: Subscription[] | null = null,
	proxyGroups: SingboxProxyGroup[] = [],
	singboxTunnels: SingboxTunnel[] = [],
): OutboundDisplay {
	const legacyDirect = getDnsDirectLegacyDetour(server);
	if (legacyDirect) {
		const base =
			legacyDirect === 'direct'
				? resolveOutboundDisplay(
						'direct',
						'direct',
						outbounds,
						outboundOptions,
						subscriptions,
						proxyGroups,
						singboxTunnels,
					)
				: resolveOutboundDisplay(
						legacyDirect,
						'route',
						outbounds,
						outboundOptions,
						subscriptions,
						proxyGroups,
						singboxTunnels,
					);
		return {
			...base,
			tone: 'invalid',
			invalidHint: INVALID_DNS_DIRECT_TITLE,
		};
	}

	const detour = server.detour?.trim() ?? '';

	if (detour === 'direct' || !detour) {
		return resolveOutboundDisplay(
			'direct',
			'direct',
			outbounds,
			outboundOptions,
			subscriptions,
			proxyGroups,
			singboxTunnels,
		);
	}

	return resolveOutboundDisplay(
		detour,
		'route',
		outbounds,
		outboundOptions,
		subscriptions,
		proxyGroups,
		singboxTunnels,
	);
}
