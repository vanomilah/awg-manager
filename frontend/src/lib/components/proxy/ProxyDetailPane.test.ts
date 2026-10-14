// Мастер подменял деталь у ненастроенного инстанса: клик по строке уводил в
// мастер, параметры были недоступны, и единственным входом в форму оказывался
// тумблер запуска — инстанс поднимался с пустым конфигом и падал.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { FreeTurnConfig, WdttConfig, WdttServerConfig } from '$lib/types';

vi.hoisted(() => {
	Object.defineProperty(globalThis, 'matchMedia', {
		writable: true,
		configurable: true,
		value: (query: string) => ({
			matches: false,
			media: query,
			onchange: null,
			addEventListener: () => {},
			removeEventListener: () => {},
			addListener: () => {},
			removeListener: () => {},
			dispatchEvent: () => false,
		}),
	});
});

const apiMock = vi.hoisted(() => ({
	ensureWdttWgTunnel: vi.fn().mockResolvedValue(undefined),
	ensureWdttRawTunnel: vi.fn().mockResolvedValue(undefined),
	importConfig: vi.fn(),
	getFreeTurnCaptchaStatus: vi.fn(),
	getSettings: vi.fn().mockResolvedValue({}),
	getWdttServerClients: vi.fn().mockResolvedValue({ clients: [] }),
	listAccessPolicies: vi.fn().mockResolvedValue([]),
	openfluxLogs: vi.fn().mockResolvedValue({ logs: '' }),
	openfluxStart: vi.fn().mockResolvedValue({}),
	openfluxStop: vi.fn().mockResolvedValue({}),
	openfluxSaveConfig: vi.fn().mockResolvedValue({}),
	openfluxClientLogs: vi.fn().mockResolvedValue({ logs: '' }),
	openfluxClientStart: vi.fn().mockResolvedValue({}),
	openfluxClientStop: vi.fn().mockResolvedValue({}),
	openfluxSaveClientConfig: vi.fn().mockResolvedValue({}),
	mihomoNativeProxies: vi.fn().mockResolvedValue([]),
}));
vi.mock('$lib/api/client', () => ({ api: apiMock }));

const notify = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }));
vi.mock('$lib/stores/notifications', () => ({ notifications: notify }));

import { render, waitFor } from '@testing-library/svelte';
import ProxyDetailPane from './ProxyDetailPane.svelte';
import type { ProxyInstanceRow } from './rows';

/** Сервер ровно в том состоянии, что привёз стенд: заведён, пароля нет. */
const UNCONFIGURED: WdttServerConfig = {
	listen: '0.0.0.0:56002',
	wgPort: 56001,
	natMode: 'full',
	relayMode: 'wg',
	openFirewall: true,
};

const shareRow: ProxyInstanceRow = {
	key: 'wdtt:server:default',
	id: 'default',
	protocol: 'wdtt',
	role: 'server',
	name: 'Сервер',
	state: 'stopped',
	autostart: false,
	orphanedPid: false,
	binaryPresent: true,
	mode: 'wg',
};

function mount(wizard: 'new' | 'instance' | null) {
	const wdttConfig = {
		clients: [],
		servers: [{ id: 'default', name: 'Сервер', config: { ...UNCONFIGURED } }],
	} as unknown as WdttConfig;
	const ftConfig = { clients: [], servers: [] } as unknown as FreeTurnConfig;
	return render(ProxyDetailPane, {
		props: {
			activeTab: 'share' as const,
			exitRow: null,
			exitKey: null,
			shareRow,
			shareKey: shareRow.key,
			wdttConfig,
			ftConfig,
			wdttStatus: null,
			ftStatus: null,
			policies: [],
			tunnels: [],
			busyKeys: [],
			exitWizard: null,
			shareWizard: wizard,
			ontoggle: async () => {},
			onstatuses: async () => {},
			onconfigs: async () => {},
			onreload: async () => {},
			onexitdone: () => {},
			onsharedone: () => {},
		},
	});
}

