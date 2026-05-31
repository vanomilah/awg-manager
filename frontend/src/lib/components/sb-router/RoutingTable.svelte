<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (RoutingTable)
  Reuse adapters.ts из F2 для action + matchers расшифровки.
-->

<script lang="ts">
  import type { SingboxRouterRule, SingboxRouterOutbound } from '$lib/types';
  import { Badge } from '$lib/components/ui';
  import { ChevronUp, ChevronDown, Edit3, Trash2 } from 'lucide-svelte';
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
          <button type="button" class="icon-btn" title="Вверх" disabled={row.idx === 0} onclick={() => onMove(row.idx, 'up')}>
            <ChevronUp size={12} />
          </button>
          <button type="button" class="icon-btn" title="Вниз" disabled={row.idx === rules.length - 1} onclick={() => onMove(row.idx, 'down')}>
            <ChevronDown size={12} />
          </button>
        {/if}
      </div>
      <div>
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
          <button type="button" class="icon-btn" title={`Редактировать правило #${row.idx}`} onclick={() => onEdit(row.idx)}>
            <Edit3 size={12} />
          </button>
          <button type="button" class="icon-btn danger" title={`Удалить правило #${row.idx}`} onclick={() => onDelete(row.idx)}>
            <Trash2 size={12} />
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
  }
  .row.sys {
    opacity: 0.6;
  }
  .row.route {
    background: rgba(122, 162, 247, 0.025);
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
  .icon-btn {
    background: transparent;
    border: 0;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .icon-btn:hover:not(:disabled) {
    color: var(--text-primary);
    background: var(--bg-tertiary);
  }
  .icon-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }
  .icon-btn.danger:hover {
    color: var(--color-error, #dc2626);
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
      grid-template-columns: 24px 50px 88px 1fr 60px;
    }
    .header > div:nth-child(5), .row > div:nth-child(5) {
      display: none;
    }
  }
  /* Bare mode для embed внутри SidePanel — parent даёт chrome */
  .table.bare {
    background: transparent;
    border: 0;
    border-radius: 0;
  }
</style>
