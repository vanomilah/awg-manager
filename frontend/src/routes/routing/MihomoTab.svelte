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

  let installing = $state(false);
  let updating = $state(false);

  async function installMihomo(): Promise<void> {
    if (installing) return;
    installing = true;
    try {
      status = await api.mihomoInstall();
      notifications.success('Mihomo установлен');
      await refresh(true);
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      installing = false;
    }
  }

  async function updateMihomo(): Promise<void> {
    if (updating) return;
    updating = true;
    try {
      status = await api.mihomoUpdate();
      notifications.success('Mihomo обновлён');
      await refresh(true);
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      updating = false;
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

  let reconciling = $state(false);

  async function reconcile(): Promise<void> {
    if (reconciling) return;
    reconciling = true;
    try {
      await api.mihomoReconcile('rollback_to_lkg', true);
      notifications.success('Восстановление Mihomo выполнено успешно');
      await refresh(true);
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      reconciling = false;
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

  {#if status?.degraded}
    <div class="degraded-banner" role="alert">
      <div class="degraded-content">
        <strong>Внимание: Mihomo в защитном режиме (Degraded)</strong>
        <p>Произошел сбой транзакции или обнаружен маркер восстановления. Изменения заблокированы до завершения административного восстановления.</p>
      </div>
      <Button variant="danger" size="sm" onclick={reconcile} disabled={reconciling}>
        <span class:spin={reconciling}><RotateCw size={14} /></span>
        Восстановить (LKG)
      </Button>
    </div>
  {/if}

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
            <div>
              <dt>Статус бинарника</dt>
              <dd>
                {#if status?.installed}
                  <span class="text-success">Установлен (v{status.currentVersion || status.version || '—'})</span>
                  {#if status?.updateAvailable}
                    <Badge variant="warning">Доступно обновление v{status.requiredVersion}</Badge>
                  {/if}
                {:else}
                  <span class="text-warning">Не установлен</span>
                {/if}
              </dd>
            </div>
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
            {#if status && !status.installed}
              <Button
                variant="primary"
                size="md"
                onclick={installMihomo}
                loading={installing}
              >
                {installing ? 'Установка...' : 'Установить Mihomo в 1 клик'}
              </Button>
            {:else if status?.updateAvailable}
              <Button
                variant="primary"
                size="md"
                onclick={updateMihomo}
                loading={updating}
              >
                {updating ? 'Обновление...' : 'Обновить Mihomo'}
              </Button>
            {/if}
            <Button
              variant="secondary"
              size="md"
              onclick={reload}
              loading={reloading}
              disabled={!status?.enabled || !status?.installed}
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
  .degraded-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.875rem 1.125rem;
    border-radius: var(--radius-sm, 6px);
    background: rgba(239, 68, 68, 0.12);
    border: 1px solid rgba(239, 68, 68, 0.35);
    color: var(--color-error, #ef4444);
  }
  .degraded-content p {
    margin: 0.25rem 0 0 0;
    font-size: 0.8125rem;
    color: var(--color-text-secondary);
  }
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
