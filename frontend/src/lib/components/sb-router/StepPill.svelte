<!--
  Источник дизайна: singbox-router/project/screens/AddRuleFlow.jsx (StepPill)
-->

<script lang="ts">
  import { Check } from 'lucide-svelte';

  interface Props {
    n: number;
    label: string;
    active: boolean;
    done: boolean;
  }

  let { n, label, active, done }: Props = $props();
</script>

<div class="pill" class:active class:done>
  <span class="bullet">
    {#if done}
      <Check size={12} color="#fff" />
    {:else}
      {n}
    {/if}
  </span>
  <span class="label">{label}</span>
</div>

<style>
  .pill {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 12px 4px 4px;
    border-radius: 999px;
    background: transparent;
    border: 1px solid transparent;
  }
  .pill.active {
    background: var(--accent-soft);
    border-color: var(--accent-line);
  }
  .bullet {
    width: 22px;
    height: 22px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--bg-tertiary);
    color: var(--text-muted);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 11px;
    font-weight: 700;
  }
  .pill.active .bullet {
    background: var(--accent);
    color: #fff;
  }
  .pill.done .bullet {
    background: var(--color-success, #22c55e);
    color: #fff;
  }
  .label {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--text-muted);
  }
  .pill.active .label {
    font-weight: 600;
    color: var(--text-primary);
  }
</style>
