<script lang="ts">
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { RefreshCw, RotateCw } from 'lucide-svelte';
  import { api } from '$lib/api/client';
  import { singboxRouter } from '$lib/stores/singboxRouter';
  import { notifications } from '$lib/stores/notifications';
  import type { MihomoStatus } from '$lib/types';
  import { Badge, Button, Card, SegmentedControl, StatusDot } from '$lib/components/ui';
  import MihomoRuntimePanel from './MihomoRuntimePanel.svelte';
  import MihomoConfigPanel from './MihomoConfigPanel.svelte';
  import MihomoPolicyPanel from './MihomoPolicyPanel.svelte';

  const settingsStore = singboxRouter.settings;
  let status = $state<MihomoStatus | null>(null);
  let loading = $state(true);
  let reloading = $state(false);
  let selecting = $state(false);
  let loadError = $state('');
  let workspace = $state<'config' | 'runtime'>('config');

  async function refresh(silent = false): Promise<void> {
    if (!silent) loading = true;
    try {
      status = await api.mihomoStatus();
      loadError = '';
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function reload(): Promise<void> {
    if (reloading) return;
    reloading = true;
    try {
      await api.mihomoReload();
      notifications.success('Конфигурация Mihomo обновлена');
      await refresh(true);
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
      await refresh(true);
    } finally {
      reloading = false;
    }
  }

  async function setEngineMode(mode: 'sing-box' | 'mihomo'): Promise<void> {
    if (selecting) return;
    selecting = true;
    try {
      if (!get(settingsStore)) await singboxRouter.reloadSettings();
      const settings = get(settingsStore);
      if (!settings) throw new Error('Настройки маршрутизации ещё не загружены');
      await api.singboxRouterPutSettings({ ...settings, routingEngine: mode });
      await singboxRouter.loadAll();
      const labels: Record<string, string> = {
        'sing-box': 'Sing-box выбран основным движком',
        'mihomo': 'Mihomo выбран основным движком'
      };
      notifications.success(labels[mode]);
      await refresh(true);
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      selecting = false;
    }
  }

  $effect(() => {
    const s = $settingsStore;
    if (s) currentEngine = s.routingEngine === 'mihomo' ? 'mihomo' : 'sing-box';
  });
  let currentEngine = $state<'sing-box' | 'mihomo'>('sing-box');

  onMount(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(true), 5000);
    return () => window.clearInterval(timer);
  });
</script>

