<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (RoutingTable)
  Reuse adapters.ts из F2 для action + matchers расшифровки.
-->

<script lang="ts">
  import type { SingboxRouterRule, SingboxRouterOutbound } from '$lib/types';
  import { Badge } from '$lib/components/ui';
  import { isSystemRule } from './adapters';

  interface Props {
    rules: SingboxRouterRule[];
    outbounds: SingboxRouterOutbound[];
    onEdit: (idx: number) => void;
    onDelete: (idx: number) => void;
    onMove: (idx: number, dir: 'up' | 'down') => void;
    bare?: boolean;
  }

  let { rules, onEdit, onDelete, onMove, bare = false }: Props = $props();

  type ActionLabel = 'SNIFF' | 'HIJACK' | 'BYPASS' | 'REJECT' | 'ROUTE';
  type ActionVariant = 'default' | 'accent' | 'success' | 'error' | 'warning' | 'info' | 'muted';

  interface RowData {
    idx: number;
    sys: boolean;
    actionLabel: ActionLabel;
    actionVariant: ActionVariant;
    matchers: string;
    outbound: string;
    outboundKind: 'route' | 'direct' | 'reject' | 'none';
  }

  function compileMatchers(r: SingboxRouterRule): string {
    const parts: string[] = [];
    if (r.protocol) parts.push(`protocol=${r.protocol}`);
    if (r.domain_suffix?.length) {
      const head = r.domain_suffix[0];
      const rest = r.domain_suffix.length > 1 ? ` +${r.domain_suffix.length - 1}` : '';
      parts.push(`domain: ${head}${rest}`);
    }
    if (r.ip_cidr?.length) {
      const head = r.ip_cidr[0];
      const rest = r.ip_cidr.length > 1 ? ` +${r.ip_cidr.length - 1}` : '';
      parts.push(`ip: ${head}${rest}`);
    }
    if (r.source_ip_cidr?.length) {
      parts.push(`src: ${r.source_ip_cidr[0]}${r.source_ip_cidr.length > 1 ? ` +${r.source_ip_cidr.length - 1}` : ''}`);
    }
    if (r.port?.length) {
      parts.push(`port: ${r.port.join(',')}`);
    }
    if (r.rule_set?.length) {
      parts.push(`set: ${r.rule_set.join(', ')}`);
    }
    if (r.ip_is_private) {
      parts.push('ip_is_private');
    }
    return parts.length > 0 ? parts.join(' · ') : '—';
  }

  function actionDisplay(r: SingboxRouterRule): { label: ActionLabel; variant: ActionVariant } {
    if (r.action === 'sniff') return { label: 'SNIFF', variant: 'default' };
    if (r.action === 'hijack-dns') return { label: 'HIJACK', variant: 'default' };
    if (r.ip_is_private && r.action === 'route' && (!r.outbound || r.outbound === 'direct')) {
      return { label: 'BYPASS', variant: 'default' };
    }
    if (r.action === 'reject') return { label: 'REJECT', variant: 'error' };
    return { label: 'ROUTE', variant: 'success' };
  }

  const rowData = $derived<RowData[]>(
    rules.map((r, idx) => {
      const sys = isSystemRule(r);
      const a = actionDisplay(r);
      const outbound = r.outbound ?? (r.action === 'reject' ? 'reject' : '—');
      const outboundKind: RowData['outboundKind'] = outbound === '—' ? 'none'
        : outbound === 'direct' ? 'direct'
        : outbound === 'reject' ? 'reject' : 'route';
      return {
        idx,
        sys,
        actionLabel: a.label,
        actionVariant: a.variant,
        matchers: compileMatchers(r),
        outbound,
        outboundKind,
      };
    }),
  );
</script>

