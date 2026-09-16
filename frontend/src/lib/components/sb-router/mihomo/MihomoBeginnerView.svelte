<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { Button, ConfirmModal, SectionLabel } from '$lib/components/ui';
  import { LoadingSpinner } from '$lib/components/layout';
  import { Sparkles } from 'lucide-svelte';
  import { pluralize, RULE_WORDS } from '$lib/utils/pluralize';
  import FlowGraph from '../FlowGraph.svelte';
  import { openAddWizard } from '../addWizardStore';
  import type {
    MihomoNativeGroup,
    MihomoNativeProxy,
    MihomoNativeRule,
    MihomoNativeRuleProvider,
    MihomoNativeSubscription,
    MihomoRuntimeProxy,
  } from '$lib/types';
  import { subscriptionsStore } from '$lib/stores/subscriptions';
  import MihomoRuleCard, { type MihomoBeginnerGroupedCard } from './MihomoRuleCard.svelte';
  import MihomoRuleEditModal from './MihomoRuleEditModal.svelte';
  import MihomoGroupEditModal from './MihomoGroupEditModal.svelte';
  import MihomoTemplateModal from './MihomoTemplateModal.svelte';
  import MihomoProxyGroupCard from './MihomoProxyGroupCard.svelte';

  interface Props {
    rules: MihomoNativeRule[];
    groups: MihomoNativeGroup[];
    proxies: MihomoNativeProxy[];
    subscriptions: MihomoNativeSubscription[];
    ruleProviders: MihomoNativeRuleProvider[];
    runtimeProxies: MihomoRuntimeProxy[];
    loading: boolean;
    onReload: () => void;
  }

  let {
    rules = [],
    groups = [],
    proxies = [],
    subscriptions = [],
    ruleProviders = [],
    runtimeProxies = [],
    loading = false,
    onReload,
  }: Props = $props();

  onMount(() => {
    void subscriptionsStore.refetch();
  });

  let editModalOpen = $state(false);
  let editingRule = $state<MihomoNativeRule | null>(null);
  let groupModalOpen = $state(false);
  let editingGroup = $state<MihomoNativeGroup | null>(null);
  let deleteConfirmOpen = $state(false);
  let deletingRules = $state<MihomoNativeRule[]>([]);
  let deletingGroup = $state<MihomoNativeGroup | null>(null);

  let templateModalOpen = $state(false);

  function handleOpenTemplates() {
    templateModalOpen = true;
  }

  // Drag & drop state for reordering
  let draggedIndex = $state<number | null>(null);
  let dragOverIndex = $state<number | null>(null);

  function parseServiceMeta(type: string, payload: string = '') {
    const p = (payload || '').toLowerCase();
    const chipKind: 'ip' | 'ruleset' = type === 'GEOIP' ? 'ip' : 'ruleset';

    if (p.includes('youtube')) {
      return {
        serviceKey: 'youtube',
        title: 'YouTube',
        subtitle: 'Видеосервис',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: youtube' : 'набор: youtube' }],
      };
    }
    if (p.includes('telegram')) {
      return {
        serviceKey: 'telegram',
        title: 'Telegram',
        subtitle: 'Мессенджер',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: telegram' : 'набор: telegram' }],
      };
    }
    if (p.includes('discord')) {
      return {
        serviceKey: 'discord',
        title: 'Discord',
        subtitle: 'Голосовой и текстовый чат',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: discord' : 'набор: discord' }],
      };
    }
    if (p.includes('instagram')) {
      return {
        serviceKey: 'instagram',
        title: 'Instagram',
        subtitle: 'Социальная сеть',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: instagram' : 'набор: instagram' }],
      };
    }
    if (p.includes('gemini') || p.includes('google-gemini')) {
      return {
        serviceKey: 'gemini',
        title: 'Gemini',
        subtitle: 'Искусственный интеллект Google',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: google-gemini' : 'набор: google-gemini' }],
      };
    }
    if (p.includes('anthropic') || p.includes('claude')) {
      return {
        serviceKey: 'anthropic',
        title: 'Claude / Anthropic',
        subtitle: 'Искусственный интеллект',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: anthropic' : 'набор: anthropic' }],
      };
    }
    if (p.includes('openai') || p.includes('chatgpt')) {
      return {
        serviceKey: 'chatgpt',
        title: 'ChatGPT / OpenAI',
        subtitle: 'Искусственный интеллект',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: openai' : 'набор: openai' }],
      };
    }
    if (p.includes('xai') || p.includes('grok')) {
      return {
        serviceKey: 'grok',
        title: 'Grok (xAI)',
        subtitle: 'Искусственный интеллект',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: xai' : 'набор: xai' }],
      };
    }
    if (p.includes('tiktok')) {
      return {
        serviceKey: 'tiktok',
        title: 'TikTok',
        subtitle: 'Короткие видео',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: tiktok' : 'набор: tiktok' }],
      };
    }
    if (p.includes('spotify')) {
      return {
        serviceKey: 'spotify',
        title: 'Spotify',
        subtitle: 'Музыкальный стриминг',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: spotify' : 'набор: spotify' }],
      };
    }
    if (p.includes('steam')) {
      return {
        serviceKey: 'steam',
        title: 'Steam',
        subtitle: 'Игровая платформа',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: steam' : 'набор: steam' }],
      };
    }
    if (p.includes('netflix')) {
      return {
        serviceKey: 'netflix',
        title: 'Netflix',
        subtitle: 'Кино и сериалы',
        chips: [{ kind: chipKind, label: type === 'GEOIP' ? 'geoip: netflix' : 'набор: netflix' }],
      };
    }
    if (p.includes('category-ru') || p.includes('russian-services')) {
      return {
        serviceKey: 'russian-services',
        title: 'Российские сервисы',
        subtitle: 'Яндекс, VK, Mail.ru, Госуслуги',
        chips: [{ kind: chipKind, label: 'geosite: category-ru' }],
      };
    }
    if (p.includes('category-media-ru-blocked') || p.includes('rkn') || p.includes('all-blocked') || p.includes('unavailable-in-russia')) {
      return {
        serviceKey: 'rkn',
        title: 'Заблокировано в РФ',
        subtitle: 'Сайты и СМИ из реестра блокировок',
        chips: [{ kind: chipKind, label: 'geosite: category-media-ru-blocked' }],
      };
    }
    if (p.includes('category-ads-all') || p.includes('adblock') || p === 'ads') {
      return {
        serviceKey: 'ads',
        title: 'Блокировка рекламы',
        subtitle: 'Баннеры и трекеры',
        chips: [{ kind: chipKind, label: 'geosite: category-ads-all' }],
      };
    }

    if (type.startsWith('IP-CIDR') || type === 'GEOIP' || type === 'IP-ASN') {
      return {
        title: payload || type,
        subtitle: 'IP-адреса и подсети',
        chips: [{ kind: 'ip' as const, label: type === 'GEOIP' ? `geoip: ${payload}` : payload }],
      };
    }

    if (type.startsWith('DOMAIN')) {
      return {
        title: payload || type,
        subtitle: 'Доменные имена',
        chips: [{ kind: 'domain' as const, label: payload }],
      };
    }

    if (type === 'GEOSITE') {
      return {
        title: payload || type,
        subtitle: 'Набор доменов',
        chips: [{ kind: 'ruleset' as const, label: `набор: ${payload}` }],
      };
    }

    if (type === 'RULE-SET') {
      return {
        title: payload || type,
        subtitle: 'Внешний набор правил (.mrs)',
        chips: [{ kind: 'ruleset' as const, label: `rule-set: ${payload}` }],
      };
    }

    return {
      title: payload || type,
      subtitle: type,
      chips: [{ kind: 'custom' as const, label: `${type}: ${payload}` }],
    };
  }

  function groupRulesForBeginner(ruleList: MihomoNativeRule[]): MihomoBeginnerGroupedCard[] {
    const cards: MihomoBeginnerGroupedCard[] = [];

    for (const rule of ruleList) {
      const parsed = parseServiceMeta(rule.type, rule.payload || '');
      const last = cards[cards.length - 1];

      // Merge into last card if same service and same outbound
      if (
        last &&
        parsed.serviceKey &&
        last.serviceKey === parsed.serviceKey &&
        last.outbound === rule.outbound
      ) {
        last.rules.push(rule);
        for (const chip of parsed.chips) {
          if (!last.chips.some((c) => c.kind === chip.kind && c.label === chip.label)) {
            last.chips.push(chip);
          }
        }
        if (rule.noResolve) last.hasNoResolve = true;
        if (!rule.enabled) last.enabled = false;
      } else {
        cards.push({
          id: rule.id || `${parsed.serviceKey || 'rule'}-${cards.length}`,
          rules: [rule],
          serviceKey: parsed.serviceKey,
          title: parsed.title,
          subtitle: parsed.subtitle,
          chips: [...parsed.chips],
          outbound: rule.outbound,
          enabled: rule.enabled ?? true,
          hasNoResolve: !!rule.noResolve,
        });
      }
    }

    return cards;
  }

  const groupedCards = $derived(groupRulesForBeginner(rules));

  function handleOpenAdd() {
    openAddWizard();
  }

  function handleEditRule(rule: MihomoNativeRule) {
    editingRule = rule;
    editModalOpen = true;
  }

  function handleDeleteCard(rulesToDelete: MihomoNativeRule[]) {
    deletingRules = rulesToDelete;
    deletingGroup = null;
    deleteConfirmOpen = true;
  }

  function handleDeleteGroup(groupId: string) {
    const target = groups.find((g) => g.id === groupId || g.name === groupId);
    if (target) {
      deletingGroup = target;
      deletingRules = [];
      deleteConfirmOpen = true;
    }
  }

  async function confirmDelete() {
    if (deletingGroup) {
      try {
        if (deletingGroup.id) {
          await api.mihomoNativeDeleteGroup(deletingGroup.id, true);
        }
        notifications.success(`Группа «${deletingGroup.name}» удалена`);
        onReload();
      } catch (e) {
        notifications.error(e instanceof Error ? e.message : 'Не удалось удалить группу');
      } finally {
        deletingGroup = null;
        deleteConfirmOpen = false;
      }
      return;
    }

    if (deletingRules.length === 0) return;
    try {
      for (let i = 0; i < deletingRules.length; i++) {
        const r = deletingRules[i];
        const isLast = i === deletingRules.length - 1;
        if (r.id) {
          await api.mihomoNativeDeleteRule(r.id, isLast);
        }
      }
      notifications.success(deletingRules.length > 1 ? 'Правила удалены' : 'Правило удалено');
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Не удалось удалить правила');
    } finally {
      deletingRules = [];
      deleteConfirmOpen = false;
    }
  }

  async function handleToggleCard(card: MihomoBeginnerGroupedCard) {
    try {
      const nextEnabled = !card.enabled;
      for (let i = 0; i < card.rules.length; i++) {
        const r = card.rules[i];
        const isLast = i === card.rules.length - 1;
        await api.mihomoNativeSaveRule({
          ...r,
          enabled: nextEnabled,
        }, isLast);
      }
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Не удалось обновить статус правил');
    }
  }

  // Drag and drop reordering
  function handleDragStart(index: number) {
    draggedIndex = index;
  }

  function handleDragOver(e: DragEvent, index: number) {
    e.preventDefault();
    dragOverIndex = index;
  }

  async function handleDrop(targetCardIndex: number) {
    if (draggedIndex === null || draggedIndex === targetCardIndex) {
      draggedIndex = null;
      dragOverIndex = null;
      return;
    }

    const reorderedCards = [...groupedCards];
    const [moved] = reorderedCards.splice(draggedIndex, 1);
    reorderedCards.splice(targetCardIndex, 0, moved);

    draggedIndex = null;
    dragOverIndex = null;

    try {
      const allReorderedRules = reorderedCards.flatMap((c) => c.rules);
      const ids = allReorderedRules.map((r) => r.id);
      await api.mihomoNativeReorderRules(ids);
      await api.mihomoReload();
      notifications.success('Порядок правил обновлён');
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Не удалось сохранить порядок правил');
    }
  }

  const uniqueOutbounds = $derived.by(() => {
    const set = new Set<string>();
    for (const r of rules) {
      if (r.outbound && r.outbound !== 'DIRECT' && r.outbound !== 'direct' && r.outbound !== 'REJECT') {
        set.add(r.outbound);
      }
    }
    return Array.from(set);
  });
