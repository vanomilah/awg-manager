import type { CatalogPreset } from '$lib/types';

/** Inline DNS entries above this — warn in NDMS / HR Neo pickers. */
export const DNS_LARGE_LIST_THRESHOLD = 300;

/** Shown on catalog tiles with large DNS lists (NDMS / HR Neo only). */
export const DNS_LARGE_LIST_NOTICE =
	'Список содержит много записей и может работать нестабильно — рекомендуется для использования только в sing-box';

export function presetDnsEntryCount(p: CatalogPreset): number {
	const dns = p.engines.dns;
	return (dns?.domains?.length ?? 0) + (dns?.subnets?.length ?? 0);
}

/** Large DNS list risk for NDMS/HR: >300 inline domain/CIDR entries (not subscription-only). */
export function presetDnsLargeListRisk(p: CatalogPreset): boolean {
	return presetDnsEntryCount(p) > DNS_LARGE_LIST_THRESHOLD;
}

/** Tooltip / footer text for a catalog card (builtin notice + optional large-list warn). */
export function catalogPresetCardNotice(
	p: CatalogPreset,
	warnLargeDnsLists: boolean,
): string | undefined {
	const parts: string[] = [];
	if (warnLargeDnsLists && presetDnsLargeListRisk(p)) {
		parts.push(DNS_LARGE_LIST_NOTICE);
	}
	if (p.notice?.trim()) parts.push(p.notice.trim());
	return parts.length > 0 ? parts.join('\n\n') : undefined;
}

/** HR Neo: only presets with inline domain/CIDR lists (no subscription-only lists). */
export function hrNeoCatalogPresetFilter(p: CatalogPreset): boolean {
	return presetDnsEntryCount(p) > 0;
}

export function dnsRouteCatalogPresetFilter(p: CatalogPreset): boolean {
	return !!p.engines.dns;
}

/** sing-box router: presets with a singbox engine (same set as ListPresets). */
export function singboxRouterCatalogPresetFilter(p: CatalogPreset): boolean {
	return !!p.engines.singbox;
}

export function splitPresetDnsEntries(p: CatalogPreset): {
	domainLines: string[];
	cidrLines: string[];
} {
	const dns = p.engines.dns;
	const domainLines: string[] = [];
	const cidrLines: string[] = [];

	for (const e of dns?.domains ?? []) {
		if (e.startsWith('geoip:') || /^[\d.:a-fA-F]+\/\d+$/.test(e)) cidrLines.push(e);
		else domainLines.push(e);
	}
	for (const e of dns?.subnets ?? []) {
		cidrLines.push(e);
	}

	return { domainLines, cidrLines };
}
