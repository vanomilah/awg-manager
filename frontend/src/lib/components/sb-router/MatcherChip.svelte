<!--
  Источник дизайна: singbox-router/project/parts/RuleCard.jsx (MatcherChip)
  При правках сверять с JSX напрямую — не угадывать spacing/typography/layout.
-->

<script lang="ts" module>
  import type { MatcherKind } from './types';

  /** Локализованная подпись для каждой категории матчера. */
  const LABELS: Record<MatcherKind, string> = {
    domain:   'домен',
    ip:       'IP',
    port:     'порт',
    src:      'источник',
    ruleset:  'набор',
    protocol: 'proto',
    private:  'тип',
  };
</script>

<script lang="ts">
  interface Props {
    kind: MatcherKind;
    label: string;
    /** Если true — значение mono шрифтом (для IP/port/cidr). Подпись слева всегда sans. */
    mono?: boolean;
  }
  let { kind, label, mono = false }: Props = $props();
</script>

<span class="chip">
  <span class="chip-key">{LABELS[kind]}:</span>
  <span class="chip-val" class:is-mono={mono}>{label}</span>
</span>

<style>
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    padding: 2px 7px;
    border-radius: 4px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    white-space: nowrap;
    line-height: 1.4;
  }
  .chip-key {
    font-size: 10px;
    color: var(--text-muted);
    font-family: var(--font-sans);
  }
  .chip-val {
    color: var(--text-primary);
    font-family: var(--font-sans);
  }
  .chip-val.is-mono {
    font-family: var(--font-mono);
  }
</style>
