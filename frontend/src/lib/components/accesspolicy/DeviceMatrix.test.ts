import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import DeviceMatrix from './DeviceMatrix.svelte';
import type { AccessPolicy, PolicyDevice, PolicyGlobalInterface } from '$lib/types';

const mockPolicies: AccessPolicy[] = [
	{
		name: 'Policy0',
		description: 'Тестовая 2',
		deviceCount: 1,
		interfaces: [
			{ name: 'NextAWG', label: 'NextAWG' },
			{ name: 'Ethernet', label: 'Подключение Ethernet' },
		],
	},
	{
		name: 'Policy1',
		description: 'Прокси политика',
		deviceCount: 0,
		interfaces: [
			{
				name: 'proxy1',
				label: 'USA (США) (Premium) [awg-manager:mihomo:proxy:24a9b71501fc255b2410f03d]',
			},
		],
	},
];

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
		policy: '',
	},
];

describe('DeviceMatrix', () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it('renders list view by default with devices and online/offline lamps', () => {
		const onassign = vi.fn();
		const onunassign = vi.fn();

		const { container, getByText, getAllByText } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign,
				onunassign,
			},
		});

		// Device names are shown (in both desktop table and mobile cards)
		expect(getAllByText('Ivan-PC').length).toBe(2);
		expect(getAllByText('Smart-TV').length).toBe(2);

		// Online lamp and offline lamp are rendered
		const onlineLeds = container.querySelectorAll('.device-led--online');
		const offlineLeds = container.querySelectorAll('.device-led--offline');
		expect(onlineLeds.length).toBeGreaterThanOrEqual(1);
		expect(offlineLeds.length).toBeGreaterThanOrEqual(1);

		// Policy button shows assigned policy name
		expect(getAllByText('Тестовая 2').length).toBeGreaterThanOrEqual(1);
		// Unassigned device shows 'Основная'
		expect(getAllByText('Основная').length).toBeGreaterThanOrEqual(1);
	});

	it('cleans internal technical IDs from interface labels', () => {
		const onassign = vi.fn();
		const onunassign = vi.fn();

		const { container } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign,
				onunassign,
			},
		});

		// The raw [awg-manager:mihomo:proxy...] should NOT appear in the rendered text
		expect(container.textContent).not.toContain('awg-manager:mihomo:proxy:24a9b71501fc255b2410f03d');
	});

	it('supports switching layout between List and Matrix', async () => {
		const onassign = vi.fn();
		const onunassign = vi.fn();

		const { container, getByTitle } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign,
				onunassign,
			},
		});

		// Initially in list mode
		expect(container.querySelector('.device-list-table')).toBeTruthy();
		expect(container.querySelector('.matrix-table')).toBeNull();

		// Click layout switcher to matrix
		const matrixBtn = getByTitle('Матрица устройств');
		await fireEvent.click(matrixBtn);

		// Now matrix table is visible
		expect(container.querySelector('.matrix-table')).toBeTruthy();
		expect(container.querySelector('.device-list-table')).toBeNull();
	});

	it('filters devices by search query', async () => {
		const onassign = vi.fn();
		const onunassign = vi.fn();

		const { queryAllByText, container } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign,
				onunassign,
			},
		});

		const searchInput = container.querySelector('.search-input') as HTMLInputElement;
		await fireEvent.input(searchInput, { target: { value: 'Ivan' } });

		expect(queryAllByText('Ivan-PC').length).toBe(2);
		expect(queryAllByText('Smart-TV').length).toBe(0);
	});

	it('opens policy dropdown on click and triggers onassign', async () => {
		const onassign = vi.fn().mockResolvedValue(undefined);
		const onunassign = vi.fn().mockResolvedValue(undefined);

		const { container } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign,
				onunassign,
			},
		});

		// Find the policy selector buttons (2 in desktop table, 2 in mobile cards = 4)
		const buttons = container.querySelectorAll('.policy-selector-btn');
		expect(buttons.length).toBe(4);

		// Click the desktop button for Smart-TV (unassigned, index 1)
		await fireEvent.click(buttons[1]);

		// Portal menu is rendered in document.body
		const portalMenu = document.getElementById('device-policy-dropdown-portal');
		expect(portalMenu).toBeTruthy();

		// Items inside dropdown
		const items = portalMenu!.querySelectorAll('.dropdown-item');
		expect(items.length).toBeGreaterThanOrEqual(2);

		// Click on Policy0 (Тестовая 2)
		await fireEvent.click(items[1]);

		// onassign should be called with Smart-TV MAC and Policy0
		expect(onassign).toHaveBeenCalledWith('aa:bb:cc:dd:ee:02', 'Policy0');
	});

	it('has proper responsive class attributes for desktop and mobile segregation', () => {
		const { container } = render(DeviceMatrix, {
			props: {
				accessPolicies: mockPolicies,
				policyDevices: mockDevices,
				onassign: vi.fn(),
				onunassign: vi.fn(),
			},
		});

		// Desktop table has desktop-only class
		const desktopTable = container.querySelector('.matrix-table-container.desktop-only');
		expect(desktopTable).toBeTruthy();

		// Mobile card list has mobile-only and mobile-cards-list classes
		const mobileCardList = container.querySelector('.mobile-cards-list.mobile-only');
		expect(mobileCardList).toBeTruthy();
		expect(mobileCardList?.classList.contains('mobile-card-list-view')).toBe(true);
	});
});
