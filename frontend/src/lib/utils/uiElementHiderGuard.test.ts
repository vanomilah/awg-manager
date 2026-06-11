import { describe, it, expect, beforeEach } from 'vitest';
import {
	canHideElement,
	isSelectorHideProtected,
	UI_ELEMENT_HIDER_PROTECTED_ATTR,
} from './uiElementHiderGuard';

describe('uiElementHiderGuard', () => {
	beforeEach(() => {
		document.body.innerHTML = `
			<header class="app-header">
				<nav class="nav">
					<button type="button" class="tab">ТУННЕЛИ</button>
					<span ${UI_ELEMENT_HIDER_PROTECTED_ATTR}><button type="button" class="tab settings">НАСТРОЙКИ</button></span>
				</nav>
			</header>
			<main class="main">
				<div id="experimental-settings" ${UI_ELEMENT_HIDER_PROTECTED_ATTR}>
					<button type="button" class="toggle">toggle</button>
				</div>
				<div class="card">ok</div>
				<div class="wrapper">
					<div class="inner">nested</div>
				</div>
			</main>
		`;
	});

	it('blocks html, body and main', () => {
		expect(canHideElement(document.documentElement)).toBe(false);
		expect(canHideElement(document.body)).toBe(false);
		expect(canHideElement(document.querySelector('main')!)).toBe(false);
	});

	it('allows ordinary nav items but blocks settings tab', () => {
		const tunnels = document.querySelector('.tab:not(.settings)')!;
		const settings = document.querySelector('.tab.settings')!;
		expect(canHideElement(tunnels)).toBe(true);
		expect(canHideElement(settings)).toBe(false);
	});

	it('blocks protected nodes and their descendants', () => {
		const toggle = document.querySelector('.toggle')!;
		expect(canHideElement(toggle)).toBe(false);
	});

	it('blocks wrappers that contain protected nodes', () => {
		document.body.innerHTML = `
			<main class="main">
				<div class="settings-layout">
					<div id="experimental-settings" ${UI_ELEMENT_HIDER_PROTECTED_ATTR}>x</div>
					<div class="card">y</div>
				</div>
			</main>
		`;
		expect(canHideElement(document.querySelector('.settings-layout')!)).toBe(false);
	});

	it('allows ordinary content blocks', () => {
		expect(canHideElement(document.querySelector('.card')!)).toBe(true);
		expect(canHideElement(document.querySelector('.inner')!)).toBe(true);
	});

	it('flags protected selectors', () => {
		expect(isSelectorHideProtected('.tab.settings')).toBe(true);
		expect(isSelectorHideProtected('.tab:not(.settings)')).toBe(false);
	});
});
