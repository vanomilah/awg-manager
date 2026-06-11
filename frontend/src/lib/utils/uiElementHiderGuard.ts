export const UI_ELEMENT_HIDER_PROTECTED_ATTR = 'data-awg-ui-protected';

const PROTECTED_ROOT_TAGS = new Set(['HTML', 'BODY', 'HEAD', 'MAIN']);

/** Нельзя скрывать сам узел, его предков в protected-зоне и обёртки над protected-зонами. */
export function canHideElement(el: Element): boolean {
	const tag = el.tagName;
	if (PROTECTED_ROOT_TAGS.has(tag)) return false;
	if (el.closest(`[${UI_ELEMENT_HIDER_PROTECTED_ATTR}]`)) return false;
	if (el.querySelector(`[${UI_ELEMENT_HIDER_PROTECTED_ATTR}]`)) return false;
	return true;
}

/** Правило из localStorage не применяем, если оно бьёт в protected-зону. */
export function isSelectorHideProtected(selector: string): boolean {
	if (typeof document === 'undefined') return false;
	try {
		const matches = document.querySelectorAll(selector);
		if (matches.length === 0) return false;
		for (const el of matches) {
			if (!canHideElement(el)) return true;
		}
		return false;
	} catch {
		return true;
	}
}