<div class="table" class:bare>
  <div class="header">
    <div>#</div>
    <div>Порядок</div>
    <div>Действие</div>
    <div>Условия</div>
    <div>Выход</div>
    <div class="actions-col">Действия</div>
  </div>
  {#each rowData as row (row.idx)}
    <div class="row" class:sys={row.sys} class:route={!row.sys && row.outboundKind === 'route'}>
      <div class="idx">{row.idx}</div>
      <div class="reorder">
        {#if !row.sys}
          <button
            type="button"
            class="route-action-btn"
            title={`Поднять правило #${row.idx}`}
            aria-label={`Поднять правило ${row.idx}`}
            disabled={row.idx === 0}
            onclick={() => onMove(row.idx, 'up')}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <polyline points="18 15 12 9 6 15" />
            </svg>
          </button>
          <button
            type="button"
            class="route-action-btn"
            title={`Опустить правило #${row.idx}`}
            aria-label={`Опустить правило ${row.idx}`}
            disabled={row.idx === rules.length - 1}
            onclick={() => onMove(row.idx, 'down')}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>
        {/if}
      </div>
      <div class="action-badge-cell">
        <Badge variant={row.actionVariant} size="sm" mono>{row.actionLabel}</Badge>
      </div>
      <div class="matchers" title={row.matchers}>{row.matchers}</div>
      <div>
        {#if row.outboundKind === 'none'}
          <span class="dash">—</span>
        {:else if row.outboundKind === 'direct'}
          <Badge variant="muted" mono size="sm">direct</Badge>
        {:else if row.outboundKind === 'reject'}
          <Badge variant="error" mono size="sm">reject</Badge>
        {:else}
          <Badge variant="accent" mono size="sm">{row.outbound}</Badge>
        {/if}
      </div>
      <div class="actions-col actions">
        {#if !row.sys}
          <button
            type="button"
            class="route-action-btn"
            title={`Редактировать правило #${row.idx}`}
            aria-label={`Редактировать правило ${row.idx}`}
            onclick={() => onEdit(row.idx)}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
            </svg>
          </button>
          <button
            type="button"
            class="route-action-btn danger"
            title={`Удалить правило #${row.idx}`}
            aria-label={`Удалить правило ${row.idx}`}
            onclick={() => onDelete(row.idx)}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
          </button>
        {/if}
      </div>
    </div>
  {/each}
  {#if rules.length === 0}
    <div class="empty">Нет правил</div>
  {/if}
</div>

<style>
  .table {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .header, .row {
    display: grid;
    grid-template-columns: 24px 52px 92px minmax(0, 1fr) 96px 72px;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
  }
  .header {
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    font-weight: 500;
  }
  .header > div:nth-child(2),
  .header > div:nth-child(3),
  .row > div:nth-child(3),
  .header > div:nth-child(4),
  .header > div:nth-child(5) {
    text-align: center;
  }
  .header > div:nth-child(5),
  .row > div:nth-child(5) {
    min-width: 0;
  }
  .row > div:nth-child(5) {
    justify-self: center;
    text-align: center;
  }
  .row {
    padding: 6px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    font-size: 13px;
    --route-action-color: var(--accent);
  }
  .row.sys {
    opacity: 0.6;
    --route-action-color: var(--text-muted);
  }
  .row.route {
    background: rgba(122, 162, 247, 0.025);
    --route-action-color: var(--color-success, #22c55e);
  }
  .idx {
    font-family: var(--font-mono);
    color: var(--text-muted);
    font-size: 12px;
  }
  .reorder {
    display: flex;
    gap: 2px;
  }
  .matchers {
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
    white-space: normal;
    overflow-wrap: anywhere;
    word-break: break-word;
    line-height: 1.45;
  }
  .action-badge-cell {
    min-width: 0;
    justify-self: center;
  }
  .dash {
    color: var(--text-muted);
  }
  .actions-col {
    text-align: right;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 2px;
  }
  .route-action-btn {
    align-items: center;
    justify-content: center;
    display: inline-flex;
    width: 32px;
    min-width: 32px;
    height: 18px;
    padding: 0;
    border-radius: 9px;
    border: 1px solid color-mix(in srgb, var(--route-action-color, var(--accent)) 50%, transparent);
    background: color-mix(in srgb, var(--route-action-color, var(--accent)) 8%, transparent);
    color: color-mix(in srgb, var(--route-action-color, var(--accent)) 58%, transparent);
    box-shadow: 0 0 8px color-mix(in srgb, var(--route-action-color, var(--accent)) 18%, transparent);
    cursor: pointer;
    transition:
      color 0.16s ease,
      border-color 0.16s ease,
      background 0.16s ease,
      box-shadow 0.16s ease,
      transform 0.12s ease;
  }

  .route-action-btn svg {
    width: 13px;
    height: 13px;
    flex-shrink: 0;
  }

  .route-action-btn:hover:not(:disabled) {
    color: var(--route-action-color, var(--accent));
    border-color: color-mix(in srgb, var(--route-action-color, var(--accent)) 80%, transparent);
    background: color-mix(in srgb, var(--route-action-color, var(--accent)) 16%, transparent);
    box-shadow: 0 0 10px color-mix(in srgb, var(--route-action-color, var(--accent)) 34%, transparent);
  }

  .route-action-btn.danger:hover:not(:disabled) {
    color: var(--color-error, #dc2626);
    border-color: color-mix(in srgb, var(--color-error, #dc2626) 80%, transparent);
    background: color-mix(in srgb, var(--color-error, #dc2626) 14%, transparent);
    box-shadow: 0 0 10px color-mix(in srgb, var(--color-error, #dc2626) 30%, transparent);
  }

  .route-action-btn:active:not(:disabled) {
    transform: translateY(1px);
  }

  .route-action-btn:focus-visible {
    outline: 1px solid color-mix(in srgb, var(--route-action-color, var(--accent)) 90%, transparent);
    outline-offset: 2px;
  }

  .route-action-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
    box-shadow: none;
  }
  .empty {
    padding: 14px;
    color: var(--text-muted);
    text-align: center;
    font-size: 12px;
  }
  @media (max-width: 768px) {
    .table {
      overflow-x: auto;
    }
    .header, .row {
      grid-template-columns: 8px 42px 36px minmax(0, 1fr) 56px;
      gap: 4px;
      padding-inline: 8px;
    }
    .idx {
      min-width: 0;
      width: 8px;
      font-size: 9px;
      text-align: center;
      justify-self: center;
    }
    .header > div:first-child {
      width: 8px;
      min-width: 0;
      font-size: 0;
      color: transparent;
    }
    .header > div:nth-child(2) {
      font-size: 8px;
      line-height: 1;
      letter-spacing: 0.02em;
      white-space: nowrap;
      text-align: center;
    }
    .header > div:nth-child(4),
    .header > div:nth-child(6) {
      font-size: 8px;
      line-height: 1;
      letter-spacing: 0.02em;
      white-space: nowrap;
      text-align: center;
    }
    .header > div:nth-child(3) {
      color: transparent;
      font-size: 0;
      letter-spacing: 0;
      position: relative;
    }
    .header > div:nth-child(3)::after {
      content: 'ТИП';
      color: var(--text-muted);
      font-size: 8px;
      line-height: 1;
      letter-spacing: 0.02em;
    }
    .header > div:nth-child(6) {
      color: transparent;
      font-size: 0;
      letter-spacing: 0;
      position: relative;
    }
    .header > div:nth-child(6)::after {
      content: 'УПР.';
      color: var(--text-muted);
      font-size: 8px;
      line-height: 1;
      letter-spacing: 0.02em;
    }
    .header > div:nth-child(5), .row > div:nth-child(5) {
      display: none;
    }
    .action-badge-cell :global(.badge) {
      font-size: 8px;
      line-height: 1.15;
      padding: 0 0.1875rem;
      min-height: 12px;
      border-radius: 4px;
      letter-spacing: 0;
    }
    .reorder {
      min-width: 42px;
      width: 42px;
      justify-content: center;
      gap: 2px;
    }
    .reorder .route-action-btn {
      flex: 0 0 19px;
      width: 19px;
      min-width: 19px;
      height: 12px;
      min-height: 12px;
      padding: 0;
      border-radius: 6px;
    }
    .reorder .route-action-btn svg {
      width: 8px;
      height: 8px;
    }

    .actions-col {
      text-align: center;
      justify-self: end;
      width: 56px;
      min-width: 56px;
    }

    .actions {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: flex-end;
      gap: 4px;
    }

    .actions .route-action-btn {
      flex: 0 0 26px;
      width: 26px;
      min-width: 26px;
      height: 16px;
      min-height: 16px;
      padding: 0;
    }
  }
  /* Bare mode для embed внутри SidePanel — parent даёт chrome */
  .table.bare {
    background: transparent;
    border: 0;
    border-radius: 0;
  }
</style>
