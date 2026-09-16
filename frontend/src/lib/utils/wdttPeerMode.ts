import type { WdttClientConfig } from '$lib/types';

type ConnMode = 'wg' | 'raw';

function modeOf(c: WdttClientConfig): ConnMode {
	return c.connMode === 'raw' ? 'raw' : 'wg';
}

/**
 * У raw и wg разные порты сервера, поэтому адрес каждого режима живёт в своём
 * слоте (peerWg/peerRaw). Инвариант «peer = слот активного режима» держит
 * бэкенд (wdtt.normalizePeers), причём peer у него главнее слота — значит
 * редактирование активного слота обязано писать и в peer, иначе правку
 * затрёт при сохранении.
 */
export function setPeer(c: WdttClientConfig, value: string): void {
	c.peer = value;
	if (modeOf(c) === 'raw') c.peerRaw = value;
	else c.peerWg = value;
}

export function setPeerWg(c: WdttClientConfig, value: string): void {
	c.peerWg = value;
	if (modeOf(c) === 'wg') c.peer = value;
}

export function setPeerRaw(c: WdttClientConfig, value: string): void {
	c.peerRaw = value;
	if (modeOf(c) === 'raw') c.peer = value;
}

/**
 * Переключение режима подставляет адрес из слота нового режима. Пустой слот
 * даёт пустое поле — лучше, чем молча уехать на порт соседнего режима.
 * Бэкенд подставить не может: он не отличает смену режима пользователем от
 * connMode, приехавшего в подписке.
 */
export function switchConnMode(c: WdttClientConfig, next: ConnMode): void {
	setPeer(c, c.peer);
	c.connMode = next;
	c.peer = (next === 'raw' ? c.peerRaw : c.peerWg)?.trim() ?? '';
}

/**
 * Заполняет недостающий слот адреса по соглашению портов WDTT (Raw = DTLS + 1).
 */
export function syncPeerSlots(c: WdttClientConfig): void {
	if (c.peerWg && !c.peerRaw) {
		const idx = c.peerWg.lastIndexOf(':');
		if (idx > 0) {
			const host = c.peerWg.slice(0, idx);
			const port = Number(c.peerWg.slice(idx + 1));
			if (!isNaN(port) && port > 0) c.peerRaw = `${host}:${port + 1}`;
		}
	} else if (c.peerRaw && !c.peerWg) {
		const idx = c.peerRaw.lastIndexOf(':');
		if (idx > 0) {
			const host = c.peerRaw.slice(0, idx);
			const port = Number(c.peerRaw.slice(idx + 1));
			if (!isNaN(port) && port > 1) c.peerWg = `${host}:${port - 1}`;
		}
	}
}

/**
 * Применяет адреса из импортированного профиля/ссылки, гарантируя заполнение
 * обоих слотов (peerWg для DTLS и peerRaw для Raw).
 */
export function applyPayloadPeers(
	c: WdttClientConfig,
	payload: { peer?: string; peerWg?: string; peerRaw?: string; connMode?: 'wg' | 'raw' },
): void {
	if (payload.connMode === 'raw' || payload.connMode === 'wg') {
		c.connMode = payload.connMode;
	}
	if (payload.peerWg) c.peerWg = payload.peerWg;
	if (payload.peerRaw) c.peerRaw = payload.peerRaw;
	if (payload.peer) {
		if (modeOf(c) === 'raw') {
			if (!c.peerRaw) c.peerRaw = payload.peer;
		} else {
			if (!c.peerWg) c.peerWg = payload.peer;
		}
	}
	syncPeerSlots(c);
	c.peer = (modeOf(c) === 'raw' ? c.peerRaw : c.peerWg)?.trim() || payload.peer || '';
}
