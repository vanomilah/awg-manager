<script lang="ts" module>
  import type { Snippet } from 'svelte';
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import { FileJson, Search, Settings } from 'lucide-svelte';
  import { mode, setMode, type RouterMode } from './modeStore';
  import { bindLiveConnectionsStore } from './liveConnectionsStore';
  import { openDrawer } from './drawerStore';
  import StatusDrawer from './StatusDrawer.svelte';
  import SourceDrawer from './SourceDrawer.svelte';
  import LiveConnectionsChip from './LiveConnectionsChip.svelte';

  interface Props {
    /** Дополнительный subtitle под title (опционально). */
    subtitle?: string;
    /** Открыть инспектор маршрута (кнопка в шапке рендерится только если задан). */
    onOpenInspector?: () => void;
    /** Открыть JSON-конфиг (кнопка в шапке рендерится только если задан). */
    onOpenJson?: () => void;
    /** Дочерний контент страницы. */
    children: Snippet;
  }

  let { subtitle, onOpenInspector, onOpenJson, children }: Props = $props();
  let currentMode = $derived($mode);

  onMount(() => {
    bindLiveConnectionsStore();
  });

  function selectMode(next: RouterMode) {
    setMode(next);
  }
</script>

<div class="sb-shell">
  <header class="sb-header">
    <div class="title-block">
      <h1 class="title">Маршрутизация · sing-box</h1>
      {#if subtitle}<div class="subtitle">{subtitle}</div>{/if}
    </div>

    <div class="header-tools">
      <LiveConnectionsChip />

      <button type="button" class="params-btn" onclick={openDrawer} aria-label="Параметры sing-box">
        <Settings size={16} aria-hidden="true" />
        <span class="params-text">Параметры sing-box</span>
      </button>

      <div class="header-actions">
        {#if onOpenInspector}
          <button type="button" class="icon-btn" onclick={onOpenInspector} aria-label="Инспектор маршрута" title="Инспектор маршрута">
            <span class="action-icon"><Search size={16} /></span>
            <span class="action-text">Инспектор</span>
          </button>
        {/if}
        {#if onOpenJson}
          <button type="button" class="icon-btn" onclick={onOpenJson} aria-label="JSON-конфиг" title="JSON-конфиг">
            <span class="action-icon"><FileJson size={16} /></span>
            <span class="action-text">Конфиг</span>
          </button>
        {/if}
      </div>

      <div class="view-mode-switch" role="group" aria-label="Режим интерфейса">
        <button
          type="button"
          class="view-mode-btn"
          class:active={currentMode === 'beginner'}
          aria-pressed={currentMode === 'beginner'}
          onclick={() => selectMode('beginner')}
        >Простой</button>
        <button
          type="button"
          class="view-mode-btn"
          class:active={currentMode === 'expert'}
          aria-pressed={currentMode === 'expert'}
          onclick={() => selectMode('expert')}
        >Эксперт</button>
      </div>
    </div>
  </header>

  <div class="sb-body">
    {@render children()}
  </div>
</div>

<StatusDrawer />
<SourceDrawer />

<style>
  .sb-shell { display: flex; flex-direction: column; gap: var(--sp-4); }

  .sb-header {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-3) 0;
    border-bottom: 1px solid var(--border);
    flex-wrap: wrap;
  }

  .title-block { flex: 1 1 auto; min-width: 0; }
  .title {
    margin: 0;
    font-size: var(--fs-h3);
    font-weight: 600;
    color: var(--text-primary);
    line-height: var(--lh-tight);
  }
  .subtitle {
    margin-top: 2px;
    font-size: var(--fs-sm);
    color: var(--text-muted);
  }

  .header-tools {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .header-tools :global(.chip),
  .params-btn,
  .header-actions .icon-btn,
  .view-mode-switch {
    height: 32px;
    box-sizing: border-box;
  }

  .params-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    background: var(--color-bg-secondary, var(--bg-secondary));
    border: 1px solid var(--color-border, var(--border));
    color: var(--color-text-secondary, var(--text-secondary));
    font-size: 12px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .params-btn:hover {
    color: var(--color-text-primary, var(--text-primary));
    background: var(--color-bg-hover, var(--bg-tertiary));
    border-color: var(--border-hover, var(--accent-line));
  }
  .params-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .icon-btn {
    background: var(--color-bg-secondary, var(--bg-secondary));
    border: 1px solid var(--color-border, var(--border));
    color: var(--color-text-secondary, var(--text-secondary));
    padding: 0 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .action-text { display: none; }
  .icon-btn:hover {
    color: var(--color-text-primary, var(--text-primary));
    background: var(--color-bg-hover, var(--bg-tertiary));
  }

  .header-actions {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .view-mode-switch {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 2px;
    border: 1px solid var(--color-border, var(--border));
    border-radius: var(--radius-sm);
    background: var(--color-bg-secondary, var(--bg-secondary));
    flex-shrink: 0;
  }

  .view-mode-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 26px;
    padding: 0 12px;
    border: none;
    border-radius: calc(var(--radius-sm) - 2px);
    background: transparent;
    color: var(--color-text-muted, var(--text-muted));
    font-size: 12px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    white-space: nowrap;
    transition:
      background var(--t-fast) ease,
      color var(--t-fast) ease;
  }

  .view-mode-btn:hover {
    background: var(--color-bg-hover, var(--bg-tertiary));
    color: var(--color-text-primary, var(--text-primary));
  }

  .view-mode-btn.active {
    background: var(--color-accent-tint, var(--accent-soft));
    color: var(--color-accent, var(--accent));
    font-weight: 600;
  }

  .view-mode-btn:focus-visible {
    outline: 2px solid var(--color-accent, var(--accent));
    outline-offset: 2px;
  }

  .sb-body { width: 100%; }

  @media (max-width: 768px) {
    .sb-header {
      flex-direction: column;
      align-items: stretch;
      gap: 10px;
    }
    .title-block {
      text-align: left;
    }
    .title {
      font-size: 18px;
    }
    .header-tools {
      width: 100%;
      flex-direction: column;
      align-items: stretch;
      gap: 8px;
    }
    .params-btn,
    .header-tools :global(.chip),
    .view-mode-switch {
      width: 100%;
      flex: none;
      justify-content: center;
    }
    .view-mode-btn {
      flex: 1;
      min-width: 0;
    }
    .header-actions {
      width: 100%;
    }
    .sb-body {
      padding: 0 12px;
    }
  }

  @media (max-width: 700px) {
    .sb-header {
      display: grid;
      grid-template-columns: 1fr;
      align-items: stretch;
      gap: 0.625rem;
    }

    .header-tools {
      width: 100%;
      min-width: 0;
      flex-direction: column;
      align-items: stretch;
      gap: 0.5rem;
    }

    .header-tools :global(.chip),
    .params-btn,
    .view-mode-switch {
      width: 100%;
      flex: none;
      min-width: 0;
      justify-content: center;
    }

    .view-mode-btn {
      flex: 1;
      min-width: 0;
    }

    .header-actions {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      align-items: stretch;
      gap: 0.5rem;
      width: 100%;
      min-width: 0;
    }

    .header-actions .icon-btn,
    .header-actions :global(button) {
      width: 100%;
      min-width: 0;
      max-width: 100%;
      height: 32px;
      justify-content: center;
      white-space: nowrap;
    }

    .header-actions .icon-btn {
      gap: 0.375rem;
      padding-inline: 0.625rem;
    }

    .header-actions .action-text {
      display: inline;
      font-size: var(--fs-sm);
      font-weight: 500;
    }
  }
</style>
