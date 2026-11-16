<script lang="ts">
  import { api } from '$lib/api/client';
  import type {
    FircMigrationOptions,
    FircMigrationSummary,
    FircReverseMigrationOptions,
    FircReverseMigrationSummary,
    FircSavedSingboxStatus,
    FircSnapshotMeta,
    FircTunnel,
    FircGroup,
  } from '$lib/types/firc';
  import { reloadFircAll } from '$lib/stores/firc';
  import { reloadSettings } from '$lib/stores/settings';
  import {
    fircMigrationModalOpen,
    fircMigrationModalTab,
    closeFircMigrationModal,
    setFircMigrationPopupLocallyDismissed,
    type FircMigrationWizardTab,
  } from '$lib/stores/fircMigrationModal';
  import { Modal, Button, Badge } from '$lib/components/ui';
  import { notifications } from '$lib/stores/notifications';

  let { onApplied }: { onApplied?: () => void } = $props();

  let activeTab = $derived($fircMigrationModalTab);
  let isOpen = $derived($fircMigrationModalOpen);

  let loadingOverview = $state(false);
  let activeEngine = $state<string>('sing-box');
  let dontShowAgain = $state(false);
  let lastExecutionNotice = $state<{
    kind: 'success' | 'warning' | 'error';
    configStatus: string;
    snapshotStatus: string;
    engineStatus: string;
    recoveryRequired?: boolean;
  } | null>(null);

  // Forward migration (Sing-box -> FIRC) state
  let fwdPreview = $state<FircMigrationSummary | null>(null);
  let fwdLoading = $state(false);
  let fwdApplying = $state(false);
  let fwdMode = $state<'merge' | 'replace'>('merge');
  let fwdRouterSource = $state<'auto' | 'tproxy' | 'fakeip' | 'both'>('auto');
  let fwdPostAction = $state<'switch_standby' | 'switch_and_stop' | 'keep_current'>('switch_standby');
  let fwdCreateSnapshot = $state(true);
  let fwdIncludeFakeIP = $state(false);
  let fwdSelectedTunnelIds = $state<string[]>([]);
  let fwdSelectedGroupIds = $state<string[]>([]);
  let fwdInitializedSelection = $state(false);
  let fwdConfirmReplace = $state(false);

  // Return to Sing-box: two distinct scenarios ('saved' vs 'convert')
  let returnScenario = $state<'saved' | 'convert'>('saved');
  let savedSingbox = $state<FircSavedSingboxStatus | null>(null);
  let savedPostAction = $state<'switch_to_singbox' | 'switch_and_stop_firc' | 'keep_current'>('switch_to_singbox');
  let savedCreateSnapshot = $state(true);
  let savedReturning = $state(false);

  // Reverse migration (Scenario 2: FIRC -> Sing-box) state
  let revPreview = $state<FircReverseMigrationSummary | null>(null);
  let revLoading = $state(false);
  let revApplying = $state(false);
  let revMode = $state<'merge' | 'replace'>('merge');
  let revPostAction = $state<'switch_to_singbox' | 'switch_and_stop_firc' | 'keep_current'>('switch_to_singbox');
  let revCreateSnapshot = $state(true);
  let revSelectedTunnelIds = $state<string[]>([]);
  let revSelectedGroupIds = $state<string[]>([]);
  let revInitializedSelection = $state(false);
  let revConfirmReplace = $state(false);

  // Snapshots state
  let snapshots = $state<FircSnapshotMeta[]>([]);
  let snapLabelInput = $state('');
  let snapCreating = $state(false);
  let snapRestoringId = $state<string | null>(null);
  let snapDeletingId = $state<string | null>(null);
  let snapConfirmRestoreId = $state<string | null>(null);

  // Sequence counters to discard stale async responses
  let overviewReqSeq = 0;
  let fwdPreviewReqSeq = 0;
  let revPreviewReqSeq = 0;

  let isBusy = $derived(
    loadingOverview ||
      fwdLoading ||
      fwdApplying ||
      revLoading ||
      revApplying ||
      savedReturning ||
      snapCreating ||
      snapRestoringId !== null ||
      snapDeletingId !== null,
  );

  function setTab(tab: FircMigrationWizardTab) {
    if (isBusy) return;
    fircMigrationModalTab.set(tab);
  }

  function isSubscriptionTunnel(tun: FircTunnel): boolean {
    return (tun.sources ?? []).some((s) => s.kind === 'subscription');
  }

  function isClientRouteGroup(grp: FircGroup): boolean {
    return (grp.devices?.allow?.length ?? 0) > 0 && grp.id.startsWith('cr');
  }

  function formatTs(unixSec?: number): string {
    if (!unixSec) return '—';
    try {
      return new Date(unixSec * 1000).toLocaleString('ru-RU', {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      });
    } catch {
      return String(unixSec);
    }
  }

  async function loadFullWizardData(preserveUserSelections = false) {
    const seq = ++overviewReqSeq;
    loadingOverview = true;
    try {
      const ov = await api.fircGetMigrationState();
      if (seq !== overviewReqSeq) return;
      activeEngine = ov.activeEngine || 'sing-box';
      dontShowAgain = Boolean(ov.wizardState?.dismissedAutoPopup);
      snapshots = ov.snapshots ?? [];
      savedSingbox = ov.savedSingbox ?? null;
      if (!savedSingbox?.available && returnScenario === 'saved') {
        returnScenario = 'convert';
      }

      if (ov.forwardPreview && fwdRouterSource === 'auto' && fwdMode === 'merge') {
        fwdPreview = ov.forwardPreview;
        if (!preserveUserSelections || !fwdInitializedSelection) {
          fwdSelectedTunnelIds = (ov.forwardPreview.tunnels ?? []).map((t) => t.id);
          fwdSelectedGroupIds = (ov.forwardPreview.groups ?? []).map((g) => g.id);
          fwdInitializedSelection = true;
        } else {
          const validTun = new Set((ov.forwardPreview.tunnels ?? []).map((t) => t.id));
          const validGrp = new Set((ov.forwardPreview.groups ?? []).map((g) => g.id));
          fwdSelectedTunnelIds = fwdSelectedTunnelIds.filter((id) => validTun.has(id));
          fwdSelectedGroupIds = fwdSelectedGroupIds.filter((id) => validGrp.has(id));
        }
      } else {
        await refreshForwardPreview(fwdRouterSource, !preserveUserSelections || !fwdInitializedSelection);
      }

      if (ov.reversePreview && revMode === 'merge') {
        revPreview = ov.reversePreview;
        if (!preserveUserSelections || !revInitializedSelection) {
          revSelectedTunnelIds = (ov.reversePreview.tunnels ?? []).map((t) => t.id);
          revSelectedGroupIds = (ov.reversePreview.groups ?? []).map((g) => g.id);
          revInitializedSelection = true;
        } else {
          const validTun = new Set((ov.reversePreview.tunnels ?? []).map((t) => t.id));
          const validGrp = new Set((ov.reversePreview.groups ?? []).map((g) => g.id));
          revSelectedTunnelIds = revSelectedTunnelIds.filter((id) => validTun.has(id));
          revSelectedGroupIds = revSelectedGroupIds.filter((id) => validGrp.has(id));
        }
      } else {
        await refreshReversePreview(!preserveUserSelections || !revInitializedSelection);
      }
    } catch (e: unknown) {
      if (seq === overviewReqSeq) {
        notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка загрузки данных мастера миграции');
      }
    } finally {
      if (seq === overviewReqSeq) {
        loadingOverview = false;
      }
    }
  }

  $effect(() => {
    if (isOpen) {
      lastExecutionNotice = null;
      snapConfirmRestoreId = null;
      void loadFullWizardData(false);
    }
  });

  async function handleToggleDontShow(checked: boolean) {
    dontShowAgain = checked;
    setFircMigrationPopupLocallyDismissed(checked);
    try {
      await api.fircUpdateMigrationState({ dismissedAutoPopup: checked });
    } catch {
      // ignore network error; local storage already saved
    }
  }

  async function handleCloseModal() {
    if (isBusy) return;
    if (dontShowAgain) {
      setFircMigrationPopupLocallyDismissed(true);
      try {
        await api.fircUpdateMigrationState({ dismissedAutoPopup: true });
      } catch {
        // ignore
      }
    }
    closeFircMigrationModal();
  }

  // ── Forward Migration (Sing-box -> FIRC) ──

  async function refreshForwardPreview(
    routerSource: 'auto' | 'tproxy' | 'fakeip' | 'both' = fwdRouterSource,
    resetSelection = false,
  ) {
    const seq = ++fwdPreviewReqSeq;
    fwdLoading = true;
    try {
      const res = await api.fircMigrateFromSingbox(true, {
        mode: fwdMode,
        routerSource,
        includeTunnels: true,
        includeSubscriptions: true,
        includeGroups: true,
        includeClientRoutes: true,
      });
      if (seq !== fwdPreviewReqSeq) return;
      fwdPreview = res;
      if (resetSelection || !fwdInitializedSelection) {
        fwdSelectedTunnelIds = (res.tunnels ?? []).map((t) => t.id);
        fwdSelectedGroupIds = (res.groups ?? []).map((g) => g.id);
        fwdInitializedSelection = true;
      } else {
        const validTun = new Set((res.tunnels ?? []).map((t) => t.id));
        const validGrp = new Set((res.groups ?? []).map((g) => g.id));
        fwdSelectedTunnelIds = fwdSelectedTunnelIds.filter((id) => validTun.has(id));
        fwdSelectedGroupIds = fwdSelectedGroupIds.filter((id) => validGrp.has(id));
      }
    } catch (e: unknown) {
      if (seq === fwdPreviewReqSeq) {
        notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка обновления предпросмотра Sing-box');
      }
    } finally {
      if (seq === fwdPreviewReqSeq) {
        fwdLoading = false;
      }
    }
  }

  function toggleFwdTunnel(id: string) {
    if (isBusy) return;
    if (fwdSelectedTunnelIds.includes(id)) {
      fwdSelectedTunnelIds = fwdSelectedTunnelIds.filter((x) => x !== id);
    } else {
      fwdSelectedTunnelIds = [...fwdSelectedTunnelIds, id];
    }
  }

  function selectFwdTunnelsFilter(filter: 'all' | 'subs' | 'nodes' | 'none') {
    if (isBusy) return;
    const all = fwdPreview?.tunnels ?? [];
    if (filter === 'all') fwdSelectedTunnelIds = all.map((t) => t.id);
    else if (filter === 'subs') fwdSelectedTunnelIds = all.filter(isSubscriptionTunnel).map((t) => t.id);
    else if (filter === 'nodes') fwdSelectedTunnelIds = all.filter((t) => !isSubscriptionTunnel(t)).map((t) => t.id);
    else fwdSelectedTunnelIds = [];
  }

  function toggleFwdGroup(id: string) {
    if (isBusy) return;
    if (fwdSelectedGroupIds.includes(id)) {
      fwdSelectedGroupIds = fwdSelectedGroupIds.filter((x) => x !== id);
    } else {
      fwdSelectedGroupIds = [...fwdSelectedGroupIds, id];
    }
  }

  function selectFwdGroupsFilter(filter: 'all' | 'rules_only' | 'devices_only' | 'none') {
    if (isBusy) return;
    const all = fwdPreview?.groups ?? [];
    if (filter === 'all') fwdSelectedGroupIds = all.map((g) => g.id);
    else if (filter === 'rules_only') fwdSelectedGroupIds = all.filter((g) => !isClientRouteGroup(g)).map((g) => g.id);
    else if (filter === 'devices_only') fwdSelectedGroupIds = all.filter(isClientRouteGroup).map((g) => g.id);
    else fwdSelectedGroupIds = [];
  }

  async function applyForwardMigration() {
    if (isBusy) return;
    if (
      fwdSelectedTunnelIds.length === 0 &&
      fwdSelectedGroupIds.length === 0 &&
      !fwdIncludeFakeIP &&
      fwdMode !== 'replace'
    ) {
      notifications.error('Выберите хотя бы один туннель, группу или настройку FakeIP');
      return;
    }
    if (fwdMode === 'replace' && !fwdConfirmReplace) {
      notifications.error('Подтвердите замену текущей конфигурации FIRC (режим Replace)');
      return;
    }
    fwdApplying = true;
    lastExecutionNotice = null;
    try {
      const options: FircMigrationOptions = {
        mode: fwdMode,
        routerSource: fwdRouterSource,
        includeTunnels: fwdSelectedTunnelIds.length > 0 || fwdMode === 'replace',
        includeSubscriptions: fwdSelectedTunnelIds.length > 0 || fwdMode === 'replace',
        includeGroups: fwdSelectedGroupIds.length > 0 || fwdMode === 'replace',
        includeClientRoutes: fwdSelectedGroupIds.length > 0 || fwdMode === 'replace',
        includeFakeIPConfig: fwdIncludeFakeIP,
        selectedTunnelIds: fwdSelectedTunnelIds.length > 0 ? fwdSelectedTunnelIds : ['__none__'],
        selectedGroupIds: fwdSelectedGroupIds.length > 0 ? fwdSelectedGroupIds : ['__none__'],
        postSingboxAction: fwdPostAction,
        createSnapshot: fwdCreateSnapshot,
      };
      const res = await api.fircMigrateFromSingbox(false, options);
      if (dontShowAgain) {
        setFircMigrationPopupLocallyDismissed(true);
        await api.fircUpdateMigrationState({ dismissedAutoPopup: true }).catch(() => {});
      }
      const tCount = res.tunnels?.length ?? res.tunnelsCreated ?? 0;
      const gCount = res.groups?.length ?? res.groupsCreated ?? 0;
      await Promise.all([reloadFircAll(), reloadSettings(), loadFullWizardData(true)]);
      onApplied?.();

      const snapStatus = fwdCreateSnapshot
        ? res.snapshotCreated
          ? `Создан снимок ${res.snapshotCreated.id}`
          : 'Ошибка: автоснимок не подтверждён'
        : 'Автоснимок отключён пользователем';
      const hasPostErr = Boolean(res.postActionError) || res.postActionApplied !== fwdPostAction;
      const engStatus = res.postActionError
        ? `Сбой переключения движка: ${res.postActionError}`
        : res.postActionApplied === 'keep_current'
          ? `Движок не переключался (активен ${activeEngine})`
          : `Движок переключён на FIRC (${res.postActionApplied})`;

      lastExecutionNotice = {
        kind: hasPostErr || res.recoveryRequired ? 'error' : 'success',
        configStatus: `Конфигурация FIRC сохранена (туннелей: ${tCount}, групп: ${gCount})`,
        snapshotStatus: snapStatus,
        engineStatus: engStatus,
        recoveryRequired: res.recoveryRequired,
      };

      if (hasPostErr || res.recoveryRequired) {
        notifications.error(
          `Конфигурация перенесена, но переключение движка завершилось ошибкой: ${res.postActionError || 'действие не подтверждено'}`,
        );
      } else {
        notifications.success(
          `Миграция Sing-box → FIRC выполнена: туннелей ${tCount}, групп ${gCount}${res.snapshotCreated ? ' (снимок ' + res.snapshotCreated.id + ')' : ''}`,
        );
        closeFircMigrationModal();
      }
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка миграции в FIRC');
    } finally {
      fwdApplying = false;
    }
  }

  // ── Return to Sing-box: Scenario 1 (Saved Config) & Scenario 2 (Reverse Migration) ──

  async function applyReturnToSavedSingbox() {
    if (isBusy) return;
    if (!savedSingbox?.available) {
      notifications.error('Сохранённая проверенная конфигурация Sing-box недоступна');
      return;
    }
    savedReturning = true;
    lastExecutionNotice = null;
    try {
      const res = await api.fircReturnToSavedSingbox({
        createSnapshot: savedCreateSnapshot,
        postFircAction: savedPostAction,
      });
      await Promise.all([reloadFircAll(), reloadSettings(), loadFullWizardData(true)]);
      onApplied?.();

      const snapStatus = savedCreateSnapshot
        ? res.snapshotCreated
          ? `Создан снимок ${res.snapshotCreated.id}`
          : 'Автоснимок не подтверждён'
        : 'Автоснимок отключён';
      const hasPostErr = Boolean(res.postActionError) || res.postActionApplied !== savedPostAction;
      const engStatus = res.postActionError
        ? `Сбой переключения на Sing-box: ${res.postActionError}`
        : res.postActionApplied === 'keep_current'
          ? `Файлы Sing-box восстановлены без переключения движка`
          : `Движок переключён на Sing-box (${res.postActionApplied})`;

      lastExecutionNotice = {
        kind: hasPostErr || res.recoveryRequired ? 'error' : 'success',
        configStatus: `Сохранённая конфигурация Sing-box восстановлена (файлов: ${res.restoredFiles}, настройки FIRC сохранены)`,
        snapshotStatus: snapStatus,
        engineStatus: engStatus,
        recoveryRequired: res.recoveryRequired,
      };

      if (hasPostErr || res.recoveryRequired) {
        notifications.error(
          `Файлы Sing-box восстановлены, но переключение движка не выполнено: ${res.postActionError || 'ошибка перехода'}`,
        );
      } else {
        notifications.success(
          `Выполнен возврат к сохранённой конфигурации Sing-box (файлов: ${res.restoredFiles})`,
        );
        closeFircMigrationModal();
      }
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка возврата к сохранённой конфигурации Sing-box');
    } finally {
      savedReturning = false;
    }
  }

  async function refreshReversePreview(resetSelection = false) {
    const seq = ++revPreviewReqSeq;
    revLoading = true;
    try {
      const res = await api.fircReverseMigrateToSingbox(true, {
        mode: revMode,
        includeTunnels: true,
        includeGroups: true,
      });
      if (seq !== revPreviewReqSeq) return;
      revPreview = res;
      if (resetSelection || !revInitializedSelection) {
        revSelectedTunnelIds = (res.tunnels ?? []).map((t) => t.id);
        revSelectedGroupIds = (res.groups ?? []).map((g) => g.id);
        revInitializedSelection = true;
      } else {
        const validTun = new Set((res.tunnels ?? []).map((t) => t.id));
        const validGrp = new Set((res.groups ?? []).map((g) => g.id));
        revSelectedTunnelIds = revSelectedTunnelIds.filter((id) => validTun.has(id));
        revSelectedGroupIds = revSelectedGroupIds.filter((id) => validGrp.has(id));
      }
    } catch (e: unknown) {
      if (seq === revPreviewReqSeq) {
        notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка предпросмотра обратной миграции');
      }
    } finally {
      if (seq === revPreviewReqSeq) {
        revLoading = false;
      }
    }
  }

  function toggleRevTunnel(id: string) {
    if (isBusy) return;
    if (revSelectedTunnelIds.includes(id)) {
      revSelectedTunnelIds = revSelectedTunnelIds.filter((x) => x !== id);
    } else {
      revSelectedTunnelIds = [...revSelectedTunnelIds, id];
    }
  }

  function toggleRevGroup(id: string) {
    if (isBusy) return;
    if (revSelectedGroupIds.includes(id)) {
      revSelectedGroupIds = revSelectedGroupIds.filter((x) => x !== id);
    } else {
      revSelectedGroupIds = [...revSelectedGroupIds, id];
    }
  }

  async function applyReverseMigration() {
    if (isBusy) return;
    if (revSelectedTunnelIds.length === 0 && revSelectedGroupIds.length === 0 && revMode !== 'replace') {
      notifications.error('Выберите хотя бы один туннель или группу FIRC для обратной миграции');
      return;
    }
    if (revMode === 'replace' && !revConfirmReplace) {
      notifications.error('Подтвердите замену файлов конфигурации Sing-box (режим Replace)');
      return;
    }
    revApplying = true;
    lastExecutionNotice = null;
    try {
      const options: FircReverseMigrationOptions = {
        mode: revMode,
        includeTunnels: revSelectedTunnelIds.length > 0 || revMode === 'replace',
        includeGroups: revSelectedGroupIds.length > 0 || revMode === 'replace',
        selectedTunnelIds: revSelectedTunnelIds.length > 0 ? revSelectedTunnelIds : ['__none__'],
        selectedGroupIds: revSelectedGroupIds.length > 0 ? revSelectedGroupIds : ['__none__'],
        postFircAction: revPostAction,
        createSnapshot: revCreateSnapshot,
      };
      const res = await api.fircReverseMigrateToSingbox(false, options);
      await Promise.all([reloadFircAll(), reloadSettings(), loadFullWizardData(true)]);
      onApplied?.();

      const snapStatus = revCreateSnapshot
        ? res.snapshotCreated
          ? `Создан снимок ${res.snapshotCreated.id}`
          : 'Автоснимок не подтверждён'
        : 'Автоснимок отключён';
      const hasPostErr = Boolean(res.postActionError) || res.postActionApplied !== revPostAction;
      const engStatus = res.postActionError
        ? `Сбой переключения на Sing-box: ${res.postActionError}`
        : res.postActionApplied === 'keep_current'
          ? `Файлы Sing-box подготовлены и сохранены без переключения движка`
          : `Движок переключён на Sing-box (${res.postActionApplied})`;

      lastExecutionNotice = {
        kind: hasPostErr || res.recoveryRequired ? 'error' : 'success',
        configStatus: `Конфигурация Sing-box сохранена (узлов: ${res.outboundsCreated ?? 0}, подписок: ${res.subscriptionsMigrated ?? 0}, правил: ${res.rulesMigrated ?? 0})`,
        snapshotStatus: snapStatus,
        engineStatus: engStatus,
        recoveryRequired: res.recoveryRequired,
      };

      if (hasPostErr || res.recoveryRequired) {
        notifications.error(
          `Конфигурация Sing-box сохранена, но переключение движка завершилось ошибкой: ${res.postActionError || 'действие не подтверждено'}`,
        );
      } else {
        notifications.success(
          `Обратная миграция FIRC → Sing-box выполнена: узлов ${res.outboundsCreated ?? 0}, подписок ${res.subscriptionsMigrated ?? 0}, правил ${res.rulesMigrated ?? 0}`,
        );
        closeFircMigrationModal();
      }
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка обратной миграции в Sing-box');
    } finally {
      revApplying = false;
    }
  }

  // ── System Snapshots ──

  async function handleCreateSnapshot() {
    if (isBusy) return;
    snapCreating = true;
    try {
      const created = await api.fircCreateSnapshot(snapLabelInput.trim());
      snapLabelInput = '';
      snapshots = [created, ...snapshots.filter((s) => s.id !== created.id)];
      notifications.success(`Снапшот создан: ${created.label} (${created.id})`);
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Не удалось создать снапшот');
    } finally {
      snapCreating = false;
    }
  }

  async function handleRestoreSnapshot(snap: FircSnapshotMeta) {
    if (isBusy) return;
    if (snapConfirmRestoreId !== snap.id) {
      snapConfirmRestoreId = snap.id;
      return;
    }
    snapRestoringId = snap.id;
    lastExecutionNotice = null;
    try {
      const restored = await api.fircRestoreSnapshot(snap.id, true);
      snapConfirmRestoreId = null;
      await Promise.all([reloadFircAll(), reloadSettings(), loadFullWizardData(true)]);
      onApplied?.();

      const hasPostErr = Boolean(restored.postActionError);
      lastExecutionNotice = {
        kind: hasPostErr || restored.recoveryRequired ? 'error' : 'success',
        configStatus: `Восстановлены файлы из снимка «${restored.label || restored.id}» (${restored.filesCount} файлов)`,
        snapshotStatus: `Точка восстановления: ${formatTs(restored.createdAt)}`,
        engineStatus: restored.postActionError
          ? `Сбой переключения на движок ${restored.activeEngine}: ${restored.postActionError}`
          : `Активный движок: ${restored.activeEngine} (${restored.postActionApplied || 'подтверждён'})`,
        recoveryRequired: restored.recoveryRequired,
      };

      if (hasPostErr || restored.recoveryRequired) {
        notifications.error(
          `Файлы снимка восстановлены, но переключение на движок ${restored.activeEngine} завершилось ошибкой: ${restored.postActionError}`,
        );
      } else {
        notifications.success(
          `Состояние восстановлено из резервной копии «${restored.label}» (активный движок: ${restored.activeEngine})`,
        );
      }
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка восстановления снапшота');
    } finally {
      snapRestoringId = null;
    }
  }

  async function handleDeleteSnapshot(id: string) {
    if (isBusy) return;
    snapDeletingId = id;
    try {
      await api.fircDeleteSnapshot(id);
      snapshots = snapshots.filter((s) => s.id !== id);
      if (snapConfirmRestoreId === id) {
        snapConfirmRestoreId = null;
      }
      notifications.info('Снапшот удалён');
    } catch (e: unknown) {
      notifications.error((e instanceof Error ? e.message : undefined) ?? 'Ошибка удаления снапшота');
    } finally {
      snapDeletingId = null;
    }
  }
