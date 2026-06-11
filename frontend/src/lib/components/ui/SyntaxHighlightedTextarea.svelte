<script lang="ts">
	import { handleJsonEditorKeydown, type CodeTextareaKeyContext } from '$lib/utils/codeTextareaKeys';
	import { tick } from 'svelte';

	interface Props {
		value?: string;
		placeholder?: string;
		disabled?: boolean;
		wrap?: 'pre' | 'pre-wrap';
		highlight: (raw: string) => string;
		/** Tab / Shift+Tab / smart Enter indent for JSON. */
		indentMode?: 'json' | null;
		class?: string;
		textareaRef?: HTMLTextAreaElement | null;
		onscroll?: () => void;
	}

	let {
		value = $bindable(''),
		placeholder = '',
		disabled = false,
		wrap = 'pre-wrap',
		highlight,
		indentMode = null,
		class: className = '',
		textareaRef = $bindable(null),
		onscroll,
	}: Props = $props();

	let back = $state<HTMLDivElement | null>(null);

	/** Cursor restore after programmatic value edits (bind:value resets selection). */
	let restoreSelection: { start: number; end: number } | null = null;
	let composing = $state(false);

	function keyContext(): CodeTextareaKeyContext | null {
		if (!textareaRef) return null;
		const ta = textareaRef;
		return {
			getValue: () => value,
			setValue: (v) => {
				value = v;
			},
			getSelection: () => ({ start: ta.selectionStart, end: ta.selectionEnd }),
			setSelection: (start, end) => {
				restoreSelection = { start, end };
			},
		};
	}

	function onKeydown(e: KeyboardEvent): void {
		if (disabled || indentMode !== 'json') return;
		const ctx = keyContext();
		if (!ctx) return;
		handleJsonEditorKeydown(e, ctx);
	}

	let highlightHtml = $derived(highlight(value));

	function syncGutter(): void {
		if (!textareaRef || !back) return;
		const gw = textareaRef.offsetWidth - textareaRef.clientWidth;
		const gh = textareaRef.offsetHeight - textareaRef.clientHeight;
		back.style.paddingRight = gw > 0 ? `${gw}px` : '';
		back.style.paddingBottom = gh > 0 ? `${gh}px` : '';
	}

	function syncScroll(): void {
		syncGutter();
		if (textareaRef && back) {
			back.scrollTop = textareaRef.scrollTop;
			back.scrollLeft = textareaRef.scrollLeft;
		}
		onscroll?.();
	}

	function reapplySelectionAndScroll(): void {
		if (!textareaRef || composing) return;
		const start = restoreSelection?.start ?? textareaRef.selectionStart;
		const end = restoreSelection?.end ?? textareaRef.selectionEnd;
		restoreSelection = null;
		// Re-apply selection so the browser scrolls the caret into view (native Enter on
		// the last visible line otherwise leaves the caret painted above the fold).
		textareaRef.setSelectionRange(start, end);
		syncScroll();
	}

	$effect(() => {
		value;
		void tick().then(reapplySelectionAndScroll);
	});

	$effect(() => {
		if (!textareaRef || typeof ResizeObserver === 'undefined') return;
		const ro = new ResizeObserver(() => syncScroll());
		ro.observe(textareaRef);
		return () => ro.disconnect();
	});
</script>

<div class="shl-stack" class:shl-wrap-pre={wrap === 'pre'} class:shl-wrap-pre-wrap={wrap === 'pre-wrap'}>
	<div class="shl-back" aria-hidden="true" bind:this={back}>{@html highlightHtml}</div>
	<textarea
		class="shl-ta {className}"
		bind:this={textareaRef}
		bind:value
		rows={1}
		{placeholder}
		{disabled}
		spellcheck="false"
		autocomplete="off"
		autocapitalize="off"
		onkeydown={onKeydown}
		oncompositionstart={() => (composing = true)}
		oncompositionend={() => {
			composing = false;
			reapplySelectionAndScroll();
		}}
		onscroll={syncScroll}
		oninput={syncScroll}
	></textarea>
</div>

