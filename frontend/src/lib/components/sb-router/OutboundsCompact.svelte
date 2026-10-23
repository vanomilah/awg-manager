<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (OutboundsCompact)
-->

<script lang="ts">
  import type { SingboxProxyGroup, SingboxRouterOutbound, Subscription, SubscriptionGroup } from '$lib/types';
  import type { OutboundGroup } from '$lib/components/routing/singboxRouter/outboundOptions';
  import { Badge } from '$lib/components/ui';
  import { Edit3, Trash2, Zap, SlidersHorizontal, ChevronDown, RefreshCw } from 'lucide-svelte';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { resolveMemberLabel } from '$lib/utils/memberLabel';
  import { singboxProxies, latencyHistory } from '$lib/stores/singboxProxies';
  import { singboxDelayHistory } from '$lib/stores/singbox';
  import { subscriptionsStore } from '$lib/stores/subscriptions';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
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
  let testingSingle = $state<Record<string, boolean>>({});
  let switching = $state<Record<string, boolean>>({});
  let localDelays = $state<Record<string, Record<string, number>>>({});

  // Контекстное меню для выбора узла подписки
  let activeSubRef = $state<string | null>(null);
  let activeSubTrigger = $state<HTMLElement | null>(null);
  let activeSubOpen = $state(false);

  function handleOpenSubMenu(tag: string, event: MouseEvent): void {
    event.stopPropagation();
    const currentTarget = event.currentTarget as HTMLElement;
    const anchor = currentTarget.closest<HTMLElement>('.sub-action-group') || currentTarget;
    if (activeSubRef === tag && activeSubOpen) {
      activeSubOpen = false;
      activeSubRef = null;
      activeSubTrigger = null;
    } else {
      activeSubTrigger = anchor;
      activeSubRef = tag;
      activeSubOpen = true;
    }
  }

  async function handleTestSingleServer(
    groupTag: string,
    memberTag: string,
    memberLabel: string,
    e: MouseEvent
  ): Promise<void> {
    e.stopPropagation();
    if (!memberTag || testingSingle[memberTag]) return;
    testingSingle[memberTag] = true;
    try {
      const res = await api.singboxDelayCheck(memberTag).catch(async () => {
        if (memberLabel && memberLabel !== memberTag) {
          return await api.singboxDelayCheck(memberLabel);
        }
        throw new Error('Timeout');
      });
      const delay = res?.delay ?? 0;
      const next = { ...localDelays };
      for (const ob of outbounds) {
        const mems = getGroupMembers(ob);
        if (mems.includes(memberTag) || ob.tag === memberTag || ob.tag === groupTag) {
          next[ob.tag] = {
            ...(next[ob.tag] ?? {}),
            [memberTag]: delay,
          };
        }
      }
      localDelays = next;
      if (delay > 0) {
        notifications.success(`Задержка «${memberLabel}»: ${delay}ms`);
      } else {
        notifications.warning(`Сервер «${memberLabel}» не ответил (timeout)`);
      }
      void singboxProxies.refetch();
    } catch (err) {
      notifications.error(err instanceof Error ? err.message : 'Ошибка замера задержки сервера');
    } finally {
      testingSingle[memberTag] = false;
    }
  }

  function activeView(o: SingboxRouterOutbound) {
    if (!COMPOSITE_OUTBOUND_TYPES.has(o.type)) return null;
    if (o.type === 'loadbalance' || o.type === 'load-balance') return null;
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
    if (o.type === 'loadbalance' || o.type === 'load-balance') return '';
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
      {@const isAuto = sub?.mode ? sub.mode !== 'selector' : o.type !== 'selector'}

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
          <div class="sub-action-group">
            <button
              type="button"
              class="sub-server-test-btn"
              class:testing={!!testingSingle[activeTag]}
              disabled={!!testingSingle[activeTag] || !activeTag}
              onclick={(e) => handleTestSingleServer(o.tag, activeTag, activeLabel, e)}
              title={activeTag ? `Замерить задержку сервера «${activeLabel}»` : 'Сервер не выбран'}
            >
              <span class="member-dot active"></span>
              <span class="sub-mode-badge" class:auto={isAuto}>
                {isAuto ? 'АВТО' : 'РУЧНОЙ'}
              </span>
              <span class="sub-active-name">{activeLabel}</span>
              {#if testingSingle[activeTag]}
                <span class="member-delay tone-none">
                  <RefreshCw size={10} class="spin" />
                  <span>тест...</span>
                </span>
              {:else if activeDelay !== null}
                {@const tone = getDelayTone(activeDelay)}
                <span class="member-delay {tone}">
                  {activeDelay > 0 ? `${activeDelay}ms` : 'timeout'}
                </span>
              {:else}
                <span class="sub-single-test-hint" title="Проверить пинг">
                  <Zap size={11} />
                </span>
              {/if}
            </button>

            <button
              type="button"
              class="sub-menu-open-btn"
              onclick={(e) => handleOpenSubMenu(o.tag, e)}
              title="Выбрать сервер или изменить режим"
            >
              <SlidersHorizontal size={12} />
              <span>Выбрать сервер</span>
              <ChevronDown size={12} />
            </button>
          </div>
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
          {@const isLoadBalance = o.type === 'load-balance' || o.type === 'loadbalance'}
          <div class="members-grid">
            {#each members as m, idx (m)}
              {@const isActive = !isLoadBalance && m === activeTag}
              {@const delay = getMemberDelay(o.tag, m)}
              {@const tone = getDelayTone(delay)}
              {@const label = resolveMemberLabel(m, subscriptions, outboundOptions)}
              {@const isSwitching = !!switching[`${o.tag}:${m}`]}
              {@const isTestingThis = !!testingSingle[m]}

              {#if isSelector}
                <button
                  type="button"
                  class="member-chip selectable"
                  class:active={isActive}
                  class:switching={isSwitching}
                  class:testing={isTestingThis}
                  disabled={isSwitching || isTestingThis}
                  onclick={(e) => {
                    if (isActive) {
                      handleTestSingleServer(o.tag, m, label, e);
                    } else {
                      handleSelectMember(o.tag, m, o);
                    }
                  }}
                  title={isTestingThis
                    ? `Замер задержки «${label}»...`
                    : isActive
                      ? `Активный узел: ${label} (нажмите для проверки связи)`
                      : `Нажмите для выбора: ${label}`}
                >
                  <span class="member-dot" class:active={isActive}></span>
                  <span class="member-order" class:primary={idx === 0}>
                    #{idx + 1}
                  </span>
                  <span class="member-label">{label}</span>
                  {#if isTestingThis}
                    <span class="member-delay tone-none">
                      <RefreshCw size={10} class="spin" />
                      <span>тест...</span>
                    </span>
                  {:else}
                    <span
                      class="member-delay {tone} click-target"
                      role="button"
                      tabindex="0"
                      onclick={(e) => {
                        e.stopPropagation();
                        handleTestSingleServer(o.tag, m, label, e);
                      }}
                      onkeydown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.stopPropagation();
                          e.preventDefault();
                          handleTestSingleServer(o.tag, m, label, e as unknown as MouseEvent);
                        }
                      }}
                      title="Замерить задержку «{label}»"
                    >
                      {delay !== null ? (delay > 0 ? `${delay}ms` : 'timeout') : '—'}
                    </span>
                  {/if}
                </button>
              {:else}
                <button
                  type="button"
                  class="member-chip testable"
                  class:active={isActive}
                  class:load-balance-member={isLoadBalance}
                  class:testing={isTestingThis}
                  disabled={isTestingThis}
                  onclick={(e) => handleTestSingleServer(o.tag, m, label, e)}
                  title={isTestingThis
                    ? `Замер задержки «${label}»...`
                    : isLoadBalance
                      ? `Узел балансировки: ${label} (нажмите для замера задержки)`
                      : o.type === 'fallback' && idx === 0
                        ? `Основной узел: ${label} (нажмите для замера задержки)`
                        : isActive
                          ? `Активный узел: ${label} (нажмите для замера задержки)`
                          : `Узел #${idx + 1}: ${label} (нажмите для замера задержки)`}
                >
                  {#if !isLoadBalance}
                    <span class="member-dot" class:active={isActive}></span>
                    <span class="member-order" class:primary={idx === 0}>
                      {#if o.type === 'fallback'}
                        {idx === 0 ? '#1 (Основной)' : `#${idx + 1}`}
                      {:else}
                        #{idx + 1}
                      {/if}
                    </span>
                  {/if}
                  <span class="member-label">{label}</span>
                  {#if isTestingThis}
                    <span class="member-delay tone-none">
                      <RefreshCw size={10} class="spin" />
                      <span>тест...</span>
                    </span>
                  {:else}
                    <span class="member-delay {tone}">
                      {delay !== null ? (delay > 0 ? `${delay}ms` : 'timeout') : '—'}
                    </span>
                  {/if}
                </button>
              {/if}
            {/each}
          </div>
        {:else}
          <div class="empty-members">Нет участников в группе</div>
        {/if}
      </div>

    {:else}
      {@const av = activeView(o)}
      {@const singleDelay = getMemberDelay(o.tag, o.tag)}
      {@const tone = getDelayTone(singleDelay)}
      {@const isTestingThis = !!testingSingle[o.tag]}
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
        {#if isTestingThis}
          <span class="member-delay tone-none">
            <RefreshCw size={10} class="spin" />
            <span>тест...</span>
          </span>
        {:else if singleDelay !== null}
          <span class="member-delay {tone}">{singleDelay > 0 ? `${singleDelay}ms` : 'timeout'}</span>
        {/if}
        <Badge variant="default" size="sm">{kindLabel(o)}</Badge>
        <div class="actions">
          <button
            type="button"
            class="group-test-btn"
            class:spinning={isTestingThis}
            disabled={isTestingThis}
            onclick={(e) => handleTestSingleServer(o.tag, o.tag, d.title, e)}
            title={`Замерить задержку «${d.title}»`}
          >
            <Zap size={12} class={isTestingThis ? 'spin' : ''} />
            <span>{isTestingThis ? 'Замер...' : 'Тест'}</span>
          </button>
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
  {@const currentOutbound = outbounds.find((o) => o.tag === activeSubRef)}
  {@const currentMembers = currentOutbound ? getGroupMembers(currentOutbound) : []}
  {@const currentActiveTag = currentOutbound ? getActiveMemberTag(currentOutbound, currentMembers) : null}
  <MihomoSubQuickContextMenu
    open={activeSubOpen}
    subRef={activeSubRef}
    triggerEl={activeSubTrigger}
    activeMember={currentActiveTag}
    initialDelays={activeSubRef ? localDelays[activeSubRef] : undefined}
    subscription={subscriptions.find((s) => s.selectorTag === activeSubRef || s.id === activeSubRef || (activeSubRef.startsWith('sub-') && s.id.startsWith(activeSubRef.slice(4))))}
    singboxSubscriptions={subscriptions}
    onClose={() => {
      activeSubOpen = false;
      activeSubRef = null;
      activeSubTrigger = null;
    }}
    onUpdated={async () => {
      await Promise.allSettled([
        subscriptionsStore.refetch(),
        singboxProxies.refetch(),
        singboxRouterStore.loadRulesSnapshot(),
      ]);
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

  :global(.spin) {
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

  .sub-action-group {
    display: inline-flex;
    align-items: stretch;
    background: var(--color-bg-secondary, rgba(255, 255, 255, 0.06));
    border: 1px solid var(--color-border, rgba(255, 255, 255, 0.15));
    border-radius: 6px;
    max-width: 100%;
    overflow: hidden;
    transition: border-color 0.15s ease;
  }

  :global(.dark) .sub-action-group {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.16);
  }

  .sub-action-group:hover {
    border-color: var(--color-border-hover, rgba(255, 255, 255, 0.25));
  }

  .sub-server-test-btn {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: none;
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-primary, #0f172a);
    cursor: pointer;
    transition: background-color 0.15s ease, color 0.15s ease;
    min-width: 0;
  }

  :global(.dark) .sub-server-test-btn {
    color: var(--text-primary, #f8fafc);
  }

  .sub-server-test-btn:hover:not(:disabled) {
    background: var(--color-bg-hover, rgba(255, 255, 255, 0.1));
  }

  .sub-server-test-btn:disabled {
    cursor: default;
  }

  .sub-mode-badge {
    font-size: 9.5px;
    font-weight: 700;
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--color-bg-tertiary, rgba(0, 0, 0, 0.08));
    color: var(--text-secondary, #64748b);
    flex-shrink: 0;
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

  .sub-single-test-hint {
    display: inline-flex;
    align-items: center;
    color: var(--color-text-muted, #94a3b8);
    opacity: 0.6;
    margin-left: 2px;
    transition: opacity 0.15s ease, color 0.15s ease;
  }

  .sub-server-test-btn:hover .sub-single-test-hint {
    color: var(--color-accent, #3b82f6);
    opacity: 1;
  }

  .sub-menu-open-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: transparent;
    border: none;
    border-left: 1px solid var(--color-border, rgba(255, 255, 255, 0.15));
    padding: 5px 10px;
    font-size: 11px;
    font-weight: 500;
    color: var(--color-accent, #3b82f6);
    cursor: pointer;
    transition: background-color 0.15s ease, color 0.15s ease;
    white-space: nowrap;
    flex-shrink: 0;
  }

  :global(.dark) .sub-menu-open-btn {
    border-left-color: rgba(255, 255, 255, 0.16);
    color: var(--color-accent, #60a5fa);
  }

  .sub-menu-open-btn:hover {
    background: var(--color-bg-hover, rgba(59, 130, 246, 0.12));
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

  .member-chip.selectable,
  .member-chip.testable {
    cursor: pointer;
    font-family: inherit;
  }

  .member-chip.selectable:hover,
  .member-chip.testable:hover {
    background: var(--color-bg-hover, rgba(255, 255, 255, 0.12));
    border-color: var(--color-accent, #3b82f6);
    color: var(--text-primary, #fff);
    transform: translateY(-1px);
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.08);
  }

  :global(.dark) .member-chip.selectable:hover,
  :global(.dark) .member-chip.testable:hover {
    background: #334155;
    border-color: var(--color-accent, #60a5fa);
    color: #f8fafc;
  }

  .member-chip.testing {
    opacity: 0.65 !important;
    pointer-events: none;
  }

  .member-delay.click-target {
    cursor: pointer;
    transition: transform 0.15s ease, filter 0.15s ease;
  }

  .member-delay.click-target:hover {
    filter: brightness(1.25);
    text-decoration: underline;
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
