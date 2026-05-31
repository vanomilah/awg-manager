<!--
  Источник дизайна: singbox-router/project/parts/RuleCard.jsx (RuleCard)
  Grid: order(28) | main(1fr) | arrow+outbound(auto) | system_badge(auto, опц.)
  В F2 НЕ рендерим: drag handle (F5), edit/menu кнопки (F5).
-->

<script lang="ts">
  import type { RuleCardData } from './types';
  import ServiceTile from './ServiceTile.svelte';
  import MatcherChip from './MatcherChip.svelte';
  import OutboundTile from './OutboundTile.svelte';
  import { Badge } from '$lib/components/ui';
  import { Edit3, GripVertical, X } from 'lucide-svelte';

  interface Props {
    card: RuleCardData;
    /** 0-based index — отображается как 01/02/... */
    index: number;
    onDelete?: () => void;
    onEdit?: () => void;
    onHandlePointerDown?: (event: PointerEvent) => void;
    dragging?: boolean;
    dragOverBefore?: boolean;
    dragOverAfter?: boolean;
  }
  let {
    card,
    index,
    onDelete,
    onEdit,
    onHandlePointerDown,
    dragging = false,
    dragOverBefore = false,
    dragOverAfter = false,
  }: Props = $props();

  const MAX_CHIPS = 4;
  let visibleChips = $derived(card.matchers.slice(0, MAX_CHIPS));
  let hiddenCount = $derived(Math.max(0, card.matchers.length - MAX_CHIPS));
  let orderStr = $derived(String(index + 1).padStart(2, '0'));
  let useServiceTile = $derived(!card.isSystem);
  let editTip = $derived(actionTooltip('edit', card, index));
  let deleteTip = $derived(actionTooltip('delete', card, index));

  function outboundLabel(cardData: RuleCardData): string {
    if (cardData.action === 'block' || cardData.outbound.kind === 'block') return 'Заблокировать';
    if (cardData.outbound.kind === 'direct') return 'Напрямую';
    return cardData.outbound.label;
  }

  function ruleActionTarget(cardData: RuleCardData, idx: number): string {
    const n = String(idx + 1).padStart(2, '0');
    return `правило #${n}: ${cardData.title} → ${outboundLabel(cardData)}`;
  }

  function actionTooltip(action: 'edit' | 'delete', cardData: RuleCardData, idx: number): string {
    const prefix = action === 'edit' ? 'Редактировать' : 'Удалить';
    return `${prefix} ${ruleActionTarget(cardData, idx)}`;
  }
</script>

