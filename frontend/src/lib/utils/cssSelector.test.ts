import { describe, it, expect, beforeEach } from 'vitest';
import { buildCssSelector, buildElementLabel } from './cssSelector';

describe('cssSelector', () => {
	beforeEach(() => {
		document.body.innerHTML = '';
	});

	it('buildCssSelector prefers unique id', () => {
		document.body.innerHTML = '<div id="only-one">x</div>';
		const el = document.getElementById('only-one')!;
		expect(buildCssSelector(el)).toBe('#only-one');
	});

	it('buildCssSelector builds nested path', () => {
		document.body.innerHTML = '<section class="card"><div class="row">text</div></section>';
		const el = document.querySelector('.row')!;
		const selector = buildCssSelector(el);
		expect(document.querySelectorAll(selector).length).toBe(1);
	});

	it('buildElementLabel includes tag and text', () => {
		document.body.innerHTML = '<button class="save">Сохранить</button>';
		const el = document.querySelector('button')!;
		expect(buildElementLabel(el)).toContain('button');
		expect(buildElementLabel(el)).toContain('Сохранить');
	});
});
