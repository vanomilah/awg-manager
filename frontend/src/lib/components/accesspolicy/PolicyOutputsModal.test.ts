import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import PolicyOutputsModal from './PolicyOutputsModal.svelte';
import type { AccessPolicy, PolicyDevice, PolicyGlobalInterface } from '$lib/types';

vi.mock('$lib/api/client', () => ({
	api: {
		setAccessPolicyDescription: vi.fn().mockResolvedValue({}),
		setAccessPolicyStandalone: vi.fn().mockResolvedValue({}),
		permitPolicyInterface: vi.fn().mockResolvedValue({}),
		denyPolicyInterface: vi.fn().mockResolvedValue({}),
		unassignDeviceFromPolicy: vi.fn().mockResolvedValue({}),
		setPolicyInterfaceUp: vi.fn().mockResolvedValue({}),
	},
}));

vi.mock('$lib/stores/notifications', () => ({
	notifications: {
		success: vi.fn(),
		error: vi.fn(),
		warning: vi.fn(),
	},
}));

vi.mock('$lib/stores/mihomoNative', () => ({
	mihomoInventoryStore: {
		init: vi.fn(),
		subscribe: (fn: (val: any) => void) => {
			fn({ data: { proxies: [], subscriptions: [], groups: [] } });
			return () => {};
		},
	},
}));

vi.mock('$lib/stores/subscriptions', () => ({
	subscriptionsStore: {
		init: vi.fn(),
		subscribe: (fn: (val: any) => void) => {
			fn({ data: [] });
			return () => {};
		},
	},
	subscriptionGroupsStore: {
		init: vi.fn(),
		subscribe: (fn: (val: any) => void) => {
			fn({ data: [] });
			return () => {};
		},
	},
}));

const mockPolicy: AccessPolicy = {
	name: 'Policy0',
	description: 'VPN-Политика',
	deviceCount: 1,
	standalone: true,
	interfaces: [
		{ name: 'Wireguard2', label: 'Wireguard2 (AWGM)' },
	],
};

const mockDevices: PolicyDevice[] = [
	{
		mac: 'aa:bb:cc:dd:ee:01',
		ip: '192.168.90.50',
		name: 'Ivan-PC',
		hostname: 'ivan-pc.lan',
		active: true,
		link: 'up',
		policy: 'Policy0',
	},
	{
		mac: 'aa:bb:cc:dd:ee:02',
		ip: '192.168.90.51',
		name: 'Smart-TV',
		hostname: 'tv.lan',
		active: false,
		link: 'down',
		policy: 'default',
	},
];

const mockGlobalIfaces: PolicyGlobalInterface[] = [
	{ name: 'Wireguard2', up: true, defaultgw: false },
	{ name: 'Ethernet', up: true, defaultgw: true },
];

describe('PolicyOutputsModal', () => {
	it('renders policy details, standalone toggle, interface list, and assigned devices', () => {
		const onclose = vi.fn();
		const onupdate = vi.fn().mockResolvedValue(undefined);

		const { getByText, getByDisplayValue } = render(PolicyOutputsModal, {
			props: {
				open: true,
				policy: mockPolicy,
				devices: mockDevices,
				globalInterfaces: mockGlobalIfaces,
				onclose,
				onupdate,
			},
		});

		// Policy name rendered in header and description field
		expect(getByDisplayValue('VPN-Политика')).toBeTruthy();

		// Standalone badge
		expect(getByText('Standalone')).toBeTruthy();

		// Assigned device Ivan-PC is listed
		expect(getByText('Ivan-PC')).toBeTruthy();
		expect(getByText('192.168.90.50')).toBeTruthy();
	});

	it('unassigns device when clicking remove button on device chip', async () => {
		const onclose = vi.fn();
		const onupdate = vi.fn().mockResolvedValue(undefined);
		const ondeviceunassigned = vi.fn();

		const { container } = render(PolicyOutputsModal, {
			props: {
				open: true,
				policy: mockPolicy,
				devices: mockDevices,
				globalInterfaces: mockGlobalIfaces,
				onclose,
				onupdate,
				ondeviceunassigned,
			},
		});

		const removeBtn = document.body.querySelector('.device-chip-remove');
		expect(removeBtn).toBeTruthy();

		await fireEvent.click(removeBtn!);

		expect(ondeviceunassigned).toHaveBeenCalledWith('aa:bb:cc:dd:ee:01', 'Policy0');
		expect(onupdate).toHaveBeenCalled();
	});
});
