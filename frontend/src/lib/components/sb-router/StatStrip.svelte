<!--
  Источник дизайна: singbox-router/project/screens/MainExpert.jsx (StatCell + status strip)
-->

<script lang="ts" module>
  export interface StatCellData {
    label: string;
    value: string;
    tone?: 'success' | 'error' | 'muted' | 'default';
  }
</script>

<script lang="ts">
  interface Props {
    cells: StatCellData[];
  }
  let { cells }: Props = $props();

  function colorFor(tone?: StatCellData['tone']): string {
    switch (tone) {
      case 'success': return 'var(--color-success, #22c55e)';
      case 'error': return 'var(--color-error, #dc2626)';
      case 'muted': return 'var(--text-muted)';
      default: return 'var(--text-primary)';
    }
  }
</script>

<div class="strip" style:--cols={cells.length}>
  {#each cells as cell, i (i)}
    <div class="cell" class:last={i === cells.length - 1}>
      <div class="label">{cell.label}</div>
      <div class="value" style:color={colorFor(cell.tone)}>{cell.value}</div>
    </div>
  {/each}
</div>

<style>
  .strip {
    display: grid;
    grid-template-columns: repeat(var(--cols, 6), minmax(0, 1fr));
    gap: 0;
    margin-bottom: 24px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  @media (max-width: 768px) {
    .strip {
      grid-template-columns: repeat(3, 1fr);
    }
    .cell.last, .cell:nth-child(3) {
      border-right: 0;
    }
  }
  .cell {
    padding: 12px 16px;
    border-right: 1px solid var(--border);
  }
  .cell.last {
    border-right: 0;
  }
  .label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }
  .value {
    font-size: 18px;
    font-weight: 700;
    margin-top: 2px;
    font-family: var(--font-mono);
  }
</style>
