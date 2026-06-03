import {
	Shuffle,
	Home,
	Shield,
	Route,
	Wrench,
	UserRound,
	Briefcase,
	Gamepad2,
	Tv,
	Panda,
	Plug,
	Layers,
	Unplug,
	FlaskConical,
	Copy,
	Wifi,
	Heart,
} from 'lucide-svelte';

/** Lucide icon id resolved from policy description / name. */
export type PolicyIconId =
	| 'shuffle'
	| 'home'
	| 'shield'
	| 'route'
	| 'tools'
	| 'guest'
	| 'kids'
	| 'work'
	| 'gaming'
	| 'tv'
	| 'iot'
	| 'hydraroute'
	| 'direct'
	| 'test'
	| 'backup'
	| 'wifi'
	| 'parents';

type PolicyIconComponent = typeof Shuffle;

export const POLICY_ICON_COMPONENTS: Record<PolicyIconId, PolicyIconComponent> = {
	shuffle: Shuffle,
	home: Home,
	shield: Shield,
	route: Route,
	tools: Wrench,
	guest: UserRound,
	kids: Panda,
	work: Briefcase,
	gaming: Gamepad2,
	tv: Tv,
	iot: Plug,
	hydraroute: Layers,
	direct: Unplug,
	test: FlaskConical,
	backup: Copy,
	wifi: Wifi,
	parents: Heart,
};

/** First matching rule wins (most specific keywords first). */
const POLICY_ICON_RULES: { icon: PolicyIconId; keywords: string[] }[] = [
	{ icon: 'hydraroute', keywords: ['hydraroute', 'hr_neo', 'hrneo'] },
	{ icon: 'tools', keywords: ['nfqws', 'zapret', 'dpi_bypass', 'dpi', 'service'] },
	{
		icon: 'route',
		keywords: [
			'magitrickle',
			'magicitrickle',
			'singbox',
			'sing_box',
			'sb_router',
			'split',
			'sbr',
		],
	},
	{
		icon: 'shield',
		keywords: [
			'amnezia',
			'awgm',
			'tunnel',
			'awg_manager',
			'awg-manager',
			'awg',
			'wireguard',
			'vless',
			'adguard',
			'vmess',
			'trojan',
			'warp',
			'clash',
			'xray',
			'hysteria',
			'hy2',
			'wg',
			'vpn',
		],
	},
	{ icon: 'iot', keywords: ['smarthome', 'smart_home', 'iot'] },
	{ icon: 'guest', keywords: ['wifi_guest', 'guest_wifi', 'guest', 'gost'] },
	{ icon: 'kids', keywords: ['kids', 'child'] },
	{ icon: 'parents', keywords: ['parents', 'babushka', 'family', 'wife', 'husband'] },
	{ icon: 'gaming', keywords: ['gaming', 'gamepad', 'ps5', 'xbox', 'steam'] },
	{ icon: 'tv', keywords: ['stream', 'media', 'tv', 'tube', 'kino'] },
	{ icon: 'work', keywords: ['office', 'corp', 'work', 'job', 'business'] },
	{ icon: 'home', keywords: ['default', 'home', 'dom', 'house', 'floor', 'apartment'] },
	{ icon: 'direct', keywords: ['direct', 'wan', 'isp', 'internet', 'russia', 'rus'] },
	{ icon: 'test', keywords: ['test', 'tmp', 'dev'] },
	{ icon: 'backup', keywords: ['failover', 'backup', 'reserve'] },
	{ icon: 'wifi', keywords: ['wifi'] },
];

function escapeRegExp(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Case-insensitive: descriptions may be HOME, WiFi_Guest, VPN, etc. */
function normalizeLabel(label: string): string {
	return label.trim().toLocaleLowerCase('en').replace(/-/g, '_');
}

function tokenize(normalized: string): string[] {
	return normalized.split(/[_\s]+/).filter(Boolean);
}

function normalizeKeyword(keyword: string): string {
	return keyword.trim().toLocaleLowerCase('en').replace(/-/g, '_');
}

/** Match keyword as a token or as a _/-delimited segment (avoids `dom` in `freedom`). */
function matchesKeyword(keyword: string, normalized: string, tokens: string[]): boolean {
	const kw = normalizeKeyword(keyword);
	if (!kw) return false;
	if (kw.includes('_')) {
		return normalized.includes(kw);
	}
	if (tokens.includes(kw)) return true;
	const re = new RegExp(`(^|[_-])${escapeRegExp(kw)}([_-]|$)`);
	return re.test(normalized);
}

/**
 * Pick a Lucide icon for an access-policy card from description or policy name.
 * Pass `isHydraRoute` so HR-managed policies without description still get an icon.
 */
export function resolvePolicyIcon(
	label: string,
	options?: { policyName?: string; isHydraRoute?: boolean },
): PolicyIconId {
	const source = label.trim() || options?.policyName?.trim() || '';
	if (!source) {
		return options?.isHydraRoute ? 'hydraroute' : 'shuffle';
	}

	const normalized = normalizeLabel(source);
	const tokens = tokenize(normalized);

	for (const rule of POLICY_ICON_RULES) {
		if (rule.keywords.some((kw) => matchesKeyword(kw, normalized, tokens))) {
			return rule.icon;
		}
	}

	return 'shuffle';
}

export function getPolicyIconComponent(id: PolicyIconId): PolicyIconComponent {
	return POLICY_ICON_COMPONENTS[id];
}
