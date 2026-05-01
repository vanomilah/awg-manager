<script lang="ts">
	import Modal from '$lib/components/ui/Modal.svelte';
	import { Dropdown, type DropdownOption } from '$lib/components/ui';
	import type { SingboxRouterOutbound } from '$lib/types';
	import type { OutboundGroup } from './outboundOptions';

	const STRATEGY_OPTIONS: DropdownOption[] = [
		{ value: 'round_robin', label: 'round_robin' },
		{ value: 'consistent_hashing', label: 'consistent_hashing' },
	];

	interface Props {
		outbound?: SingboxRouterOutbound;
		outboundOptions: OutboundGroup[];
		onClose: () => void;
		onSave: (o: SingboxRouterOutbound) => Promise<void> | void;
	}
	let { outbound, outboundOptions, onClose, onSave }: Props = $props();

	// svelte-ignore state_referenced_locally
	let type: 'urltest' | 'selector' | 'loadbalance' = $state(
		(outbound?.type as 'urltest' | 'selector' | 'loadbalance') ?? 'urltest'
	);
	// svelte-ignore state_referenced_locally
	let tag = $state(outbound?.tag ?? '');
	// svelte-ignore state_referenced_locally
	let members = $state<string[]>([...(outbound?.outbounds ?? [])]);
	// svelte-ignore state_referenced_locally
	let url = $state(outbound?.url ?? 'https://www.gstatic.com/generate_204');
	// svelte-ignore state_referenced_locally
	let interval = $state(outbound?.interval ?? '3m');
	// svelte-ignore state_referenced_locally
	let tolerance = $state(outbound?.tolerance ?? 50);
	// svelte-ignore state_referenced_locally
	let defaultOutbound = $state(outbound?.default ?? '');
	// svelte-ignore state_referenced_locally
	let strategy = $state(outbound?.strategy ?? 'round_robin');

	let busy = $state(false);
	let error = $state('');
	let memberPicker = $state('');

	// Flat options with group labels for the Dropdown native grouping.
	// Filter out tags already added so the user can't pick duplicates.
	const memberDropdownOptions = $derived<DropdownOption[]>(
		outboundOptions.flatMap((g) =>
			g.items
				.filter((i) => !members.includes(i.value))
				.map((i) => ({ value: i.value, label: i.label, group: g.group }))
		)
	);

	// Default-picker options: only members already chosen.
	const defaultOptions = $derived<DropdownOption[]>(
		members.map((m) => ({ value: m, label: m }))
	);

	function addMember(v: string): void {
		if (!v) return;
		if (members.includes(v)) return;
		members = [...members, v];
		// Reset picker so the same slot can be reused for the next addition.
		memberPicker = '';
	}

	function removeMember(v: string): void {
		members = members.filter((m) => m !== v);
		if (defaultOutbound === v) defaultOutbound = '';
	}

	async function save(): Promise<void> {
		busy = true;
		error = '';
		try {
			if (!tag.trim()) {
				error = 'Tag обязателен';
				busy = false;
				return;
			}
			if (members.length < 2) {
				error = 'Нужно минимум 2 члена';
				busy = false;
				return;
			}

			const built: SingboxRouterOutbound = {
				type,
				tag: tag.trim(),
				outbounds: [...members],
			};
			if (type === 'urltest') {
				built.url = url;
				built.interval = interval;
				built.tolerance = tolerance;
			} else if (type === 'selector') {
				built.default = defaultOutbound || members[0];
			} else if (type === 'loadbalance') {
				built.strategy = strategy;
			}

			await onSave(built);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	const typeDescription = $derived.by(() => {
		if (type === 'urltest') return 'Периодически пингует каждого члена и автоматически направляет через самого быстрого.';
		if (type === 'selector') return 'Ручное переключение через Clash API. Новые подключения идут через выбранный default.';
		return 'Равномерно распределяет нагрузку между членами по выбранной стратегии.';
	});
</script>

<Modal open onclose={onClose} title={outbound ? 'Редактировать outbound' : 'Новый outbound'}>
	<div class="form">
		<div class="section-label">Тип</div>
		<div class="segment">
			<button class:active={type === 'urltest'} onclick={() => (type = 'urltest')} type="button">URLTest</button>
			<button class:active={type === 'selector'} onclick={() => (type = 'selector')} type="button">Selector</button>
			<button class:active={type === 'loadbalance'} onclick={() => (type = 'loadbalance')} type="button">LoadBalance</button>
		</div>
		<div class="type-hint">{typeDescription}</div>

		<label class="field">
			<div class="lbl">Tag (имя)</div>
			<input bind:value={tag} placeholder="fast-de" />
		</label>

		<div class="field">
			<div class="lbl">Members (минимум 2)</div>
			<div class="member-chips" class:empty={members.length === 0}>
				{#if members.length === 0}
					<span class="chips-placeholder">Участники не выбраны</span>
				{:else}
					{#each members as m (m)}
						<span class="member-chip">
							<span class="member-chip-label">{m}</span>
							<button
								type="button"
								class="member-chip-remove"
								aria-label={`Удалить ${m}`}
								title="Удалить"
								onclick={() => removeMember(m)}
							>
								<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
									<line x1="18" y1="6" x2="6" y2="18" />
									<line x1="6" y1="6" x2="18" y2="18" />
								</svg>
							</button>
						</span>
					{/each}
				{/if}
			</div>
			<Dropdown
				value={memberPicker}
				options={memberDropdownOptions}
				placeholder="Добавить участника"
				onchange={addMember}
				fullWidth
			/>
		</div>

		{#if type === 'urltest'}
			<label class="field">
				<div class="lbl">Test URL</div>
				<input bind:value={url} />
			</label>
			<div class="row2">
				<label class="field">
					<div class="lbl">Interval</div>
					<input bind:value={interval} placeholder="3m" />
				</label>
				<label class="field">
					<div class="lbl">Tolerance (ms)</div>
					<input type="number" bind:value={tolerance} />
				</label>
			</div>
		{:else if type === 'selector'}
			<div class="field">
				<div class="lbl">Default (один из members)</div>
				<Dropdown
					bind:value={defaultOutbound}
					options={defaultOptions}
					placeholder={members.length === 0 ? 'Сначала добавьте участников' : '— выбрать —'}
					disabled={members.length === 0}
					fullWidth
				/>
			</div>
		{:else}
			<div class="field">
				<div class="lbl">Strategy</div>
				<Dropdown bind:value={strategy} options={STRATEGY_OPTIONS} fullWidth />
			</div>
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
	.type-hint {
		font-size: 0.75rem;
		color: var(--muted-text);
		background: var(--bg);
		padding: 0.5rem 0.75rem;
		border-radius: 4px;
		line-height: 1.5;
	}
	.field {
		display: grid;
		gap: 0.25rem;
	}
	.lbl {
		font-size: 0.75rem;
		color: var(--muted-text);
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
	.segment {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 4px;
		overflow: hidden;
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
		color: white;
		font-weight: 600;
	}
	.row2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.5rem;
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

	.member-chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
		padding: 0.4rem 0.5rem;
		min-height: 2.1rem;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: 4px;
		align-items: center;
	}
	.member-chips.empty {
		justify-content: flex-start;
	}
	.chips-placeholder {
		font-size: 0.78rem;
		color: var(--muted-text);
		font-style: italic;
	}
	.member-chip {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		background: var(--color-accent-tint, var(--bg));
		color: var(--color-accent, var(--text));
		border: 1px solid var(--color-accent-border, var(--border));
		border-radius: 999px;
		padding: 0.15rem 0.25rem 0.15rem 0.6rem;
		font-family: ui-monospace, monospace;
		font-size: 0.78rem;
		line-height: 1.3;
		max-width: 100%;
	}
	.member-chip-label {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.member-chip-remove {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.1rem;
		height: 1.1rem;
		padding: 0;
		background: transparent;
		border: none;
		border-radius: 999px;
		color: inherit;
		opacity: 0.7;
		cursor: pointer;
		transition: opacity var(--t-fast, 120ms) ease, background var(--t-fast, 120ms) ease;
	}
	.member-chip-remove:hover {
		opacity: 1;
		background: rgba(0, 0, 0, 0.12);
	}
	.member-chip-remove svg {
		width: 12px;
		height: 12px;
	}
</style>
