<script lang="ts">
  import { onMount } from 'svelte';
  import {
    Plus, Globe, Zap, ShieldOff, Layers, Activity, RefreshCw, Sparkles,
    Edit3, Trash2, ChevronUp, ChevronDown, Check, ExternalLink, FileText, LayoutGrid
  } from 'lucide-svelte';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { Button, Badge, ConfirmModal } from '$lib/components/ui';
  import { LoadingSpinner } from '$lib/components/layout';
  import StatStrip, { type StatCellData } from '../StatStrip.svelte';
  import SidePanel from '../SidePanel.svelte';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import { singboxMemory } from '$lib/stores/singboxMemory';
  import { singboxTrafficLive } from '$lib/stores/singboxEngineStats';
  import { formatBytes, formatByteRate } from '$lib/utils/format';
  import { awgTags as awgTagsStore } from '$lib/stores/awgTags';
  import { subscriptionsStore } from '$lib/stores/subscriptions';
  import { formatOutboundHumanName } from '$lib/utils/outboundHumanName';
  import { pluralize, RULE_WORDS } from '$lib/utils/pluralize';
  import type {
    MihomoNativeGroup,
    MihomoNativeProxy,
    MihomoNativeRule,
    MihomoNativeRuleProvider,
    MihomoNativeSubscription,
    MihomoRuntimeProxy,
    MihomoRuntimeProvider,
  } from '$lib/types';
  import { openAddWizard } from '../addWizardStore';
  import MihomoRuleEditModal from './MihomoRuleEditModal.svelte';
  import MihomoGroupEditModal from './MihomoGroupEditModal.svelte';
  import MihomoProviderModal from './MihomoProviderModal.svelte';
  import MihomoTemplateModal from './MihomoTemplateModal.svelte';
  import MihomoRuleSetCatalogModal from './MihomoRuleSetCatalogModal.svelte';
  import MihomoProxyGroupCard from './MihomoProxyGroupCard.svelte';
  import DnsServersCompact from '../DnsServersCompact.svelte';
  import DNSServerEditModal from '$lib/components/routing/singboxRouter/DNSServerEditModal.svelte';
  import DNSRuleEditModal from '$lib/components/routing/singboxRouter/DNSRuleEditModal.svelte';
  import { get } from 'svelte/store';
  import type { SingboxRouterDNSServer, SingboxRouterDNSRule } from '$lib/types';

  interface Props {
    rules: MihomoNativeRule[];
    groups: MihomoNativeGroup[];
    proxies: MihomoNativeProxy[];
    subscriptions: MihomoNativeSubscription[];
    ruleProviders: MihomoNativeRuleProvider[];
    runtimeProxies: MihomoRuntimeProxy[];
    runtimeProviders?: Record<string, MihomoRuntimeProvider>;
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
    runtimeProviders = {},
    loading = false,
    onReload,
  }: Props = $props();

  const storeStatus = singboxRouterStore.status;
  const storeSettings = singboxRouterStore.settings;

  const isEngineActive = $derived(
    Boolean($storeStatus?.active || runtimeProxies.length > 0 || Object.keys(runtimeProviders).length > 0)
  );

  const engineStat = $derived.by<{ value: string; tone: StatCellData['tone'] }>(() => {
    if ($storeStatus && !$storeStatus.enabled) return { value: 'OFF', tone: 'muted' };
    if (loading && !isEngineActive) return { value: '...', tone: 'muted' };
    return isEngineActive
      ? { value: 'ON', tone: 'success' }
      : { value: 'СБОЙ', tone: 'error' };
  });

  const nameContext = $derived({
    awgTags: $awgTagsStore.data,
    subscriptions: $subscriptionsStore.data,
    mihomoSubscriptions: subscriptions,
  });

  function formatOutbound(raw: string): string {
    return formatOutboundHumanName(raw, nameContext);
  }

  const storeDnsServers = singboxRouterStore.dnsServers;
  const storeDnsRules = singboxRouterStore.dnsRules;
  const storeOptions = singboxRouterStore.options;

  let templateModalOpen = $state(false);
  let providerCatalogOpen = $state(false);

  function handleOpenTemplates() {
    templateModalOpen = true;
  }

  function handleOpenProviderCatalog() {
    providerCatalogOpen = true;
  }

  let dnsServerAddOpen = $state(false);
  let dnsServerEditTag = $state<string | null>(null);
  let dnsRuleAddOpen = $state(false);
  let dnsRuleEditIdx = $state<number | null>(null);

  const editingDnsServer = $derived(
    dnsServerEditTag ? $storeDnsServers.find((s) => s.tag === dnsServerEditTag) : undefined,
  );
  const editingDnsRule = $derived(
    dnsRuleEditIdx !== null ? $storeDnsRules[dnsRuleEditIdx] : undefined,
  );

  async function handleDnsServerAddSave(server: SingboxRouterDNSServer) {
    await api.singboxRouterAddDNSServer(server);
    dnsServerAddOpen = false;
    await singboxRouterStore.loadAll();
    onReload();
  }

  async function handleDnsServerEditSave(server: SingboxRouterDNSServer) {
    if (dnsServerEditTag !== null) {
      await api.singboxRouterUpdateDNSServer(dnsServerEditTag, server);
    }
    dnsServerEditTag = null;
    await singboxRouterStore.loadAll();
    onReload();
  }

  async function handleDeleteDnsServer(tag: string) {
    try {
      await api.singboxRouterDeleteDNSServer(tag);
      await singboxRouterStore.loadAll();
      notifications.success(`DNS-сервер «${tag}» удален`);
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка удаления DNS-сервера');
    }
  }

  async function handleDnsRuleAddSave(rule: SingboxRouterDNSRule) {
    await api.singboxRouterAddDNSRule(rule);
    dnsRuleAddOpen = false;
    await singboxRouterStore.loadAll();
    onReload();
  }

  async function handleDnsRuleEditSave(rule: SingboxRouterDNSRule) {
    if (dnsRuleEditIdx !== null) {
      await api.singboxRouterUpdateDNSRule(dnsRuleEditIdx, rule);
    }
    dnsRuleEditIdx = null;
    await singboxRouterStore.loadAll();
    onReload();
  }

  async function handleDeleteDnsRule(idx: number) {
    try {
      await api.singboxRouterDeleteDNSRule(idx);
      await singboxRouterStore.loadAll();
      notifications.success('DNS-правило удалено');
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка удаления DNS-правила');
    }
  }

  async function handleMoveDnsRule(from: number, to: number) {
    const snapshot = get(singboxRouterStore.dnsRules);
    const next = snapshot.slice();
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    singboxRouterStore.applyDNSRules(next);
    try {
      await api.singboxRouterMoveDNSRule(from, to);
      await singboxRouterStore.loadAll();
      onReload();
    } catch (e) {
      singboxRouterStore.applyDNSRules(snapshot);
      notifications.error(`Ошибка перемещения: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  // Modals state
  let ruleModalOpen = $state(false);
  let editingRule = $state<MihomoNativeRule | null>(null);

  let groupModalOpen = $state(false);
  let editingGroup = $state<MihomoNativeGroup | null>(null);

  let providerModalOpen = $state(false);
  let editingProvider = $state<MihomoNativeRuleProvider | null>(null);

  let deleteType = $state<'rule' | 'group' | 'provider' | null>(null);
  let deleteId = $state<string | null>(null);
  let deleteConfirmOpen = $state(false);

  let checkingGroupDelay = $state<string | null>(null);
  let refreshingProvider = $state<string | null>(null);

  // Filter tab for Rule Providers
  let providerTab = $state<'all' | 'http' | 'file'>('all');
  let filteredProviders = $derived.by(() => {
    if (providerTab === 'http') return ruleProviders.filter((p) => p.type === 'http');
    if (providerTab === 'file') return ruleProviders.filter((p) => p.type === 'file');
    return ruleProviders;
  });

  const liveStats = $derived($singboxTrafficLive);
  const memCellValue = $derived(
    isEngineActive && $singboxMemory > 0 ? formatBytes($singboxMemory) : '—',
  );
  const rateCellValue = $derived(
    isEngineActive && liveStats.rate.hasRate
      ? formatByteRate(liveStats.rate.downloadRate)
      : '—',
  );

  // Top metric stat strip
  let statCells = $derived<StatCellData[]>([
    {
      label: 'ДВИЖОК',
      value: engineStat.value,
      tone: engineStat.tone,
      helpTitle: 'Ядро маршрутизации Mihomo',
      helpText: 'Mihomo выполняет интерцепцию TProxy и маршрутизацию сетевых пакетов.',
    },
    {
      label: 'ПАМЯТЬ',
      value: memCellValue,
      tone: isEngineActive && $singboxMemory > 0 ? undefined : 'muted',
      helpTitle: 'Память Mihomo',
      helpText: 'Память Go-рантайма Mihomo по данным Clash API. Обновляется каждые ~2 секунды, пока движок работает.',
    },
    {
      label: 'ТРАФИК ↓',
      value: rateCellValue,
      tone: isEngineActive && liveStats.rate.hasRate ? undefined : 'muted',
      helpTitle: 'Трафик через Mihomo',
      helpText: 'Агрегатная скорость скачивания через Mihomo (кумулятивные счётчики Clash).',
      helpItems: [
        `Отдача: ${isEngineActive && liveStats.rate.hasRate ? formatByteRate(liveStats.rate.uploadRate) : '—'}`,
        `За сессию: ${isEngineActive ? formatBytes(liveStats.totals.downloadBytes + liveStats.totals.uploadBytes) : '—'}`,
      ],
    },
    {
      label: 'ПРАВИЛ',
      value: String(rules.length),
      helpTitle: 'Правила маршрутизации',
      helpText: 'Количество правил в цепочке first-match.',
    },
    {
      label: 'RULE-SETS',
      value: String(ruleProviders.length),
      helpTitle: 'Провайдеры правил',
      helpText: 'Внешние и локальные списки правил (RULE-SET).',
    },
    {
      label: 'OUTBOUNDS',
      value: String(groups.length),
      helpTitle: 'Группы прокси',
      helpText: 'Группы автоматического или ручного выбора узлов.',
    },
    {
      label: 'DNS',
      value: '1',
      helpTitle: 'Встроенный DNS',
      helpText: 'Mihomo DNS (enhanced-mode: redir-host).',
    },
    {
      label: 'ПРОКСИ',
      value: String(proxies.length + subscriptions.length),
      helpTitle: 'Прокси и подписки',
      helpText: 'Всего настроенных прокси-узлов и подписок.',
    },
  ]);

  // Fallback / MATCH default rule
  const matchRule = $derived(rules.find((r) => (r.type || '').toUpperCase() === 'MATCH'));
  const fallbackOutbound = $derived(matchRule ? matchRule.outbound : 'DIRECT');
  let editingIsFallback = $state(false);

  // Actions
  function handleAddRule() {
    editingIsFallback = false;
    editingRule = null;
    ruleModalOpen = true;
  }
  function handleEditDefaultFallback() {
    editingIsFallback = true;
    if (matchRule) {
      editingRule = matchRule;
    } else {
      editingRule = {
        id: '',
        type: 'MATCH',
        payload: '',
        outbound: 'DIRECT',
        enabled: true,
      };
    }
    ruleModalOpen = true;
  }
  function handleEditRule(rule: MihomoNativeRule) {
    editingIsFallback = (rule.type || '').toUpperCase() === 'MATCH';
    editingRule = rule;
    ruleModalOpen = true;
  }
  function handleDeleteRule(id: string) {
    deleteType = 'rule';
    deleteId = id;
    deleteConfirmOpen = true;
  }

  function handleAddGroup() {
    editingGroup = null;
    groupModalOpen = true;
  }
  function handleEditGroup(group: MihomoNativeGroup) {
    editingGroup = group;
    groupModalOpen = true;
  }
  function handleDeleteGroup(id: string) {
    deleteType = 'group';
    deleteId = id;
    deleteConfirmOpen = true;
  }

  function handleAddProvider() {
    editingProvider = null;
    providerModalOpen = true;
  }
  function handleEditProvider(provider: MihomoNativeRuleProvider) {
    editingProvider = provider;
    providerModalOpen = true;
  }
  function handleDeleteProvider(id: string) {
    deleteType = 'provider';
    deleteId = id;
    deleteConfirmOpen = true;
  }

  async function handleMoveRule(fromIdx: number, dir: 'up' | 'down') {
    const toIdx = dir === 'up' ? fromIdx - 1 : fromIdx + 1;
    if (toIdx < 0 || toIdx >= rules.length) return;

    const reordered = [...rules];
    const [moved] = reordered.splice(fromIdx, 1);
    reordered.splice(toIdx, 0, moved);

    try {
      const ids = reordered.map((r) => r.id);
      await api.mihomoNativeReorderRules(ids);
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Не удалось переместить правило');
    }
  }

  async function handleRefreshProvider(name: string) {
    refreshingProvider = name;
    try {
      await api.mihomoRuntimeRefreshProvider(name);
      notifications.success(`Провайдер «${name}» обновлён`);
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка обновления провайдера');
    } finally {
      refreshingProvider = null;
    }
  }

  async function handleCheckGroupDelay(name: string) {
    checkingGroupDelay = name;
    try {
      const delay = await api.mihomoRuntimeDelay(name);
      notifications.success(`Задержка «${name}»: ${delay} мс`);
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка проверки задержки группы');
    } finally {
      checkingGroupDelay = null;
    }
  }

  async function confirmDelete() {
    if (!deleteId || !deleteType) return;
    try {
      if (deleteType === 'rule') {
        await api.mihomoNativeDeleteRule(deleteId);
        notifications.success('Правило удалено');
      } else if (deleteType === 'group') {
        await api.mihomoNativeDeleteGroup(deleteId);
        notifications.success('Группа прокси удалена');
      } else if (deleteType === 'provider') {
        await api.mihomoNativeDeleteRuleProvider(deleteId);
        notifications.success('Провайдер правил удален');
      }
      onReload();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка при удалении');
    } finally {
      deleteConfirmOpen = false;
      deleteId = null;
      deleteType = null;
    }
  }
</script>

<div class="expert-view">
  <!-- Top Stat Strip -->
  <StatStrip cells={statCells} />

  <!-- SECTION 1: Rules Table -->
  <SidePanel
    title="Правила маршрутизации"
    count={String(rules.length)}
    section="rules"
  >
    {#snippet actions()}
      <Button variant="secondary" size="sm" onclick={handleOpenTemplates}>
        {#snippet iconBefore()}
          <Sparkles size={14} aria-hidden="true" />
        {/snippet}
        Шаблоны
      </Button>
      <Button variant="secondary" size="sm" onclick={openAddWizard}>
        {#snippet iconBefore()}
          <LayoutGrid size={14} aria-hidden="true" />
        {/snippet}
        Каталог
      </Button>
      <Button variant="primary" size="sm" onclick={handleAddRule}>
        + Правило
      </Button>
    {/snippet}

    <div class="first-match-bar">
      <span class="fm-tag">FIRST-MATCH-WINS · ЕСЛИ НИЧЕГО НЕ ПОДОШЛО →</span>
      <button
        type="button"
        class="fm-chip-btn"
        onclick={handleEditDefaultFallback}
        title="Нажмите, чтобы изменить действие по умолчанию (MATCH)"
      >
        {#if (fallbackOutbound || '').toUpperCase() === 'DIRECT'}
          <div class="tone-chip tone-chip-compact tone-direct">
            <Globe size={11} />
            <span>direct (мимо VPN)</span>
            <Edit3 size={10} class="edit-icon" />
          </div>
        {:else if (fallbackOutbound || '').toUpperCase() === 'REJECT' || (fallbackOutbound || '').toUpperCase() === 'BLOCK'}
          <div class="tone-chip tone-chip-compact tone-block">
            <ShieldOff size={11} />
            <span>Заблокировать</span>
            <Edit3 size={10} class="edit-icon" />
          </div>
        {:else}
          <div class="tone-chip tone-chip-compact tone-composite">
            <Zap size={11} />
            <span>{formatOutbound(fallbackOutbound)}</span>
            <Edit3 size={10} class="edit-icon" />
          </div>
        {/if}
      </button>
    </div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-num">#</th>
            <th class="col-order">Порядок</th>
            <th class="col-action">Действие</th>
            <th class="col-matchers">Условия</th>
            <th class="col-outbound">Выход</th>
            <th class="col-actions">Действия</th>
          </tr>
        </thead>
        <tbody>
          {#if $storeSettings?.susaninEnabled}
            {@const susaninOutbound = $storeSettings.susaninOutbound || 'DIRECT'}
            {@const susaninCount = runtimeProviders['susanin']?.ruleCount ?? 0}
            <tr class="susanin-rule-row">
              <td class="col-num font-mono">⚡</td>
              <td class="col-order">
                <Badge variant="success" size="sm">РАДАР</Badge>
              </td>
              <td class="col-action">
                <span class="action-tag">ROUTE</span>
              </td>
              <td class="col-matchers">
                <div class="matcher-group">
                  <span class="m-type">RULE-SET:</span>
                  <span class="m-val font-mono">susanin</span>
                  <Badge variant="muted" size="sm">{susaninCount} IP</Badge>
                </div>
              </td>
              <td class="col-outbound">
                <div class="tone-chip tone-chip-compact tone-composite">
                  <Zap size={11} />
                  <span>{formatOutbound(susaninOutbound)}</span>
                </div>
              </td>
              <td class="col-actions">
                <span class="managed-label" title="Динамическое правило Susanin формируется автоматически">
                  Радар Susanin
                </span>
              </td>
            </tr>
          {/if}

          {#if rules.length === 0 && !$storeSettings?.susaninEnabled}
            <tr>
              <td colspan="6" class="empty-cell">
                Нет настроенных правил. Нажмите «+ Правило», чтобы добавить.
              </td>
            </tr>
          {:else}
            {#each rules as rule, i (rule.id || i)}
              {@const isDirect = (rule.outbound || '').toUpperCase() === 'DIRECT'}
              {@const isReject = (rule.outbound || '').toUpperCase() === 'REJECT' || (rule.outbound || '').toUpperCase() === 'BLOCK'}
              {@const runtime = runtimeProxies.find((p) => p.name === rule.outbound)}
              <tr>
                <td class="col-num font-mono">{i}</td>
                <td class="col-order">
                  <div class="order-btns">
                    <button
                      type="button"
                      class="order-btn"
                      disabled={i === 0}
                      onclick={() => handleMoveRule(i, 'up')}
                      title="Поднять выше"
                    >
                      <ChevronUp size={14} />
                    </button>
                    <button
                      type="button"
                      class="order-btn"
                      disabled={i === rules.length - 1}
                      onclick={() => handleMoveRule(i, 'down')}
                      title="Опустить ниже"
                    >
                      <ChevronDown size={14} />
                    </button>
                  </div>
                </td>
                <td class="col-action">
                  {#if isReject}
                    <Badge variant="error" size="sm">REJECT</Badge>
                  {:else if isDirect}
                    <Badge variant="default" size="sm">DIRECT</Badge>
                  {:else}
                    <Badge variant="accent" size="sm">ROUTE</Badge>
                  {/if}
                </td>
                <td class="col-matchers font-mono">
                  <div class="matchers-cell">
                    <span class="matcher-text">{rule.type.toLowerCase()}: {rule.payload}</span>
                    {#if rule.noResolve}
                      <span class="tag-badge">no-resolve</span>
                    {/if}
                  </div>
                </td>
                <td class="col-outbound">
                  {#if isDirect}
                    <div class="tone-chip tone-chip-compact tone-direct">
                      <Globe size={11} />
                      <span>direct (мимо VPN)</span>
                    </div>
                  {:else if isReject}
                    <div class="tone-chip tone-chip-compact tone-block">
                      <ShieldOff size={11} />
                      <span>Заблокировать</span>
                    </div>
                  {:else}
                    <div class="tone-chip tone-chip-compact tone-composite">
                      <Zap size={11} />
                      <span>{formatOutbound(rule.outbound)}</span>
                      {#if runtime?.now && runtime.now !== rule.outbound}
                        <span class="live-dot" title={`Активен: ${formatOutbound(runtime.now)}`}>➜ {formatOutbound(runtime.now)}</span>
                      {/if}
                    </div>
                  {/if}
                </td>
                <td class="col-actions">
                  <div class="row-actions">
                    <button
                      type="button"
                      class="act-btn"
                      onclick={() => handleEditRule(rule)}
                      title="Редактировать правило"
                    >
                      <Edit3 size={14} />
                    </button>
                    <button
                      type="button"
                      class="act-btn danger"
                      onclick={() => handleDeleteRule(rule.id)}
                      title="Удалить правило"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </SidePanel>

  <!-- SECTION 2: Rule Providers Table -->
  <SidePanel
    title="Rule-sets / Провайдеры"
    count={String(ruleProviders.length)}
    section="ruleSets"
  >
    {#snippet actions()}
      <Button variant="secondary" size="sm" onclick={handleOpenProviderCatalog}>
        {#snippet iconBefore()}
          <LayoutGrid size={14} aria-hidden="true" />
        {/snippet}
        Каталог
      </Button>
      <Button variant="primary" size="sm" onclick={handleAddProvider}>
        + Провайдер
      </Button>
    {/snippet}

    <!-- Tabs inside panel -->
    <div class="tabs-sub-bar">
      <button
        type="button"
        class="sub-tab-btn"
        class:active={providerTab === 'all'}
        onclick={() => (providerTab = 'all')}
      >
        Все <span class="tab-cnt">({ruleProviders.length})</span>
      </button>
      <button
        type="button"
        class="sub-tab-btn"
        class:active={providerTab === 'http'}
        onclick={() => (providerTab = 'http')}
      >
        Remote (HTTP) <span class="tab-cnt">({ruleProviders.filter((p) => p.type === 'http').length})</span>
      </button>
      <button
        type="button"
        class="sub-tab-btn"
        class:active={providerTab === 'file'}
        onclick={() => (providerTab = 'file')}
      >
        Local (File) <span class="tab-cnt">({ruleProviders.filter((p) => p.type === 'file').length})</span>
      </button>
    </div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>Тег / Имя</th>
            <th>Тип</th>
            <th>Формат / Поведение</th>
            <th>Источник (URL / Path)</th>
            <th>Загрузка через</th>
            <th>Интервал</th>
            <th class="col-actions">Действия</th>
          </tr>
        </thead>
        <tbody>
          {#if filteredProviders.length === 0}
            <tr>
              <td colspan="7" class="empty-cell">Нет провайдеров правил</td>
            </tr>
          {:else}
            {#each filteredProviders as provider (provider.id || provider.name)}
              <tr>
                <td class="font-mono font-bold">
                  <div class="provider-title">
                    <Layers size={13} class="icon-muted" />
                    <span>{provider.name}</span>
                  </div>
                </td>
                <td>
                  <Badge variant={provider.type === 'http' ? 'info' : 'default'} size="sm">
                    {provider.type}
                  </Badge>
                </td>
                <td class="text-secondary text-sm">
                  {provider.behavior} · {provider.format}
                </td>
                <td class="font-mono text-xs text-muted url-cell">
                  {provider.url || provider.path || '—'}
                </td>
                <td>
                  {#if provider.type === 'http'}
                    {#if provider.proxy}
                      <Badge variant="warning" size="sm">
                        {provider.proxy}
                      </Badge>
                    {:else}
                      <span class="text-xs text-muted">DIRECT</span>
                    {/if}
                  {:else}
                    <span class="text-xs text-muted">Локально</span>
                  {/if}
                </td>
                <td class="text-sm text-secondary">
                  {provider.interval ? `${provider.interval}с` : '—'}
                </td>
                <td class="col-actions">
                  <div class="row-actions">
                    {#if provider.type === 'http'}
                      <button
                        type="button"
                        class="act-btn"
                        disabled={refreshingProvider === provider.name}
                        onclick={() => handleRefreshProvider(provider.name)}
                        title="Обновить правила сейчас"
                      >
                        <RefreshCw size={14} class={refreshingProvider === provider.name ? 'spin' : ''} />
                      </button>
                    {/if}
                    <button
                      type="button"
                      class="act-btn"
                      onclick={() => handleEditProvider(provider)}
                      title="Редактировать провайдер"
                    >
                      <Edit3 size={14} />
                    </button>
                    <button
                      type="button"
                      class="act-btn danger"
                      onclick={() => handleDeleteProvider(provider.id)}
                      title="Удалить провайдер"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </SidePanel>

  <!-- SECTION 3: Outbounds / Proxy Groups -->
  <SidePanel
    title="Outbounds / Группы прокси"
    count={String(groups.length)}
    section="outbounds"
  >
    {#snippet actions()}
      <Button variant="primary" size="sm" onclick={handleAddGroup}>
        + Группа
      </Button>
    {/snippet}

    <div class="outbounds-list">
      {#if groups.length === 0}
        <div class="empty-cell" style="padding: 16px;">
          Нет созданных групп прокси. Нажмите «+ Группа», чтобы добавить.
        </div>
      {:else}
        {#each groups as group (group.id || group.name)}
          <MihomoProxyGroupCard
            {group}
            {runtimeProxies}
            {subscriptions}
            onEdit={handleEditGroup}
            onDelete={handleDeleteGroup}
            {onReload}
          />
        {/each}
      {/if}
    </div>
  </SidePanel>

  <!-- SECTION 4: DNS-серверы и правила -->
  <SidePanel
    section="dnsServers"
    title="DNS-серверы и правила"
    count={String($storeDnsServers.length)}
  >
    {#snippet actions()}
      <div style="display: flex; align-items: center; gap: 8px;">
        <Button variant="secondary" size="sm" onclick={() => (dnsRuleAddOpen = true)}>
          + Правило
        </Button>
        <Button variant="primary" size="sm" onclick={() => (dnsServerAddOpen = true)}>
          + Сервер
        </Button>
      </div>
    {/snippet}

    <DnsServersCompact
      servers={$storeDnsServers}
      rules={$storeDnsRules}
      outboundOptions={$storeOptions}
      onEditServer={(tag) => (dnsServerEditTag = tag)}
      onDeleteServer={handleDeleteDnsServer}
      onEditRule={(idx) => (dnsRuleEditIdx = idx)}
      onDeleteRule={handleDeleteDnsRule}
      onMoveRule={handleMoveDnsRule}
      onAddRule={() => (dnsRuleAddOpen = true)}
    />
  </SidePanel>

  <!-- Modals -->
  {#if dnsServerAddOpen}
    <DNSServerEditModal
      servers={$storeDnsServers}
      outboundOptions={$storeOptions}
      onClose={() => (dnsServerAddOpen = false)}
      onSave={handleDnsServerAddSave}
    />
  {/if}

  {#if dnsServerEditTag !== null && editingDnsServer}
    <DNSServerEditModal
      server={editingDnsServer}
      servers={$storeDnsServers}
      outboundOptions={$storeOptions}
      onClose={() => (dnsServerEditTag = null)}
      onSave={handleDnsServerEditSave}
    />
  {/if}

  {#if dnsRuleAddOpen}
    <DNSRuleEditModal
      servers={$storeDnsServers}
      availableRuleSets={ruleProviders.map(p => ({ tag: p.name, type: 'remote' as const, format: (p.format === 'yaml' ? 'source' : 'binary') as 'source' | 'binary' }))}
      existingRoutingRules={rules}
      onClose={() => (dnsRuleAddOpen = false)}
      onSave={handleDnsRuleAddSave}
    />
  {/if}

  {#if dnsRuleEditIdx !== null && editingDnsRule}
    <DNSRuleEditModal
      rule={editingDnsRule}
      ruleIndex={dnsRuleEditIdx}
      servers={$storeDnsServers}
      availableRuleSets={ruleProviders.map(p => ({ tag: p.name, type: 'remote' as const, format: (p.format === 'yaml' ? 'source' : 'binary') as 'source' | 'binary' }))}
      existingRoutingRules={rules}
      onClose={() => (dnsRuleEditIdx = null)}
      onSave={handleDnsRuleEditSave}
    />
  {/if}
  {#if ruleModalOpen}
    <MihomoRuleEditModal
      open={true}
      rule={editingRule}
      isFallback={editingIsFallback}
      {groups}
      {proxies}
      {subscriptions}
      {ruleProviders}
      onClose={() => {
        ruleModalOpen = false;
        editingRule = null;
        editingIsFallback = false;
      }}
      onSaved={() => {
        ruleModalOpen = false;
        editingRule = null;
        editingIsFallback = false;
        onReload();
      }}
    />
  {/if}

  {#if groupModalOpen}
    <MihomoGroupEditModal
      open={true}
      group={editingGroup}
      {groups}
      {proxies}
      {subscriptions}
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

  {#if providerModalOpen}
    <MihomoProviderModal
      open={true}
      provider={editingProvider}
      groups={groups}
      proxies={proxies}
      subscriptions={subscriptions}
      onClose={() => {
        providerModalOpen = false;
        editingProvider = null;
      }}
      onSaved={() => {
        providerModalOpen = false;
        editingProvider = null;
        onReload();
      }}
    />
  {/if}

  {#if templateModalOpen}
    <MihomoTemplateModal
      open={true}
      groups={groups}
      proxies={proxies}
      subscriptions={subscriptions}
      currentRules={rules}
      onClose={() => (templateModalOpen = false)}
      onApplied={() => {
        templateModalOpen = false;
        onReload();
      }}
    />
  {/if}

  {#if providerCatalogOpen}
    <MihomoRuleSetCatalogModal
      open={true}
      groups={groups}
      proxies={proxies}
      subscriptions={subscriptions}
      existingProviders={ruleProviders}
      onClose={() => (providerCatalogOpen = false)}
      onAdded={() => {
        providerCatalogOpen = false;
        onReload();
      }}
    />
  {/if}

  <!-- Delete Confirm Modal -->
  <ConfirmModal
    open={deleteConfirmOpen}
    title={deleteType === 'group' ? 'Удалить группу прокси?' : deleteType === 'provider' ? 'Удалить провайдер правил?' : 'Удалить правило?'}
    message={deleteType === 'group'
      ? 'Вы действительно хотите удалить эту группу прокси Mihomo? Связанные с ней правила могут потребовать обновления.'
      : deleteType === 'provider'
        ? 'Вы действительно хотите удалить этот провайдер правил Mihomo?'
        : 'Вы действительно хотите удалить это правило маршрутизации Mihomo?'}
    confirmLabel="Удалить"
    variant="danger"
    onConfirm={confirmDelete}
    onClose={() => {
      deleteConfirmOpen = false;
      deleteId = null;
      deleteType = null;
    }}
  />
</div>

<style>
  @import '../outboundTone.css';

  .expert-view {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .first-match-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 14px;
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.03));
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    font-family: var(--font-mono);
  }

  .fm-tag {
    color: var(--text-secondary);
    font-weight: 600;
  }

  .fm-chip-btn {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    border-radius: var(--radius-sm);
    transition: transform 0.15s ease, opacity 0.15s ease;
  }

  .fm-chip-btn:hover {
    opacity: 0.85;
    transform: translateY(-1px);
  }

  .fm-chip-btn :global(.edit-icon) {
    margin-left: 4px;
    opacity: 0.6;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
    text-align: left;
  }

  .data-table th {
    padding: 8px 14px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border);
    background: transparent;
  }

  .data-table td {
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    color: var(--text-primary);
    vertical-align: middle;
  }

  .data-table tr:last-child td {
    border-bottom: none;
  }

  .data-table tr:hover td {
    background: var(--bg-hover, rgba(255, 255, 255, 0.02));
  }

  .col-num {
    width: 32px;
    text-align: center;
    color: var(--text-muted);
    font-size: 12px;
  }

  .col-order {
    width: 50px;
  }

  .order-btns {
    display: inline-flex;
    gap: 2px;
  }

  .order-btn {
    background: transparent;
    border: none;
    padding: 2px;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 3px;
  }

  .order-btn:hover:not(:disabled) {
    color: var(--text-primary);
    background: var(--bg-tertiary);
  }

  .order-btn:disabled {
    opacity: 0.25;
    cursor: not-allowed;
  }

  .col-action {
    width: 90px;
  }

  .col-matchers {
    min-width: 200px;
  }

  .matchers-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .matcher-text {
    font-size: 12px;
    color: var(--text-primary);
  }

  .tag-badge {
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    border: 1px dashed var(--border);
    color: var(--text-muted);
  }

  .col-outbound {
    width: 220px;
  }

  .live-dot {
    font-size: 11px;
    color: var(--text-muted);
    font-weight: 500;
  }

  .col-actions {
    width: 80px;
    text-align: right;
  }

  .row-actions {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    justify-content: flex-end;
  }

  .act-btn {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: color var(--t-fast), background var(--t-fast);
  }

  .act-btn:hover {
    color: var(--text-primary);
    background: var(--bg-tertiary);
  }

  .act-btn.danger:hover {
    color: var(--color-error, #ef4444);
    background: rgba(239, 68, 68, 0.1);
  }

  .empty-cell {
    padding: 24px 14px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  /* Sub-tabs bar for Rule Providers */
  .tabs-sub-bar {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 14px;
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.02));
    border-bottom: 1px solid var(--border);
  }

  .sub-tab-btn {
    background: transparent;
    border: none;
    padding: 5px 10px;
    border-radius: var(--radius-sm, 4px);
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: color var(--t-fast), background var(--t-fast);
  }

  .sub-tab-btn:hover {
    color: var(--text-primary);
    background: var(--bg-hover, rgba(255, 255, 255, 0.05));
  }

  .sub-tab-btn.active {
    color: var(--text-primary);
    background: var(--bg-secondary, rgba(255, 255, 255, 0.08));
    font-weight: 600;
  }

  .tab-cnt {
    font-size: 11px;
    color: var(--text-muted);
  }

  .provider-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .icon-muted {
    color: var(--text-muted);
  }

  .url-cell {
    max-width: 250px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Outbounds compact list */
  .outbounds-list {
    display: flex;
    flex-direction: column;
  }

  .outbound-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    transition: background var(--t-fast);
  }

  .outbound-item:last-child {
    border-bottom: none;
  }

  .outbound-item:hover {
    background: var(--bg-hover, rgba(255, 255, 255, 0.02));
  }

  .outbound-main {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 160px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-success, #22c55e);
  }

  .outbound-titles {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .outbound-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .outbound-sub {
    font-size: 11px;
    color: var(--text-muted);
  }

  .outbound-runtime {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .now-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-secondary);
  }

  .now-lbl {
    color: var(--text-muted);
  }

  .now-val {
    font-weight: 600;
    color: var(--text-primary);
  }

  .outbound-actions {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .font-mono {
    font-family: var(--font-mono);
  }

  .font-bold {
    font-weight: 600;
  }

  .text-secondary {
    color: var(--text-secondary);
  }

  .text-muted {
    color: var(--text-muted);
  }

  .text-xs {
    font-size: 11px;
  }

  .text-sm {
    font-size: 12px;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .spin {
    animation: spin 1s linear infinite;
  }

  .susanin-rule-row {
    background: rgba(34, 197, 94, 0.05);
    border-bottom: 1px solid rgba(34, 197, 94, 0.2);
  }

  .managed-label {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
  }
</style>
