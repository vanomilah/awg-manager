<script lang="ts">
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { Button, ConfirmModal } from '$lib/components/ui';
	import { Trash2 } from 'lucide-svelte';
	import CreateIcon from '$lib/components/ui/icons/CreateIcon.svelte';
	import type { SingboxRouterDNSRewrite } from '$lib/types';
	import DNSRewriteEditModal from './DNSRewriteEditModal.svelte';

	interface Props {
		rewrites: SingboxRouterDNSRewrite[];
		onChange: () => Promise<void> | void;
		/** Показывать встроенный заголовок (счётчик + кнопка «Добавить»). */
		showHeader?: boolean;
		/** Режим добавления — bindable, чтобы триггерить add из родителя. */
		addMode?: boolean;
	}
	let { rewrites, onChange, showHeader = true, addMode = $bindable(false) }: Props = $props();

	let editIndex = $state<number | null>(null);
	let deleteIndex = $state<number | null>(null);
	let deleteBusy = $state(false);

	function requestDelete(i: number): void {
		deleteIndex = i;
	}

	async function confirmDelete(): Promise<void> {
		if (deleteIndex === null) return;
		deleteBusy = true;
		try {
			await api.singboxRouterDeleteDNSRewrite(deleteIndex);
			deleteIndex = null;
			await onChange();
		} catch (e) {
			notifications.error((e as Error).message);
		} finally {
			deleteBusy = false;
		}
	}
</script>

