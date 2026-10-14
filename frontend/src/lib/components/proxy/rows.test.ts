import { describe, it, expect } from 'vitest';
import type {
	WdttClientConfig,
	WdttClientInstance,
	WdttConfig,
	WdttInstanceStatus,
	WdttProcessStatus,
} from '$lib/types';
import { exitRows, rowKeyFromInstanceKey, shareRows } from './rows';

function instance(id: string, running: boolean, lastError?: string): WdttInstanceStatus {
	const status: WdttProcessStatus = { running, lastError, binary: 'wdtt', binaryPresent: true };
	return { id, name: id, status };
}

function client(id: string, enabled: boolean): WdttClientInstance {
	// Строка читает из конфига только enabled и connMode.
	return { id, name: id, config: { enabled } as unknown as WdttClientConfig };
}

function sources(status: WdttInstanceStatus[], config: WdttClientInstance[]) {
	const dead: WdttProcessStatus = { running: false, binary: '', binaryPresent: false };
	return {
		wdttStatus: {
			clients: status,
			servers: [],
			client: dead,
			server: dead,
			installAvailable: false,
			updateAvailable: false,
			installing: false,
		},
		wdttConfig: { clients: config, servers: [] } as unknown as WdttConfig,
		ftStatus: null,
		ftConfig: null,
	};
}

describe('exitRows: состояние строки', () => {
	it('живой процесс — «Запущен»', () => {
		const rows = exitRows(sources([instance('a', true)], [client('a', true)]));
		expect(rows[0].state).toBe('running');
	});

	it('должен работать, но не работает — «Не запускается»', () => {
		const rows = exitRows(sources([instance('a', false, 'connection refused')], [client('a', true)]));
		expect(rows[0].state).toBe('error');
	});

	it('после явного стопа — «Остановлен», даже если ошибка последнего запуска жива', () => {
		const rows = exitRows(
			sources([instance('a', false, 'connection refused')], [client('a', false)]),
		);
		expect(rows[0].state).toBe('stopped');
	});

	it('остановлен без ошибки — «Остановлен»', () => {
		const rows = exitRows(sources([instance('a', false)], [client('a', false)]));
		expect(rows[0].state).toBe('stopped');
	});
});

describe('rowKeyFromInstanceKey: ключ бэкенда → ключ строки', () => {
	it('клиент wdtt', () => {
		expect(rowKeyFromInstanceKey('wdtt-client:nl')).toEqual({
			key: 'wdtt:client:nl',
			role: 'client',
		});
	});

	it('клиент freeturn', () => {
		expect(rowKeyFromInstanceKey('freeturn-client:default')).toEqual({
			key: 'freeturn:client:default',
			role: 'client',
		});
	});

	it('клиент openflux', () => {
		expect(rowKeyFromInstanceKey('openflux-client:default')).toEqual({
			key: 'openflux:client:default',
			role: 'client',
		});
	});

	it('серверы всех протоколов', () => {
		expect(rowKeyFromInstanceKey('wdtt-server:default')).toEqual({
			key: 'wdtt:server:default',
			role: 'server',
		});
		expect(rowKeyFromInstanceKey('freeturn-server:s2')).toEqual({
			key: 'freeturn:server:s2',
			role: 'server',
		});
		expect(rowKeyFromInstanceKey('openflux-server:default')).toEqual({
			key: 'openflux:server:default',
			role: 'server',
		});
	});

	it('ключ строки совпадает с тем, что собирает exitRows', () => {
		const rows = exitRows(sources([instance('nl', true)], [client('nl', true)]));
		expect(rowKeyFromInstanceKey('wdtt-client:nl')?.key).toBe(rows[0].key);
	});

	it('неизвестный вид, пустой id и мусор — null', () => {
		expect(rowKeyFromInstanceKey('singbox-client:nl')).toBeNull();
		expect(rowKeyFromInstanceKey('wdtt-client:')).toBeNull();
		expect(rowKeyFromInstanceKey('wdtt-client')).toBeNull();
		expect(rowKeyFromInstanceKey('')).toBeNull();
	});

	it('двоеточие в id ключ не рвёт', () => {
		expect(rowKeyFromInstanceKey('wdtt-client:a:b')?.key).toBe('wdtt:client:a:b');
	});
});