</script>

<Modal
  open={isOpen}
  onclose={handleCloseModal}
  title="Центр миграции маршрутизации и снапшотов (Sing-box ⇄ FIRC)"
  width="740px"
>
  <div class="wiz-body">
    <!-- Top Tabs -->
    <div class="wiz-tabs">
      <button
        type="button"
        class="wiz-tab"
        class:active={activeTab === 'forward'}
        disabled={isBusy}
        onclick={() => setTab('forward')}
      >
        <span>1. Из Sing-box в FIRC</span>
        {#if (fwdPreview?.tunnels?.length ?? 0) + (fwdPreview?.groups?.length ?? 0) > 0}
          <Badge variant="info" size="sm">
            {(fwdPreview?.tunnels?.length ?? 0) + (fwdPreview?.groups?.length ?? 0)}
          </Badge>
        {/if}
      </button>

      <button
        type="button"
        class="wiz-tab"
        class:active={activeTab === 'reverse'}
        disabled={isBusy}
        onclick={() => setTab('reverse')}
      >
        <span>2. Возврат на Sing-box</span>
        {#if savedSingbox?.available}
          <Badge variant="success" size="sm">сохранён</Badge>
        {:else if (revPreview?.tunnels?.length ?? 0) + (revPreview?.groups?.length ?? 0) > 0}
          <Badge variant="muted" size="sm">
            {(revPreview?.tunnels?.length ?? 0) + (revPreview?.groups?.length ?? 0)}
          </Badge>
        {/if}
      </button>

      <button
        type="button"
        class="wiz-tab"
        class:active={activeTab === 'snapshots'}
        disabled={isBusy}
        onclick={() => setTab('snapshots')}
      >
        <span>3. Восстановить резервную копию</span>
        <Badge variant={snapshots.length > 0 ? 'success' : 'muted'} size="sm">
          {snapshots.length}
        </Badge>
      </button>
    </div>

    {#if lastExecutionNotice}
      <div class="wiz-exec-notice" class:is-error={lastExecutionNotice.kind === 'error'}>
        <div class="wiz-exec-row">
          <strong>1. Конфигурация:</strong>
          <span>{lastExecutionNotice.configStatus}</span>
        </div>
        <div class="wiz-exec-row">
          <strong>2. Снимок:</strong>
          <span>{lastExecutionNotice.snapshotStatus}</span>
        </div>
        <div class="wiz-exec-row">
          <strong>3. Переключение движка:</strong>
          <span>{lastExecutionNotice.engineStatus}</span>
        </div>
        {#if lastExecutionNotice.recoveryRequired}
          <div class="wiz-exec-recovery">
            Внимание: зафиксировано состояние <code>recovery_required</code>. Обычные изменения заблокированы до выполнения процедуры сверки/восстановления.
          </div>
        {/if}
      </div>
    {/if}

    {#if loadingOverview}
      <div class="wiz-loading">Анализ конфигурации Sing-box, FIRC и снапшотов системы…</div>
    {:else if activeTab === 'forward'}
      <!-- TAB 1: SING-BOX -> FIRC -->
      <div class="wiz-section">
        <div class="wiz-intro-banner">
          <div>
            <strong>Переход с Sing-box на аппаратно-ускоренный движок FIRC</strong>
            <div class="hint">
              Выберите, какие туннели, подписки и правила маршрутизации перенести в FIRC, а также укажите, что сделать с сервисом Sing-box после завершения миграции. Перед применением автоматически сохраняется проверенная конфигурация Sing-box и создаётся снимок для возврата.
            </div>
          </div>
        </div>

        <!-- Post-migration action for Sing-box -->
        <div class="wiz-box">
          <div class="lbl">Что сделать с Sing-box после миграции?</div>
          <div class="action-cards">
            <label class="action-card" class:selected={fwdPostAction === 'switch_standby'}>
              <input
                type="radio"
                name="fwdPostAction"
                value="switch_standby"
                disabled={isBusy}
                bind:group={fwdPostAction}
              />
              <div class="action-card-body">
                <div class="action-card-title">
                  <span>Переключить маршрутизацию на FIRC, перевести Sing-box в ожидание</span>
                  <Badge variant="success" size="sm">Рекомендуется</Badge>
                </div>
                <span class="hint">
                  Делает FIRC активным движком, аккуратно отключает роутерные слоты Sing-box (<code>20-router.json</code> / <code>21-fakeip.json</code>) без удаления настроек. Позволяет вернуться обратно в любой момент.
                </span>
              </div>
            </label>

            <label class="action-card" class:selected={fwdPostAction === 'switch_and_stop'}>
              <input
                type="radio"
                name="fwdPostAction"
                value="switch_and_stop"
                disabled={isBusy}
                bind:group={fwdPostAction}
              />
              <div class="action-card-body">
                <div class="action-card-title">
                  <span>Переключить на FIRC и полностью остановить демон Sing-box</span>
                  <Badge variant="info" size="sm">Экономия ОЗУ</Badge>
                </div>
                <span class="hint">
                  Активирует FIRC и только после успешного подтверждения перехода останавливает процесс <code>sing-box</code> (конфигурационные файлы сохраняются).
                </span>
              </div>
            </label>

            <label class="action-card" class:selected={fwdPostAction === 'keep_current'}>
              <input
                type="radio"
                name="fwdPostAction"
                value="keep_current"
                disabled={isBusy}
                bind:group={fwdPostAction}
              />
              <div class="action-card-body">
                <div class="action-card-title">
                  <span>Только импортировать конфигурацию в FIRC (не менять активный движок)</span>
                </div>
                <span class="hint">
                  Записывает туннели и группы маршрутизации в FIRC для предварительной проверки, оставляя текущий движок ({activeEngine}) активным.
                </span>
              </div>
            </label>
          </div>
        </div>

        <!-- Mode & Router Source -->
        <div class="field-grid-2">
          <div class="field">
            <label class="lbl" for="wiz-fwd-mode">Режим записи в FIRC</label>
            <select
              id="wiz-fwd-mode"
              class="inp"
              value={fwdMode}
              disabled={isBusy}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value as 'merge' | 'replace';
                fwdMode = v;
                fwdConfirmReplace = false;
                void refreshForwardPreview(fwdRouterSource, false);
              }}
            >
              <option value="merge">Объединить с текущими (Merge)</option>
              <option value="replace">Заменить текущие выбранными (Replace)</option>
            </select>
          </div>

          <div class="field">
            <label class="lbl" for="wiz-fwd-source">Источник правил роутера Sing-box</label>
            <select
              id="wiz-fwd-source"
              class="inp"
              value={fwdRouterSource}
              disabled={isBusy}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value as 'auto' | 'tproxy' | 'fakeip' | 'both';
                fwdRouterSource = v;
                void refreshForwardPreview(v, false);
              }}
            >
              <option value="auto">Авто (TProxy 20-router + FakeIP 21-fakeip)</option>
              <option value="tproxy">Только TProxy (20-router.json)</option>
              <option value="fakeip">Только FakeIP-TUN (21-fakeip.json)</option>
              <option value="both">Оба слота (20-router + 21-fakeip)</option>
            </select>
          </div>
        </div>

        {#if fwdMode === 'replace'}
          <label class="check-row warn-confirm-box">
            <input type="checkbox" disabled={isBusy} bind:checked={fwdConfirmReplace} />
            <span>
              Подтверждаю режим <strong>Replace</strong>: текущие туннели и группы FIRC (<code>tunnels.yaml</code>, <code>groups.yaml</code>) будут полностью заменены выбранным списком.
            </span>
          </label>
        {/if}

        <label class="check-row">
          <input type="checkbox" disabled={isBusy} bind:checked={fwdCreateSnapshot} />
          <span>
            Автоматически создать проверенный <strong>Snapshot конфигурации</strong> перед применением миграции (при сбое снимка миграция прерывается до изменения файлов)
          </span>
        </label>

        {#if fwdPreview?.fakeip?.detected}
          <div class="wiz-box fakeip-box">
            <div class="sec-head">
              <span class="lbl">Обнаружен слот Sing-box FakeIP-TUN</span>
              {#if fwdPreview.fakeip.sourceFile || fwdPreview.fakeip.slotFile}
                <Badge variant="info" size="sm" mono>
                  {fwdPreview.fakeip.sourceFile || fwdPreview.fakeip.slotFile}
                </Badge>
              {/if}
            </div>
            <div class="hint">
              Пул IPv4: <code>{fwdPreview.fakeip.inet4Range || '198.18.0.0/15'}</code>
              {#if fwdPreview.fakeip.realDns}
                · Реальный DNS: <code>{fwdPreview.fakeip.realDns}</code>
              {/if}
              {#if fwdPreview.fakeip.routeFinal || fwdPreview.fakeip.finalOutbound}
                · Outbound по умолчанию: <code>{fwdPreview.fakeip.routeFinal || fwdPreview.fakeip.finalOutbound}</code>
              {/if}
            </div>
            <label class="check-row">
              <input type="checkbox" disabled={isBusy} bind:checked={fwdIncludeFakeIP} />
              <span>
                Перенести настройки пула FakeIP (<code>{fwdPreview.fakeip.inet4Range || '198.18.0.0/15'}</code>)
                {#if fwdPreview.fakeip.realDns}
                  и апстрим DNS (<code>{fwdPreview.fakeip.realDns}</code>)
                {/if}
                в конфигурацию <code>firc.conf</code> и демон FIRC
              </span>
            </label>
          </div>
        {/if}

        <!-- Tunnels & Subscriptions -->
        <div class="wiz-box">
          <div class="sec-head">
            <span class="lbl">
              Туннели и подписки ({fwdSelectedTunnelIds.length} из {fwdPreview?.tunnels?.length ?? 0})
            </span>
            <div class="filter-btns">
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdTunnelsFilter('all')}>Все</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdTunnelsFilter('subs')}>Подписки</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdTunnelsFilter('nodes')}>Узлы</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdTunnelsFilter('none')}>Снять</button>
            </div>
          </div>

          {#if (fwdPreview?.tunnels?.length ?? 0) === 0}
            <div class="hint">Туннели и подписки в Sing-box не обнаружены.</div>
          {:else}
            <div class="item-list">
              {#each fwdPreview?.tunnels ?? [] as tun (tun.id)}
                {@const checked = fwdSelectedTunnelIds.includes(tun.id)}
                {@const isSub = isSubscriptionTunnel(tun)}
                <button
                  type="button"
                  class="list-item"
                  class:selected={checked}
                  disabled={isBusy}
                  onclick={() => toggleFwdTunnel(tun.id)}
                >
                  <span class="chk" class:checked>{checked ? '✓' : ''}</span>
                  <span class="item-main">
                    <strong>{tun.description || tun.id}</strong>
                    <span class="item-sub mono">{tun.device} · id:{tun.id}</span>
                  </span>
                  <span class="item-badges">
                    <Badge variant={isSub ? 'info' : 'muted'} size="sm">
                      {isSub ? 'Подписка' : 'Прокси-узел'}
                    </Badge>
                    <span class="mono-pill">ист.: {tun.sources?.length ?? 0}</span>
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Routing Groups -->
        <div class="wiz-box">
          <div class="sec-head">
            <span class="lbl">
              Группы маршрутизации и клиентские маршруты ({fwdSelectedGroupIds.length} из {fwdPreview?.groups?.length ?? 0})
            </span>
            <div class="filter-btns">
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdGroupsFilter('all')}>Все</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdGroupsFilter('rules_only')}>Группы</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdGroupsFilter('devices_only')}>Устройства</button>
              <button type="button" class="filter-btn" disabled={isBusy} onclick={() => selectFwdGroupsFilter('none')}>Снять</button>
            </div>
          </div>

          {#if (fwdPreview?.groups?.length ?? 0) === 0}
            <div class="hint">Группы маршрутизации для выбранного слота не найдены.</div>
          {:else}
            <div class="item-list">
              {#each fwdPreview?.groups ?? [] as grp (grp.id)}
                {@const checked = fwdSelectedGroupIds.includes(grp.id)}
                {@const isDev = isClientRouteGroup(grp)}
                <button
                  type="button"
                  class="list-item"
                  class:selected={checked}
                  disabled={isBusy}
                  onclick={() => toggleFwdGroup(grp.id)}
                >
                  <span class="chk" class:checked>{checked ? '✓' : ''}</span>
                  <span class="item-main">
                    <strong>{grp.name}</strong>
                    <span class="item-sub mono">
                      → {Array.isArray(grp.interface) ? grp.interface.join(' → ') : grp.interface}
                      {#if (grp.devices?.allow?.length ?? 0) > 0}
                        · клиенты: {grp.devices?.allow.join(', ')}
                      {/if}
                    </span>
                  </span>
                  <span class="item-badges">
                    {#if isDev}
                      <Badge variant="warning" size="sm">Устройство</Badge>
                    {/if}
                    {#if grp.list?.url}
                      <Badge variant="info" size="sm">.srs список</Badge>
                    {/if}
                    <span class="mono-pill">правил: {grp.rules?.length ?? 0}</span>
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </div>

    {:else if activeTab === 'reverse'}
      <!-- TAB 2: TWO DISTINCT RETURN SCENARIOS FROM FIRC TO SING-BOX -->
      <div class="wiz-section">
        <div class="wiz-box">
          <div class="lbl">Выберите сценарий возврата с FIRC на Sing-box</div>
          <div class="action-cards">
            <label class="action-card" class:selected={returnScenario === 'saved'}>
              <input
                type="radio"
                name="returnScenario"
                value="saved"
                disabled={isBusy}
                bind:group={returnScenario}
              />
              <div class="action-card-body">
                <div class="action-card-title">
                  <span>Сценарий 1. Вернуться к сохранённой конфигурации Sing-box</span>
                  {#if savedSingbox?.available}
                    <Badge variant="success" size="sm">Доступна</Badge>
                  {:else}
                    <Badge variant="warning" size="sm">Нет сохранённой копии</Badge>
                  {/if}
                </div>
                <span class="hint">
                  Восстанавливает проверенную конфигурацию Sing-box, сохранённую до перехода на FIRC, без запуска конвертера FIRC. Настройки самого FIRC не удаляются.
                </span>
              </div>
            </label>

            <label class="action-card" class:selected={returnScenario === 'convert'}>
              <input
                type="radio"
                name="returnScenario"
                value="convert"
                disabled={isBusy}
                bind:group={returnScenario}
              />
              <div class="action-card-body">
                <div class="action-card-title">
                  <span>Сценарий 2. Перенести текущие настройки FIRC в Sing-box</span>
                  <Badge variant="info" size="sm">Конвертация правил</Badge>
                </div>
                <span class="hint">
                  Формирует новую конфигурацию Sing-box (<code>10-tunnels.json</code>, <code>subscriptions.json</code>, <code>20-router.json</code>, <code>client-routes.json</code>) из выбранных текущих туннелей и правил FIRC.
                </span>
              </div>
            </label>
          </div>
        </div>

        {#if returnScenario === 'saved'}
          <!-- SCENARIO 1: RETURN TO SAVED SING-BOX CONFIG -->
          <div class="wiz-box">
            <div class="sec-head">
              <span class="lbl">Сохранённая до перехода конфигурация Sing-box</span>
              {#if savedSingbox?.available}
                <Badge variant="success" size="sm">Проверена</Badge>
              {:else}
                <Badge variant="error" size="sm">Недоступна</Badge>
              {/if}
            </div>

            {#if savedSingbox?.available}
              <div class="hint">
                Дата сохранения: <strong>{formatTs(savedSingbox.createdAt)}</strong>
                {#if savedSingbox.label}
                  · Источник: <code>{savedSingbox.label}</code>
                {/if}
              </div>
              <div class="hint">
                Состав: туннелей <strong>{savedSingbox.tunnelsCount}</strong>, подписок <strong>{savedSingbox.subscriptionsCount}</strong>, групп/правил <strong>{savedSingbox.groupsCount}</strong>, файлов <strong>{savedSingbox.filesCount}</strong>.
              </div>
              <div class="warn-confirm-box">
                <strong>Важно:</strong> правила и туннели, добавленные или изменённые в FIRC после первоначального перехода, в этот сценарий <strong>не переносятся</strong>. Конфигурация FIRC (<code>tunnels.yaml</code>, <code>groups.yaml</code>) сохраняется на диске без удаления.
              </div>
            {:else}
              <div class="warn-confirm-box">
                {#if savedSingbox?.unavailableReason}
                  <strong>Причина недоступности:</strong> {savedSingbox.unavailableReason}<br />
                {/if}
                Проверенная сохранённая конфигурация Sing-box не найдена или непригодна. Автоматическая подмена обратной миграцией или текущими файлами отключена: если вы хотите сформировать конфигурацию Sing-box из текущих правил FIRC, явно выберите «Сценарий 2. Перенести текущие настройки FIRC в Sing-box».
              </div>
            {/if}
          </div>

          <div class="wiz-box">
            <div class="lbl">Действие с движком при возврате</div>
            <div class="action-cards">
              <label class="action-card" class:selected={savedPostAction === 'switch_to_singbox'}>
                <input
                  type="radio"
                  name="savedPostAction"
                  value="switch_to_singbox"
                  disabled={isBusy || !savedSingbox?.available}
                  bind:group={savedPostAction}
                />
                <div class="action-card-body">
                  <div class="action-card-title">
                    <span>Восстановить файлы Sing-box и переключить маршрутизацию на Sing-box</span>
                    <Badge variant="success" size="sm">Рекомендуется</Badge>
                  </div>
                  <span class="hint">
                    Проверяет конфигурацию, запускает Sing-box и переключает активный движок. При ошибке переключения рабочий FIRC сохраняется активным.
                  </span>
                </div>
              </label>

              <label class="action-card" class:selected={savedPostAction === 'switch_and_stop_firc'}>
                <input
                  type="radio"
                  name="savedPostAction"
                  value="switch_and_stop_firc"
                  disabled={isBusy || !savedSingbox?.available}
                  bind:group={savedPostAction}
                />
                <div class="action-card-body">
                  <div class="action-card-title">
                    <span>Переключить на Sing-box и остановить демон fircd после подтверждения перехода</span>
                  </div>
                  <span class="hint">
                    Остановка <code>fircd</code> выполняется только после подтверждённого успешного запуска и переключения на Sing-box.
                  </span>
                </div>
              </label>
            </div>

            <label class="check-row" style="margin-top: 6px;">
              <input
                type="checkbox"
                disabled={isBusy || !savedSingbox?.available}
                bind:checked={savedCreateSnapshot}
              />
              <span>Создать страховочный снимок перед возвратом к сохранённому Sing-box</span>
            </label>
          </div>
        {:else}
          <!-- SCENARIO 2: CONVERT CURRENT FIRC SETTINGS TO SING-BOX -->
          <div class="wiz-box">
            <div class="lbl">Что сделать после сохранения подготовленной конфигурации Sing-box?</div>
            <div class="action-cards">
              <label class="action-card" class:selected={revPostAction === 'switch_to_singbox'}>
                <input
                  type="radio"
                  name="revPostAction"
                  value="switch_to_singbox"
                  disabled={isBusy}
                  bind:group={revPostAction}
                />
                <div class="action-card-body">
                  <div class="action-card-title">
                    <span>Переключить активный движок маршрутизации на Sing-box</span>
                    <Badge variant="success" size="sm">Рекомендуется</Badge>
                  </div>
                  <span class="hint">
                    Сохраняет подготовленные файлы в Sing-box и выполняет контролируемое переключение движка.
                  </span>
                </div>
              </label>

              <label class="action-card" class:selected={revPostAction === 'switch_and_stop_firc'}>
                <input
                  type="radio"
                  name="revPostAction"
                  value="switch_and_stop_firc"
                  disabled={isBusy}
                  bind:group={revPostAction}
                />
                <div class="action-card-body">
                  <div class="action-card-title">
                    <span>Переключить на Sing-box и остановить fircd после подтверждённого перехода</span>
                  </div>
                  <span class="hint">
                    Останавливает <code>fircd</code> только в случае успешного запуска и переключения на Sing-box.
                  </span>
                </div>
              </label>

              <label class="action-card" class:selected={revPostAction === 'keep_current'}>
                <input
                  type="radio"
                  name="revPostAction"
                  value="keep_current"
                  disabled={isBusy}
                  bind:group={revPostAction}
                />
                <div class="action-card-body">
                  <div class="action-card-title">
                    <span>Только подготовить и сохранить файлы конфигурации Sing-box (оставить FIRC активным)</span>
                  </div>
                  <span class="hint">
                    Обновляет конфиги Sing-box на диске без переключения текущего активного движка.
                  </span>
                </div>
              </label>
            </div>
          </div>

          <div class="field-grid-2">
            <div class="field">
              <label class="lbl" for="wiz-rev-mode">Режим записи в Sing-box</label>
              <select
                id="wiz-rev-mode"
                class="inp"
                value={revMode}
                disabled={isBusy}
                onchange={(e) => {
                  const v = (e.currentTarget as HTMLSelectElement).value as 'merge' | 'replace';
                  revMode = v;
                  revConfirmReplace = false;
                  void refreshReversePreview(false);
                }}
              >
                <option value="merge">Объединить с существующими в Sing-box (Merge)</option>
                <option value="replace">Заменить файлы Sing-box выбранными (Replace)</option>
              </select>
            </div>
            <div class="field" style="justify-content: flex-end;">
              <label class="check-row">
                <input type="checkbox" disabled={isBusy} bind:checked={revCreateSnapshot} />
                <span>Создать проверенный авто-снапшот перед записью</span>
              </label>
            </div>
          </div>

          {#if revMode === 'replace'}
            <label class="check-row warn-confirm-box">
              <input type="checkbox" disabled={isBusy} bind:checked={revConfirmReplace} />
              <span>
                Подтверждаю режим <strong>Replace</strong>: файлы <code>10-tunnels.json</code>, <code>subscriptions.json</code>, <code>20-router.json</code> и <code>client-routes.json</code> будут перезаписаны только выбранными элементами.
              </span>
            </label>
          {/if}

          {#if (revPreview?.skipped?.length ?? 0) > 0}
            <div class="warn-confirm-box">
              <strong>Предупреждения совместимости ({revPreview?.skipped?.length}):</strong>
              <ul class="skip-list">
                {#each revPreview?.skipped ?? [] as sk}
                  <li>{sk}</li>
                {/each}
              </ul>
            </div>
          {/if}

          <!-- FIRC Tunnels to Reverse Migrate -->
          <div class="wiz-box">
            <div class="sec-head">
              <span class="lbl">
                Туннели и подписки FIRC ({revSelectedTunnelIds.length} из {revPreview?.tunnels?.length ?? 0})
              </span>
              <div class="filter-btns">
                <button
                  type="button"
                  class="filter-btn"
                  disabled={isBusy}
                  onclick={() => (revSelectedTunnelIds = (revPreview?.tunnels ?? []).map((t) => t.id))}
                >
                  Все
                </button>
                <button
                  type="button"
                  class="filter-btn"
                  disabled={isBusy}
                  onclick={() => (revSelectedTunnelIds = [])}
                >
                  Снять
                </button>
              </div>
            </div>

            {#if (revPreview?.tunnels?.length ?? 0) === 0}
              <div class="hint">В FIRC нет туннелей для обратной миграции.</div>
            {:else}
              <div class="item-list">
                {#each revPreview?.tunnels ?? [] as tun (tun.id)}
                  {@const checked = revSelectedTunnelIds.includes(tun.id)}
                  <button
                    type="button"
                    class="list-item"
                    class:selected={checked}
                    disabled={isBusy}
                    onclick={() => toggleRevTunnel(tun.id)}
                  >
                    <span class="chk" class:checked>{checked ? '✓' : ''}</span>
                    <span class="item-main">
                      <strong>{tun.name}</strong>
                      <span class="item-sub mono">{tun.device} → тег Sing-box: {tun.targetTag}</span>
                    </span>
                    <span class="item-badges">
                      <Badge variant={tun.kind === 'subscription' ? 'info' : 'muted'} size="sm">
                        {tun.kind === 'subscription' ? 'Подписка' : 'Прокси-узлы'}
                      </Badge>
                      {#if tun.linksCount > 0}
                        <span class="mono-pill">узлов: {tun.linksCount}</span>
                      {/if}
                    </span>
                  </button>
                {/each}
              </div>
            {/if}
          </div>

          <!-- FIRC Groups to Reverse Migrate -->
          <div class="wiz-box">
            <div class="sec-head">
              <span class="lbl">
                Группы маршрутизации FIRC ({revSelectedGroupIds.length} из {revPreview?.groups?.length ?? 0})
              </span>
              <div class="filter-btns">
                <button
                  type="button"
                  class="filter-btn"
                  disabled={isBusy}
                  onclick={() => (revSelectedGroupIds = (revPreview?.groups ?? []).map((g) => g.id))}
                >
                  Все
                </button>
                <button
                  type="button"
                  class="filter-btn"
                  disabled={isBusy}
                  onclick={() => (revSelectedGroupIds = [])}
                >
                  Снять
                </button>
              </div>
            </div>

            {#if (revPreview?.groups?.length ?? 0) === 0}
              <div class="hint">В FIRC нет групп маршрутизации для обратного переноса.</div>
            {:else}
              <div class="item-list">
                {#each revPreview?.groups ?? [] as grp (grp.id)}
                  {@const checked = revSelectedGroupIds.includes(grp.id)}
                  <button
                    type="button"
                    class="list-item"
                    class:selected={checked}
                    disabled={isBusy}
                    onclick={() => toggleRevGroup(grp.id)}
                  >
                    <span class="chk" class:checked>{checked ? '✓' : ''}</span>
                    <span class="item-main">
                      <strong>{grp.name}</strong>
                      <span class="item-sub mono">→ outbound: {grp.outboundTag}</span>
                    </span>
                    <span class="item-badges">
                      {#if grp.ruleSetUrl}
                        <Badge variant="info" size="sm">.srs список</Badge>
                      {/if}
                      <span class="mono-pill">правил: {grp.rulesCount}</span>
                    </span>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/if}
      </div>

    {:else}
      <!-- TAB 3: EMERGENCY CONFIGURATION SNAPSHOT RESTORE -->
      <div class="wiz-section">
        <div class="wiz-intro-banner">
          <div>
            <strong>Аварийное восстановление резервной копии конфигурационных файлов</strong>
            <div class="hint">
              Снимок охватывает только конфигурационные файлы маршрутизации: <code>settings.json</code>, слоты <code>sing-box/config.d/*.json</code> (включая <code>pending/</code> и <code>disabled/</code>), <code>subscriptions.json</code>, <code>client-routes.json</code>, а также файлы FIRC (<code>firc.conf</code>, <code>tunnels.yaml</code>, <code>groups.yaml</code>, <code>awgm-ext.json</code>). Перед заменой выполняется полная проверка целостности (preflight) с автоматическим откатом при ошибке.
            </div>
          </div>
        </div>

        <div class="wiz-box">
          <div class="lbl">Создать ручной снимок текущих конфигурационных файлов</div>
          <div class="snap-create-row">
            <input
              type="text"
              class="inp"
              disabled={isBusy}
              placeholder="Описание снимка (например: До настройки FakeIP)"
              bind:value={snapLabelInput}
            />
            <Button
              variant="secondary"
              size="sm"
              loading={snapCreating}
              disabled={isBusy}
              onclick={handleCreateSnapshot}
            >
              + Создать снимок
            </Button>
          </div>
        </div>

        <div class="wiz-box">
          <div class="sec-head">
            <span class="lbl">Сохранённые резервные копии ({snapshots.length})</span>
          </div>

          {#if snapshots.length === 0}
            <div class="hint">
              Сохранённых снимков пока нет. Они создаются автоматически перед миграцией или вручную кнопкой выше.
            </div>
          {:else}
            <div class="snap-list">
              {#each snapshots as snap (snap.id)}
                <div class="snap-card">
                  <div class="snap-card-main">
                    <div class="snap-card-title">
                      <strong>{snap.label || snap.id}</strong>
                      <Badge
                        variant={snap.trigger === 'manual' ? 'info' : 'muted'}
                        size="sm"
                      >
                        {snap.trigger === 'manual' ? 'Ручной' : 'Авто до миграции'}
                      </Badge>
                      <Badge variant="success" size="sm" mono>
                        движок: {snap.activeEngine}
                      </Badge>
                    </div>
                    <div class="hint mono">
                      ID: {snap.id} · Создан: {formatTs(snap.createdAt)} · Конфиг. файлов: {snap.filesCount}
                    </div>
                    <div class="hint">
                      Охват снимка: Sing-box туннелей <strong>{snap.singboxTunnelsCount}</strong> (подписок <strong>{snap.singboxSubscriptionsCount}</strong>), групп <strong>{snap.singboxGroupsCount}</strong>
                      · FIRC туннелей <strong>{snap.fircTunnelsCount}</strong>, групп <strong>{snap.fircGroupsCount}</strong>
                    </div>
                    {#if snapConfirmRestoreId === snap.id}
                      <div class="warn-confirm-box" style="margin-top: 6px;">
                        Подтвердите восстановление: текущие изменения в <code>sing-box/config.d</code>, <code>settings.json</code> и <code>firc/*.yaml</code> будут заменены состоянием от <strong>{formatTs(snap.createdAt)}</strong> (движок: <code>{snap.activeEngine}</code>).
                      </div>
                    {/if}
                  </div>
                  <div class="snap-card-actions">
                    {#if snapConfirmRestoreId === snap.id}
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={isBusy}
                        onclick={() => (snapConfirmRestoreId = null)}
                      >
                        Отмена
                      </Button>
                    {/if}
                    <Button
                      variant={snapConfirmRestoreId === snap.id ? 'danger' : 'primary'}
                      size="sm"
                      loading={snapRestoringId === snap.id}
                      disabled={isBusy}
                      onclick={() => handleRestoreSnapshot(snap)}
                    >
                      {snapConfirmRestoreId === snap.id ? 'Подтвердить замену' : 'Восстановить'}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      loading={snapDeletingId === snap.id}
                      disabled={isBusy}
                      onclick={() => handleDeleteSnapshot(snap.id)}
                    >
                      Удалить
                    </Button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>

  {#snippet actions()}
    <div class="wiz-footer">
      <label class="check-row">
        <input
          type="checkbox"
          checked={dontShowAgain}
          disabled={isBusy}
          onchange={(e) => handleToggleDontShow((e.currentTarget as HTMLInputElement).checked)}
        />
        <span>Не показывать это окно автоматически при входе в UI</span>
      </label>

      <div class="wiz-footer-btns">
        <Button variant="ghost" size="sm" disabled={isBusy} onclick={handleCloseModal}>Закрыть</Button>
        {#if activeTab === 'forward'}
          <Button
            variant="primary"
            size="sm"
            loading={fwdApplying || fwdLoading}
            disabled={isBusy || (fwdMode === 'replace' && !fwdConfirmReplace)}
            onclick={applyForwardMigration}
          >
            Выполнить миграцию в FIRC
          </Button>
        {:else if activeTab === 'reverse'}
          {#if returnScenario === 'saved'}
            <Button
              variant="primary"
              size="sm"
              loading={savedReturning}
              disabled={isBusy || !savedSingbox?.available}
              onclick={applyReturnToSavedSingbox}
            >
              Вернуться к сохранённому Sing-box
            </Button>
          {:else}
            <Button
              variant="primary"
              size="sm"
              loading={revApplying || revLoading}
              disabled={isBusy || (revMode === 'replace' && !revConfirmReplace)}
              onclick={applyReverseMigration}
            >
              Перенести настройки FIRC в Sing-box
            </Button>
          {/if}
        {/if}
      </div>
    </div>
  {/snippet}
</Modal>

<style>
  .wiz-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .wiz-tabs {
    display: flex;
    gap: 6px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 8px;
    flex-wrap: wrap;
  }

  .wiz-tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 12px;
    border-radius: 7px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s;
  }

  .wiz-tab.active {
    background: color-mix(in srgb, var(--color-accent) 14%, var(--bg-secondary));
    border-color: var(--color-accent);
    color: var(--text-primary);
  }

  .wiz-loading {
    padding: 28px;
    text-align: center;
    font-size: 13px;
    color: var(--text-secondary);
  }

  .wiz-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .wiz-intro-banner {
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--color-accent) 35%, var(--border));
    background: color-mix(in srgb, var(--color-accent) 7%, var(--bg-secondary));
    font-size: 12px;
    color: var(--text-primary);
  }

  .wiz-exec-notice {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--color-success, #22c55e) 45%, var(--border));
    background: color-mix(in srgb, var(--color-success, #22c55e) 10%, var(--bg-secondary));
    font-size: 12px;
  }

  .wiz-exec-notice.is-error {
    border-color: color-mix(in srgb, var(--color-danger, #ef4444) 55%, var(--border));
    background: color-mix(in srgb, var(--color-danger, #ef4444) 10%, var(--bg-secondary));
  }

  .wiz-exec-row {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .wiz-exec-recovery {
    margin-top: 4px;
    padding-top: 6px;
    border-top: 1px dashed var(--border);
    font-weight: 600;
    color: var(--color-danger, #ef4444);
  }

  .warn-confirm-box {
    padding: 8px 10px;
    border-radius: 7px;
    border: 1px solid color-mix(in srgb, var(--color-warning, #f59e0b) 45%, var(--border));
    background: color-mix(in srgb, var(--color-warning, #f59e0b) 9%, var(--bg-secondary));
    font-size: 11.5px;
    line-height: 1.45;
  }

  .skip-list {
    margin: 4px 0 0 18px;
    padding: 0;
    font-size: 11px;
  }

  .wiz-box {
    padding: 11px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .fakeip-box {
    border-color: color-mix(in srgb, var(--color-info, #3b82f6) 40%, var(--border));
    background: color-mix(in srgb, var(--color-info, #3b82f6) 6%, var(--bg-secondary));
  }

  .action-cards {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .action-card {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    padding: 8px 10px;
    border-radius: 7px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    cursor: pointer;
    transition: border-color 0.12s, background 0.12s;
  }

  .action-card.selected {
    border-color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 10%, var(--bg-tertiary));
  }

  .action-card input[type='radio'] {
    margin-top: 3px;
  }

  .action-card-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
  }

  .action-card-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    flex-wrap: wrap;
  }

  .field-grid-2 {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 10px;
  }

  @media (max-width: 580px) {
    .field-grid-2 {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .lbl {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .hint {
    font-size: 11px;
    color: var(--text-secondary);
    line-height: 1.4;
  }

  .mono {
    font-family: var(--font-mono);
  }

  .inp {
    padding: 7px 10px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 12px;
    width: 100%;
    box-sizing: border-box;
  }

  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }

  .filter-btns {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }

  .filter-btn {
    padding: 2px 8px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: 11px;
    cursor: pointer;
  }

  .filter-btn:hover {
    border-color: var(--color-accent);
    color: var(--text-primary);
  }

  .item-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 165px;
    overflow-y: auto;
  }

  .list-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    text-align: left;
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: 12px;
    cursor: pointer;
    box-sizing: border-box;
  }

  .list-item.selected {
    background: color-mix(in srgb, var(--color-accent) 10%, var(--bg-tertiary));
    border-color: color-mix(in srgb, var(--color-accent) 32%, var(--border));
  }

  .chk {
    width: 15px;
    height: 15px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 10px;
    font-weight: 700;
    color: transparent;
    flex-shrink: 0;
  }

  .chk.checked {
    background: color-mix(in srgb, var(--color-accent) 18%, var(--bg-tertiary));
    border-color: var(--color-accent);
    color: var(--color-accent);
  }

  .item-main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
  }

  .item-main strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-sub {
    font-size: 10.5px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-badges {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex-shrink: 0;
  }

  .mono-pill {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-muted);
    background: var(--bg-secondary);
    padding: 1px 5px;
    border-radius: 4px;
  }

  .check-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--text-primary);
    cursor: pointer;
  }

  .snap-create-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .snap-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 260px;
    overflow-y: auto;
  }

  .snap-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 9px 10px;
    border-radius: 7px;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    flex-wrap: wrap;
  }

  .snap-card-main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1 1 auto;
    min-width: 0;
  }

  .snap-card-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--text-primary);
    flex-wrap: wrap;
  }

  .snap-card-actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }

  .wiz-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    flex-wrap: wrap;
  }

  .wiz-footer-btns {
    display: flex;
    gap: 8px;
    align-items: center;
  }
</style>
