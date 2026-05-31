<!--
  Источник дизайна: singbox-router/project/screens/AddRuleFlow.jsx
  (Step 1 «Описать вручную» disclosure)
-->

<script lang="ts">
  import { Code, Plus } from 'lucide-svelte';
  import { singboxRouter as singboxRouterStore } from '$lib/stores/singboxRouter';
  import {
    wizardCustom, updateCustomField, toggleCustomRuleSet,
  } from './addWizardStore';

  const ruleSets = singboxRouterStore.ruleSets;

  function handleInput<K extends 'domainSuffix' | 'ipCidr' | 'sourceIpCidr' | 'port'>(key: K) {
    return (e: Event) => {
      const value = (e.currentTarget as HTMLInputElement | HTMLTextAreaElement).value;
      updateCustomField(key, value);
    };
  }
</script>

<details class="form">
  <summary class="summary">
    <Code size={14} color="var(--text-muted)" />
    <span>Описать вручную</span>
    <span class="meta">· домены, IP, порты, rule-set, source</span>
  </summary>
  <div class="body">
    <div class="field">
      <label class="lbl" for="cm-domain">Домены (домен или *.example.com)</label>
      <textarea
        id="cm-domain"
        class="ta"
        rows="2"
        placeholder="*.netflix.com&#10;youtube.com"
        value={$wizardCustom.domainSuffix}
        oninput={handleInput('domainSuffix')}
      ></textarea>
    </div>

    <div class="field">
      <label class="lbl" for="cm-ip">IP / CIDR</label>
      <textarea
        id="cm-ip"
        class="ta"
        rows="2"
        placeholder="1.1.1.1/32&#10;104.16.0.0/12"
        value={$wizardCustom.ipCidr}
        oninput={handleInput('ipCidr')}
      ></textarea>
    </div>

    <div class="field-row">
      <div class="field">
        <label class="lbl" for="cm-port">Порт</label>
        <input
          id="cm-port"
          class="inp"
          type="text"
          placeholder="443 или 80,443"
          value={$wizardCustom.port}
          oninput={handleInput('port')}
        />
      </div>
      <div class="field">
        <label class="lbl" for="cm-src">Source IP (опц.)</label>
        <textarea
          id="cm-src"
          class="ta"
          rows="1"
          placeholder="192.168.1.10/32"
          value={$wizardCustom.sourceIpCidr}
          oninput={handleInput('sourceIpCidr')}
        ></textarea>
      </div>
    </div>

    {#if $ruleSets.length > 0}
      <div class="field">
        <span class="lbl">Rule-sets (опц.)</span>
        <div class="chips">
          {#each $ruleSets as rs (rs.tag)}
            {@const selected = $wizardCustom.ruleSetTags.has(rs.tag)}
            <button type="button" class="rs-chip" class:selected onclick={() => toggleCustomRuleSet(rs.tag)}>
              {#if !selected}<Plus size={10} />{/if}
              <span>{rs.tag}</span>
            </button>
          {/each}
        </div>
      </div>
    {/if}
  </div>
</details>

<style>
  .form {
    padding: 12px;
    border-radius: var(--radius-sm);
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
  }
  .summary {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 12.5px;
    font-weight: 500;
    list-style: none;
  }
  .summary::-webkit-details-marker { display: none; }
  .meta {
    color: var(--text-muted);
    font-weight: 400;
  }
  .body {
    margin-top: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .lbl {
    font-size: 11px;
    color: var(--text-muted);
    font-weight: 500;
  }
  .ta, .inp {
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-primary);
    border: 1px solid var(--border);
    color: var(--text-primary);
    font-size: 12.5px;
    font-family: var(--font-mono);
    resize: vertical;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .rs-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: 999px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 11.5px;
    cursor: pointer;
  }
  .rs-chip.selected {
    background: var(--accent-soft);
    border-color: var(--accent-line);
    color: var(--accent);
    font-weight: 600;
  }
</style>
