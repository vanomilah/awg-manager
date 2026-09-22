<script lang="ts">
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';
  import { Button } from '$lib/components/ui';
  import { AlertTriangle, RotateCcw, RefreshCw, Download } from 'lucide-svelte';

  interface Props {
    onRestored?: () => void;
  }

  let { onRestored }: Props = $props();

  let reconciling = $state(false);
  let downloading = $state(false);

  async function handleReconcile(action: 'rollback_to_lkg' | 'regenerate_from_desired') {
    if (reconciling) return;
    reconciling = true;
    try {
      await api.mihomoReconcile(action, true);
      const actionLabel = action === 'rollback_to_lkg' ? 'Откат к LKG' : 'Пересборка конфигурации';
      notifications.success(`${actionLabel} выполнен успешно`);
      onRestored?.();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      reconciling = false;
    }
  }

  async function handleDownloadEvidence() {
    if (downloading) return;
    downloading = true;
    try {
      const blob = await api.mihomoRecoveryEvidence();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `mihomo-recovery-evidence-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(url);
      notifications.success('Отчёт об аварии сохранён');
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : String(e));
    } finally {
      downloading = false;
    }
  }
</script>

<div class="mihomo-degraded-banner" role="alert">
  <div class="banner-header">
    <div class="icon-wrap">
      <AlertTriangle size={20} class="text-danger" />
    </div>
    <div class="banner-text">
      <h4>Mihomo находится в аварийном режиме (Recovery Required)</h4>
      <p>
        Обнаружен сбой транзакции конфигурации или активен маркер восстановления. Все изменения конфигурации заблокированы (HTTP 503).
        Выберите действие для восстановления штатной работы:
      </p>
    </div>
  </div>

  <div class="banner-actions">
    <Button
      variant="danger"
      size="sm"
      onclick={() => handleReconcile('rollback_to_lkg')}
      disabled={reconciling}
    >
      <RotateCcw size={14} />
      Откатить к LKG
    </Button>

    <Button
      variant="secondary"
      size="sm"
      onclick={() => handleReconcile('regenerate_from_desired')}
      disabled={reconciling}
    >
      <RefreshCw size={14} class={reconciling ? 'spin' : ''} />
      Пересобрать из настроек
    </Button>

    <Button
      variant="ghost"
      size="sm"
      onclick={handleDownloadEvidence}
      disabled={downloading}
    >
      <Download size={14} class={downloading ? 'spin' : ''} />
      Скачать отчёт (Evidence)
    </Button>
  </div>
</div>

<style>
  .mihomo-degraded-banner {
    background: rgba(239, 68, 68, 0.08);
    border: 1px solid var(--danger, #ef4444);
    border-radius: var(--radius-lg, 10px);
    padding: 16px 20px;
    margin-bottom: 20px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    box-shadow: 0 4px 16px rgba(239, 68, 68, 0.12);
  }

  .banner-header {
    display: flex;
    align-items: flex-start;
    gap: 14px;
  }

  .icon-wrap {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .banner-text h4 {
    margin: 0 0 4px 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--danger, #ef4444);
  }

  .banner-text p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-secondary, #94a3b8);
  }

  .banner-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding-left: 34px;
  }

  :global(.spin) {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
