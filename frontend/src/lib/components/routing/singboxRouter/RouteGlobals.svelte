<script lang="ts">
	import { api } from '$lib/api/client';
	import { Dropdown, type DropdownOption } from '$lib/components/ui';
	import { notifications } from '$lib/stores/notifications';
	import type { OutboundGroup } from './outboundOptions';

	interface Props {
		currentFinal: string;
		outboundOptions: OutboundGroup[];
		onChange: () => Promise<void> | void;
	}
	let { currentFinal, outboundOptions, onChange }: Props = $props();

	const dropdownOptions = $derived<DropdownOption[]>([
		{ value: 'direct', label: 'direct (мимо VPN)' },
		...outboundOptions
			.filter((g) => g.group !== 'Специальные')
			.flatMap((g) => g.items.map((i) => ({ value: i.value, label: i.label, group: g.group }))),
	]);

	// svelte-ignore state_referenced_locally
	let draftFinal = $state(currentFinal || 'direct');
	let busy = $state(false);

	$effect(() => {
		draftFinal = currentFinal || 'direct';
	});

	const dirty = $derived(draftFinal !== (currentFinal || 'direct'));

	async function save(): Promise<void> {
		if (!dirty || busy) return;
		busy = true;
		try {
			await api.singboxRouterPutRouteFinal(draftFinal);
			await onChange();
		} catch (e) {
			notifications.error((e as Error).message);
		} finally {
			busy = false;
		}
	}
</script>

<div class="card">
	<div class="title">Маршрутизация — Final</div>
	<div class="field">
		<Dropdown bind:value={draftFinal} options={dropdownOptions} fullWidth />
		<div class="hint">
			Куда направлять трафик, если ни одно правило не подошло. По умолчанию
			<code>direct</code> — прямое соединение мимо VPN.
		</div>
	</div>
	<div class="actions">
		<button class="btn btn-primary" onclick={save} disabled={busy || !dirty} type="button">
			Сохранить
		</button>
	</div>
</div>

<style>
	.card {
		background: var(--surface-bg);
		padding: 0.8rem 1rem;
		border-radius: 6px;
		margin-bottom: 1rem;
	}
	.title {
		font-size: 0.8rem;
		font-weight: 600;
		margin-bottom: 0.6rem;
		color: var(--text);
	}
	.field {
		display: grid;
		gap: 0.25rem;
	}
	.hint {
		font-size: 0.72rem;
		color: var(--muted-text);
		line-height: 1.3;
	}
	.hint code {
		background: var(--bg);
		padding: 0.05rem 0.25rem;
		border-radius: 2px;
		font-family: ui-monospace, monospace;
	}
	.actions {
		margin-top: 0.75rem;
		display: flex;
		justify-content: flex-end;
	}
</style>
