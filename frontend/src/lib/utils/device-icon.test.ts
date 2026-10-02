import { describe, it, expect } from 'vitest';
import { detectDeviceCategory } from './device-icon';
import type { PolicyDevice } from '$lib/types';

function makeDevice(name: string, hostname = '', mac = 'aa:bb:cc:dd:ee:01'): PolicyDevice {
	return {
		mac,
		ip: '192.168.1.10',
		name,
		hostname,
		active: true,
		link: 'up',
		policy: '',
	};
}

describe('detectDeviceCategory', () => {
	it('detects TVs and media boxes', () => {
		expect(detectDeviceCategory(makeDevice('LG OLED TV'))).toBe('tv');
		expect(detectDeviceCategory(makeDevice('AppleTV-LivingRoom'))).toBe('tv');
		expect(detectDeviceCategory(makeDevice('', 'androidtv'))).toBe('tv');
		expect(detectDeviceCategory(makeDevice('MiBox S'))).toBe('tv');
	});

	it('detects gaming consoles', () => {
		expect(detectDeviceCategory(makeDevice('PlayStation 5'))).toBe('gaming');
		expect(detectDeviceCategory(makeDevice('Xbox-Series-X'))).toBe('gaming');
		expect(detectDeviceCategory(makeDevice('Nintendo Switch'))).toBe('gaming');
		expect(detectDeviceCategory(makeDevice('SteamDeck'))).toBe('gaming');
	});

	it('detects smartphones and tablets', () => {
		expect(detectDeviceCategory(makeDevice('iPhone 15 Pro'))).toBe('phone');
		expect(detectDeviceCategory(makeDevice('Galaxy S24'))).toBe('phone');
		expect(detectDeviceCategory(makeDevice('Xiaomi 13'))).toBe('phone');
		expect(detectDeviceCategory(makeDevice('iPad Air'))).toBe('tablet');
		expect(detectDeviceCategory(makeDevice('Galaxy Tab S9'))).toBe('tablet');
	});

	it('detects computers and laptops', () => {
		expect(detectDeviceCategory(makeDevice('MacBook Pro 16'))).toBe('laptop');
		expect(detectDeviceCategory(makeDevice('ThinkPad-X1'))).toBe('laptop');
		expect(detectDeviceCategory(makeDevice('Workstation-PC'))).toBe('pc');
		expect(detectDeviceCategory(makeDevice('iMac 27'))).toBe('pc');
	});

	it('detects smart speakers', () => {
		expect(detectDeviceCategory(makeDevice('Yandex Station Max'))).toBe('speaker');
		expect(detectDeviceCategory(makeDevice('SberBox-Speaker'))).toBe('speaker');
	});

	it('detects cameras', () => {
		expect(detectDeviceCategory(makeDevice('Ezviz-Camera-Yard'))).toBe('camera');
		expect(detectDeviceCategory(makeDevice('Hikvision-Cam'))).toBe('camera');
	});

	it('falls back to other for unknown devices', () => {
		expect(detectDeviceCategory(makeDevice('Unknown-Gadget'))).toBe('other');
	});
});
