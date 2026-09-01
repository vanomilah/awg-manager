<script lang="ts">
  import { Modal, Button } from '$lib/components/ui';
  import type { MihomoNativeRuleProvider } from '$lib/types';
  import { api } from '$lib/api/client';
  import { notifications } from '$lib/stores/notifications';

  interface Props {
    open: boolean;
    provider?: MihomoNativeRuleProvider | null;
    onClose: () => void;
    onSaved: () => void;
  }

  let {
    open,
    provider = null,
    onClose,
    onSaved,
  }: Props = $props();

  let id = $state('');
  let name = $state('');
  let type = $state<'http' | 'file'>('http');
  let url = $state('');
  let path = $state('');
  let behavior = $state<'classical' | 'domain' | 'ipcidr'>('classical');
  let format = $state<'yaml' | 'text' | 'mrs'>('yaml');
  let interval = $state(86400);
  let enabled = $state(true);
  let saving = $state(false);

  $effect(() => {
    if (open) {
      if (provider) {
        id = provider.id;
        name = provider.name;
        type = provider.type;
        url = provider.url || '';
        path = provider.path || '';
        behavior = provider.behavior || 'classical';
        format = provider.format || 'yaml';
        interval = provider.interval || 86400;
        enabled = provider.enabled ?? true;
      } else {
        id = '';
        name = '';
        type = 'http';
        url = '';
        path = '';
        behavior = 'classical';
        format = 'yaml';
        interval = 86400;
        enabled = true;
      }
    }
  });

  async function handleSave() {
    if (saving) return;
    if (!name.trim()) {
      notifications.error('Укажите имя набора правил');
      return;
    }
    if (type === 'http' && !url.trim()) {
      notifications.error('Укажите URL для загрузки набора правил');
      return;
    }
    if (type === 'file' && !path.trim()) {
      notifications.error('Укажите путь к локальному файлу');
      return;
    }
    saving = true;
    try {
      await api.mihomoNativeSaveRuleProvider({
        id: id || undefined,
        name: name.trim(),
        type,
        url: type === 'http' ? url.trim() : undefined,
        path: type === 'file' ? path.trim() : undefined,
        behavior,
        format,
        interval,
        enabled,
      });
      notifications.success('Rule Provider сохранён');
      onSaved();
      onClose();
    } catch (e) {
      notifications.error(e instanceof Error ? e.message : 'Ошибка сохранения Rule Provider');
    } finally {
      saving = false;
    }
  }
</script>

<Modal {open} title={provider ? `Редактирование Rule Provider: ${provider.name}` : 'Новый набор правил (Rule Provider)'} onclose={onClose} size="md">
  <div class="form-stack">
    <label class="form-group">
      <span class="label-text">Имя набора правил</span>
      <input type="text" bind:value={name} placeholder="Например: antizapret или direct-list" class="text-input" />
    </label>

    <label class="form-group">
      <span class="label-text">Тип источника</span>
      <select bind:value={type} class="select-input">
        <option value="http">HTTP / HTTPS (Автоматическая загрузка по ссылке)</option>
        <option value="file">Локальный файл на роутере</option>
      </select>
    </label>

    {#if type === 'http'}
      <label class="form-group">
        <span class="label-text">URL источника правил</span>
        <input type="text" bind:value={url} placeholder="https://raw.githubusercontent.com/.../ruleset.yaml" class="text-input" />
      </label>

      <label class="form-group">
        <span class="label-text">Интервал авто-обновления (сек)</span>
        <input type="number" min="300" max="604800" bind:value={interval} class="text-input" />
      </label>
    {:else}
      <label class="form-group">
        <span class="label-text">Путь к файлу на роутере</span>
        <input type="text" bind:value={path} placeholder="/opt/etc/awg-manager/rules.yaml" class="text-input" />
      </label>
    {/if}

    <div class="two-col">
      <label class="form-group">
        <span class="label-text">Тип содержимого (Behavior)</span>
        <select bind:value={behavior} class="select-input">
          <option value="classical">classical (Полные правила Clash)</option>
          <option value="domain">domain (Список доменов)</option>
          <option value="ipcidr">ipcidr (Список IP/подсетей)</option>
        </select>
      </label>

      <label class="form-group">
        <span class="label-text">Формат файла</span>
        <select bind:value={format} class="select-input">
          <option value="yaml">yaml (YAML/Текст)</option>
          <option value="text">text (Построчный текст)</option>
          <option value="mrs">mrs (Бинарный Mihomo Rule Set)</option>
        </select>
      </label>
    </div>

    <label class="check-label">
      <input type="checkbox" bind:checked={enabled} />
      <span>Провайдер включен</span>
    </label>
  </div>

  {#snippet actions()}
    <Button variant="ghost" onclick={onClose} disabled={saving}>Отмена</Button>
    <Button variant="primary" loading={saving} onclick={handleSave}>Сохранить</Button>
  {/snippet}
</Modal>

<style>
  .form-stack {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 8px 0;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .label-text {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary, #9ba1a6);
  }

  .select-input, .text-input {
    background: var(--bg-tertiary, #18181f);
    border: 1px solid var(--border, #2e2e38);
    border-radius: 6px;
    padding: 8px 12px;
    color: var(--text-primary, #fff);
    font-size: 14px;
    outline: none;
  }

  .select-input:focus, .text-input:focus {
    border-color: var(--accent, #3b82f6);
  }

  .two-col {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .check-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-primary, #fff);
    cursor: pointer;
    margin-top: 4px;
  }
</style>
