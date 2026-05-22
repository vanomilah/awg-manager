import { describe, expect, it } from 'vitest';
import { isHydraRouteAccessPolicy, isStandardAccessPolicyName } from './accessPolicy';

describe('isStandardAccessPolicyName', () => {
	it('accepts PolicyN', () => {
		expect(isStandardAccessPolicyName('Policy0')).toBe(true);
		expect(isStandardAccessPolicyName('Policy12')).toBe(true);
	});

	it('rejects custom NDMS names', () => {
		expect(isStandardAccessPolicyName('HydraRoute')).toBe(false);
		expect(isStandardAccessPolicyName('germany-vpn')).toBe(false);
		expect(isStandardAccessPolicyName('policy0')).toBe(false);
	});
});

describe('isHydraRouteAccessPolicy', () => {
	it('uses isStandard when present', () => {
		expect(isHydraRouteAccessPolicy({ name: 'Policy0', isStandard: true })).toBe(false);
		expect(isHydraRouteAccessPolicy({ name: 'HydraRoute', isStandard: false })).toBe(true);
	});

	it('falls back to name when isStandard omitted', () => {
		expect(isHydraRouteAccessPolicy({ name: 'Policy1' })).toBe(false);
		expect(isHydraRouteAccessPolicy({ name: 'HydraRoute' })).toBe(true);
	});
});
