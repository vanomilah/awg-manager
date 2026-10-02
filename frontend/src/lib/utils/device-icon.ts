import {
	Tv,
	Smartphone,
	Tablet,
	Laptop,
	Monitor,
	Gamepad2,
	Speaker,
	Camera,
	Printer,
	Wifi,
	Cpu,
	HelpCircle,
} from 'lucide-svelte';
import type { PolicyDevice } from '$lib/types';

export type DeviceCategory =
	| 'tv'
	| 'phone'
	| 'tablet'
	| 'laptop'
	| 'pc'
	| 'gaming'
	| 'speaker'
	| 'camera'
	| 'printer'
	| 'network'
	| 'iot'
	| 'other';

export interface DeviceTypeInfo {
	category: DeviceCategory;
	label: string;
	icon: any;
}

export function detectDeviceCategory(device: PolicyDevice): DeviceCategory {
	const text = `${device.name || ''} ${device.hostname || ''}`.toLowerCase();

	// TV & Media streaming
	if (
		text.includes('tv') ||
		text.includes('appletv') ||
		text.includes('androidtv') ||
		text.includes('webos') ||
		text.includes('tizen') ||
		text.includes('bravia') ||
		text.includes('roku') ||
		text.includes('chromecast') ||
		text.includes('firetv') ||
		text.includes('mibox') ||
		text.includes('shield')
	) {
		return 'tv';
	}

	// Gaming consoles
	if (
		text.includes('playstation') ||
		text.includes('ps4') ||
		text.includes('ps5') ||
		text.includes('xbox') ||
		text.includes('nintendo') ||
		text.includes('switch') ||
		text.includes('steamdeck')
	) {
		return 'gaming';
	}

	// Laptops & Notebooks
	if (
		text.includes('thinkpad') ||
		text.includes('ideapad') ||
		text.includes('macbook') ||
		text.includes('laptop') ||
		text.includes('notebook') ||
		text.includes('zenbook') ||
		text.includes('vivobook') ||
		text.includes('chromebook')
	) {
		return 'laptop';
	}

	// Desktop PCs
	if (
		text.includes('desktop') ||
		text.includes('workstation') ||
		text.includes('imac') ||
		text.includes('mac-mini') ||
		text.includes('macmini') ||
		text.includes('macpro') ||
		text.includes('mac-pro') ||
		text.includes('windows') ||
		text.includes('linux') ||
		/\bpc\b/.test(text) ||
		text.endsWith('-pc') ||
		text.includes('-pc-') ||
		text.startsWith('pc-')
	) {
		return 'pc';
	}

	// Tablets
	if (
		text.includes('ipad') ||
		text.includes('tablet') ||
		/\btab\b/.test(text) ||
		text.includes('galaxy-tab') ||
		text.includes('galaxy tab') ||
		text.includes('mi pad') ||
		text.includes('mipad')
	) {
		return 'tablet';
	}

	// Mobile phones
	if (
		text.includes('iphone') ||
		text.includes('phone') ||
		text.includes('galaxy') ||
		text.includes('pixel') ||
		text.includes('xiaomi') ||
		text.includes('redmi') ||
		text.includes('poco') ||
		text.includes('huawei') ||
		text.includes('honor') ||
		text.includes('oneplus') ||
		text.includes('oppo') ||
		text.includes('vivo') ||
		text.includes('realme') ||
		text.includes('android') ||
		text.includes('mobile')
	) {
		return 'phone';
	}


	// Smart speakers & audio
	if (
		text.includes('yandex') ||
		text.includes('station') ||
		text.includes('alisa') ||
		text.includes('sber') ||
		text.includes('marusia') ||
		text.includes('homepod') ||
		text.includes('echo') ||
		text.includes('alexa') ||
		text.includes('speaker') ||
		text.includes('sonos')
	) {
		return 'speaker';
	}

	// Surveillance & Cameras
	if (
		text.includes('camera') ||
		text.includes('cam') ||
		text.includes('doorbell') ||
		text.includes('nvr') ||
		text.includes('dvr') ||
		text.includes('ezviz') ||
		text.includes('hikvision') ||
		text.includes('dahua') ||
		text.includes('imou') ||
		text.includes('reolink')
	) {
		return 'camera';
	}

	// Printers
	if (
		text.includes('printer') ||
		text.includes('print') ||
		text.includes('canon') ||
		text.includes('epson') ||
		text.includes('brother') ||
		text.includes('xerox') ||
		text.includes('kyocera')
	) {
		return 'printer';
	}

	// Routers & Network equipment
	if (
		text.includes('router') ||
		text.includes('repeater') ||
		text.includes('extender') ||
		text.includes('switch') ||
		text.includes('keenetic') ||
		text.includes('mikrotik') ||
		text.includes('accesspoint') ||
		text.includes('ap')
	) {
		return 'network';
	}

	// IoT & Smart Home sensors
	if (
		text.includes('esp32') ||
		text.includes('esp8266') ||
		text.includes('arduino') ||
		text.includes('tasmota') ||
		text.includes('shelly') ||
		text.includes('zigbee') ||
		text.includes('tuya') ||
		text.includes('aqara')
	) {
		return 'iot';
	}

	return 'other';
}

export const DEVICE_CATEGORY_CONFIG: Record<DeviceCategory, { label: string; icon: any }> = {
	tv: { label: 'Телевизор / Медиа', icon: Tv },
	phone: { label: 'Смартфон', icon: Smartphone },
	tablet: { label: 'Планшет', icon: Tablet },
	laptop: { label: 'Ноутбук', icon: Laptop },
	pc: { label: 'Компьютер', icon: Monitor },
	gaming: { label: 'Игровая консоль', icon: Gamepad2 },
	speaker: { label: 'Умная колонка', icon: Speaker },
	camera: { label: 'Камера наблюдения', icon: Camera },
	printer: { label: 'Принтер', icon: Printer },
	network: { label: 'Сетевое устройство', icon: Wifi },
	iot: { label: 'Умный дом / IoT', icon: Cpu },
	other: { label: 'Устройство', icon: HelpCircle },
};

export function getDeviceTypeInfo(device: PolicyDevice): DeviceTypeInfo {
	const category = detectDeviceCategory(device);
	const conf = DEVICE_CATEGORY_CONFIG[category];
	return {
		category,
		label: conf.label,
		icon: conf.icon,
	};
}