describe('exitRows: поддержка OpenFlux-клиента', () => {
	it('добавляет openflux:client:default строку при наличии клиентского статуса или конфига', () => {
		const src = {
			wdttStatus: null,
			wdttConfig: null,
			ftStatus: null,
			ftConfig: null,
			openfluxStatus: {
				installed: true,
				running: false,
				archSupported: true,
				binary: '/opt/bin/openflux',
				arch: 'aarch64',
				client: {
					installed: true,
					running: true,
					pid: 4321,
					uptimeS: 120,
					socks5Port: 1085,
					socks5Uri: 'socks5://127.0.0.1:1085#VPS-OpenFlux',
					config: {
						enabled: true,
						name: 'VPS-OpenFlux',
						transportMode: 'session' as const,
						transports: ['direct', 'mailru'],
						socks5Listen: '127.0.0.1:1085',
						httpProxyListen: '127.0.0.1:1086',
						directDial: '203.0.113.10:8445',
						debug: 0,
						autoReconnect: true,
					},
				},
			},
		};
		const rows = exitRows(src);
		expect(rows).toHaveLength(1);
		expect(rows[0]).toMatchObject({
			key: 'openflux:client:default',
			id: 'default',
			protocol: 'openflux',
			role: 'client',
			name: 'VPS-OpenFlux',
			state: 'running',
			autostart: true,
			pid: 4321,
			binaryPresent: true,
		});
		expect(rows[0].flow).toEqual([
			{ label: 'Этот роутер', detail: 'SOCKS5 :1085' },
			{ label: '203.0.113.10', detail: 'Direct / Mail.ru' },
			{ label: 'Интернет', detail: 'через выход' },
		]);
	});

	it('не добавляет openflux:client строку если клиент удален (config=null и running=false)', () => {
		const src = {
			wdttStatus: null,
			wdttConfig: null,
			ftStatus: null,
			ftConfig: null,
			openfluxStatus: {
				installed: true,
				running: false,
				archSupported: true,
				binary: '/opt/bin/openflux',
				arch: 'aarch64',
				client: {
					installed: true,
					running: false,
					pid: 0,
					socks5Port: 1085,
					config: null,
				},
			},
			openfluxClientConfig: null,
		};
		const rows = exitRows(src);
		expect(rows).toHaveLength(0);
	});
});

describe('shareRows: поддержка OpenFlux', () => {
	it('добавляет openflux строку при наличии статуса или конфига', () => {
		const src = {
			wdttStatus: null,
			wdttConfig: null,
			ftStatus: null,
			ftConfig: null,
			openfluxStatus: {
				installed: true,
				running: true,
				pid: 1234,
				uptimeS: 60,
				archSupported: true,
				binary: '/opt/bin/openflux',
				arch: 'aarch64',
			},
			openfluxConfig: {
				enabled: true,
				mode: 'l4' as const,
				transportMode: 'session' as const,
				transports: ['direct', 'mailru'],
				directListen: '0.0.0.0:8445',
				openFirewall: true,
				debug: 0,
			},
		};
		const rows = shareRows(src);
		expect(rows).toHaveLength(1);
		expect(rows[0]).toMatchObject({
			key: 'openflux:server:default',
			id: 'default',
			protocol: 'openflux',
			role: 'server',
			name: 'OpenFlux',
			state: 'running',
			autostart: true,
			pid: 1234,
			binaryPresent: true,
		});
		expect(rows[0].flow).toEqual([
			{ label: 'Клиенты', detail: 'qThrone / Direct / Mail.ru' },
			{ label: 'Этот роутер', detail: 'L4 · :8445' },
			{ label: 'Интернет', detail: 'NAT: полный' },
		]);
	});

	it('openfluxServerFlow отражает egress-маршрутизацию, WAN для internet-only и политику', () => {
		const src = {
			wdttStatus: null,
			wdttConfig: null,
			ftStatus: null,
			ftConfig: null,
			openfluxConfig: {
				enabled: true,
				mode: 'l3' as const,
				transportMode: 'session' as const,
				transports: ['direct'],
				directListen: '0.0.0.0:8445',
				openFirewall: true,
				debug: 0,
				natMode: 'internet-only' as const,
				natStaticWan: 'ISP',
				egressInterface: 'ISP',
				policy: 'Policy1',
			},
		};
		const rows = shareRows(src as any);
		expect(rows[0].flow).toEqual([
			{ label: 'Клиенты', detail: 'qThrone / Direct' },
			{ label: 'Этот роутер', detail: 'L3 · :8445 → ISP' },
			{ label: 'Интернет', detail: 'NAT: ISP · Policy1' },
		]);
	});
});
