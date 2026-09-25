<script lang="ts">
    import type { Snippet } from 'svelte';
    import ConfirmModal from './ConfirmModal.svelte';

    interface Props {
        open: boolean;
        title: string;
        size?: 'sm' | 'md' | 'lg' | 'xl' | 'wide';
        /** When `fill`, body does not scroll — children manage their own scroll regions. */
        bodyLayout?: 'default' | 'fill';
        onclose: () => void;
        children: Snippet;
        actions?: Snippet;
        /**
         * Close the modal when the user clicks the dimmed backdrop.
         * Disable for forms with unsaved input where an accidental
         * outside click would lose state — pair with an explicit
         * cancel button + Esc handling (Esc stays enabled regardless).
         */
        closeOnBackdrop?: boolean;
        /**
         * Returns true if the embedded form has unsaved user input. When provided
         * and returns true, the backdrop click, Esc keypress, and the close-X button
         * show a ConfirmModal before invoking onclose. The explicit footer Cancel
         * button is sacred — it bypasses this and calls onclose directly, treating
         * the click as the user's deliberate discard gesture.
         */
        hasUnsavedChanges?: () => boolean;
        /**
         * Explicit body min-height (CSS length). The default body is
         * `flex:1; min-height:0`, which lets it collapse to ~padding height when
         * its content streams in AFTER open (e.g. the switch-progress step list).
         * Set this to pin a minimum so such content can't be clipped to a strip.
         */
        bodyMinHeight?: string;
        /** Allow user to maximize modal to full screen */
        allowMaximize?: boolean;
        /** Allow user to freely resize modal with mouse drag like a Windows window */
        resizable?: boolean;
    }

    let {
        open = $bindable(false),
        title,
        size = 'md',
        bodyLayout = 'default',
        onclose,
        children,
        actions,
        closeOnBackdrop = true,
        hasUnsavedChanges,
        bodyMinHeight,
        allowMaximize = false,
        resizable = false,
    }: Props = $props();

    let isMaximized = $state(false);
    let cardEl = $state<HTMLElement | null>(null);
    let customWidth = $state<number | null>(null);
    let customHeight = $state<number | null>(null);
    let isResizing = $state(false);

    const canResize = $derived(resizable || allowMaximize);

    function startResize(e: PointerEvent, dir: 'se' | 'e' | 's') {
        if (!cardEl) return;
        e.preventDefault();
        e.stopPropagation();

        const startX = e.clientX;
        const startY = e.clientY;
        const rect = cardEl.getBoundingClientRect();
        const startW = rect.width;
        const startH = rect.height;

        if (isMaximized) {
            isMaximized = false;
        }
        isResizing = true;

        const target = e.currentTarget as HTMLElement | null;
        if (target && typeof target.setPointerCapture === 'function') {
            try {
                target.setPointerCapture(e.pointerId);
            } catch {
                // Ignore capture failure
            }
        }

        function onPointerMove(ev: PointerEvent) {
            const dx = ev.clientX - startX;
            const dy = ev.clientY - startY;

            if (dir === 'se' || dir === 'e') {
                const minW = 380;
                const maxW = Math.max(minW, window.innerWidth - 24);
                customWidth = Math.max(minW, Math.min(maxW, Math.round(startW + dx)));
            }
            if (dir === 'se' || dir === 's') {
                const minH = 260;
                const maxH = Math.max(minH, window.innerHeight - 24);
                customHeight = Math.max(minH, Math.min(maxH, Math.round(startH + dy)));
            }
        }

        function onPointerUp(ev: PointerEvent) {
            isResizing = false;
            if (target && typeof target.releasePointerCapture === 'function') {
                try {
                    target.releasePointerCapture(ev.pointerId);
                } catch {
                    // Ignore
                }
            }
            window.removeEventListener('pointermove', onPointerMove);
            window.removeEventListener('pointerup', onPointerUp);
            window.removeEventListener('pointercancel', onPointerUp);
        }

        window.addEventListener('pointermove', onPointerMove);
        window.addEventListener('pointerup', onPointerUp);
        window.addEventListener('pointercancel', onPointerUp);
    }

    const sizeClasses = {
        sm: 'max-w-sm',
        md: 'max-w-md',
        lg: 'max-w-lg',
        xl: 'max-w-xl',
        wide: 'max-w-wide',
    };

    function attemptClose() {
        let dirty = false;
        try {
            dirty = hasUnsavedChanges?.() === true;
        } catch {
            // Treat thrown check as clean — better to let the user out than trap
            // them in a modal whose dirty detector is broken.
            dirty = false;
        }
        if (dirty) {
            confirmOpen = true;
        } else {
            onclose();
        }
    }

    function handleKeydown(e: KeyboardEvent) {
        if (e.key === 'Escape') {
            if (confirmOpen) return; // ConfirmModal owns Esc while open
            attemptClose();
        }
    }

    // Tracks whether the current pointer gesture started on the backdrop.
    // Without this, a drag that begins inside .modal-card (text selection,
    // slider, etc.) and releases on the dimmed area would close the modal
    // — a classic accidental-dismiss bug. We require BOTH pointerdown and
    // click to land on the backdrop element itself.
    let backdropEl: HTMLElement | null = $state(null);
    let pointerDownOnBackdrop = false;
    let confirmOpen = $state(false);

    $effect(() => {
        if (!open) confirmOpen = false;
    });

    function handleBackdropPointerDown(e: PointerEvent) {
        pointerDownOnBackdrop = e.target === backdropEl;
    }

    function handleBackdropClick(e: MouseEvent) {
        if (!closeOnBackdrop) return;
        if (confirmOpen) return; // ConfirmModal owns dismissal while open
        if (e.target !== backdropEl) return;
        if (!pointerDownOnBackdrop) return;
        pointerDownOnBackdrop = false;
        attemptClose();
    }

    function handleBackdropKeydown(e: KeyboardEvent) {
        if (!closeOnBackdrop) return;
        if (confirmOpen) return;
        if (e.target !== backdropEl) return;
        if (e.key !== 'Enter' && e.key !== ' ') return;
        e.preventDefault();
        attemptClose();
    }

    // Portal action: moves the backdrop to <body> so it escapes any
    // ancestor stacking context (e.g. position: sticky, transform, filter).
    // Without this, an ancestor with z-index: auto becomes the cap of our
    // z-index: 200 backdrop, letting later siblings paint on top of it.
    function portal(node: HTMLElement) {
        document.body.appendChild(node);
        return {
            destroy() {
                if (node.parentNode) node.parentNode.removeChild(node);
            },
        };
    }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
        bind:this={backdropEl}
        use:portal
        class="modal-backdrop"
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        tabindex="-1"
        onpointerdown={handleBackdropPointerDown}
        onclick={handleBackdropClick}
        onkeydown={handleBackdropKeydown}
    >
        <div
            bind:this={cardEl}
            class="modal-card {sizeClasses[size]}"
            class:modal-card-maximized={isMaximized}
            class:is-resizing={isResizing}
            role="document"
            style="{!isMaximized && customWidth ? `width: ${customWidth}px; max-width: ${customWidth}px;` : ''} {!isMaximized && customHeight ? `height: ${customHeight}px; max-height: ${customHeight}px;` : ''}"
        >
            <header
                class="modal-header"
                ondblclick={() => {
                    if (allowMaximize) isMaximized = !isMaximized;
                }}
            >
                <h3 id="modal-title">{title}</h3>
                <div class="modal-header-actions">
                    {#if allowMaximize}
                        <button
                            type="button"
                            class="modal-close"
                            onclick={() => (isMaximized = !isMaximized)}
                            aria-label={isMaximized ? "Restore modal" : "Maximize modal"}
                            title={isMaximized ? "Свернуть в окно" : "Развернуть во весь экран"}
                        >
                            {#if isMaximized}
                                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 14h6v6"/><path d="M20 10h-6V4"/><path d="M14 10l7-7"/><path d="M3 21l7-7"/></svg>
                            {:else}
                                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/></svg>
                            {/if}
                        </button>
                    {/if}
                    <button
                        type="button"
                        class="modal-close"
                        onclick={attemptClose}
                        aria-label="Close modal"
                    >
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
                        </svg>
                    </button>
                </div>
            </header>

            <section
                class="modal-body"
                class:modal-body-fill={bodyLayout === 'fill'}
                style={bodyMinHeight ? `min-height: ${bodyMinHeight}` : undefined}
            >
                {@render children()}
            </section>

            {#if actions}
                <footer class="modal-footer">
                    {@render actions()}
                </footer>
            {/if}

            {#if canResize && !isMaximized}
                <!-- Resize handle: Right edge -->
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <div
                    class="modal-resize-edge-e"
                    onpointerdown={(e) => startResize(e, 'e')}
                    title="Потяните для изменения ширины"
                ></div>
                <!-- Resize handle: Bottom edge -->
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <div
                    class="modal-resize-edge-s"
                    onpointerdown={(e) => startResize(e, 's')}
                    title="Потяните для изменения высоты"
                ></div>
                <!-- Resize handle: Bottom-right corner -->
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <div
                    class="modal-resize-grip"
                    onpointerdown={(e) => startResize(e, 'se')}
                    title="Потяните в любом направлении для изменения размера"
                >
                    <svg viewBox="0 0 16 16" width="10" height="10" fill="currentColor">
                        <circle cx="13" cy="13" r="1.3" />
                        <circle cx="13" cy="8.5" r="1.3" />
                        <circle cx="8.5" cy="13" r="1.3" />
                        <circle cx="13" cy="4" r="1.3" />
                        <circle cx="8.5" cy="8.5" r="1.3" />
                        <circle cx="4" cy="13" r="1.3" />
                    </svg>
                </div>
            {/if}
        </div>
    </div>
    {#if hasUnsavedChanges}
        <ConfirmModal
            open={confirmOpen}
            title="Закрыть без сохранения?"
            message="Все правки будут потеряны."
            confirmLabel="Закрыть"
            cancelLabel="Остаться"
            variant="danger"
            onConfirm={() => { confirmOpen = false; onclose(); }}
            onClose={() => { confirmOpen = false; }}
        />
    {/if}
{/if}

<style>
    .modal-backdrop {
        position: fixed;
        inset: 0;
        z-index: var(--z-modal);
        display: flex;
        align-items: center;
        justify-content: center;
        padding: 1rem;
        background: rgba(0, 0, 0, 0.5);
        overflow-y: auto;
        cursor: pointer;
    }

    .modal-card {
        background: var(--bg-secondary);
        border: 1px solid var(--border);
        border-radius: var(--radius);
        width: 100%;
        cursor: auto;
        position: relative;
        /* min-width: 0 + box-sizing keeps the card from being inflated
           past its size-class max-width by an intrinsic min-content child
           (long URL placeholder, monospace text without break-points). */
        min-width: 0;
        box-sizing: border-box;
        /* 100vh on mobile includes hidden browser chrome (address bar,
           toolbar) so the card overflows the visible area. dvh (dynamic
           viewport height) tracks the actual visible space. Fallback to
           vh for older browsers that don't support dvh. */
        max-height: calc(100vh - 2rem);
        max-height: calc(100dvh - 2rem);
        display: flex;
        flex-direction: column;
    }

    .modal-card.is-resizing {
        user-select: none;
        transition: none !important;
    }

    .modal-resize-edge-e {
        position: absolute;
        top: 0;
        right: -4px;
        bottom: 0;
        width: 8px;
        cursor: ew-resize;
        z-index: 20;
    }

    .modal-resize-edge-s {
        position: absolute;
        left: 0;
        bottom: -4px;
        right: 0;
        height: 8px;
        cursor: ns-resize;
        z-index: 20;
    }

    .modal-resize-grip {
        position: absolute;
        right: 3px;
        bottom: 3px;
        width: 16px;
        height: 16px;
        display: flex;
        align-items: center;
        justify-content: center;
        cursor: nwse-resize;
        color: var(--color-text-muted, #888);
        opacity: 0.5;
        transition: opacity 0.15s, color 0.15s;
        z-index: 30;
        user-select: none;
    }

    .modal-resize-grip:hover {
        opacity: 1;
        color: var(--color-accent, #3b82f6);
    }

    /* Each size class caps at its target width but never exceeds the
       visible viewport (minus backdrop padding). */
    .max-w-sm { max-width: min(24rem, calc(100vw - 2rem)); }
    .max-w-md { max-width: min(32rem, calc(100vw - 2rem)); }
    .max-w-lg { max-width: min(40rem, calc(100vw - 2rem)); }
    .max-w-xl { max-width: min(48rem, calc(100vw - 2rem)); }
    .max-w-wide { max-width: min(1180px, calc(100vw - 2rem)); }

    .modal-card-maximized {
        max-width: calc(100vw - 2rem) !important;
        width: calc(100vw - 2rem) !important;
        height: calc(100dvh - 2rem) !important;
        max-height: calc(100dvh - 2rem) !important;
    }

    .modal-header-actions {
        display: flex;
        align-items: center;
        gap: 0.25rem;
    }

    .modal-body-fill {
        overflow: hidden;
        display: flex;
        flex-direction: column;
        padding: 0 8px;
        min-height: 0;
    }

    .modal-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 1rem;
        border-bottom: 1px solid var(--border);
    }

    .modal-header h3 {
        font-size: 1.125rem;
        font-weight: 600;
    }

    .modal-close {
        display: flex;
        align-items: center;
        justify-content: center;
        padding: 0.375rem;
        border: none;
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--text-secondary);
        cursor: pointer;
        flex-shrink: 0;
        transition: color 0.15s ease, background 0.15s ease;
    }

    .modal-close:hover {
        color: var(--text-primary);
        background: var(--bg-hover);
    }

    .modal-close svg {
        width: 1.25rem;
        height: 1.25rem;
    }

    .modal-body {
        padding: 1rem;
        overflow-y: auto;
        overflow-x: hidden;
        flex: 1;
        min-height: 0;
        min-width: 0;
        display: flex;
        flex-direction: column;
    }

    /* Defensive: ensure form controls inside any modal never push the body
       wider than the card. Inputs/textareas/selects with width:100% +
       box-sizing:border-box should already fit, but min-width:auto on grid
       items can leak intrinsic widths through. */
    .modal-body :global(input),
    .modal-body :global(textarea),
    .modal-body :global(select) {
        max-width: 100%;
        min-width: 0;
        box-sizing: border-box;
    }

    .modal-footer {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        padding: 1rem;
        border-top: 1px solid var(--border);
    }

    @media (max-width: 640px) {
        .modal-footer {
            justify-content: stretch;
            align-items: stretch;
        }

        .modal-footer :global(button),
        .modal-footer :global(a),
        .modal-footer :global(.btn) {
            flex: 1 1 0;
            min-width: 0;
            width: 100%;
        }
    }
</style>
