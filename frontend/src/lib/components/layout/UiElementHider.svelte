<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import {
		uiElementHiderEnabled,
		uiElementHiderPickerActive,
		uiElementHiderRules,
		rulesForPath,
		buildHideStyles,
	} from '$lib/stores/uiElementHider';
	import { buildCssSelector, buildElementLabel } from '$lib/utils/cssSelector';
	import { canHideElement } from '$lib/utils/uiElementHiderGuard';
	import { notifications } from '$lib/stores/notifications';
	import { Eraser } from 'lucide-svelte';

	const HIDER_ROOT = 'awg-ui-hider-root';
	const STYLE_ID = 'awg-ui-hider-styles';
	const LONG_PRESS_MS = 550;

	let hoveredEl = $state<Element | null>(null);
	let highlightRect = $state<DOMRect | null>(null);
	let longPressArmed = $state(false);
	let longPressTimer: ReturnType<typeof setTimeout> | null = null;
	let longPressHandled = false;

	const enabled = $derived($uiElementHiderEnabled);
	const pickerActive = $derived($uiElementHiderPickerActive);
	const pathname = $derived($page.url.pathname);
	const activeRules = $derived(rulesForPath($uiElementHiderRules, pathname));
	const hideCss = $derived(buildHideStyles(activeRules));

	function isHiderUi(el: Element | null): boolean {
		return !!el?.closest(`[data-${HIDER_ROOT}]`);
	}

	function updateHighlight(el: Element | null) {
		if (!el || isHiderUi(el) || !canHideElement(el)) {
			hoveredEl = null;
			highlightRect = null;
			return;
		}
		hoveredEl = el;
		highlightRect = el.getBoundingClientRect();
	}

	function pickTargetAt(x: number, y: number): Element | null {
		for (const el of document.elementsFromPoint(x, y)) {
			if (isHiderUi(el)) return null;
			if (canHideElement(el)) return el;
		}
		return null;
	}

	function hideElement(el: Element) {
		if (!canHideElement(el)) {
			notifications.warning('Этот блок нельзя скрыть');
			return;
		}
		const selector = buildCssSelector(el);
		uiElementHiderRules.add({
			selector,
			label: buildElementLabel(el),
			path: pathname,
		});
	}

	function handlePointerMove(e: PointerEvent) {
		updateHighlight(pickTargetAt(e.clientX, e.clientY));
	}

	function handlePointerDown(e: PointerEvent) {
		const target = pickTargetAt(e.clientX, e.clientY);
		if (!target) return;
		e.preventDefault();
		e.stopPropagation();
		hideElement(target);
		updateHighlight(null);
	}

	function togglePicker() {
		uiElementHiderPickerActive.toggle();
		updateHighlight(null);
	}

	function clearLongPressTimer() {
		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}
	}

	function disableHiderMode() {
		uiElementHiderPickerActive.set(false);
		uiElementHiderEnabled.set(false);
		updateHighlight(null);
	}

	function handleFabPointerDown() {
		longPressHandled = false;
		longPressArmed = false;
		clearLongPressTimer();
		longPressTimer = setTimeout(() => {
			longPressArmed = true;
			longPressHandled = true;
			disableHiderMode();
		}, LONG_PRESS_MS);
	}

	function handleFabPointerUp() {
		clearLongPressTimer();
		if (longPressHandled) {
			longPressArmed = false;
			return;
		}
		longPressArmed = false;
		togglePicker();
	}

	function handleFabPointerCancel() {
		clearLongPressTimer();
		longPressArmed = false;
		longPressHandled = false;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && pickerActive) {
			uiElementHiderPickerActive.set(false);
			updateHighlight(null);
		}
	}

	$effect(() => {
		const styleEl = document.getElementById(STYLE_ID) as HTMLStyleElement | null;

		if (!enabled || !hideCss) {
			styleEl?.remove();
			return;
		}

		const el =
			styleEl ??
			(() => {
				const node = document.createElement('style');
				node.id = STYLE_ID;
				document.head.appendChild(node);
				return node;
			})();

		el.textContent = hideCss;
	});

	$effect(() => {
		if (!pickerActive) {
			document.body.style.removeProperty('cursor');
			return;
		}

		document.body.style.cursor = 'crosshair';
		window.addEventListener('pointermove', handlePointerMove);
		window.addEventListener('pointerdown', handlePointerDown, true);

		return () => {
			document.body.style.removeProperty('cursor');
			window.removeEventListener('pointermove', handlePointerMove);
			window.removeEventListener('pointerdown', handlePointerDown, true);
		};
	});

	onMount(() => {
		window.addEventListener('keydown', handleKeydown);
		return () => {
			window.removeEventListener('keydown', handleKeydown);
			document.getElementById(STYLE_ID)?.remove();
			document.body.style.removeProperty('cursor');
		};
	});
</script>

{#if enabled}
	{#if pickerActive}
		{#if highlightRect}
			<div
				class="highlight"
				data-awg-ui-hider-root
		data-awg-ui-protected
				style:left="{highlightRect.left}px"
				style:top="{highlightRect.top}px"
				style:width="{highlightRect.width}px"
				style:height="{highlightRect.height}px"
			></div>
		{/if}

		<div class="picker-hint" data-awg-ui-hider-root>
			Клик — скрыть · Esc — выйти · Удержание ластика — выключить
		</div>
	{/if}

	<button
		type="button"
		class="fab"
		class:active={pickerActive}
		class:long-press={longPressArmed}
		data-awg-ui-hider-root
		data-awg-ui-protected
		aria-label={pickerActive ? 'Выйти из режима скрытия' : 'Скрыть элемент на странице'}
		title="Клик — выбор блока · удержание — выключить режим"
		onpointerdown={handleFabPointerDown}
		onpointerup={handleFabPointerUp}
		onpointercancel={handleFabPointerCancel}
		onpointerleave={handleFabPointerCancel}
		oncontextmenu={(e) => e.preventDefault()}
	>
		<Eraser size={20} strokeWidth={2.25} />
	</button>
{/if}

<style>
	.fab {
		position: fixed;
		left: 1.25rem;
		bottom: 1.25rem;
		z-index: var(--z-fab);
		width: 3rem;
		height: 3rem;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border: 2px solid var(--color-border-strong);
		border-radius: var(--radius-sm);
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
		cursor: pointer;
		box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
		transition:
			background var(--t-fast) ease,
			border-color var(--t-fast) ease,
			color var(--t-fast) ease,
			transform var(--t-fast) ease;
	}

	.fab:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
		transform: scale(1.05);
	}

	.fab.active {
		border-color: var(--color-accent);
		background: var(--color-accent-tint);
		color: var(--color-accent);
	}

	.fab.long-press {
		border-color: var(--color-error);
		background: var(--color-error-tint);
		color: var(--color-error);
	}

	.fab:active {
		transform: scale(0.98);
	}

	.highlight {
		position: fixed;
		z-index: calc(var(--z-fab) - 1);
		pointer-events: none;
		border: 2px solid var(--color-accent);
		background: color-mix(in srgb, var(--color-accent) 18%, transparent);
		border-radius: 2px;
		box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-accent) 35%, transparent);
	}

	.picker-hint {
		position: fixed;
		left: 50%;
		bottom: 5.5rem;
		transform: translateX(-50%);
		z-index: var(--z-fab);
		padding: 0.375rem 0.75rem;
		border-radius: var(--radius-sm);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
		font-size: 0.8125rem;
		box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
		pointer-events: none;
	}
</style>
