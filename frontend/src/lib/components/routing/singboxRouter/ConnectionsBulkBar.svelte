<!-- frontend/src/lib/components/routing/singboxRouter/ConnectionsBulkBar.svelte -->
<script lang="ts">
	import type { Connection } from '$lib/types/singboxConnections';
	import { formatBytes } from '$lib/utils/format';
	import { Modal } from '$lib/components/ui';

	interface Props {
		visible: Connection[];
		total: number;
		onConfirmKill: () => Promise<void>;
	}

	let { visible, total, onConfirmKill }: Props = $props();

	let showModal = $state(false);
	let busy = $state(false);

	const totalUp = $derived(visible.reduce((s, c) => s + c.upload, 0));
	const totalDown = $derived(visible.reduce((s, c) => s + c.download, 0));

	const outboundCounts = $derived.by(() => {
		const m = new Map<string, number>();
		for (const c of visible) {
			const k = c.outboundLabel;
			m.set(k, (m.get(k) ?? 0) + 1);
		}
		return Array.from(m.entries()).sort((a, b) => b[1] - a[1]);
	});

	async function confirm(): Promise<void> {
		busy = true;
		try {
			await onConfirmKill();
		} finally {
			busy = false;
			showModal = false;
		}
	}
</script>

{#if visible.length > 0 && visible.length < total}
	<div class="bar">
		<span>
			Видимо: <strong>{visible.length}</strong> из {total}
			<span class="dot">·</span>
			↑ {formatBytes(totalUp)} · ↓ {formatBytes(totalDown)}
		</span>
		<button class="kill-btn" type="button" onclick={() => (showModal = true)}>Закрыть видимые</button>
	</div>
{/if}

{#if showModal}
	<Modal open={showModal} onclose={() => (showModal = false)} title="Подтверждение">
		<p>Закрыть <strong>{visible.length}</strong> соединений?</p>
		<p class="muted small">Затронутые outbound'ы:</p>
		<ul class="outbound-list">
			{#each outboundCounts as [k, n]}
				<li><span class="badge">{k}</span> <span class="muted">({n})</span></li>
			{/each}
		</ul>
		<p class="muted small">Это действие необратимо — клиенты переподключатся.</p>
		<div class="actions">
			<button type="button" disabled={busy} onclick={() => (showModal = false)}>Отмена</button>
			<button class="primary" type="button" disabled={busy} onclick={confirm}>
				{busy ? 'Закрываю…' : 'Закрыть всё'}
			</button>
		</div>
	</Modal>
{/if}

<style>
	.bar {
		display: flex; justify-content: space-between; align-items: center;
		padding: 8px 12px; margin-bottom: 8px;
		background: var(--surface-1, #1f2425);
		border: 1px solid var(--border-1, #2c3134);
		border-radius: 6px;
		font-size: 13px;
	}
	.dot { color: var(--text-tertiary, #6e6e6e); margin: 0 6px; }
	.kill-btn {
		all: unset; cursor: pointer;
		padding: 4px 12px;
		border-radius: 4px;
		background: rgba(255, 107, 107, 0.1);
		color: #ff6b6b;
		font-size: 12px;
	}
	.kill-btn:hover { background: rgba(255, 107, 107, 0.2); }
	.outbound-list {
		list-style: none; padding: 0; margin: 8px 0;
		display: flex; flex-wrap: wrap; gap: 6px;
	}
	.badge {
		display: inline-block;
		padding: 2px 6px;
		border-radius: 3px;
		background: rgba(218, 119, 86, 0.12);
		color: #da7756;
		font-size: 11px;
		font-family: ui-monospace, monospace;
	}
	.muted { color: var(--text-tertiary, #6e6e6e); }
	.small { font-size: 11px; }
	.actions {
		display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px;
	}
	.actions button {
		padding: 6px 14px; border-radius: 4px; cursor: pointer;
		background: var(--surface-1, #1f2425);
		color: var(--text-primary, #e8e6e3);
		border: 1px solid var(--border-1, #2c3134);
		font-size: 13px;
	}
	.actions button.primary {
		background: #da7756;
		border-color: #da7756;
		color: white;
	}
	.actions button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