{#snippet createIcon()}
	<CreateIcon />
{/snippet}

{#if showHeader}
	<div class="header">
		<div class="hint">{rewrites.length} перезаписей</div>
		<Button variant="primary" size="sm" onclick={() => (addMode = true)} iconBefore={createIcon}>
			Добавить
		</Button>
	</div>
{/if}

{#if rewrites.length === 0}
	<div class="empty-mild">
		Перезаписей нет. «Перезапись» возвращает заданный IP для домена/паттерна.
	</div>
{:else}
	<div class="col-header">
		<div>Шаблон</div>
		<div></div>
		<div>IP-адреса</div>
		<div></div>
		<div></div>
	</div>
	<div class="rows">
		{#each rewrites as rw, i (i)}
			<div class="row">
				<code class="pat mono" title={rw.pattern}>{rw.pattern}</code>
				<span class="arrow">→</span>
				<span class="ips mono" title={rw.ips.join(', ')}>{rw.ips.join(', ')}</span>
				<button
					type="button"
					class="route-action-btn"
					onclick={() => (editIndex = i)}
					aria-label={`Редактировать DNS-перезапись ${rw.pattern}`}
					title={`Редактировать DNS-перезапись «${rw.pattern}»`}
				>
					✎
				</button>
				<button
					type="button"
					class="route-action-btn danger"
					onclick={() => requestDelete(i)}
					aria-label={`Удалить DNS-перезапись ${rw.pattern}`}
					title={`Удалить DNS-перезапись «${rw.pattern}»`}
				>
					<Trash2 size={13} />
				</button>
			</div>
		{/each}
	</div>
{/if}

{#if addMode}
	<DNSRewriteEditModal
		onClose={() => (addMode = false)}
		onSave={async (rw) => {
			await api.singboxRouterAddDNSRewrite(rw);
			addMode = false;
			await onChange();
		}}
	/>
{/if}

{#if editIndex !== null}
	{@const idx = editIndex}
	<DNSRewriteEditModal
		rewrite={rewrites[idx]}
		onClose={() => (editIndex = null)}
		onSave={async (rw) => {
			await api.singboxRouterUpdateDNSRewrite(idx, rw);
			editIndex = null;
			await onChange();
		}}
	/>
{/if}

<ConfirmModal
	open={deleteIndex !== null}
	title="Удалить перезапись"
	message={deleteIndex !== null ? `Удалить перезапись «${rewrites[deleteIndex]?.pattern ?? ''}»?` : ''}
	busy={deleteBusy}
	onConfirm={confirmDelete}
	onClose={() => { if (!deleteBusy) deleteIndex = null; }}
/>

<style>
	.header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.75rem;
	}
	.hint {
		color: var(--muted-text);
		font-size: 0.85rem;
	}
	.empty-mild {
		padding: 0.6rem 0.9rem;
		background: var(--surface-bg);
		border-radius: 4px;
		color: var(--muted-text);
		font-size: 0.85rem;
	}
	.col-header {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 40px;
		gap: 0.5rem;
		padding: 0.25rem 0.75rem;
		font-size: 0.65rem;
		letter-spacing: 0.5px;
		text-transform: uppercase;
		color: var(--muted-text);
	}
	.rows {
		display: grid;
		gap: 0.2rem;
		min-width: 0;
	}
	.col-header,
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1.35fr) 16px minmax(0, 1fr) 36px 36px;
		gap: 0.4rem;
		align-items: center;
		min-width: 0;
	}
	.row {
		background: var(--surface-bg);
		padding: 0.5rem 0.75rem;
		border-radius: 4px;
		--route-action-color: var(--accent);
		overflow: hidden;
	}

	.mono {
		font-family: ui-monospace, monospace;
		font-size: 0.8rem;
	}
	.pat {
		color: var(--text);
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ips {
		color: var(--success, #22c55e);
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.route-action-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		min-width: 32px;
		height: 18px;
		padding: 0;
		border-radius: 9px;
		border: 1px solid color-mix(in srgb, var(--route-action-color, var(--accent)) 50%, transparent);
		background: color-mix(in srgb, var(--route-action-color, var(--accent)) 8%, transparent);
		color: color-mix(in srgb, var(--route-action-color, var(--accent)) 58%, transparent);
		box-shadow: 0 0 8px color-mix(in srgb, var(--route-action-color, var(--accent)) 18%, transparent);
		cursor: pointer;
		font-size: 0.9rem;
		justify-self: center;
		transition:
			color 0.16s ease,
			border-color 0.16s ease,
			background 0.16s ease,
			box-shadow 0.16s ease,
			transform 0.12s ease;
	}

	.route-action-btn :global(svg) {
		width: 13px;
		height: 13px;
		flex-shrink: 0;
	}

	.route-action-btn:hover:not(:disabled) {
		color: var(--route-action-color, var(--accent));
		border-color: color-mix(in srgb, var(--route-action-color, var(--accent)) 80%, transparent);
		background: color-mix(in srgb, var(--route-action-color, var(--accent)) 16%, transparent);
		box-shadow: 0 0 10px color-mix(in srgb, var(--route-action-color, var(--accent)) 34%, transparent);
	}

	.route-action-btn.danger:hover:not(:disabled) {
		color: var(--danger, #dc2626);
		border-color: color-mix(in srgb, var(--danger, #dc2626) 80%, transparent);
		background: color-mix(in srgb, var(--danger, #dc2626) 14%, transparent);
		box-shadow: 0 0 10px color-mix(in srgb, var(--danger, #dc2626) 30%, transparent);
	}

	.route-action-btn:active:not(:disabled) {
		transform: translateY(1px);
	}

	.route-action-btn:focus-visible {
		outline: 1px solid color-mix(in srgb, var(--route-action-color, var(--accent)) 90%, transparent);
		outline-offset: 2px;
	}

	.route-action-btn:disabled {
		opacity: 0.35;
		cursor: not-allowed;
		box-shadow: none;
	}

	@media (max-width: 720px) {
		.col-header {
			display: none;
		}

		.row {
			grid-template-columns: minmax(0, 1fr) 40px;
			grid-template-areas:
				'pattern edit'
				'ips delete';
			gap: 0.5rem 0.625rem;
			padding: 0.75rem 0.875rem;
			border: 1px solid var(--border);
			overflow: hidden;
		}

		.pat { grid-area: pattern; }
		.ips { grid-area: ips; }
		.arrow { display: none; }
		.route-action-btn:first-of-type { grid-area: edit; }
		.route-action-btn.danger { grid-area: delete; }
		.route-action-btn {
			justify-self: end;
			align-self: center;
		}
	}
</style>
