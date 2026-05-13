<script lang="ts">
	import Modal from '$lib/components/ui/Modal.svelte';
	import { Dropdown, type DropdownOption } from '$lib/components/ui';
	import type { SingboxRouterRuleSet } from '$lib/types';
	import type { OutboundGroup } from './outboundOptions';

	interface Props {
		ruleSet?: SingboxRouterRuleSet;
		outboundOptions: OutboundGroup[];
		onClose: () => void;
		onSave: (rs: SingboxRouterRuleSet) => Promise<void> | void;
	}
	let { ruleSet, outboundOptions, onClose, onSave }: Props = $props();

	const UPDATE_INTERVAL_OPTIONS: DropdownOption[] = [
		{ value: '6h', label: '6h' },
		{ value: '12h', label: '12h' },
		{ value: '24h', label: '24h (рекомендуется)' },
		{ value: '168h', label: '168h (неделя)' },
	];

	const downloadDetourOptions = $derived<DropdownOption[]>([
		{ value: '', label: 'автоматически (direct)' },
		...outboundOptions.flatMap((g) =>
			g.items.map((i) => ({ value: i.value, label: i.label, group: g.group })),
		),
	]);

	const isEditing = $derived(Boolean(ruleSet));

	// svelte-ignore state_referenced_locally
	let type: 'remote' | 'local' | 'inline' = $state(ruleSet?.type ?? 'remote');
	// svelte-ignore state_referenced_locally
	let format: 'binary' | 'source' = $state(ruleSet?.format ?? 'binary');
	// svelte-ignore state_referenced_locally
	let tag = $state(ruleSet?.tag ?? '');
	// svelte-ignore state_referenced_locally
	let url = $state(ruleSet?.url ?? '');
	// svelte-ignore state_referenced_locally
	let updateInterval = $state(ruleSet?.update_interval ?? '24h');
	// svelte-ignore state_referenced_locally
	let downloadDetour = $state(ruleSet?.download_detour ?? '');
	// svelte-ignore state_referenced_locally
	let path = $state(ruleSet?.path ?? '');
	// svelte-ignore state_referenced_locally
	let rulesJson = $state(
		ruleSet?.rules?.length
			? JSON.stringify(ruleSet.rules, null, 2)
			: `[
  {
    "domain_suffix": [
      ".example.com"
    ]
  }
]`,
	);

	let busy = $state(false);
	let error = $state('');

	// Snapshot initial state for isDirty detection
	let initialType: 'remote' | 'local' | 'inline' = $state('remote');
	let initialFormat: 'binary' | 'source' = $state('binary');
	let initialTag = $state('');
	let initialUrl = $state('');
	let initialUpdateInterval = $state('24h');
	let initialDownloadDetour = $state('');
	let initialPath = $state('');
	let initialRulesJson = $state('');

	// Initialize snapshot when modal opens
	$effect(() => {
		if (ruleSet) {
			initialType = ruleSet.type;
			initialFormat = ruleSet.format ?? 'binary';
			initialTag = ruleSet.tag;
			initialUrl = ruleSet.url ?? '';
			initialUpdateInterval = ruleSet.update_interval ?? '24h';
			initialDownloadDetour = ruleSet.download_detour ?? '';
			initialPath = ruleSet.path ?? '';
			initialRulesJson = ruleSet.rules?.length ? JSON.stringify(ruleSet.rules, null, 2) : '';
		} else {
			initialType = 'remote';
			initialFormat = 'binary';
			initialTag = '';
			initialUrl = '';
			initialUpdateInterval = '24h';
			initialDownloadDetour = '';
			initialPath = '';
			initialRulesJson = '';
		}
	});

	const isDirty = $derived.by(() => {
		return (
			type !== initialType ||
			format !== initialFormat ||
			tag !== initialTag ||
			url !== initialUrl ||
			updateInterval !== initialUpdateInterval ||
			downloadDetour !== initialDownloadDetour ||
			path !== initialPath ||
			rulesJson !== initialRulesJson
		);
	});

	async function save(): Promise<void> {
		busy = true;
		error = '';
		try {
			const cleanTag = isEditing ? (ruleSet?.tag ?? '') : tag.trim();
			if (!cleanTag) {
				error = 'Tag обязателен';
				busy = false;
				return;
			}
			if (type === 'remote' && !url.trim()) {
				error = 'URL обязателен для type=remote';
				busy = false;
				return;
			}
			if (type === 'local' && !path.trim()) {
				error = 'Path обязателен для type=local';
				busy = false;
				return;
			}

			let parsedRules: Record<string, unknown>[] | undefined;
			if (type === 'inline') {
				try {
					const parsed = JSON.parse(rulesJson);
					if (!Array.isArray(parsed) || parsed.length === 0) {
						error = 'Для inline rule set нужен непустой JSON-массив правил';
						busy = false;
						return;
					}
					parsedRules = parsed as Record<string, unknown>[];
				} catch (e) {
					error = `Некорректный JSON: ${(e as Error).message}`;
					busy = false;
					return;
				}
			}

			const built: SingboxRouterRuleSet = {
				tag: cleanTag,
				type,
				format: type === 'inline' ? undefined : format,
				url: type === 'remote' ? url.trim() : undefined,
				update_interval: type === 'remote' ? updateInterval : undefined,
				download_detour: type === 'remote' && downloadDetour ? downloadDetour : undefined,
				path: type === 'local' ? path.trim() : undefined,
				rules: type === 'inline' ? parsedRules : undefined,
			};
			await onSave(built);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<Modal open onclose={onClose} title={ruleSet ? 'Редактировать rule set' : 'Новый rule set'} hasUnsavedChanges={() => isDirty}>
	<div class="form">
		<div class="section-label">Тип</div>
		<div class="segment">
			<button class:active={type === 'remote'} onclick={() => (type = 'remote')} type="button">Remote</button>
			<button class:active={type === 'local'} onclick={() => (type = 'local')} type="button">Local</button>
			<button class:active={type === 'inline'} onclick={() => (type = 'inline')} type="button">Inline</button>
		</div>

		<label class="field">
			<div class="lbl">Tag (имя)</div>
			<input bind:value={tag} placeholder="geosite-example" disabled={isEditing} />
			{#if isEditing}<div class="hint">Tag нельзя менять у существующего набора.</div>{/if}
		</label>

		{#if type !== 'inline'}
			<label class="field">
				<div class="lbl">Формат</div>
				<div class="segment">
					<button class:active={format === 'binary'} onclick={() => (format = 'binary')} type="button">Binary (.srs)</button>
					<button class:active={format === 'source'} onclick={() => (format = 'source')} type="button">Source (JSON)</button>
				</div>
			</label>
		{/if}

		{#if type === 'remote'}
			<label class="field">
				<div class="lbl">URL к файлу</div>
				<input bind:value={url} placeholder="https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-example.srs" />
			</label>

			<label class="field">
				<div class="lbl">Интервал обновления</div>
				<Dropdown bind:value={updateInterval} options={UPDATE_INTERVAL_OPTIONS} fullWidth />
			</label>

			<div class="field highlight">
				<div class="lbl">Скачивать через (download detour)</div>
				<Dropdown bind:value={downloadDetour} options={downloadDetourOptions} fullWidth />
				<div class="hint">
					Через какой outbound скачивать этот файл. Полезно если URL заблокирован у провайдера — используйте VPN-туннель.
				</div>
			</div>
		{:else if type === 'local'}
			<label class="field">
				<div class="lbl">Путь к файлу</div>
				<input bind:value={path} placeholder="/opt/etc/awg-manager/singbox/rulesets/my-custom.srs" />
				<div class="hint">Абсолютный путь. Файл должен существовать на роутере.</div>
			</label>
		{:else}
			<label class="field">
				<div class="lbl">Правила (JSON-массив)</div>
				<textarea class="rules-json" bind:value={rulesJson} rows="10" spellcheck="false"></textarea>
				<div class="hint">
					Массив объектов с матчерами sing-box: <code>domain_suffix</code>, <code>ip_cidr</code>,
					<code>process_name</code>, <code>port</code> и др. Хорошо для маленьких пользовательских списков.
				</div>
			</label>
		{/if}

		{#if error}<div class="error">{error}</div>{/if}

		<div class="actions">
			<button class="btn btn-secondary" onclick={onClose} type="button">Отмена</button>
			<button class="btn btn-primary" onclick={save} disabled={busy} type="button">Сохранить</button>
		</div>
	</div>
</Modal>

<style>
	.form {
		display: grid;
		gap: 0.6rem;
		min-width: 0;
	}
	.section-label {
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.5px;
		color: var(--muted-text);
	}
	.field {
		display: grid;
		gap: 0.25rem;
	}
	.field.highlight {
		padding: 0.6rem;
		background: var(--bg);
		border-left: 2px solid var(--accent, #3b82f6);
		border-radius: 4px;
	}
	.lbl {
		font-size: 0.75rem;
		color: var(--muted-text);
	}
	.hint {
		font-size: 0.75rem;
		color: var(--muted-text);
		line-height: 1.4;
		margin-top: 0.25rem;
	}
	.field input {
		background: var(--bg);
		border: 1px solid var(--border);
		padding: 0.4rem 0.6rem;
		border-radius: 4px;
		color: var(--text);
		font-family: ui-monospace, monospace;
		font-size: 0.85rem;
		width: 100%;
		box-sizing: border-box;
	}
	.field input:disabled {
		opacity: 0.7;
		cursor: not-allowed;
	}
	.rules-json {
		background: var(--bg);
		border: 1px solid var(--border);
		padding: 0.5rem 0.6rem;
		border-radius: 4px;
		color: var(--text);
		font-family: ui-monospace, monospace;
		font-size: 0.8rem;
		width: 100%;
		box-sizing: border-box;
		resize: vertical;
		line-height: 1.45;
	}
	.hint code {
		font-family: ui-monospace, monospace;
		background: var(--bg-tertiary, var(--bg));
		padding: 0 0.25rem;
		border-radius: 3px;
	}
	.segment {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 4px;
		overflow: hidden;
		width: fit-content;
	}
	.segment button {
		background: transparent;
		border: none;
		padding: 0.4rem 0.9rem;
		font-size: 0.85rem;
		cursor: pointer;
		color: var(--muted-text);
	}
	.segment button + button {
		border-left: 1px solid var(--border);
	}
	.segment button.active {
		background: var(--accent, #3b82f6);
		color: var(--color-accent-contrast, #ffffff);
		font-weight: 600;
	}
	.error {
		color: var(--danger, #dc2626);
		font-size: 0.85rem;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
	}
</style>