describe('ProxyDetailPane: мастер открывается только явно', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ненастроенный сервер показывает деталь, а не мастер', async () => {
		const { queryByRole, findByRole } = mount(null);
		// Заголовок детали — имя инстанса; мастер назвался бы «Настроить раздачу».
		expect(await findByRole('heading', { name: 'Сервер', level: 2 })).toBeTruthy();
		expect(queryByRole('heading', { name: 'Настроить раздачу' })).toBeNull();
	});

	it('поля пароля владельца в детали нет — оно техническое', async () => {
		// Пароль владельца форку не обязателен (`serverWrapKeys.Count() == 0`
		// роняет старт только при полном отсутствии паролей), а панель им не
		// пользуется: абонентов она пишет в passwords.json сама.
		const { findByRole, queryByLabelText } = mount(null);
		await findByRole('heading', { name: 'Сервер', level: 2 });
		expect(queryByLabelText('Главный пароль')).toBeNull();
	});

	it('явно открытый мастер деталь подменяет', async () => {
		const { findByRole } = mount('instance');
		expect(await findByRole('heading', { name: 'Настроить раздачу' })).toBeTruthy();
	});

	it('мастер существующего сервера не спрашивает протокол заново', async () => {
		// Шаг «Протокол» для заведённого инстанса — вопрос о решённом: сменить
		// протокол существующего сервера нельзя. Активный шаг помечен классом
		// `.active` в списке шагов (WizardSteps).
		const { container, findByRole } = mount('instance');
		await findByRole('heading', { name: 'Настроить раздачу' });
		await waitFor(() => {
			const active = container.querySelector('.step.active');
			expect(active?.textContent).toContain('Параметры сервера');
		});
	});

	it('мастер НОВОГО сервера начинается с протокола', async () => {
		// Обратная страховка: пропуск первого шага касается только существующего
		// инстанса — у нового протокол ещё не выбран.
		const { container, findByRole } = mount('new');
		await findByRole('heading', { name: 'Настроить раздачу' });
		await waitFor(() => {
			const active = container.querySelector('.step.active');
			expect(active?.textContent).toContain('Протокол');
		});
	});

	it('при выборе строки openflux на вкладке раздачи монтируется OpenFluxDetail с секцией «Куда идёт трафик» и настройками', async () => {
		const ofRow: ProxyInstanceRow = {
			key: 'openflux:server:default',
			id: 'default',
			protocol: 'openflux',
			role: 'server',
			name: 'OpenFlux',
			state: 'stopped',
			autostart: false,
			orphanedPid: false,
			binaryPresent: true,
		};
		const { findByRole, findByText, getByText } = render(ProxyDetailPane, {
			props: {
				activeTab: 'share' as const,
				exitRow: null,
				exitKey: null,
				shareRow: ofRow,
				shareKey: ofRow.key,
				wdttConfig: null,
				ftConfig: null,
				openfluxConfig: {
					enabled: false,
					mode: 'l4',
					transportMode: 'session',
					transports: ['direct'],
					openFirewall: true,
					debug: 0,
					name: 'OpenFlux',
					natMode: 'full',
				},
				openfluxStatus: {
					installed: true,
					running: false,
					archSupported: true,
					binary: '/opt/bin/openflux',
					arch: 'aarch64',
				},
				wdttStatus: null,
				ftStatus: null,
				policies: [{ id: 'Policy0', name: 'Основная' }],
				tunnels: [],
				busyKeys: [],
				exitWizard: null,
				shareWizard: null,
				ontoggle: async () => {},
				onstatuses: async () => {},
				onconfigs: async () => {},
				onreload: async () => {},
				onexitdone: () => {},
				onsharedone: () => {},
			},
		});
		expect(await findByRole('heading', { name: 'OpenFlux', level: 2 })).toBeTruthy();
		expect(await findByText('Куда идёт трафик')).toBeTruthy();
		expect(await findByText('Полный')).toBeTruthy();

		// Нажатие кнопки Настройки открывает SideDrawer с секцией Сеть
		const settingsBtn = getByText('Настройки');
		settingsBtn.click();
		expect(await findByText('Настройки раздачи')).toBeTruthy();
		expect(await findByText('Режим NAT')).toBeTruthy();
	});

	it('при выборе строки openflux на вкладке выхода монтируется OpenFluxClientDetail', async () => {
		const ofClientRow: ProxyInstanceRow = {
			key: 'openflux:client:default',
			id: 'default',
			protocol: 'openflux',
			role: 'client',
			name: 'OpenFlux Client',
			state: 'stopped',
			autostart: false,
			orphanedPid: false,
			binaryPresent: true,
		};
		const { findByRole, findByText, queryByText } = render(ProxyDetailPane, {
			props: {
				activeTab: 'exit' as const,
				exitRow: ofClientRow,
				exitKey: ofClientRow.key,
				shareRow: null,
				shareKey: null,
				wdttConfig: null,
				ftConfig: null,
				openfluxClientConfig: {
					enabled: false,
					name: 'OpenFlux Client',
					transportMode: 'session',
					transports: ['direct'],
					socks5Listen: '127.0.0.1:1085',
					debug: 0,
					autoReconnect: true,
				},
				openfluxClientStatus: {
					installed: true,
					running: false,
					socks5Port: 1085,
					socks5Uri: 'socks5://127.0.0.1:1085#OpenFlux',
				},
				wdttStatus: null,
				ftStatus: null,
				policies: [],
				tunnels: [],
				busyKeys: [],
				exitWizard: null,
				shareWizard: null,
				ontoggle: async () => {},
				onstatuses: async () => {},
				onconfigs: async () => {},
				onreload: async () => {},
				onexitdone: () => {},
				onsharedone: () => {},
			},
		});
		expect(await findByRole('heading', { name: 'OpenFlux Client', level: 2 })).toBeTruthy();
		expect(await findByText('OpenFlux-клиент')).toBeTruthy();
		// Без правок кнопка «Отменить» не должна отображаться даже при omitempty-полях
		expect(queryByText('Отменить')).toBeNull();
	});

	it('OpenFluxClientDetail: распознаёт прокси в Mihomo по nativeConfig и сохраняет client.config (не перезатирая серверным config)', async () => {
		apiMock.mihomoNativeProxies.mockResolvedValueOnce([
			{
				id: 'p1',
				name: 'OpenFlux-Node',
				protocol: 'socks5',
				transport: 'tcp',
				enginePreference: 'mihomo',
				selectedEngine: 'mihomo',
				rawUri: 'socks5://127.0.0.1:1085#OpenFlux-Node',
				nativeConfig: {
					name: 'OpenFlux-Node',
					type: 'socks5',
					server: '127.0.0.1',
					port: 1085,
				},
				compatibility: {
					'sing-box': { supported: true },
					mihomo: { supported: true },
				},
				enabled: true,
				bridge: {
					listenPort: 51100,
					proxyIndex: 0,
					proxyInterface: 'Proxy0',
					kernelInterface: 'prs0',
				},
			},
		]);
		apiMock.openfluxSaveClientConfig.mockResolvedValueOnce({
			installed: true,
			running: true,
			archSupported: true,
			binary: '/opt/bin/openflux',
			arch: 'aarch64',
			// Серверный конфиг имеет другое имя ("KeeneticServer") — он НЕ должен перезаписать draft клиента!
			config: {
				enabled: true,
				mode: 'l4',
				transportMode: 'session',
				transports: ['direct'],
				openFirewall: true,
				debug: 0,
				name: 'KeeneticServer',
			},
			client: {
				installed: true,
				running: false,
				socks5Port: 1085,
				config: {
					enabled: false,
					name: 'MyClientProfile',
					transportMode: 'session',
					transports: ['direct'],
					directDial: '203.0.113.10:8445',
					socks5Listen: '127.0.0.1:1085',
					debug: 0,
					autoReconnect: true,
				},
			},
		});

		const ofClientRow: ProxyInstanceRow = {
			key: 'openflux:client:default',
			id: 'default',
			protocol: 'openflux',
			role: 'client',
			name: 'MyClientProfile',
			state: 'stopped',
			autostart: false,
			orphanedPid: false,
			binaryPresent: true,
		};
		const { findByText, getByText } = render(ProxyDetailPane, {
			props: {
				activeTab: 'exit' as const,
				exitRow: ofClientRow,
				exitKey: ofClientRow.key,
				shareRow: null,
				shareKey: null,
				wdttConfig: null,
				ftConfig: null,
				openfluxClientConfig: {
					enabled: false,
					name: 'MyClientProfile',
					transportMode: 'session',
					transports: ['direct'],
					directDial: '203.0.113.10:8445',
					socks5Listen: '127.0.0.1:1085',
					debug: 0,
					autoReconnect: true,
				},
				openfluxClientStatus: {
					installed: true,
					running: false,
					socks5Port: 1085,
				},
				wdttStatus: null,
				ftStatus: null,
				policies: [],
				tunnels: [],
				busyKeys: [],
				exitWizard: null,
				shareWizard: null,
				ontoggle: async () => {},
				onstatuses: async () => {},
				onconfigs: async () => {},
				onreload: async () => {},
				onexitdone: () => {},
				onsharedone: () => {},
			},
		});

		expect(await findByText('Подключен в маршрутизаторе: OpenFlux-Node')).toBeTruthy();
		expect(await findByText('Мост: Proxy0')).toBeTruthy();

		const settingsBtn = getByText('Настройки');
		settingsBtn.click();
		const saveBtn = await findByText('Сохранить настройки');
		saveBtn.click();
		await waitFor(() => {
			expect(apiMock.openfluxSaveClientConfig).toHaveBeenCalledTimes(1);
			expect(notify.success).toHaveBeenCalledWith('Настройки клиента OpenFlux сохранены');
		});
	});
});

