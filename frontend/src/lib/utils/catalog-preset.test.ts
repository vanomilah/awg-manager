import { describe, expect, it } from 'vitest';
import type { CatalogPreset } from '$lib/types';
import {
	DNS_LARGE_LIST_THRESHOLD,
	DNS_LARGE_LIST_NOTICE,
	catalogPresetCardNotice,
	hrNeoCatalogPresetFilter,
	presetDnsLargeListRisk,
	singboxRouterCatalogPresetFilter,
	splitPresetDnsEntries,
} from './catalog-preset';

const base = {
	id: 'x',
	name: 'X',
	iconSlug: 'x',
	category: 'media',
	origin: 'builtin' as const,
	engines: {},
};

describe('splitPresetDnsEntries', () => {
	it('maps domains and subnets arrays to HR editor fields', () => {
		const p: CatalogPreset = {
			...base,
			engines: {
				dns: {
					domains: ['example.com', 'geoip:ru'],
					subnets: ['91.108.4.0/22', '10.0.0.0/8'],
				},
			},
		};
		expect(splitPresetDnsEntries(p)).toEqual({
			domainLines: ['example.com'],
			cidrLines: ['geoip:ru', '91.108.4.0/22', '10.0.0.0/8'],
		});
	});
});

describe('hrNeoCatalogPresetFilter', () => {
	it('accepts subnet-only presets', () => {
		const p: CatalogPreset = {
			...base,
			engines: { dns: { subnets: ['10.0.0.0/8'] } },
		};
		expect(hrNeoCatalogPresetFilter(p)).toBe(true);
	});
});

describe('presetDnsLargeListRisk', () => {
	it('does not flag subscription-only lists', () => {
		const p: CatalogPreset = {
			...base,
			engines: { dns: { subscriptionUrl: 'https://example.com/list.txt' } },
		};
		expect(presetDnsLargeListRisk(p)).toBe(false);
	});

	it(`flags inline lists above ${DNS_LARGE_LIST_THRESHOLD}`, () => {
		const domains = Array.from({ length: DNS_LARGE_LIST_THRESHOLD + 1 }, (_, i) => `d${i}.com`);
		const p: CatalogPreset = { ...base, engines: { dns: { domains } } };
		expect(presetDnsLargeListRisk(p)).toBe(true);
	});

	it('ignores small inline lists', () => {
		const p: CatalogPreset = { ...base, engines: { dns: { domains: ['a.com'] } } };
		expect(presetDnsLargeListRisk(p)).toBe(false);
	});
});

describe('catalogPresetCardNotice', () => {
	it('includes large-list notice for NDMS picker', () => {
		const p: CatalogPreset = {
			...base,
			notice: 'Sensitive',
			engines: {
				dns: {
					domains: Array.from(
						{ length: DNS_LARGE_LIST_THRESHOLD + 1 },
						(_, i) => `x${i}.com`,
					),
				},
			},
		};
		const text = catalogPresetCardNotice(p, true);
		expect(text).toContain(DNS_LARGE_LIST_NOTICE);
		expect(text).toContain('Sensitive');
	});

	it('omits large-list notice when disabled (sing-box catalog)', () => {
		const p: CatalogPreset = {
			...base,
			engines: { dns: { domains: Array.from({ length: 250 }, (_, i) => `x${i}.com`) } },
		};
		expect(catalogPresetCardNotice(p, false)).toBeUndefined();
	});
});

describe('singboxRouterCatalogPresetFilter', () => {
	it('accepts presets with singbox engine', () => {
		const p: CatalogPreset = {
			...base,
			engines: { singbox: { action: 'route', ruleSets: [] } },
		};
		expect(singboxRouterCatalogPresetFilter(p)).toBe(true);
	});

	it('rejects dns-only presets', () => {
		const p: CatalogPreset = {
			...base,
			engines: { dns: { domains: ['a.com'] } },
		};
		expect(singboxRouterCatalogPresetFilter(p)).toBe(false);
	});
});
