import { describe, it, expect } from 'vitest';
import {
	isStandardAccessPolicyName,
	isHydraRouteAccessPolicy,
	findPolicyForInterface,
	isDeviceOnline,
} from './accessPolicy';

describe('accessPolicy utils', () => {
	it('identifies standard policy names', () => {
		expect(isStandardAccessPolicyName('Policy0')).toBe(true);
		expect(isStandardAccessPolicyName('Policy3')).toBe(true);
		expect(isStandardAccessPolicyName('Policy63')).toBe(true);
		expect(isStandardAccessPolicyName('HydraRoute')).toBe(false);
		expect(isStandardAccessPolicyName('ProxyMir')).toBe(false);
		expect(isStandardAccessPolicyName('default')).toBe(false);
	});

	it('identifies HydraRoute and custom access policies', () => {
		expect(isHydraRouteAccessPolicy({ name: 'HydraRoute', isStandard: false })).toBe(true);
		expect(isHydraRouteAccessPolicy({ name: 'ProxyMir', isStandard: false })).toBe(true);
		expect(isHydraRouteAccessPolicy({ name: 'Policy0', isStandard: true })).toBe(false);
		expect(isHydraRouteAccessPolicy({ name: 'Policy3', isStandard: true })).toBe(false);
	});

	it('finds policy for permitted interface', () => {
		const policies = [
			{ name: 'Policy0', interfaces: [{ name: 'GigabitEthernet1', denied: false }] },
			{ name: 'Policy2', interfaces: [{ name: 'Proxy4', denied: false }, { name: 'Wireguard4', denied: false }] },
		];
		expect(findPolicyForInterface(policies, 'Proxy4')?.name).toBe('Policy2');
		expect(findPolicyForInterface(policies, 'GigabitEthernet1')?.name).toBe('Policy0');
		expect(findPolicyForInterface(policies, 'Unknown')).toBeNull();
	});

	describe('isDeviceOnline', () => {
		it('marks directly connected devices with link: "up" as online', () => {
			expect(isDeviceOnline({
				active: true,
				link: 'up',
				ip: '192.168.90.28',
			})).toBe(true);
		});

		it('marks MWS extender backhaul devices with empty link as online', () => {
			// Ivan-PC case: connected via Keenetic Giga extender wire, link is '' in RCI
			expect(isDeviceOnline({
				active: true,
				link: '',
				ip: '192.168.90.50',
			})).toBe(true);

			// Laptop case: connected via extender port 4
			expect(isDeviceOnline({
				active: true,
				link: undefined,
				ip: '192.168.90.32',
			})).toBe(true);
		});

		it('marks disconnected devices with link: "down" as offline', () => {
			expect(isDeviceOnline({
				active: false,
				link: 'down',
				ip: '192.168.90.91',
			})).toBe(false);
		});

		it('marks inactive devices as offline even with IP', () => {
			expect(isDeviceOnline({
				active: false,
				link: '',
				ip: '192.168.90.50',
			})).toBe(false);
		});

		it('marks devices with missing or 0.0.0.0 IP as offline', () => {
			expect(isDeviceOnline({
				active: true,
				link: 'up',
				ip: '0.0.0.0',
			})).toBe(false);

			expect(isDeviceOnline({
				active: true,
				link: 'up',
				ip: '',
			})).toBe(false);
		});
	});
});
