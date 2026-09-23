import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SusaninAdaptiveTab from './SusaninAdaptiveTab.svelte';
import { api } from '$lib/api/client';

vi.mock('$lib/api/client', () => ({
	api: {
		getAdaptiveRoutingStatus: vi.fn().mockResolvedValue({
			settings: {
				enabled: false,
				routingTableId: 105,
				fwmarkMask: '0x30000000',
				fwmarkTest: '0x10000000',
				fwmarkOk: '0x20000000',
				rulePriorityTest: 96,
				rulePriorityOk: 95,
				source: { type: 'all_lan' },
				primaryEgress: { kind: 'mihomo-group', resourceId: 'grp-1', engine: 'mihomo' },
				failurePolicy: 'direct',
				detection: {
					fastIntervalSeconds: 1,
					softIntervalSeconds: 1,
					judgeIntervalSeconds: 1,
					healthIntervalSeconds: 5,
					tcpSynRetries: 2,
					lateStallBytes: 1500,
				},
				persistence: {
					okTtlSeconds: 0,
					maxEntries: 4096,
					separateTcpUdp: true,
				},
				alwaysFileEnabled: true,
				neverFileEnabled: true,
				alwaysEntries: ['custom-always.com'],
				neverEntries: ['bank.ru'],
			},
			state: {
				appliedGeneration: 'gen-1',
				routingOwner: 'susanin',
				status: 'running',
				learnedTcpCount: 14,
				learnedUdpCount: 5,
				testingTcpCount: 2,
				testingUdpCount: 1,
				alwaysCount: 1,
				neverCount: 1,
				lastReconcile: '2026-09-22T20:00:00Z',
			},
		}),
		getAdaptiveRoutingEgresses: vi.fn().mockResolvedValue({
			items: [
				{
					ref: { kind: 'mihomo-group', resourceId: 'grp-1', engine: 'mihomo' },
					displayName: 'Быстрый прокси',
					interface: 'awgsus0',
					capabilities: { tcp: true, udp: true, icmp: false, ipv4: true, ipv6: false },
					available: true,
				},
			],
		}),
		getAdaptiveRoutingLearned: vi.fn().mockResolvedValue({
			always: ['custom-always.com'],
			never: ['bank.ru'],
			okTcpCount: 14,
			okUdpCount: 5,
			testTcpCount: 2,
			testUdpCount: 1,
		}),
		applyAdaptiveRouting: vi.fn().mockResolvedValue({
			settings: { enabled: true },
			state: { status: 'running', routingOwner: 'susanin' },
		}),
		startAdaptiveRouting: vi.fn().mockResolvedValue({
			state: { status: 'running', routingOwner: 'susanin' },
		}),
		stopAdaptiveRouting: vi.fn().mockResolvedValue({
			state: { status: 'stopped', routingOwner: 'none' },
		}),
		clearAdaptiveRoutingCache: vi.fn().mockResolvedValue({ cleared: true }),
		testAdaptiveRoutingEgress: vi.fn().mockResolvedValue({
			available: true,
			interface: 'awgsus0',
		}),
	},
}));

describe('SusaninAdaptiveTab', () => {
	it('renders status bar with running state and routing owner badge', async () => {
		render(SusaninAdaptiveTab);

		expect(await screen.findByText('Susanin работает')).toBeDefined();
		expect(screen.getByText('Владелец сети: Susanin')).toBeDefined();
		expect(screen.getByText('Direct Fail-Open')).toBeDefined();
	});

	it('renders 3 main configuration cards', async () => {
		render(SusaninAdaptiveTab);

		expect(await screen.findByText('Источник трафика')).toBeDefined();
		expect(screen.getByText('Выход для обхода')).toBeDefined();
		expect(screen.getByText('Поведение при сбое')).toBeDefined();
	});

	it('displays learning statistics tiles', async () => {
		render(SusaninAdaptiveTab);

		expect(await screen.findByText('14')).toBeDefined();
		expect(screen.getByText('Изучено TCP (ОК)')).toBeDefined();
		expect(screen.getByText('Изучено UDP (ОК)')).toBeDefined();
		expect(screen.getByText('На проверке (Testing)')).toBeDefined();
		expect(screen.getByText('5')).toBeDefined();
	});

	it('keeps low-level routing marks out of the user interface', async () => {
		render(SusaninAdaptiveTab);

		const expertBtn = await screen.findByText('Расширенные сетевые параметры (Expert)');
		await fireEvent.click(expertBtn);

		expect(screen.queryByText('Сетевая таблица ядра и Fwmark')).toBeNull();
		expect(screen.getByText('Всегда через прокси-выход (Always)')).toBeDefined();
	});
});
