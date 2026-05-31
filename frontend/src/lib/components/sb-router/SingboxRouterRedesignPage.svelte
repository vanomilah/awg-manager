<script lang="ts">
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { ArrowLeft } from 'lucide-svelte';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import { DeviceProxySubTab, StagingBanner, EngineSubTab, PresetsSubTab, RouteInspector, JsonConfigDrawer } from '$lib/components/singbox-routing';
  import { ConnectionsSubTab } from '$lib/components/routing/singboxRouter';
  import {
    PageShell,
    RulesPanel,
    FlowGraph,
    TracePanel,
    traceOpen,
    AddWizardPanel,
    addWizardOpen,
    EmptyState,
    ExpertPanel,
    mode as sbMode,
    type EngineStatus,
  } from '$lib/components/sb-router';

  let activeSingboxSub = $derived($page.url.searchParams.get('sub'));
  let inspectorOpen = $state(false);
  let jsonOpen = $state(false);
  const singboxRouterStatus = singboxRouterStore.status;
  const singboxRulesStore = singboxRouterStore.rules;
  let singboxRulesCount = $derived($singboxRulesStore.length);

  let sbEngineStatus: EngineStatus = $derived.by(() => {
    const s = $singboxRouterStatus;
    if (!s || !s.installed) return 'unknown';
    return s.enabled ? 'ok' : 'down';
  });

  // Активен ли отдельный sub-вид (рендерится на всю страницу) — для кнопки «Назад».
  let inSubView = $derived(
    activeSingboxSub === 'deviceproxy' ||
    activeSingboxSub === 'connections' ||
    ((activeSingboxSub === 'engine' || activeSingboxSub === 'presets') && $sbMode === 'expert'),
  );

  function clearSub() {
    const url = new URL(window.location.href);
    url.searchParams.delete('sub');
    void goto(`${url.pathname}${url.search}`, { keepFocus: true, noScroll: true });
  }

  $effect(() => {
    if ($sbMode === 'beginner' && (activeSingboxSub === 'engine' || activeSingboxSub === 'presets')) {
      const url = new URL(window.location.href);
      url.searchParams.delete('sub');
      void goto(`${url.pathname}${url.search}`, { replaceState: true, keepFocus: true, noScroll: true });
    }
  });
</script>

<PageShell engineStatus={sbEngineStatus} onOpenInspector={() => (inspectorOpen = true)} onOpenJson={() => (jsonOpen = true)}>
  <StagingBanner />
  {#if inSubView}
    <button type="button" class="sub-back" onclick={clearSub}>
      <ArrowLeft size={14} /> Назад
    </button>
  {/if}
  {#if activeSingboxSub === 'engine' && $sbMode === 'expert'}
    <EngineSubTab />
  {:else if activeSingboxSub === 'presets' && $sbMode === 'expert'}
    <PresetsSubTab />
  {:else if activeSingboxSub === 'deviceproxy'}
    <DeviceProxySubTab />
  {:else if activeSingboxSub === 'connections'}
    <ConnectionsSubTab />
  {:else if $sbMode === 'beginner'}
    {#if $addWizardOpen}
      <AddWizardPanel />
    {:else if $traceOpen}
      <TracePanel />
    {:else if !($singboxRouterStatus?.enabled ?? false) || singboxRulesCount === 0}
      <EmptyState />
    {:else}
      <FlowGraph />
      <RulesPanel />
    {/if}
  {:else}
    <ExpertPanel />
  {/if}
</PageShell>

<RouteInspector open={inspectorOpen} onClose={() => (inspectorOpen = false)} />
<JsonConfigDrawer open={jsonOpen} onClose={() => (jsonOpen = false)} />

<style>
  .sub-back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 12px;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
  }
  .sub-back:hover {
    color: var(--text-primary);
    border-color: var(--border-hover, var(--accent-line));
  }
</style>