<style>
	.shl-stack {
		position: relative;
		display: grid;
		grid-template: 1fr / 1fr;
		align-items: stretch;
		min-height: 0;
		width: 100%;
		height: 100%;
		font-family: inherit;
		font-size: inherit;
		font-weight: 400;
		font-style: normal;
		line-height: inherit;
		letter-spacing: inherit;
		tab-size: 4;
		-moz-tab-size: 4;
		font-synthesis: none;
	}

	.shl-stack > * {
		grid-area: 1 / 1;
		min-height: 0;
		width: 100%;
		box-sizing: border-box;
	}

	.shl-back {
		margin: 0;
		padding: 0;
		border: none;
		font-family: inherit;
		font-size: inherit;
		font-weight: 400;
		font-style: normal;
		line-height: inherit;
		letter-spacing: inherit;
		tab-size: inherit;
		-moz-tab-size: inherit;
		font-synthesis: none;
		overflow: hidden;
		height: 100%;
		z-index: 0;
		pointer-events: none;
		color: var(--text, var(--color-text-primary));
		background: transparent;
		scrollbar-width: none;
	}

	.shl-back::-webkit-scrollbar {
		display: none;
	}

	.shl-wrap-pre .shl-back,
	.shl-wrap-pre .shl-ta {
		white-space: pre;
		word-break: normal;
	}

	.shl-wrap-pre-wrap .shl-back,
	.shl-wrap-pre-wrap .shl-ta {
		white-space: pre-wrap;
		overflow-wrap: break-word;
		word-break: normal;
	}

	/* Only color may differ from textarea — weight/style change glyph widths in monospace. */
	.shl-back :global(span) {
		font-family: inherit;
		font-size: inherit;
		font-weight: 400;
		font-style: normal;
		line-height: inherit;
		letter-spacing: inherit;
	}

	.shl-back :global(.hl-json-key) {
		color: var(--hl-json-key, #0284c7);
	}
	.shl-back :global(.hl-json-str) {
		color: var(--hl-json-str, var(--text));
	}
	.shl-back :global(.hl-json-num) {
		color: var(--hl-json-num, #ea580c);
	}
	.shl-back :global(.hl-json-lit) {
		color: var(--hl-json-lit, #9333ea);
	}
	.shl-back :global(.hl-json-punct) {
		color: var(--hl-json-punct, #9333ea);
	}

	.shl-back :global(.hl-rule-prefix) {
		color: var(--hl-rule-prefix, #0284c7);
	}
	.shl-back :global(.hl-rule-comment) {
		color: var(--hl-rule-comment, var(--muted-text, var(--color-text-muted)));
	}
	.shl-back :global(.hl-rule-ip) {
		color: var(--hl-rule-ip, #9333ea);
	}
	.shl-back :global(.hl-rule-url) {
		color: var(--hl-rule-url, #2563eb);
	}
	.shl-back :global(.hl-rule-dot) {
		color: var(--hl-rule-dot, #dc2626);
	}
	.shl-back :global(.hl-rule-wild) {
		color: var(--hl-rule-wild, #9333ea);
	}
	.shl-ta {
		margin: 0;
		padding: 0;
		position: relative;
		z-index: 1;
		height: 100%;
		resize: none;
		overflow: auto;
		scrollbar-gutter: stable;
		-webkit-appearance: none;
		appearance: none;
		font-family: inherit;
		font-size: inherit;
		font-weight: 400;
		font-style: normal;
		line-height: inherit;
		letter-spacing: inherit;
		tab-size: inherit;
		-moz-tab-size: inherit;
		font-synthesis: none;
		border: none;
		border-radius: inherit;
		outline: none;
		background: transparent;
		color: transparent;
		-webkit-text-fill-color: transparent;
		caret-color: var(--text, var(--color-text-primary));
	}

	.shl-ta::placeholder {
		opacity: 1;
		color: var(--muted-text, var(--color-text-muted));
		-webkit-text-fill-color: var(--muted-text, var(--color-text-muted));
	}

	.shl-ta::selection {
		background: color-mix(in srgb, var(--accent, #3b82f6) 38%, transparent);
	}
</style>
