<!--
  Источник дизайна: singbox-router/project/screens/StatusDrawerView.jsx (StatusDrawer)
  Использует SideDrawer primitive из ui/ для shell (header/body/footer/Esc/backdrop).
-->

<script lang="ts">
  import { SideDrawer, Toggle, Button, Badge } from '$lib/components/ui';
  import { api } from '$lib/api/client';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import { singboxStatus } from '$lib/stores/singbox';
  import { systemInfo } from '$lib/stores/system';
  import { drawerOpen, closeDrawer } from './drawerStore';
  import { mode } from './modeStore';
  import DrawerSection from './DrawerSection.svelte';
  import DrawerRow from './DrawerRow.svelte';
  import DepRow from './DepRow.svelte';
  import IssueRow from './IssueRow.svelte';
  import { deriveDeps, deriveIssues } from './drawerData';

  const status = singboxRouterStore.status;
  const settings = singboxRouterStore.settings;

  let open = $derived($drawerOpen);
  let s = $derived($status);
  let cfg = $derived($settings);
  let singboxInstallState = $derived($singboxStatus);
  let singboxInstallStatus = $derived(singboxInstallState.data);
  let sysInfo = $derived($systemInfo.data);

  let deps = $derived(deriveDeps(s));
  let issues = $derived(deriveIssues(s));
  let issueCount = $derived(issues.length);

  let engineEnabled = $derived(s?.enabled ?? false);
  let isExpert = $derived($mode === 'expert');

  function versionLabel(value?: string | null): string {
    const v = (value ?? '').trim();
    return v ? `v${v}` : '—';
  }

  let sbVersionLabel = $derived(versionLabel(
    singboxInstallStatus?.version ?? singboxInstallStatus?.currentVersion ?? sysInfo?.singbox?.version,
  ));

  let bigTitle = $derived(engineEnabled ? 'Движок работает' : 'Движок выключен');
  let bigSubtitle = $derived.by(() => {
    if (!engineEnabled) return 'Не активен';
    const n = s?.ruleCount ?? 0;
    return `Трафик идёт через ${n} ${pluralRules(n)}`;
  });

  function pluralRules(n: number): string {
    if (n === 1) return 'правило';
    if (n >= 2 && n <= 4) return 'правила';
    return 'правил';
  }

  // WAN summary
  let wanMode = $derived(cfg?.wanAutoDetect ? 'Авто-определение' : 'Закреплён');
  let wanCurrent = $derived(cfg?.wanInterface || 'auto');

  async function toggleEngine(_checked: boolean) {
    try {
      if (engineEnabled) {
        await api.singboxRouterDisable();
      } else {
        await api.singboxRouterEnable();
      }
      await singboxRouterStore.reloadStatus();
    } catch (e) {
      console.error('toggleEngine failed', e);
    }
  }

  async function handleToggleClick(_e: MouseEvent) {
    await toggleEngine(!engineEnabled);
  }

  async function restartEngine(_e: MouseEvent) {
    try {
      await api.singboxControl('restart');
      await singboxRouterStore.reloadStatus();
    } catch (e) {
      console.error('restart failed', e);
    }
  }
</script>

<SideDrawer {open} onClose={closeDrawer} title="Состояние sing-box" width={420}>
  <div class="drawer-content">
    <!-- Big engine toggle -->
    <div class="big-toggle" class:is-on={engineEnabled}>
      <Toggle checked={engineEnabled} onchange={toggleEngine} />
      <div class="big-text">
        <div class="big-title">{bigTitle}</div>
        <div class="big-sub">{bigSubtitle}</div>
      </div>
      <span class="big-version">{sbVersionLabel}</span>
    </div>

    <!-- Зависимости -->
    <DrawerSection title="Зависимости">
      {#each deps as dep}
        <DepRow tone={dep.tone} label={dep.label} hint={dep.hint} />
      {/each}
    </DrawerSection>

    {#if isExpert}
      <!-- Устройства -->
      <DrawerSection title="Устройства" actionHint="Изменить (в Эксперт)">
        <DrawerRow label="NDMS policy" value={s?.policyName || '—'} mono />
        {#if s?.policyMark}
          <DrawerRow label="Mark" value={s.policyMark} mono />
        {/if}
        <DrawerRow label="Привязано" value={`${s?.deviceCount ?? 0} устройств`} />
        <DrawerRow label="Режим" value={s?.deviceMode === 'all' ? 'Все устройства' : 'Только policy'} />
      </DrawerSection>
    {/if}

    {#if isExpert}
      <!-- WAN-интерфейс -->
      <DrawerSection title="WAN-интерфейс" actionHint="Изменить (в Эксперт)">
        <DrawerRow label="Режим" value={wanMode} />
        <DrawerRow label="Текущий" value={wanCurrent} mono />
      </DrawerSection>
    {/if}

    <!-- Замечания -->
    {#if isExpert && issueCount > 0}
      <DrawerSection title="Замечания">
        {#snippet badge()}
          <Badge variant="warning" size="sm">{issueCount}</Badge>
        {/snippet}
        {#each issues as issue}
          <IssueRow tone={issue.tone} text={issue.text} ctaHint={issue.ctaHint} />
        {/each}
      </DrawerSection>
    {/if}

    <!-- Дополнительно (collapsed) -->
    <DrawerSection title="Дополнительно" collapsed>
      <DrawerRow label="Sniffer" value={s?.snifferEnabled ? 'Включён' : 'Выключен'} small />
      <DrawerRow label="Final outbound" value={s?.final || 'direct'} mono small />
      {#if isExpert}
        <DrawerRow label="AWG outbounds" value={String(s?.outboundAwgCount ?? 0)} small />
        <DrawerRow label="Composite outbounds" value={String(s?.outboundCompositeCount ?? 0)} small />
      {/if}
    </DrawerSection>
  </div>

  {#snippet footer()}
    <div class="footer-actions">
      <Button variant={engineEnabled ? 'danger' : 'primary'} size="sm" fullWidth onclick={handleToggleClick}>
        {engineEnabled ? 'Выключить' : 'Включить'}
      </Button>
      <Button variant="ghost" size="sm" onclick={restartEngine}>Перезапустить</Button>
    </div>
  {/snippet}
</SideDrawer>

<style>
  .drawer-content {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .big-toggle {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--text-muted) 5%, var(--bg-tertiary));
    border: 1px solid var(--border);
  }
  .big-toggle.is-on {
    background: color-mix(in srgb, var(--success) 8%, var(--bg-tertiary));
    border-color: color-mix(in srgb, var(--success) 25%, var(--border));
  }
  .big-text { flex: 1; min-width: 0; }
  .big-title {
    font-weight: 600;
    font-size: 14px;
    color: var(--text-primary);
  }
  .big-sub {
    font-size: 11.5px;
    color: var(--text-muted);
    margin-top: 2px;
  }
  .big-version {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .footer-actions {
    display: flex;
    gap: 6px;
    width: 100%;
  }
</style>