</script>

<div class="beginner-view">
  <!-- Interactive Top Flow Graph -->
  <FlowGraph
    isMihomo={true}
    mihomoRulesCount={rules.length}
    mihomoGroupsCount={groups.length}
    mihomoTopGroup={groups[0]?.name || ''}
    mihomoOutbounds={uniqueOutbounds}
  />

  {#if groups.length > 0}
    <section class="groups-panel">
      <div class="groups-header">
        <span class="groups-title">Группы прокси</span>
        <span class="groups-sub">автопереключение и задержки</span>
      </div>
      <div class="groups-list">
        {#each groups as group (group.id || group.name)}
          <MihomoProxyGroupCard
            {group}
            {runtimeProxies}
            {subscriptions}
            onEdit={(g) => {
              editingGroup = g;
              groupModalOpen = true;
            }}
            onDelete={handleDeleteGroup}
            {onReload}
          />
        {/each}
      </div>
    </section>
  {/if}

  <section class="rules-panel">
    <header class="panel-header">
      <div class="title-group">
        <h2 class="title">Что и куда отправлять</h2>
        <p class="sub">Правила применяются сверху вниз. Срабатывает первое подходящее.</p>
      </div>

      <div class="header-right">
        <div class="counter">
          {pluralize(groupedCards.length, RULE_WORDS)}
        </div>
        <Button variant="secondary" size="sm" onclick={handleOpenTemplates}>
          {#snippet iconBefore()}
            <Sparkles size={14} aria-hidden="true" />
          {/snippet}
          Шаблоны
        </Button>
        <Button variant="primary" size="sm" onclick={handleOpenAdd}>
          + Правило
        </Button>
      </div>
    </header>

    {#if loading && groupedCards.length === 0}
      <div class="loading-box">
        <LoadingSpinner size="md" />
      </div>
    {:else if groupedCards.length === 0}
      <div class="empty">
        <SectionLabel>Пока нет правил</SectionLabel>
        <p class="empty-text">
          Воспользуйтесь мастером настройки, чтобы направить нужные сервисы (YouTube, Telegram, Discord и др.) через прокси-группы.
        </p>
        <div class="empty-action">
          <Button variant="secondary" size="sm" onclick={handleOpenTemplates}>
            {#snippet iconBefore()}
              <Sparkles size={14} aria-hidden="true" />
            {/snippet}
            Шаблоны
          </Button>
          <Button variant="primary" size="sm" onclick={handleOpenAdd}>
            + Создать правило через мастер
          </Button>
        </div>
      </div>
    {:else}
      <div class="cards" role="list">
        {#each groupedCards as card, i (card.id || i)}
          <div
            class="card-shell"
            class:is-drag-over={dragOverIndex === i}
            draggable="true"
            ondragstart={() => handleDragStart(i)}
            ondragover={(e) => handleDragOver(e, i)}
            ondrop={() => handleDrop(i)}
            role="listitem"
          >
            <MihomoRuleCard
              {card}
              index={i}
              total={groupedCards.length}
              {runtimeProxies}
              onEdit={handleEditRule}
              onDelete={handleDeleteCard}
              onToggle={handleToggleCard}
            />
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <!-- Edit Rule Modal -->
  {#if editModalOpen && editingRule}
    <MihomoRuleEditModal
      open={true}
      rule={editingRule}
      {groups}
      {proxies}
      {subscriptions}
      {ruleProviders}
      onClose={() => {
        editModalOpen = false;
        editingRule = null;
      }}
      onSaved={() => {
        editModalOpen = false;
        editingRule = null;
        onReload();
      }}
    />
  {/if}

  {#if groupModalOpen}
    <MihomoGroupEditModal
      open={groupModalOpen}
      group={editingGroup}
      {groups}
      {proxies}
      {subscriptions}
      {runtimeProxies}
      onClose={() => {
        groupModalOpen = false;
        editingGroup = null;
      }}
      onSaved={() => {
        groupModalOpen = false;
        editingGroup = null;
        onReload();
      }}
      onDelete={handleDeleteGroup}
    />
  {/if}

  {#if templateModalOpen}
    <MihomoTemplateModal
      open={true}
      {groups}
      {proxies}
      {subscriptions}
      currentRules={rules}
      onClose={() => (templateModalOpen = false)}
      onApplied={() => {
        templateModalOpen = false;
        onReload();
      }}
    />
  {/if}

  <!-- Delete Rule Confirm Modal -->
  <ConfirmModal
    open={deleteConfirmOpen}
    title={deletingGroup ? 'Удалить группу прокси?' : 'Удалить правило?'}
    message={deletingGroup
      ? `Вы действительно хотите удалить группу «${deletingGroup.name}»? Связанные с ней правила могут потребовать обновления.`
      : deletingRules.length > 1
        ? `Удалить ${deletingRules.length} правил(а) для этого сервиса?`
        : 'Вы действительно хотите удалить это правило маршрутизации Mihomo?'}
    confirmLabel="Удалить"
    variant="danger"
    onConfirm={confirmDelete}
    onClose={() => {
      deleteConfirmOpen = false;
      deletingRules = [];
      deletingGroup = null;
    }}
  />
</div>

<style>
  .beginner-view {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .rules-panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 0 4px;
  }

  .title-group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .title {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--text-primary);
    letter-spacing: -0.01em;
  }

  .sub {
    margin: 0;
    font-size: 12px;
    color: var(--text-secondary);
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .counter {
    font-size: 13px;
    font-family: var(--font-mono);
    color: var(--text-muted);
  }

  .cards {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .card-shell {
    position: relative;
    border-radius: var(--radius-md, 10px);
    transition: transform var(--t-fast, 0.15s);
  }

  .card-shell.is-drag-over {
    border-top: 2px solid var(--accent);
  }

  .groups-panel {
    margin-bottom: 16px;
  }

  .groups-header {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 8px;
    padding: 0 2px;
  }

  .groups-title {
    font-size: 12px;
    font-weight: 700;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .groups-sub {
    font-size: 12px;
    color: var(--text-muted);
    opacity: 0.85;
  }

  .loading-box {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 48px;
  }

  .empty {
    padding: 24px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }

  .empty-text {
    margin: 0;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.4;
  }

  .empty-action {
    margin-top: 6px;
  }

  @media (max-width: 768px) {
    .panel-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
    }
    .header-right {
      width: 100%;
      justify-content: space-between;
    }
  }
</style>
