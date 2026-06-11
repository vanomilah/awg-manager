function escapeCssIdent(value: string): string {
	if (typeof CSS !== 'undefined' && typeof CSS.escape === 'function') {
		return CSS.escape(value);
	}
	return value.replace(/([ !"#$%&'()*+,./:;<=>?@[\\\]^`{|}~])/g, '\\$1');
}

/** Стабильный CSS-селектор для элемента (для пользовательского скрытия блоков UI). */
export function buildCssSelector(target: Element): string {
	if (target.id) {
		const escaped = escapeCssIdent(target.id);
		const byId = `#${escaped}`;
		if (document.querySelectorAll(byId).length === 1) return byId;
	}

	const parts: string[] = [];
	let el: Element | null = target;

	while (el && el.nodeType === Node.ELEMENT_NODE && el !== document.documentElement) {
		let part = el.tagName.toLowerCase();

		if (el.id) {
			const escaped = escapeCssIdent(el.id);
			const byId = `#${escaped}`;
			if (document.querySelectorAll(byId).length === 1) {
				parts.unshift(byId);
				break;
			}
		}

		const classes = Array.from(el.classList)
			.filter((c) => !c.startsWith('svelte-') && !c.startsWith('awg-ui-hider'))
			.slice(0, 2);
		if (classes.length > 0) {
			part += classes.map((c) => `.${escapeCssIdent(c)}`).join('');
		}

		const parent = el.parentElement;
		if (parent) {
			const siblings = Array.from(parent.children).filter((c) => c.tagName === el!.tagName);
			if (siblings.length > 1) {
				part += `:nth-of-type(${siblings.indexOf(el) + 1})`;
			}
		}

		parts.unshift(part);
		const selector = parts.join(' > ');
		try {
			if (document.querySelectorAll(selector).length === 1) return selector;
		} catch {
			// keep climbing
		}
		el = el.parentElement;
	}

	return parts.join(' > ');
}

export function buildElementLabel(el: Element): string {
	const tag = el.tagName.toLowerCase();
	const id = el.id ? `#${el.id}` : '';
	const classes = Array.from(el.classList)
		.filter((c) => !c.startsWith('svelte-'))
		.slice(0, 2)
		.join('.');
	const text = (el.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 48);
	const classPart = classes ? `.${classes}` : '';
	const textPart = text ? ` «${text}»` : '';
	return `${tag}${id}${classPart}${textPart}`.trim();
}
