import { describe, expect, it } from 'vitest';
import { api } from './client';

describe('Mihomo API client surface', () => {
	it('keeps routing and native resource methods on the public client', () => {
		for (const method of [
			'mihomoStatus',
			'mihomoConfig',
			'mihomoReload',
			'mihomoNativeProxies',
			'mihomoNativeSubscriptions',
			'mihomoNativeGroups',
			'mihomoNativeRules',
			'mihomoNativeRuleProviders',
			'mihomoRuntimeProxies',
			'mihomoRuntimeProviders',
		] as const) {
			expect(typeof api[method], method).toBe('function');
		}
	});
});
