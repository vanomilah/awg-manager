<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (MainExpertScreen)
  Главная композиция Expert вида — заменяет старый SingboxRoutingPage.

  Адаптации от шаблона:
  - onSaved → onSave (реальный prop у всех 5 модалов)
  - Все модалы требуют outboundOptions: OutboundGroup[] — берём из store.options
  - RuleEditModal требует availableRuleSets + ruleSetUsage (excludeIndex для edit)
  - DNSServerEditModal требует servers: SingboxRouterDNSServer[]
  - DNSRuleEditModal требует servers + availableRuleSets + ruleSetUsage
  - DNS данные берём из store (dnsServers/dnsRules), не грузим отдельно
  - RuleSetAddModal поддерживает edit-mode через prop ruleSet (необязательный)
  - CompositeOutboundEditModal edit-mode через prop outbound (необязательный)
-->

<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import { notifications } from '$lib/stores/notifications';
  import { api } from '$lib/api/client';
  import { computeRuleSetUsage } from '$lib/components/routing/singboxRouter';
  import type { OutboundGroup } from '$lib/components/routing/singboxRouter/outboundOptions';
  import type {
    SingboxRouterRule,
    SingboxRouterRuleSet,
    SingboxRouterOutbound,
    SingboxRouterDNSServer,
    SingboxRouterDNSRule,
  } from '$lib/types';

  import StatStrip, { type StatCellData } from './StatStrip.svelte';
  import SidePanel from './SidePanel.svelte';
  import RoutingTable from './RoutingTable.svelte';
  import RuleSetsTable from './RuleSetsTable.svelte';
  import OutboundsCompact from './OutboundsCompact.svelte';
  import DnsServersCompact from './DnsServersCompact.svelte';
  import DeviceProxyCompact from './DeviceProxyCompact.svelte';
  import ConnectionsCompact from './ConnectionsCompact.svelte';
  import { openAddWizard } from './addWizardStore';

  import RuleEditModal from '$lib/components/routing/singboxRouter/RuleEditModal.svelte';
  import RuleSetAddModal from '$lib/components/routing/singboxRouter/RuleSetAddModal.svelte';
  import CompositeOutboundEditModal from '$lib/components/routing/singboxRouter/CompositeOutboundEditModal.svelte';
  import DNSServerEditModal from '$lib/components/routing/singboxRouter/DNSServerEditModal.svelte';
  import DNSRuleEditModal from '$lib/components/routing/singboxRouter/DNSRuleEditModal.svelte';
  interface Props { readOnly?: boolean }
  let { readOnly = false }: Props = $props();

  const readOnlyActionTitle = 'Alpha-preview: на реальном роутере изменения отключены. Для изменений используйте рабочий интерфейс.';

  // Store subscriptions
  const storeStatus = singboxRouterStore.status;
  const storeRules = singboxRouterStore.rules;
  const storeRuleSets = singboxRouterStore.ruleSets;
  const storeOutbounds = singboxRouterStore.outbounds;
  const storeDnsServers = singboxRouterStore.dnsServers;
  const storeDnsRules = singboxRouterStore.dnsRules;
  const storeOptions = singboxRouterStore.options;

  let activeProxyCount = $state<number | null>(null);

  async function loadActiveProxyCount() {
    try {
      const proxyInstances = await api.listDeviceProxyInstances();
      activeProxyCount = proxyInstances.filter((in_) => in_.enabled).length;
    } catch {
      activeProxyCount = null;
    }
  }

  // Modal state
  let ruleEditIdx = $state<number | null>(null);
  let rsEditTag = $state<string | null>(null);
  let rsAddOpen = $state(false);
  let outboundEditTag = $state<string | null>(null);
  let outboundAddOpen = $state(false);
  let dnsServerEditTag = $state<string | null>(null);
  let dnsServerAddOpen = $state(false);
  let dnsRuleEditIdx = $state<number | null>(null);
  let dnsRuleAddOpen = $state(false);

  onMount(() => {
    void singboxRouterStore.loadAll();
    void loadActiveProxyCount();
  });

  // Derived modal targets
  const ruleEditTarget = $derived<SingboxRouterRule | undefined>(
    ruleEditIdx !== null ? $storeRules[ruleEditIdx] : undefined
  );
  const rsEditTarget = $derived<SingboxRouterRuleSet | undefined>(
    rsEditTag !== null ? $storeRuleSets.find((rs) => rs.tag === rsEditTag) : undefined
  );
  const outboundEditTarget = $derived<SingboxRouterOutbound | undefined>(
    outboundEditTag !== null ? $storeOutbounds.find((o) => o.tag === outboundEditTag) : undefined
  );
  const dnsServerEditTarget = $derived<SingboxRouterDNSServer | undefined>(
    dnsServerEditTag !== null ? $storeDnsServers.find((s) => s.tag === dnsServerEditTag) : undefined
  );
  const dnsRuleEditTarget = $derived<SingboxRouterDNSRule | undefined>(
    dnsRuleEditIdx !== null ? $storeDnsRules[dnsRuleEditIdx] : undefined
  );

  // ruleSetUsage for RuleEditModal: exclude currently edited index
  const ruleSetUsageForRuleAdd = $derived(computeRuleSetUsage($storeRules));
  const ruleSetUsageForRuleEdit = $derived(
    ruleEditIdx === null
      ? new Map<string, number>()
      : computeRuleSetUsage($storeRules, ruleEditIdx)
  );
  // ruleSetUsage for DNSRuleEditModal: exclude currently edited index
  const ruleSetUsageForDnsAdd = $derived(computeRuleSetUsage($storeDnsRules));
  const ruleSetUsageForDnsEdit = $derived(
    dnsRuleEditIdx === null
      ? new Map<string, number>()
      : computeRuleSetUsage($storeDnsRules, dnsRuleEditIdx)
  );

  const statCells: StatCellData[] = $derived([
    {
      label: 'Движок',
      value: $storeStatus?.enabled ? 'ON' : 'OFF',
      tone: $storeStatus?.enabled ? 'success' : 'muted',
    },
    { label: 'Правил', value: String($storeRules.length) },
    { label: 'Rule-sets', value: String($storeRuleSets.length) },
    { label: 'Outbounds', value: String($storeOutbounds.length) },
    { label: 'DNS правил', value: String($storeDnsRules.length) },
    { label: 'Прокси', value: activeProxyCount === null ? '—' : String(activeProxyCount) },
  ]);

  // Rule handlers
  async function handleDeleteRule(idx: number) {
    if (readOnly) return;
    if (!confirm(`Удалить правило #${idx}?`)) return;
    try {
      await api.singboxRouterDeleteRule(idx);
      await singboxRouterStore.loadAll();
      notifications.success('Правило удалено');
    } catch (e) {
      notifications.error(`Ошибка: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  async function handleMoveRule(idx: number, dir: 'up' | 'down') {
    if (readOnly) return;
    const to = dir === 'up' ? idx - 1 : idx + 1;
    if (to < 0 || to >= $storeRules.length) return;
    try {
      await api.singboxRouterMoveRule(idx, to);
      await singboxRouterStore.loadAll();
    } catch (e) {
      notifications.error(`Ошибка: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  // Rule save handlers (called by modals)
  async function handleRuleSave(rule: SingboxRouterRule) {
    if (readOnly) return;
    if (ruleEditIdx !== null) {
      await api.singboxRouterUpdateRule(ruleEditIdx, rule);
    } else {
      await api.singboxRouterAddRule(rule);
    }
    ruleEditIdx = null;
    await singboxRouterStore.loadAll();
  }

  // RuleSet handlers
  async function handleDeleteRs(tag: string) {
    if (readOnly) return;
    if (!confirm(`Удалить набор «${tag}»?`)) return;
    try {
      await api.singboxRouterDeleteRuleSet(tag);
      await singboxRouterStore.loadAll();
      notifications.success('Набор удалён');
    } catch (e) {
      notifications.error(`Ошибка: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  async function handleRsAddSave(rs: SingboxRouterRuleSet) {
    if (readOnly) return;
    await api.singboxRouterAddRuleSet(rs);
    rsAddOpen = false;
    await singboxRouterStore.loadAll();
  }

  async function handleRsEditSave(rs: SingboxRouterRuleSet) {
    if (readOnly) return;
    if (rsEditTag !== null) {
      await api.singboxRouterUpdateRuleSet(rsEditTag, rs);
    }
    rsEditTag = null;
    await singboxRouterStore.loadAll();
  }

  // Outbound handlers
  async function handleOutboundAddSave(o: SingboxRouterOutbound) {
    if (readOnly) return;
    await api.singboxRouterAddOutbound(o);
    outboundAddOpen = false;
    await singboxRouterStore.loadAll();
  }

  async function handleOutboundEditSave(o: SingboxRouterOutbound) {
    if (readOnly) return;
    if (outboundEditTag !== null) {
      await api.singboxRouterUpdateOutbound(outboundEditTag, o);
    }
    outboundEditTag = null;
    await singboxRouterStore.loadAll();
  }

  // DNS server handlers
  async function handleDnsServerAddSave(server: SingboxRouterDNSServer) {
    if (readOnly) return;
    await api.singboxRouterAddDNSServer(server);
    dnsServerAddOpen = false;
    await singboxRouterStore.loadAll();
  }

  async function handleDnsServerEditSave(server: SingboxRouterDNSServer) {
    if (readOnly) return;
    if (dnsServerEditTag !== null) {
      await api.singboxRouterUpdateDNSServer(dnsServerEditTag, server);
    }
    dnsServerEditTag = null;
    await singboxRouterStore.loadAll();
  }

  // DNS rule handlers
  async function handleDnsRuleAddSave(rule: SingboxRouterDNSRule) {
    if (readOnly) return;
    await api.singboxRouterAddDNSRule(rule);
    dnsRuleAddOpen = false;
    await singboxRouterStore.loadAll();
  }

  async function handleDnsRuleEditSave(rule: SingboxRouterDNSRule) {
    if (readOnly) return;
    if (dnsRuleEditIdx !== null) {
      await api.singboxRouterUpdateDNSRule(dnsRuleEditIdx, rule);
    }
    dnsRuleEditIdx = null;
    await singboxRouterStore.loadAll();
  }

  function navigateDeviceProxy() {
    void goto('/routing?tab=singbox&sub=deviceproxy');
  }
  function navigateConnections() {
    void goto('/routing?tab=singbox&sub=connections');
  }
  function navigateEngine() {
    void goto('/routing?tab=singbox&sub=engine');
  }
  function navigatePresets() {
    void goto('/routing?tab=singbox&sub=presets');
  }
</script>


<div class="wrap">
  <StatStrip cells={statCells} />

  <div class="main-grid">
    <div class="col-main">
      <SidePanel
        title="Правила маршрутизации"
        count={String($storeRules.length)}
        actionLabel="+ Правило"
        actionDisabled={readOnly}
        actionTitle={readOnly ? readOnlyActionTitle : undefined}
        onAction={() => { if (!readOnly) openAddWizard(); }}
      >
        <div class="panel-cap">first-match-wins · final → direct</div>
        <RoutingTable
          bare
          rules={$storeRules}
          outbounds={$storeOutbounds}
          onEdit={(idx) => { if (!readOnly) ruleEditIdx = idx; }}
          onDelete={handleDeleteRule}
          onMove={handleMoveRule}
        />
      </SidePanel>

      <SidePanel
        title="Rule-sets"
        count={String($storeRuleSets.length)}
        actionLabel="+ Набор"
        actionDisabled={readOnly}
        actionTitle={readOnly ? readOnlyActionTitle : undefined}
        onAction={() => { if (!readOnly) rsAddOpen = true; }}
      >
        <div class="panel-cap">наборы доменов и IP, на которые ссылаются правила</div>
        <RuleSetsTable
          bare
          ruleSets={$storeRuleSets}
          onEdit={(tag) => { if (!readOnly) rsEditTag = tag; }}
          onDelete={handleDeleteRs}
        />
      </SidePanel>
    </div>

    <div class="col-sidebar">
      <SidePanel
        title="Outbounds"
        count={String($storeOutbounds.length)}
        actionLabel="+ Composite"
        actionDisabled={readOnly}
        actionTitle={readOnly ? readOnlyActionTitle : undefined}
        onAction={() => { if (!readOnly) outboundAddOpen = true; }}
      >
        <OutboundsCompact
          outbounds={$storeOutbounds}
          onEdit={(tag) => { if (!readOnly) outboundEditTag = tag; }}
        />
      </SidePanel>

      <SidePanel
        title="DNS-серверы"
        count={String($storeDnsServers.length)}
        actionLabel="+ Сервер"
        actionDisabled={readOnly}
        actionTitle={readOnly ? readOnlyActionTitle : undefined}
        onAction={() => { if (!readOnly) dnsServerAddOpen = true; }}
      >
        <DnsServersCompact
          servers={$storeDnsServers}
          rules={$storeDnsRules}
          onEditServer={(tag) => { if (!readOnly) dnsServerEditTag = tag; }}
          onEditRule={(idx) => { if (!readOnly) dnsRuleEditIdx = idx; }}
          onAddRule={() => { if (!readOnly) dnsRuleAddOpen = true; }}
          addRuleDisabled={readOnly}
          addRuleTitle={readOnly ? readOnlyActionTitle : undefined}
        />
      </SidePanel>

      <SidePanel title="Движок" count="" actionLabel="Управление →" onAction={navigateEngine}>
        <div class="panel-cap">статус, установка, запуск/остановка, WAN, policy</div>
      </SidePanel>

      <SidePanel title="Пресеты" count="" actionLabel="Открыть →" onAction={navigatePresets}>
        <div class="panel-cap">готовые конфигурации правил</div>
      </SidePanel>

      <SidePanel
        title="Прокси устройств"
        count=""
        actionLabel="Настроить →"
        onAction={navigateDeviceProxy}
      >
        <DeviceProxyCompact bare onConfigure={navigateDeviceProxy} />
      </SidePanel>

      <SidePanel
        title="Живые соединения"
        count=""
        actionLabel="Открыть →"
        onAction={navigateConnections}
      >
        <ConnectionsCompact />
      </SidePanel>
    </div>
  </div>
</div>

<!-- RuleEditModal: add-mode (ruleEditIdx stays null until modal closes) -->
{#if ruleEditIdx !== null && ruleEditTarget !== undefined}
  <RuleEditModal
    rule={ruleEditTarget}
    outboundOptions={$storeOptions}
    availableRuleSets={$storeRuleSets}
    ruleSetUsage={ruleSetUsageForRuleEdit}
    onClose={() => (ruleEditIdx = null)}
    onSave={handleRuleSave}
  />
{/if}

<!-- RuleSetAddModal: add -->
{#if rsAddOpen}
  <RuleSetAddModal
    outboundOptions={$storeOptions}
    onClose={() => (rsAddOpen = false)}
    onSave={handleRsAddSave}
  />
{/if}

<!-- RuleSetAddModal: edit (ruleSet prop activates edit-mode) -->
{#if rsEditTag !== null && rsEditTarget !== undefined}
  <RuleSetAddModal
    ruleSet={rsEditTarget}
    outboundOptions={$storeOptions}
    onClose={() => (rsEditTag = null)}
    onSave={handleRsEditSave}
  />
{/if}

<!-- CompositeOutboundEditModal: add -->
{#if outboundAddOpen}
  <CompositeOutboundEditModal
    outboundOptions={$storeOptions}
    onClose={() => (outboundAddOpen = false)}
    onSave={handleOutboundAddSave}
  />
{/if}

<!-- CompositeOutboundEditModal: edit -->
{#if outboundEditTag !== null && outboundEditTarget !== undefined}
  <CompositeOutboundEditModal
    outbound={outboundEditTarget}
    outboundOptions={$storeOptions}
    onClose={() => (outboundEditTag = null)}
    onSave={handleOutboundEditSave}
  />
{/if}

<!-- DNSServerEditModal: add -->
{#if dnsServerAddOpen}
  <DNSServerEditModal
    servers={$storeDnsServers}
    outboundOptions={$storeOptions}
    onClose={() => (dnsServerAddOpen = false)}
    onSave={handleDnsServerAddSave}
  />
{/if}

<!-- DNSServerEditModal: edit -->
{#if dnsServerEditTag !== null && dnsServerEditTarget !== undefined}
  <DNSServerEditModal
    server={dnsServerEditTarget}
    servers={$storeDnsServers}
    outboundOptions={$storeOptions}
    onClose={() => (dnsServerEditTag = null)}
    onSave={handleDnsServerEditSave}
  />
{/if}

<!-- DNSRuleEditModal: add -->
{#if dnsRuleAddOpen}
  <DNSRuleEditModal
    servers={$storeDnsServers}
    availableRuleSets={$storeRuleSets}
    ruleSetUsage={ruleSetUsageForDnsAdd}
    onClose={() => (dnsRuleAddOpen = false)}
    onSave={handleDnsRuleAddSave}
  />
{/if}

<!-- DNSRuleEditModal: edit -->
{#if dnsRuleEditIdx !== null && dnsRuleEditTarget !== undefined}
  <DNSRuleEditModal
    rule={dnsRuleEditTarget}
    servers={$storeDnsServers}
    availableRuleSets={$storeRuleSets}
    ruleSetUsage={ruleSetUsageForDnsEdit}
    onClose={() => (dnsRuleEditIdx = null)}
    onSave={handleDnsRuleEditSave}
  />
{/if}

<style>
  .wrap {
    max-width: none;
    margin: 0 auto;
    padding: var(--sp-4);
  }
  /* Caption внутри SidePanel body — sub-title строкой над контентом */
  .panel-cap {
    padding: 8px 14px;
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .main-grid {
    display: grid;
    grid-template-columns: minmax(0, 8fr) minmax(0, 4fr);
    gap: 14px;
  }
  .col-main {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .col-sidebar {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  @media (max-width: 1023px) {
    .main-grid {
      grid-template-columns: 1fr;
    }
  }
  @media (max-width: 768px) {
    .wrap {
      padding: var(--sp-2);
    }
  }
</style>