<div class="mihomo-tab">
  <div class="tab-heading">
    <div>
      <h2>Mihomo</h2>
      <p>Состояние движка и применение сгенерированной конфигурации.</p>
    </div>
    <Button variant="secondary" size="sm" onclick={() => refresh()} disabled={loading}>
      <span class:spin={loading}><RefreshCw size={14} /></span>
      Обновить
    </Button>
  </div>

  <div class="workspace-switch">
    <SegmentedControl
      value={workspace}
      options={[{ value: 'config', label: 'Конфигурация' }, { value: 'runtime', label: 'Прокси' }]}
      ariaLabel="Раздел Mihomo"
      onchange={(value) => workspace = value as typeof workspace}
    />
  </div>

  {#if workspace === 'runtime'}
    <MihomoRuntimePanel />
  {:else}
  <MihomoConfigPanel />
  <MihomoPolicyPanel />

  {#if loadError && !status}
    <div class="error-panel" role="alert">{loadError}</div>
  {:else}
    <div class="cards">
      <Card padding="lg">
        <div class="status-card">
          <div class="status-title">
            <StatusDot
              variant={status?.running ? 'success' : status?.error ? 'error' : 'muted'}
              pulse={status?.running ?? false}
              ariaLabel={status?.running ? 'Mihomo запущен' : 'Mihomo остановлен'}
            />
            <strong>{status?.running ? 'Процесс запущен' : 'Процесс остановлен'}</strong>
            <Badge variant={status?.active ? 'success' : status?.selected ? 'warning' : 'muted'}>
              {status?.active ? 'активен' : status?.selected ? 'выбран' : 'не выбран'}
            </Badge>
          </div>

          <dl>
            <div><dt>PID</dt><dd>{status?.pid || '—'}</dd></div>
            <div><dt>Движок выбран</dt><dd>{status?.selected ? 'Да' : 'Нет'}</dd></div>
            <div><dt>Маршрутизация включена</dt><dd>{status?.enabled ? 'Да' : 'Нет'}</dd></div>
            <div><dt>Бинарный файл</dt><dd class="mono">{status?.binary || '—'}</dd></div>
          </dl>

          {#if status?.error || status?.settingsError || loadError}
            <div class="error-panel" role="alert">
              {status?.error || status?.settingsError || loadError}
            </div>
          {/if}

          <div class="engine-mode">
            <span class="engine-mode-label">Основной движок маршрутизации</span>
            <SegmentedControl
              value={currentEngine}
              options={[
                { value: 'sing-box', label: 'Sing-box' },
                { value: 'mihomo', label: 'Mihomo' }
              ]}
              ariaLabel="Основной движок маршрутизации"
              onchange={(value) => setEngineMode(value as 'sing-box' | 'mihomo')}
            />
            <small class="engine-mode-hint">
              {#if currentEngine === 'sing-box'}
                Sing-box управляет сетевой маршрутизацией. Прокси и подписки Mihomo работают в фоне.
              {:else}
                Mihomo управляет сетевой маршрутизацией. Подписки и прокси Sing-box подключены к Mihomo.
              {/if}
            </small>
          </div>

          <div class="actions">
            <Button
              variant="secondary"
              size="md"
              onclick={reload}
              loading={reloading}
              disabled={!status?.enabled}
              title={!status?.enabled
                ? 'Сначала включите маршрутизацию'
                : 'Сгенерировать конфигурацию и перезагрузить Mihomo'}
            >
              <RotateCw size={16} />
              Применить и перезагрузить
            </Button>
          </div>
        </div>
      </Card>

      <Card padding="lg">
        <div class="shared-config">
          <h3>Нативная конфигурация Mihomo</h3>
          <p>
            Прокси, подписки, providers, группы и правила хранятся отдельно от sing-box.
            Генератор собирает их в <span class="mono">mihomo/config.yaml</span>, проверяет ядром и только затем применяет.
          </p>
          <div class="ownership"><span><strong>Правила</strong><small>редактор выше</small></span><span><strong>Proxy-группы</strong><small>редактор выше</small></span><span><strong>Выбор узлов</strong><small>без перезапуска</small></span></div>
          <Button variant="ghost" size="sm" onclick={() => workspace = 'runtime'}>Открыть управление прокси</Button>
        </div>
      </Card>
    </div>
  {/if}

  {/if}
</div>

<style>
  .mihomo-tab { display: grid; gap: 1rem; min-width: 0; }
  .workspace-switch { display: flex; border-bottom: 1px solid var(--border); padding-bottom: .75rem; }
  .tab-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
  h2, h3, p { margin: 0; }
  h2 { font-size: 1.25rem; color: var(--color-text-primary); }
  h3 { font-size: 1rem; color: var(--color-text-primary); }
  .tab-heading p, .shared-config p { margin-top: 0.3rem; color: var(--color-text-secondary); font-size: 0.875rem; line-height: 1.5; }
  .cards { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(280px, 0.7fr); gap: 1rem; }
  .status-card, .shared-config { display: grid; gap: 1rem; }
  .engine-mode { display: grid; gap: 0.4rem; }
  .engine-mode-label { font-size: 0.8125rem; font-weight: 600; color: var(--color-text-primary); }
  .engine-mode-hint { color: var(--color-text-muted); font-size: 0.75rem; }
  .status-title { display: flex; align-items: center; flex-wrap: wrap; gap: 0.55rem; }
  dl { display: grid; gap: 0.6rem; margin: 0; }
  dl > div { display: grid; grid-template-columns: minmax(150px, 0.7fr) minmax(0, 1.3fr); gap: 1rem; }
  dt { color: var(--color-text-muted); font-size: 0.8125rem; }
  dd { margin: 0; color: var(--color-text-primary); overflow-wrap: anywhere; }
  .mono { font-family: var(--font-mono); font-size: 0.8125rem; }
  .actions { display: flex; flex-wrap: wrap; gap: 0.625rem; }
  .error-panel { padding: 0.75rem; border: 1px solid var(--color-error-border); border-radius: var(--radius-sm); background: var(--color-error-tint); color: var(--color-error); font-size: 0.8125rem; overflow-wrap: anywhere; }
  .ownership { display: grid; grid-template-columns: repeat(3, 1fr); gap: .5rem; }
  .ownership span { display: grid; gap: .15rem; padding: .7rem; border-radius: var(--radius-sm); background: var(--color-bg-tertiary); }
  .ownership strong { font-size: .78rem; color: var(--color-text-primary); }
  .ownership small { color: var(--color-text-muted); font-size: .68rem; }
  :global(.spin) { animation: spin 0.9s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (max-width: 820px) {
    .cards { grid-template-columns: 1fr; }
    .tab-heading { align-items: stretch; flex-direction: column; }
    .ownership { grid-template-columns: 1fr; }
  }
</style>
