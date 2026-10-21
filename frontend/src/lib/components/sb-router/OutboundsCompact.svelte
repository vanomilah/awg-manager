<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (OutboundsCompact)
-->

<script lang="ts">
  import type { SingboxProxyGroup, SingboxRouterOutbound, Subscription, SubscriptionGroup } from '$lib/types';
  import type { OutboundGroup } from '$lib/components/routing/singboxRouter/outboundOptions';
  import { Badge } from '$lib/components/ui';
  import { Edit3, Trash2, Zap, SlidersHorizontal, ChevronDown } from 'lucide-svelte';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { resolveMemberLabel } from '$lib/utils/memberLabel';
  import { singboxProxies, latencyHistory } from '$lib/stores/singboxProxies';
  import { singboxDelayHistory } from '$lib/stores/singbox';
  import { pluralize } from '$lib/utils/pluralize';
  import { isGroupOutbound, isSubscriptionOutbound, outboundDisplay } from './outboundLabel';
  import { outboundDeleteBlockReasons, type OutboundUsageInput } from './outboundUsage';
  import { COMPOSITE_OUTBOUND_TYPES, resolveCompositeOutboundView } from './compositeOutboundDisplay';
  import MihomoSubQuickContextMenu from './mihomo/MihomoSubQuickContextMenu.svelte';

  interface Props {
    outbounds: SingboxRouterOutbound[];
    onEdit: (tag: string) => void;
    onDelete?: (tag: string) => void;
    /** Subscriptions, to resolve sub-<hash> composite tags to their names. */
    subscriptions?: Subscription[];
    /** Сводные группы подписок — имя вместо agg-тега и бейдж «группа» (#572). */
    groups?: SubscriptionGroup[];
    usage?: Omit<OutboundUsageInput, 'tag'>;
    /** Живые clash-группы (`now`) — для строки «активен: …» у композитов. */
    proxyGroups?: SingboxProxyGroup[];
    /** Каталог outbound-опций — для человекочитаемых имён участников. */
    outboundOptions?: OutboundGroup[];
  }

  let {
    outbounds,
    onEdit,
    onDelete,
    subscriptions = [],
    groups = [],
    usage,
    proxyGroups = [],
    outboundOptions = [],
  }: Props = $props();

  let testingGroups = $state<Record<string, boolean>>({});
  let switching = $state<Record<string, boolean>>({});
  let localDelays = $state<Record<string, Record<string, number>>>({});

  // Контекстное меню для выбора узла подписки
  let activeSubRef = $state<string | null>(null);
  let activeSubTrigger = $state<HTMLElement | null>(null);
  let activeSubOpen = $state(false);

  function handleOpenSubMenu(tag: string, event: MouseEvent): void {
    event.stopPropagation();
    const currentTarget = event.currentTarget as HTMLElement;
    if (activeSubRef === tag && activeSubOpen) {
      activeSubOpen = false;
      activeSubRef = null;
      activeSubTrigger = null;
    } else {
      activeSubTrigger = currentTarget;
      activeSubRef = tag;
      activeSubOpen = true;
    }
  }

  function activeView(o: SingboxRouterOutbound) {
    if (!COMPOSITE_OUTBOUND_TYPES.has(o.type)) return null;
    const now = proxyGroups.find((g) => g.tag === o.tag)?.now;
    if (!now) return null;
    return resolveCompositeOutboundView(o.tag, outbounds, outboundOptions, subscriptions, proxyGroups);
  }

  function toneFor(type: string): 'success' | 'accent' | 'info' | 'muted' | 'error' {
    if (type === 'direct') return 'muted';
    if (type === 'selector' || type === 'urltest' || type === 'loadbalance' || type === 'fallback' || type === 'load-balance') return 'accent';
    return 'info';
  }

  function kindLabel(o: SingboxRouterOutbound): string {
    if (isGroupOutbound(o, groups)) return 'группа';
    if (isSubOutbound(o)) return 'подписка';
    if (o.type === 'selector' || o.type === 'urltest' || o.type === 'loadbalance' || o.type === 'fallback' || o.type === 'load-balance') return o.type;
    return o.type;
  }

  function isSubOutbound(o: SingboxRouterOutbound): boolean {
    if (o.source === 'subscription') return true;
    if (isSubscriptionOutbound(o, subscriptions)) return true;
    return subscriptions.some(
      (s) => s.selectorTag === o.tag || s.id === o.tag || (o.tag.startsWith('sub-') && s.id.startsWith(o.tag.slice(4)))
    );
  }

  function findSubscription(o: SingboxRouterOutbound): Subscription | undefined {
    return subscriptions.find(
      (s) => s.selectorTag === o.tag || s.id === o.tag || (o.tag.startsWith('sub-') && s.id.startsWith(o.tag.slice(4)))
    );
  }

  function editBlockReason(o: SingboxRouterOutbound): string | null {
    if (isGroupOutbound(o, groups))
      return 'Редактируется в разделе Туннели → Прокси → Подписки → Сводные группы';
    if (isSubOutbound(o))
      return 'Редактируется в разделе Туннели → Прокси → Подписки';
    return null;
  }

  function getGroupMembers(o: SingboxRouterOutbound): string[] {
    if (o.outbounds && o.outbounds.length > 0) return o.outbounds;
    const live = proxyGroups.find((g) => g.tag === o.tag);
    if (live && live.members?.length > 0) return live.members.map((m) => m.tag);
    const sub = findSubscription(o);
    if (sub?.memberTags?.length) return sub.memberTags;
    if (sub?.members?.length) return sub.members.map((m) => m.tag).filter(Boolean);
    return [];
  }

  function getActiveMemberTag(o: SingboxRouterOutbound, members: string[]): string {
    const live = proxyGroups.find((g) => g.tag === o.tag);
    if (live?.now) return live.now;
    const sub = findSubscription(o);
    if (sub?.activeMember) return sub.activeMember;
    return members[0] ?? '';
  }

  function getMemberDelay(groupTag: string, memberTag: string): number | null {
    if (localDelays[groupTag]?.[memberTag] !== undefined) {
      return localDelays[groupTag][memberTag];
    }
    const live = proxyGroups.find((g) => g.tag === groupTag);
    const mem = live?.members?.find((m) => m.tag === memberTag);
    if (mem?.lastDelay && mem.lastDelay > 0) {
      return mem.lastDelay;
    }
    const hist = $latencyHistory.get(memberTag);
    if (hist && hist.length > 0) {
      const last = hist[hist.length - 1];
      if (last > 0) return last;
    }
    const sbHist = $singboxDelayHistory.get(memberTag);
    if (sbHist && sbHist.length > 0) {
      const last = sbHist[sbHist.length - 1];
      if (last > 0) return last;
    }
    return null;
  }

  function getDelayTone(delay: number | null): 'good' | 'medium' | 'bad' | 'none' {
    if (delay === null || delay === undefined) return 'none';
    if (delay === 0) return 'bad';
    if (delay < 180) return 'good';
    if (delay < 380) return 'medium';
    return 'bad';
  }

  async function handleTestGroup(groupTag: string, e: MouseEvent): Promise<void> {
    e.stopPropagation();
    if (testingGroups[groupTag]) return;
    testingGroups[groupTag] = true;
    try {
      const res = await api.singboxRouterTestProxy({ group: groupTag });
      if (res?.delays) {
        localDelays[groupTag] = { ...(localDelays[groupTag] ?? {}), ...res.delays };
      }
      notifications.success(`Задержки для «${groupTag}» обновлены`);
      void singboxProxies.refetch();
    } catch (err) {
      notifications.error(err instanceof Error ? err.message : 'Ошибка замера задержек');
    } finally {
      testingGroups[groupTag] = false;
    }
  }

  async function handleSelectMember(groupTag: string, memberTag: string, o: SingboxRouterOutbound): Promise<void> {
    if (o.type !== 'selector') return;
    const key = `${groupTag}:${memberTag}`;
    if (switching[key]) return;
    switching[key] = true;
    try {
      await api.singboxRouterSelectProxy({ group: groupTag, member: memberTag });
      notifications.success(`Выбран узел: ${resolveMemberLabel(memberTag, subscriptions, outboundOptions)}`);
      void singboxProxies.refetch();
    } catch (err) {
      notifications.error(err instanceof Error ? err.message : 'Ошибка переключения узла');
    } finally {
      switching[key] = false;
    }
  }

  const deleteReasons = $derived(usage ? outboundDeleteBlockReasons(outbounds, usage) : null);
