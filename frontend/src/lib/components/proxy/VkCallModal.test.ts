import { describe, it, expect, vi, beforeEach } from 'vitest';

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
			dispatchEvent: () => false
		})
	});
});

const apiMock = vi.hoisted(() => ({
	getVKCallsConfig: vi.fn(),
	generateVKCalls: vi.fn(),
	checkVKCalls: vi.fn(),
	saveVKCallsConfig: vi.fn()
}));
vi.mock('$lib/api/client', () => ({ api: apiMock }));

const notify = vi.hoisted(() => ({
	success: vi.fn(),
	error: vi.fn(),
	warning: vi.fn(),
	info: vi.fn()
}));
vi.mock('$lib/stores/notifications', () => ({ notifications: notify }));

import { fireEvent, render, waitFor } from '@testing-library/svelte';
import VkCallModal from './VkCallModal.svelte';

describe('VkCallModal', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		apiMock.getVKCallsConfig.mockResolvedValue({
			hasToken: true,
			maskedToken: 'vk1.a.saved***',
			groupId: 0
		});
		apiMock.generateVKCalls.mockResolvedValue({
			success: true,
			links: ['https://vk.ru/call/join/test_hash_1', 'https://vk.ru/call/join/test_hash_2'],
			hashes: ['test_hash_1', 'test_hash_2']
		});
		apiMock.checkVKCalls.mockResolvedValue({
			results: [
				{ link: 'https://vk.ru/call/join/test_hash_1', hash: 'test_hash_1', alive: true }
			]
		});
	});

	it('renders modal when open and loads saved config', async () => {
		const { getByText } = render(VkCallModal, {
			open: true,
			onApply: () => {}
		});

		expect(getByText('VK Calls — ссылки и хеши')).toBeTruthy();
		await waitFor(() => {
			expect(apiMock.getVKCallsConfig).toHaveBeenCalled();
		});
		expect(getByText('vk1.a.saved***')).toBeTruthy();
	});

	it('generates links and applies them', async () => {
		const onApply = vi.fn();
		const { getByText } = render(VkCallModal, {
			open: true,
			targetFormat: 'links',
			onApply
		});

		await waitFor(() => {
			expect(apiMock.getVKCallsConfig).toHaveBeenCalled();
		});

		const genBtn = getByText('Сгенерировать ссылки VK Calls');
		await waitFor(() => {
			expect(genBtn.closest('button')?.disabled).toBe(false);
		});
		await fireEvent.click(genBtn);

		await waitFor(() => {
			expect(apiMock.generateVKCalls).toHaveBeenCalledWith(
				expect.objectContaining({ count: 2 })
			);
		});

		await waitFor(() => {
			expect(getByText('Вставить в клиент')).toBeTruthy();
		});

		const applyBtn = getByText('Вставить в клиент');
		await fireEvent.click(applyBtn);

		expect(onApply).toHaveBeenCalledWith(
			'https://vk.ru/call/join/test_hash_1,https://vk.ru/call/join/test_hash_2'
		);
	});

	it('applies hashes when targetFormat is hashes', async () => {
		const onApply = vi.fn();
		const { getByText } = render(VkCallModal, {
			open: true,
			targetFormat: 'hashes',
			onApply
		});

		await waitFor(() => {
			expect(apiMock.getVKCallsConfig).toHaveBeenCalled();
		});

		const genBtn = getByText('Сгенерировать ссылки VK Calls');
		await waitFor(() => {
			expect(genBtn.closest('button')?.disabled).toBe(false);
		});
		await fireEvent.click(genBtn);

		await waitFor(() => {
			expect(getByText('Вставить в клиент')).toBeTruthy();
		});

		const applyBtn = getByText('Вставить в клиент');
		await fireEvent.click(applyBtn);

		expect(onApply).toHaveBeenCalledWith('test_hash_1,test_hash_2');
	});

	it('checks links on check tab', async () => {
		const { getByText, getByRole } = render(VkCallModal, {
			open: true,
			initialValue: 'https://vk.ru/call/join/test_hash_1',
			onApply: () => {}
		});

		const checkTabBtn = getByText('Проверка ссылок');
		await fireEvent.click(checkTabBtn);

		const checkBtn = getByText('Проверить ссылки');
		await fireEvent.click(checkBtn);

		await waitFor(() => {
			expect(apiMock.checkVKCalls).toHaveBeenCalledWith(['https://vk.ru/call/join/test_hash_1']);
		});

		await waitFor(() => {
			expect(getByText('Активен')).toBeTruthy();
		});
	});
});
