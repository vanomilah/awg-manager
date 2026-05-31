<!--
  Источник дизайна: singbox-router/project/screens/MainBeginner.jsx
  (секция "MAIN: Routing rules as cards" + EmptyState для пустого случая)

  Reads singboxRouterStore: три отдельных readable — rules, ruleSets, outbounds
  (store НЕ имеет единого status-объекта, поля плоские).
  Маппит каждое правило через singboxRuleToCard в RuleCardData.
-->

<script lang="ts">
  import { onMount } from 'svelte';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import { SectionLabel, Button, ConfirmModal } from '$lib/components/ui';
  import { openAddWizard } from './addWizardStore';
  import RuleCard from './RuleCard.svelte';
  import { isSystemRule, singboxRuleToCard } from './adapters';
  import RuleEditModal from '$lib/components/routing/singboxRouter/RuleEditModal.svelte';
  import { computeRuleSetUsage } from '$lib/components/routing/singboxRouter';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { syncTunnelDnsRule } from './emptyStateActions';
  import type { RuleCardData } from './types';

  const rules = singboxRouterStore.rules;
  const ruleSets = singboxRouterStore.ruleSets;
  const outbounds = singboxRouterStore.outbounds;
  const options = singboxRouterStore.options;

  // Триггерим полную загрузку при mount'е. Без этого панель в Beginner
  // mode при первом заходе показывает empty state до тех пор пока юзер
  // не переключится в Expert (там данные грузятся через store.loadAll
  // на mount'е панели). Симметрично с тем как Expert грузит данные.
  onMount(() => {
    void singboxRouterStore.loadAll();
  });

  // Собираем rulesetLabels: tag → tag (у SingboxRouterRuleSet нет поля label,
  // только tag — используем его как отображаемое имя)
  let rulesetLabels: Record<string, string> = $derived.by(() => {
    const labels: Record<string, string> = {};
    for (const rs of $ruleSets) {
      if (rs.tag) labels[rs.tag] = rs.tag;
    }
    return labels;
  });

  // Map raw rules → RuleCardData[]
  let cards: RuleCardData[] = $derived.by(() =>
    $rules.map((r, i) => singboxRuleToCard(r, i, $outbounds, rulesetLabels)),
  );
  let visualOrder = $state<number[]>([]);
  let draggingFrom = $state<number | null>(null);
  let dragOverIndex = $state<number | null>(null);
  let dragPending = $state<{
    pointerId: number;
    from: number;
    startX: number;
    startY: number;
    moved: boolean;
  } | null>(null);
  let rootEl = $state<HTMLDivElement | null>(null);

  const DRAG_THRESHOLD = 7;

  $effect(() => {
    if (dragPending || draggingFrom !== null) return;
    visualOrder = $rules.map((_, i) => i);
  });

  let count = $derived(cards.length);

  function pluralRules(n: number): string {
    if (n === 1) return 'правило';
    if (n >= 2 && n <= 4) return 'правила';
    return 'правил';
  }

  let deleteIndex = $state<number | null>(null);
  let deleteBusy = $state(false);
  let editIndex = $state<number | null>(null);

  function requestDelete(index: number) {
    deleteIndex = index;
  }
  function requestEdit(index: number) {
    if (isSystemRule($rules[index])) return;
    editIndex = index;
  }

  async function confirmDelete() {
    if (deleteIndex === null) return;
    deleteBusy = true;
    try {
      await api.singboxRouterDeleteRule(deleteIndex);
      await syncTunnelDnsRule();
      await singboxRouterStore.loadAll();
      notifications.success('Правило удалено');
      deleteIndex = null;
    } catch (e) {
      notifications.error(`Ошибка: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      deleteBusy = false;
    }
  }

  function firstUserRuleIndex(): number {
    return $rules.findIndex((r) => !isSystemRule(r));
  }

  function canMoveIndexToTarget(from: number, to: number): boolean {
    if (from === to) return false;
    if (from < 0 || from >= $rules.length || to < 0 || to >= $rules.length) return false;
    if (isSystemRule($rules[from])) return false;
    if (isSystemRule($rules[to])) return false;
    const firstUser = firstUserRuleIndex();
    if (firstUser === -1) return false;
    return to >= firstUser;
  }

  async function moveRule(from: number, to: number) {
    if (!canMoveIndexToTarget(from, to)) return;
    try {
      await api.singboxRouterMoveRule(from, to);
      await singboxRouterStore.loadAll();
    } catch (e) {
      notifications.error(`Ошибка перемещения: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  function onHandlePointerDown(event: PointerEvent, originalIndex: number) {
    if (isSystemRule($rules[originalIndex])) return;
    if (event.button !== 0) return;
    dragPending = {
      pointerId: event.pointerId,
      from: originalIndex,
      startX: event.clientX,
      startY: event.clientY,
      moved: false,
    };
  }

  function rowMidpoints(): Array<{ index: number; mid: number }> {
    if (!rootEl) return [];
    const nodes = Array.from(rootEl.querySelectorAll<HTMLElement>('[data-rule-index]'));
    return nodes
      .map((node) => {
        const idx = Number(node.dataset.ruleIndex);
        if (!Number.isInteger(idx)) return null;
        const rect = node.getBoundingClientRect();
        return { index: idx, mid: rect.top + rect.height / 2 };
      })
      .filter((v): v is { index: number; mid: number } => v !== null);
  }

  function reorderPreview(from: number, to: number) {
    const fromPos = visualOrder.indexOf(from);
    const toPos = visualOrder.indexOf(to);
    if (fromPos === -1 || toPos === -1 || fromPos === toPos) return;
    const next = [...visualOrder];
    const [moved] = next.splice(fromPos, 1);
    next.splice(toPos, 0, moved);
    visualOrder = next;
  }

  function onPointerMove(event: PointerEvent) {
    if (!dragPending) return;
    if (event.pointerId !== dragPending.pointerId) return;
    const dx = event.clientX - dragPending.startX;
    const dy = event.clientY - dragPending.startY;
    if (!dragPending.moved) {
      if (Math.hypot(dx, dy) < DRAG_THRESHOLD) return;
      dragPending.moved = true;
      draggingFrom = dragPending.from;
      dragOverIndex = dragPending.from;
      (event.target as HTMLElement | null)?.setPointerCapture?.(event.pointerId);
    }
    const mids = rowMidpoints();
    if (mids.length === 0 || draggingFrom === null) return;
    let candidate = mids[mids.length - 1].index;
    for (const m of mids) {
      if (event.clientY < m.mid) {
        candidate = m.index;
        break;
      }
    }
    if (!canMoveIndexToTarget(draggingFrom, candidate)) return;
    if (candidate !== dragOverIndex) {
      dragOverIndex = candidate;
      reorderPreview(draggingFrom, candidate);
    }
  }

  async function finishDrag(commit: boolean) {
    const from = draggingFrom ?? dragPending?.from ?? null;
    const to = dragOverIndex;
    draggingFrom = null;
    dragPending = null;
    dragOverIndex = null;
    if (!commit || from === null || to === null || from === to) {
      visualOrder = $rules.map((_, i) => i);
      return;
    }
    if (!canMoveIndexToTarget(from, to)) {
      visualOrder = $rules.map((_, i) => i);
      return;
    }
    try {
      await api.singboxRouterMoveRule(from, to);
      await singboxRouterStore.loadAll();
    } catch (e) {
      visualOrder = $rules.map((_, i) => i);
      notifications.error(`Ошибка перемещения: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  function onPointerUp(event: PointerEvent) {
    if (!dragPending || event.pointerId !== dragPending.pointerId) return;
    void finishDrag(true);
  }
  function onPointerCancel(event: PointerEvent) {
    if (!dragPending || event.pointerId !== dragPending.pointerId) return;
    void finishDrag(false);
  }

  function onKeyDown(event: KeyboardEvent) {
    if (event.key === 'Escape' && (dragPending || draggingFrom !== null)) {
      event.preventDefault();
      void finishDrag(false);
    }
  }

  const editRuleSetUsage = $derived(
    editIndex === null ? new Map<string, number>() : computeRuleSetUsage($rules, editIndex),
  );

  async function handleEditSave(rule: (typeof $rules)[number]) {
    if (editIndex === null) return;
    try {
      await api.singboxRouterUpdateRule(editIndex, rule);
      await syncTunnelDnsRule();
      await singboxRouterStore.loadAll();
      notifications.success('Правило обновлено');
      editIndex = null;
    } catch (e) {
      notifications.error(`Ошибка: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  function deleteTargetLabel(index: number): string {
    const card = cards[index];
    if (!card) return `правило #${String(index + 1).padStart(2, '0')}`;
    const n = String(index + 1).padStart(2, '0');
    const target = card.action === 'block' || card.outbound.kind === 'block'
      ? 'Заблокировать'
      : card.outbound.kind === 'direct'
        ? 'Напрямую'
        : card.outbound.label;
    return `правило #${n}: ${card.title} → ${target}`;
  }

</script>

<section class="rules-panel">
  <header class="panel-header">
    <div class="title-group">
      <h2 class="title">Что и куда отправлять</h2>
      <p class="sub">Правила применяются сверху вниз. Срабатывает первое подходящее.</p>
    </div>
    <div class="header-right">
      <div class="counter">
        {count} {pluralRules(count)}
      </div>
      <Button variant="secondary" size="sm" onclick={() => openAddWizard()}>
        + Правило
      </Button>
    </div>
  </header>

  {#if count === 0}
    <div class="empty">
      <SectionLabel>Пока нет правил</SectionLabel>
      <p class="empty-text">
        Создайте правило через <strong>Эксперт-режим</strong> → подвкладка «Rules».
        В будущих версиях здесь появится мастер создания.
      </p>
    </div>
  {:else}
    <div
      class="cards"
      class:is-dragging={dragPending !== null || draggingFrom !== null}
      role="list"
      bind:this={rootEl}
      onpointermove={onPointerMove}
      onpointerup={onPointerUp}
      onpointercancel={onPointerCancel}
    >
      {#each visualOrder as originalIndex, visualIndex (originalIndex)}
        {@const card = cards[originalIndex]}
        <div data-rule-index={originalIndex} role="listitem">
          <RuleCard
            {card}
            index={visualIndex}
            dragging={draggingFrom === originalIndex}
            dragOverBefore={draggingFrom !== null && dragOverIndex === originalIndex && originalIndex !== draggingFrom && visualOrder.indexOf(originalIndex) < visualOrder.indexOf(draggingFrom)}
            dragOverAfter={draggingFrom !== null && dragOverIndex === originalIndex && originalIndex !== draggingFrom && visualOrder.indexOf(originalIndex) > visualOrder.indexOf(draggingFrom)}
            onEdit={() => requestEdit(originalIndex)}
            onDelete={() => requestDelete(originalIndex)}
            onHandlePointerDown={(e) => onHandlePointerDown(e, originalIndex)}
          />
        </div>
      {/each}
    </div>
  {/if}
</section>

<ConfirmModal
  open={deleteIndex !== null}
  title="Удалить правило"
  message={deleteIndex !== null ? `Удалить ${deleteTargetLabel(deleteIndex)}?` : ''}
  busy={deleteBusy}
  onConfirm={confirmDelete}
  onClose={() => { if (!deleteBusy) deleteIndex = null; }}
/>
<svelte:window onkeydown={onKeyDown} />

{#if editIndex !== null && $rules[editIndex]}
  <RuleEditModal
    rule={$rules[editIndex]}
    outboundOptions={$options}
    availableRuleSets={$ruleSets}
    ruleSetUsage={editRuleSetUsage}
    onClose={() => (editIndex = null)}
    onSave={handleEditSave}
  />
{/if}

<style>
  .rules-panel {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    margin-top: var(--sp-4);
  }

  .panel-header {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--sp-4);
  }

  .title-group {
    min-width: 0;
    flex: 1;
  }

  .title {
    margin: 0;
    font-size: var(--fs-h4);
    font-weight: 600;
    color: var(--text-primary);
    line-height: var(--lh-tight);
  }

  .sub {
    margin: 4px 0 0;
    font-size: var(--fs-sm);
    color: var(--text-muted);
    line-height: var(--lh-body);
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-shrink: 0;
  }

  .counter {
    font-size: var(--fs-sm);
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .cards {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .cards.is-dragging {
    user-select: none;
  }

  .empty {
    padding: var(--sp-5) var(--sp-4);
    background: var(--bg-secondary);
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    text-align: center;
  }

  .empty-text {
    margin: var(--sp-3) auto 0;
    max-width: 480px;
    font-size: var(--fs-sm);
    color: var(--text-secondary);
    line-height: var(--lh-body);
  }
</style>