</script>

<div class="list">
  {#each outbounds as o (o.tag)}
    {@const d = outboundDisplay(o, subscriptions, groups)}
    {@const deleteReason = deleteReasons?.get(o.tag) ?? null}
    {@const editReason = editBlockReason(o)}
    {@const isSub = isSubOutbound(o)}
    {@const isComposite = COMPOSITE_OUTBOUND_TYPES.has(o.type) || (o.outbounds && o.outbounds.length > 0) || isSub}

    {#if isSub}
      {@const sub = findSubscription(o)}
      {@const members = getGroupMembers(o)}
      {@const activeTag = getActiveMemberTag(o, members)}
      {@const activeLabel = resolveMemberLabel(activeTag, subscriptions, outboundOptions)}
      {@const activeDelay = getMemberDelay(o.tag, activeTag)}
      {@const isTesting = !!testingGroups[o.tag]}
      {@const isAuto = o.type !== 'selector' && sub?.mode !== 'selector'}

      <div class="composite-card sub-card">
        <div class="composite-header">
          <div class="composite-meta">
            <span class="dot" data-tone={toneFor(o.type)}></span>
            <span class="tag">{d.title}</span>
            <span class="sub-badge">ПОДПИСКА</span>
            <span class="group-summary">
              {pluralize(members.length, ['сервер', 'сервера', 'серверов'])}
              · {isAuto ? 'автовыбор' : 'ручной выбор'}
            </span>
          </div>

          <div class="actions">
            <button
              type="button"
              class="group-test-btn"
              class:spinning={isTesting}
              disabled={isTesting}
              onclick={(e) => handleTestGroup(o.tag, e)}
              title="Замерить задержки серверов подписки"
            >
              <Zap size={12} class={isTesting ? 'spin' : ''} />
              <span>{isTesting ? 'Замер...' : 'Тест'}</span>
            </button>
            <button
              type="button"
              class="route-action-btn"
              disabled={editReason !== null}
              onclick={() => onEdit(o.tag)}
              aria-label={`Редактировать outbound ${o.tag}`}
              title={editReason ?? `Редактировать outbound «${d.title}»`}
            >
              <Edit3 size={14} />
            </button>
            {#if onDelete}
              <button
                type="button"
                class="route-action-btn danger"
                disabled={deleteReason !== null}
                onclick={() => onDelete(o.tag)}
                aria-label={`Удалить outbound ${o.tag}`}
                title={deleteReason ?? `Удалить outbound «${d.title}»`}
              >
                <Trash2 size={14} />
              </button>
            {/if}
          </div>
        </div>

        <div class="sub-active-row">
          <button
            type="button"
            class="sub-active-btn"
            onclick={(e) => handleOpenSubMenu(o.tag, e)}
            title="Нажмите, чтобы выбрать сервер или переключить режим авто/ручной"
          >
            <span class="member-dot active"></span>
            <span class="sub-mode-badge" class:auto={isAuto}>
              {isAuto ? 'АВТО' : 'РУЧНОЙ'}
            </span>
            <span class="sub-active-name">{activeLabel}</span>
            {#if activeDelay !== null}
              {@const tone = getDelayTone(activeDelay)}
              <span class="member-delay {tone}">
                {activeDelay > 0 ? `${activeDelay}ms` : 'timeout'}
              </span>
            {/if}
            <span class="sub-change-hint">
              <SlidersHorizontal size={12} />
              <span>Выбрать сервер</span>
              <ChevronDown size={12} />
            </span>
          </button>
        </div>
      </div>

    {:else if isComposite}
      {@const members = getGroupMembers(o)}
      {@const activeTag = getActiveMemberTag(o, members)}
      {@const isSelector = o.type === 'selector'}
      {@const isTesting = !!testingGroups[o.tag]}

      <div class="composite-card">
        <div class="composite-header">
          <div class="composite-meta">
            <span class="dot" data-tone={toneFor(o.type)}></span>
            <button
              type="button"
              class="meta-btn"
              disabled={editReason !== null}
              title={editReason ?? undefined}
              onclick={() => onEdit(o.tag)}
            >
              <span class="tag">{d.title}</span>
            </button>
            <span class="sub-badge">{kindLabel(o).toUpperCase()}</span>
            <span class="group-summary">
              {pluralize(members.length, ['узел', 'узла', 'узлов'])}
              {#if o.type === 'fallback'}
                · авторезерв
              {:else if o.type === 'urltest'}
                · автовыбор
              {:else if o.type === 'load-balance' || o.type === 'loadbalance'}
                · балансировка
              {:else if o.type === 'selector'}
                · ручной выбор
              {/if}
            </span>
          </div>

          <div class="actions">
            <button
              type="button"
              class="group-test-btn"
              class:spinning={isTesting}
              disabled={isTesting}
              onclick={(e) => handleTestGroup(o.tag, e)}
              title="Замерить задержки всех узлов группы"
            >
              <Zap size={12} class={isTesting ? 'spin' : ''} />
              <span>{isTesting ? 'Замер...' : 'Тест'}</span>
            </button>
            <button
              type="button"
              class="route-action-btn"
              disabled={editReason !== null}
              onclick={() => onEdit(o.tag)}
              aria-label={`Редактировать outbound ${o.tag}`}
              title={editReason ?? `Редактировать outbound «${d.title}»`}
            >
              <Edit3 size={14} />
            </button>
            {#if onDelete}
              <button
                type="button"
                class="route-action-btn danger"
                disabled={deleteReason !== null}
                onclick={() => onDelete(o.tag)}
                aria-label={`Удалить outbound ${o.tag}`}
                title={deleteReason ?? `Удалить outbound «${d.title}»`}
              >
                <Trash2 size={14} />
              </button>
            {/if}
          </div>
        </div>

        {#if members.length > 0}
          <div class="members-grid">
            {#each members as m, idx (m)}
              {@const isActive = m === activeTag}
              {@const delay = getMemberDelay(o.tag, m)}
              {@const tone = getDelayTone(delay)}
              {@const label = resolveMemberLabel(m, subscriptions, outboundOptions)}
              {@const isSwitching = !!switching[`${o.tag}:${m}`]}

              {#if isSelector}
                <button
                  type="button"
                  class="member-chip selectable"
                  class:active={isActive}
                  class:switching={isSwitching}
                  disabled={isSwitching}
                  onclick={() => handleSelectMember(o.tag, m, o)}
                  title={isActive ? `Активный узел: ${label}` : `Нажмите для выбора: ${label}`}
                >
                  <span class="member-dot" class:active={isActive}></span>
                  <span class="member-order" class:primary={idx === 0}>
                    #{idx + 1}
                  </span>
                  <span class="member-label">{label}</span>
                  <span class="member-delay {tone}">
                    {delay !== null ? (delay > 0 ? `${delay}ms` : 'timeout') : '—'}
                  </span>
                </button>
              {:else}
                <div
                  class="member-chip"
                  class:active={isActive}
                  title={o.type === 'fallback' && idx === 0 ? `Основной узел: ${label}` : isActive ? `Активный узел: ${label}` : label}
                >
                  <span class="member-dot" class:active={isActive}></span>
                  <span class="member-order" class:primary={idx === 0}>
                    {#if o.type === 'fallback'}
                      {idx === 0 ? '#1 (Основной)' : `#${idx + 1}`}
                    {:else}
                      #{idx + 1}
                    {/if}
                  </span>
                  <span class="member-label">{label}</span>
                  <span class="member-delay {tone}">
                    {delay !== null ? (delay > 0 ? `${delay}ms` : 'timeout') : '—'}
                  </span>
                </div>
              {/if}
            {/each}
          </div>
        {:else}
          <div class="empty-members">Нет участников в группе</div>
        {/if}
      </div>

    {:else}
      {@const av = activeView(o)}
      <div class="row">
        <span class="dot" data-tone={toneFor(o.type)}></span>
        <button
          type="button"
          class="meta-btn"
          disabled={editReason !== null}
          title={editReason ?? undefined}
          onclick={() => onEdit(o.tag)}
        >
          <div class="meta">
            <div class="tag">{d.title}</div>
            <div class="sub">{d.subtitle}</div>
            {#if av}
              <div class="active-line" title="Текущий выбор {o.type === 'selector' ? 'selector' : 'urltest'}">
                <span class="active-dot" aria-hidden="true"></span>
                <span class="active-label">активен: {av.activeMemberLabel}</span>
                {#if av.otherMemberTags.length > 0}
                  <span class="active-rest">+{av.otherMemberTags.length}</span>
                {/if}
              </div>
            {/if}
          </div>
        </button>
        <Badge variant="default" size="sm">{kindLabel(o)}</Badge>
        <div class="actions">
          <button
            type="button"
            class="route-action-btn"
            disabled={editReason !== null}
            onclick={() => onEdit(o.tag)}
            aria-label={`Редактировать outbound ${o.tag}`}
            title={editReason ?? `Редактировать outbound «${d.title}»`}
          >
            <Edit3 size={15} />
          </button>
          {#if onDelete}
            <button
              type="button"
              class="route-action-btn danger"
              disabled={deleteReason !== null}
              onclick={() => onDelete(o.tag)}
              aria-label={`Удалить outbound ${o.tag}`}
              title={deleteReason ?? `Удалить outbound «${d.title}»`}
            >
              <Trash2 size={15} />
            </button>
          {/if}
        </div>
      </div>
    {/if}
  {/each}

  {#if outbounds.length === 0}
    <div class="empty">Нет outbounds</div>
  {/if}
</div>

{#if activeSubOpen && activeSubRef && activeSubTrigger}
  <MihomoSubQuickContextMenu
    open={activeSubOpen}
    subRef={activeSubRef}
    triggerEl={activeSubTrigger}
    onClose={() => {
      activeSubOpen = false;
      activeSubRef = null;
      activeSubTrigger = null;
    }}
    onUpdated={() => {
      void singboxProxies.refetch();
    }}
  />
{/if}

<style>
  .list {
    display: flex;
    flex-direction: column;
  }

  .composite-card {
    background: var(--color-bg-secondary, rgba(255, 255, 255, 0.04));
    border: 1px solid var(--color-border, rgba(255, 255, 255, 0.12));
    border-radius: var(--radius-sm, 6px);
    padding: 10px 12px;
    margin: 6px 10px;
    transition: border-color var(--t-fast, 0.15s), background var(--t-fast, 0.15s);
  }

  :global(.dark) .composite-card {
    background: rgba(255, 255, 255, 0.03);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .composite-card:hover {
    border-color: var(--color-border-hover, rgba(255, 255, 255, 0.2));
  }

  .composite-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }

  .composite-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex: 1;
  }

  .sub-badge {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--color-accent-tint, rgba(59, 130, 246, 0.15));
    color: var(--color-accent, #3b82f6);
    letter-spacing: 0.03em;
  }

  .group-summary {
    font-size: 11px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .group-test-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-secondary);
    background: var(--color-bg-tertiary, rgba(255, 255, 255, 0.05));
    border: 1px solid var(--color-border, rgba(255, 255, 255, 0.12));
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .group-test-btn:hover:not(:disabled) {
    background: var(--color-accent-tint, rgba(59, 130, 246, 0.15));
    color: var(--color-accent, #3b82f6);
    border-color: var(--color-accent-border, rgba(59, 130, 246, 0.3));
  }

  .group-test-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .group-test-btn :global(.spin) {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  /* Subscriptions */
  .sub-active-row {
    display: flex;
    align-items: center;
    padding: 2px 0;
  }

  .sub-active-btn {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: var(--color-bg-secondary, rgba(255, 255, 255, 0.06));
    border: 1px solid var(--color-border, rgba(255, 255, 255, 0.15));
    border-radius: 6px;
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-primary, #0f172a);
    cursor: pointer;
    transition: all 0.15s ease;
    max-width: 100%;
  }

  :global(.dark) .sub-active-btn {
    color: var(--text-primary, #f8fafc);
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.16);
  }

  .sub-active-btn:hover {
    background: var(--color-bg-hover, rgba(255, 255, 255, 0.12));
    border-color: var(--color-accent, #3b82f6);
  }

  .sub-mode-badge {
    font-size: 9.5px;
    font-weight: 700;
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--color-bg-tertiary, rgba(0, 0, 0, 0.08));
    color: var(--text-secondary, #64748b);
  }

  .sub-mode-badge.auto {
    background: var(--color-accent-tint, rgba(59, 130, 246, 0.15));
    color: var(--color-accent, #2563eb);
  }

  .sub-active-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 320px;
  }

  .sub-change-hint {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-left: 6px;
    padding-left: 8px;
    border-left: 1px solid var(--color-border, rgba(255, 255, 255, 0.15));
    font-size: 11px;
    color: var(--color-accent, #3b82f6);
    font-weight: 500;
  }

  /* Members Grid */
  .members-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .member-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--color-bg-primary, #ffffff);
    border: 1px solid var(--color-border, #cbd5e1);
    border-radius: 6px;
    padding: 4px 9px;
    font-size: 11.5px;
    font-weight: 500;
    color: var(--color-text-primary, #0f172a);
    text-align: left;
    transition: all 0.15s ease;
    user-select: none;
    max-width: 100%;
    opacity: 1 !important;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  }

  :global(.dark) .member-chip {
    color: #f8fafc;
    background: #1e293b;
    border-color: #475569;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
  }

  .member-chip.selectable {
    cursor: pointer;
  }

  .member-chip.selectable:hover {
    background: var(--color-bg-hover, rgba(255, 255, 255, 0.12));
    border-color: var(--color-accent, #3b82f6);
    color: var(--text-primary, #fff);
  }

  .member-chip.active {
    background: color-mix(in srgb, var(--color-success, #22c55e) 15%, transparent);
    border-color: var(--color-success, #22c55e);
    color: var(--text-primary, #0f172a);
    font-weight: 600;
  }

  :global(.dark) .member-chip.active {
    color: #fff;
  }

  .member-chip.switching {
    opacity: 0.5 !important;
    pointer-events: none;
  }

  .member-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-muted, #94a3b8);
    flex-shrink: 0;
  }

  .member-dot.active {
    background: var(--color-success, #22c55e);
    box-shadow: 0 0 8px rgba(34, 197, 94, 0.6);
  }

  .member-order {
    font-size: 10px;
    font-weight: 700;
    color: var(--text-primary, #334155);
    background: rgba(0, 0, 0, 0.07);
    padding: 1px 5px;
    border-radius: 4px;
    white-space: nowrap;
  }

  :global(.dark) .member-order {
    background: rgba(255, 255, 255, 0.15);
    color: #e2e8f0;
  }

  .member-order.primary {
    background: var(--color-accent, #2563eb);
    color: #fff;
  }

  .member-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 200px;
    color: inherit;
  }

  .member-delay {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    padding: 0 4px;
    border-radius: 3px;
    white-space: nowrap;
  }

  .member-delay.good {
    color: var(--color-success, #22c55e);
  }

  .member-delay.medium {
    color: var(--color-warning, #f59e0b);
  }

  .member-delay.bad {
    color: var(--color-error, #dc2626);
  }

  .member-delay.none {
    color: var(--text-muted);
  }

  .empty-members {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
    padding: 4px 0;
  }

  /* Single row */
  .row {
    transition: background-color 0.15s ease;
    display: grid;
    grid-template-columns: 6px minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  }

  @media (hover: hover) and (pointer: fine) {
    .row:hover {
      background: color-mix(in srgb, var(--bg-hover) 70%, transparent);
    }
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-muted);
    flex-shrink: 0;
  }
  .dot[data-tone='success'] { background: var(--color-success, #22c55e); }
  .dot[data-tone='accent'] { background: var(--accent); }
  .dot[data-tone='info'] { background: var(--color-info, #3b82f6); }
  .dot[data-tone='error'] { background: var(--color-error, #dc2626); }

  .meta-btn {
    min-width: 0;
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .meta {
    flex: 1;
    min-width: 0;
  }

  .tag {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .sub {
    font-size: 11px;
    color: var(--text-muted);
  }

  .active-line {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    color: var(--text-secondary);
    min-width: 0;
    max-width: 100%;
  }

  .active-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--color-success, #22c55e);
    flex-shrink: 0;
  }

  .active-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .active-rest {
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .actions {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .route-action-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .route-action-btn:hover:not(:disabled) {
    background: var(--color-bg-hover, rgba(255, 255, 255, 0.1));
    color: var(--text-primary);
  }

  .route-action-btn.danger:hover:not(:disabled) {
    background: rgba(220, 38, 38, 0.15);
    color: var(--color-error, #dc2626);
  }

  .route-action-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .empty {
    padding: 14px;
    color: var(--text-muted);
    text-align: center;
    font-size: 12px;
  }
</style>
