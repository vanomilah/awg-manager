import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import IntegrationsCard from './IntegrationsCard.svelte';
import type { SingboxStatus } from '$lib/types';

const status = (installed: boolean): SingboxStatus =>
	({ installed, running: false, version: '1.14.0' }) as SingboxStatus;

const baseProps = {
	singboxStatus: null,
	hydraStatus: null,
	singboxInstalling: false,
	singboxInstallError: null,
	oninstallSingbox: vi.fn(),
	showHydra: false,
	showMihomo: false,
	showXray: false,
};

describe('IntegrationsCard — удаление sing-box', () => {
	it('не установлен — предлагается установка, удалять нечего', () => {
		render(IntegrationsCard, {
			...baseProps,
			singboxStatus: status(false),
			onuninstallSingbox: vi.fn(),
		});
		expect(screen.queryByText('Установить')).not.toBeNull();
		expect(screen.queryByText('Удалить')).toBeNull();
	});

	it('установлен — кнопка удаления рядом с «Открыть»', () => {
		render(IntegrationsCard, {
			...baseProps,
			singboxStatus: status(true),
			onuninstallSingbox: vi.fn(),
		});
		expect(screen.queryByText('Открыть')).not.toBeNull();
		expect(screen.queryByText('Удалить')).not.toBeNull();
	});

	it('удаление идёт только после подтверждения', async () => {
		const onuninstallSingbox = vi.fn();
		render(IntegrationsCard, { ...baseProps, singboxStatus: status(true), onuninstallSingbox });

		await fireEvent.click(screen.getByText('Удалить'));
		expect(onuninstallSingbox).not.toHaveBeenCalled();
		expect(screen.queryByText('Удалить sing-box?')).not.toBeNull();

		await fireEvent.click(within(screen.getByRole('dialog')).getByText('Удалить'));
		expect(onuninstallSingbox).toHaveBeenCalledTimes(1);
	});

	it('без обработчика удаления кнопки нет (старый вызов карточки)', () => {
		render(IntegrationsCard, { ...baseProps, singboxStatus: status(true) });
		expect(screen.queryByText('Удалить')).toBeNull();
	});
});

describe('IntegrationsCard — Mihomo', () => {
	it('не установлен — отображается кнопка Установить', () => {
		render(IntegrationsCard, {
			...baseProps,
			showSingbox: false,
			showMihomo: true,
			mihomoStatus: { installed: false, running: false, engine: 'mihomo', pid: 0, binary: '', error: '' },
			oninstallMihomo: vi.fn(),
		});
		expect(screen.queryByText('Установить')).not.toBeNull();
		expect(screen.queryByText('Mihomo')).not.toBeNull();
	});

	it('установлен — отображаются кнопки Открыть и Удалить', () => {
		render(IntegrationsCard, {
			...baseProps,
			showSingbox: false,
			showMihomo: true,
			mihomoStatus: { installed: true, running: true, engine: 'mihomo', pid: 1234, binary: '/opt/bin/mihomo', error: '', version: '1.19.29' },
			onuninstallMihomo: vi.fn(),
		});
		expect(screen.queryByText('Открыть')).not.toBeNull();
		expect(screen.queryByText('Удалить')).not.toBeNull();
	});
});

describe('IntegrationsCard — Xray', () => {
	it('не установлен — отображается кнопка Установить', () => {
		render(IntegrationsCard, {
			...baseProps,
			showSingbox: false,
			showXray: true,
			xrayStatus: { installed: false, running: false, pid: 0, version: '', port: 9009, path: '/cdn-bridge', uuid: 'test-uuid', cdnHost: '', uplinkHTTPMethod: 'PUT' },
			oninstallXray: vi.fn(),
		});
		expect(screen.queryByText('Установить')).not.toBeNull();
		expect(screen.queryByText('Xray-core')).not.toBeNull();
	});

	it('установлен и запущен — отображаются кнопки Открыть и Удалить', async () => {
		const onuninstallXray = vi.fn();
		render(IntegrationsCard, {
			...baseProps,
			showSingbox: false,
			showXray: true,
			xrayStatus: {
				installed: true,
				running: true,
				pid: 1294,
				version: '26.7.28',
				port: 9008,
				source: 'managed',
				canUninstall: true,
			},
			onuninstallXray,
		});
		expect(screen.queryByText('Открыть')).not.toBeNull();
		expect(screen.queryByText('Удалить')).not.toBeNull();

		await fireEvent.click(screen.getByText('Удалить'));
		expect(onuninstallXray).not.toHaveBeenCalled();
		expect(screen.queryByText('Удалить Xray-core?')).not.toBeNull();

		await fireEvent.click(within(screen.getByRole('dialog')).getByText('Удалить'));
		expect(onuninstallXray).toHaveBeenCalledTimes(1);
	});

	it('внешний бинарник — кнопка Удалить заблокирована', () => {
		render(IntegrationsCard, {
			...baseProps,
			showSingbox: false,
			showXray: true,
			xrayStatus: {
				installed: true,
				running: false,
				pid: 0,
				version: '26.7.28',
				port: 9008,
				source: 'external',
				canUninstall: false,
				blockers: ['Внешний бинарник не управляется пакетом'],
			},
			onuninstallXray: vi.fn(),
		});
		const deleteBtn = screen.getByText('Удалить').closest('button');
		expect(deleteBtn?.disabled).toBe(true);
	});
});
