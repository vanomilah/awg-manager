<script lang="ts">
  import {
    Tv, Send, MessageSquare, Camera, Sparkles, Film, Music, Gamepad2,
    Globe, Network, Layers, Zap, ShieldOff, Edit3, Trash2, GripVertical, Check
  } from 'lucide-svelte';
  import { awgTags as awgTagsStore } from '$lib/stores/awgTags';
  import { subscriptionsStore } from '$lib/stores/subscriptions';
  import { formatOutboundHumanName } from '$lib/utils/outboundHumanName';
  import type { MihomoNativeRule, MihomoRuntimeProxy } from '$lib/types';
  import ServiceTile from '../ServiceTile.svelte';
  import MatcherChip from '../MatcherChip.svelte';

  export interface MihomoBeginnerGroupedCard {
    id: string;
    rules: MihomoNativeRule[];
    serviceKey?: string;
    title: string;
    subtitle?: string;
    chips: Array<{ kind: 'domain' | 'ip' | 'port' | 'ruleset' | 'custom'; label: string; rulesetType?: string }>;
    outbound: string;
    enabled: boolean;
    hasNoResolve?: boolean;
  }

  interface Props {
    card: MihomoBeginnerGroupedCard;
    index: number;
    total: number;
    runtimeProxies?: MihomoRuntimeProxy[];
    onEdit: (rule: MihomoNativeRule) => void;
    onDelete: (rules: MihomoNativeRule[]) => void;
    onToggle: (card: MihomoBeginnerGroupedCard) => void;
  }

  let {
    card,
    index,
    total,
    runtimeProxies = [],
    onEdit,
    onDelete,
    onToggle,
  }: Props = $props();

  let orderStr = $derived(String(index).padStart(2, '0'));

  const nameContext = $derived({
    awgTags: $awgTagsStore.data,
    subscriptions: $subscriptionsStore.data,
  });

  function formatOutbound(raw: string): string {
    return formatOutboundHumanName(raw, nameContext);
  }

  const targetKind = $derived.by(() => {
    const ob = (card.outbound || '').trim();
    if (ob.toUpperCase() === 'DIRECT') return 'direct';
    if (ob.toUpperCase() === 'REJECT') return 'reject';
    return 'group';
  });

  const liveTargetNode = $derived.by(() => {
    if (targetKind !== 'group') return null;
    const runtime = runtimeProxies.find((p) => p.name === card.outbound);
    return runtime?.now || null;
  });
</script>

<div class="card-wrap">
  <div class="card" class:disabled={!card.enabled}>
    <!-- Order index -->
    <div class="order">
      {orderStr}
    </div>

    <!-- Drag handle -->
    <div class="drag-slot">
      <button
        type="button"
        class="drag-handle"
        aria-label={`Перетащить правило #${orderStr}`}
        title="Перетащите для изменения порядка"
      >
        <GripVertical size={16} />
      </button>
    </div>

    <!-- Service tile + Matcher chips -->
    <div class="main">
      <ServiceTile
        serviceKey={card.serviceKey || card.id || 'custom'}
        name={card.title}
        sub={card.subtitle}
      />

      {#if card.chips.length > 0}
        <div class="chips">
          {#each card.chips as chip}
            <MatcherChip
              kind={chip.kind === 'custom' ? 'ruleset' : (chip.kind as any)}
              label={chip.label}
              rulesetType={chip.rulesetType ? (chip.rulesetType === 'inline' || chip.rulesetType === 'local' || chip.rulesetType === 'dat' ? chip.rulesetType : 'remote') : undefined}
            />
          {/each}
          {#if card.hasNoResolve}
            <span class="no-resolve-chip">no-resolve</span>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Outbound Destination & Actions -->
    <div class="trail">
      <div class="action">
        <span class="arrow-sep">›</span>
        {#if targetKind === 'direct'}
          <div class="tone-chip tone-direct" title="Прямое соединение мимо VPN">
            <Globe size={13} />
            <span>direct (мимо VPN)</span>
          </div>
        {:else if targetKind === 'reject'}
          <div class="tone-chip tone-block" title="Блокировка трафика">
            <ShieldOff size={13} />
            <span>Заблокировать</span>
          </div>
        {:else}
          <div class="tone-chip tone-composite" title={`Выход: ${formatOutbound(card.outbound)}`}>
            <Zap size={13} />
            <span>{formatOutbound(card.outbound)}</span>
            {#if liveTargetNode && liveTargetNode !== card.outbound}
              <span class="live-arrow">→</span>
              <span class="live-target">{formatOutbound(liveTargetNode)}</span>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Action buttons -->
      <div class="right-slot">
        <button
          type="button"
          class="route-action-btn"
          onclick={() => onEdit(card.rules[0])}
          aria-label={`Редактировать правило #${orderStr}`}
          title="Редактировать правило"
        >
          <Edit3 size={15} />
        </button>

        <button
          type="button"
          class="route-action-btn danger"
          onclick={() => onDelete(card.rules)}
          aria-label={`Удалить правило #${orderStr}`}
          title="Удалить правило"
        >
          <Trash2 size={15} />
        </button>
      </div>
    </div>
  </div>
</div>

<style>
  @import '../outboundTone.css';

  .card-wrap {
    position: relative;
    min-width: 0;
  }

  .card {
    display: grid;
    grid-template-columns: 28px 28px minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    padding: 10px 14px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 10px);
    transition: border-color var(--t-fast, 0.15s), opacity var(--t-fast, 0.15s);
    min-width: 0;
  }

  .card:hover {
    border-color: var(--border-hover, var(--accent-line));
  }

  .card.disabled {
    opacity: 0.55;
  }

  .order {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-align: center;
  }

  .drag-slot {
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .drag-handle {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 2px;
    cursor: grab;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: color var(--t-fast, 0.15s);
  }

  .drag-handle:hover {
    color: var(--text-primary);
  }

  .main {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    flex-wrap: wrap;
  }

  .chips {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .no-resolve-chip {
    display: inline-flex;
    align-items: center;
    padding: 2px 6px;
    border-radius: var(--radius-sm, 4px);
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.05));
    border: 1px dashed var(--border);
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--text-muted);
  }

  .trail {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-shrink: 0;
    margin-left: auto;
  }

  .action {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .arrow-sep {
    font-size: 16px;
    color: var(--text-muted);
    user-select: none;
  }

  .live-arrow {
    color: var(--text-muted);
    font-size: 11px;
    margin: 0 1px;
  }

  .live-target {
    color: var(--text-primary);
    font-weight: 700;
  }

  .right-slot {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .route-action-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm, 4px);
    color: var(--text-muted);
    cursor: pointer;
    transition: color var(--t-fast, 0.15s), background var(--t-fast, 0.15s);
  }

  .route-action-btn:hover {
    color: var(--text-primary);
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.06));
  }

  .route-action-btn.danger:hover {
    color: var(--color-error, #ef4444);
    background: rgba(239, 68, 68, 0.1);
  }

  @media (max-width: 768px) {
    .card {
      grid-template-columns: 28px minmax(0, 1fr) auto;
      gap: 8px 10px;
      padding: 10px 12px;
    }
    .drag-slot {
      display: none;
    }
    .trail {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
      width: 100%;
      margin-left: 0;
    }
  }
</style>