<div class="card-wrap" class:drag-over-before={dragOverBefore} class:drag-over-after={dragOverAfter}>
<div class="card" class:is-system={card.isSystem} class:dragging>
  <!-- Order number -->
  <div class="order">{orderStr}</div>

  <div class="handle-slot">
    {#if !card.isSystem}
      <button
        type="button"
        class="drag-handle"
        aria-label={`Перетащить правило #${orderStr}`}
        title={`Перетащить правило #${orderStr}`}
        onpointerdown={onHandlePointerDown}
      >
        <GripVertical size={14} />
      </button>
    {:else}
      <div class="handle-disabled" aria-hidden="true"></div>
    {/if}
  </div>

  <!-- Service tile or generic icon-tile + matchers -->
  <div class="main">
    {#if useServiceTile}
      <ServiceTile serviceKey={card.serviceKey} name={card.title} sub={card.subtitle} />
    {:else}
      <!-- System rule: Lock icon -->
      <div class="generic-tile">
        <div class="icon-box is-system">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="3" y="11" width="18" height="11" rx="2" />
            <path d="M7 11V7a5 5 0 0 1 10 0v4" />
          </svg>
        </div>
        <div class="text">
          <div class="title">{card.title}</div>
          {#if card.subtitle}<div class="subtitle">{card.subtitle}</div>{/if}
        </div>
      </div>
    {/if}

    {#if !card.isSystem && visibleChips.length > 0}
      <div class="chips">
        {#each visibleChips as chip}
          <MatcherChip kind={chip.kind} label={chip.label} mono={chip.mono} />
        {/each}
        {#if hiddenCount > 0}
          <span class="more">+{hiddenCount} ещё</span>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Arrow + outbound tile -->
  <div class="action">
    <svg class="arrow" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <line x1="5" y1="12" x2="19" y2="12" />
      <polyline points="12 5 19 12 12 19" />
    </svg>
    <OutboundTile outbound={card.outbound} />
  </div>

  <!-- System badge -->
  {#if card.isSystem}
    <div class="right-slot">
      <Badge variant="muted" size="sm">система</Badge>
    </div>
  {:else if onDelete || onEdit}
    <div class="right-slot">
      {#if onEdit}
        <span class="action-tip" data-tip={editTip}>
          <button type="button" class="edit-btn" onclick={onEdit} aria-label={editTip} title={editTip}>
            <Edit3 size={14} />
          </button>
        </span>
      {/if}
      <span class="action-tip" data-tip={deleteTip}>
        <button type="button" class="del-btn" onclick={onDelete} aria-label={deleteTip} title={deleteTip}>
          <X size={14} />
        </button>
      </span>
    </div>
  {/if}
</div>
</div>

<style>
  .card-wrap {
    position: relative;
  }
  .card-wrap::before,
  .card-wrap::after {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    height: 2px;
    background: transparent;
    pointer-events: none;
    transition: background var(--t-fast);
  }
  .card-wrap::before { top: -3px; }
  .card-wrap::after { bottom: -3px; }
  .card-wrap.drag-over-before::before,
  .card-wrap.drag-over-after::after {
    background: color-mix(in srgb, var(--accent) 75%, transparent);
  }
  .card {
    display: grid;
    grid-template-columns: 28px 28px minmax(0, 1fr) auto auto;
    gap: 12px;
    align-items: center;
    padding: 10px 14px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    transition: border-color var(--t-fast);
  }
  .card:hover { border-color: var(--border-hover); }
  .card.dragging {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.24);
    opacity: 0.82;
    transform: translateY(-1px);
  }

  .order {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-align: center;
  }
  .is-system .order { color: var(--text-muted); }

  .handle-slot {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
  }
  .drag-handle {
    width: 26px;
    min-width: 26px;
    height: 22px;
    border-radius: 7px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    color: var(--text-muted);
    cursor: grab;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    touch-action: none;
  }
  .drag-handle:hover {
    color: var(--text-primary);
    border-color: var(--border-hover);
  }
  .drag-handle:active { cursor: grabbing; }
  .handle-disabled {
    width: 26px;
    height: 22px;
    border-radius: 7px;
    border: 1px dashed var(--border);
    opacity: 0.35;
  }
  .main {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .generic-tile {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .icon-box {
    width: 32px;
    height: 32px;
    border-radius: 8px;
    background: var(--accent-soft);
    color: var(--accent);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .icon-box.is-system {
    background: rgba(255, 255, 255, 0.04);
    color: var(--text-muted);
  }
  .text { min-width: 0; line-height: 1.2; }
  .title {
    font-weight: 600;
    font-size: 14px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .is-system .title { color: var(--text-secondary); }
  .subtitle {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
  }

  .chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .more {
    display: inline-flex;
    align-items: center;
    padding: 2px 7px;
    border-radius: 4px;
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--text-muted);
    font-size: 10px;
    line-height: 1.4;
  }

  .action {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .arrow { color: var(--text-muted); }

  .right-slot {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
    overflow: visible;
    position: relative;
  }
  .action-tip {
    position: relative;
    display: inline-flex;
  }
  .action-tip:hover::after,
  .action-tip:focus-within::after {
    content: attr(data-tip);
    position: absolute;
    right: 0;
    bottom: calc(100% + 8px);
    width: max-content;
    max-width: 320px;
    white-space: normal;
    font-size: 11px;
    line-height: 1.35;
    color: var(--text-primary);
    background: color-mix(in srgb, var(--bg-tertiary) 90%, var(--bg-secondary));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.3);
    padding: 6px 8px;
    z-index: 10;
    pointer-events: none;
  }

  .edit-btn,
  .del-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 5px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .edit-btn:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
  .del-btn:hover {
    color: var(--color-danger, #ef4444);
    border-color: var(--color-danger, #ef4444);
  }

  /* ── Mobile: stack vertically ── */
  @media (max-width: 768px) {
    /*
     * Direct grid children: .order | .main | .action | .right-slot
     * Row 1: order + main (service tile) + right-slot (badge/menu)
     * Row 2: .main continues — chips wrap below service tile (flex-wrap inside .main)
     * Row 3 (full-width): .action with dashed top border
     *
     * We use named grid areas on the 3 top-row children and let .action
     * span all columns on row 2.
     */
    .card {
      grid-template-columns: 28px 28px minmax(0, 1fr) auto;
      grid-template-rows: auto auto;
      grid-template-areas:
        "order handle main right"
        "action action action action";
      row-gap: 0;
      column-gap: 10px;
    }

    .order     { grid-area: order; align-self: start; padding-top: 4px; }
    .handle-slot { grid-area: handle; align-self: start; }
    .main      { grid-area: main; flex-wrap: wrap; align-items: flex-start; gap: 8px; }
    .right-slot { grid-area: right; align-self: start; padding-top: 2px; }

    /* Arrow + OutboundTile — full-width row, dashed top border */
    .action {
      grid-area: action;
      border-top: 1px dashed var(--border);
      padding-top: 8px;
      margin-top: 6px;
    }
  }
</style>
