<script lang="ts">
	import type { PreflightStatus } from './PresetsPreflightBanner.svelte';

	interface Props {
		selectedCount: number;
		preflightStatus: PreflightStatus;
		onApply: () => void;
		onClear: () => void;
	}
	let { selectedCount, preflightStatus, onApply, onClear }: Props = $props();

	const applyDisabled = $derived(preflightStatus !== 'ok');
	const disabledTooltip = 'Сначала настройте политику доступа и привяжите устройства';
</script>

{#if selectedCount > 0}
	<div class="bar" role="region" aria-label="Применить выбранные пресеты">
		<span class="count">
			<strong>{selectedCount}</strong>
			<span class="count-label">выбрано</span>
		</span>
		<button
			type="button"
			class="btn-apply"
			disabled={applyDisabled}
			title={applyDisabled ? disabledTooltip : ''}
			onclick={onApply}
		>
			Применить
		</button>
		<button
			type="button"
			class="btn-clear"
			aria-label="Отменить выбор"
			onclick={onClear}
		>✕</button>
	</div>
{/if}

<style>
	.bar {
		position: sticky;
		bottom: 0;
		z-index: 10;
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.55rem 0.85rem;
		margin: 0.75rem -0.5rem -0.5rem;
		background: var(--color-bg-secondary, #1a1f23);
		border-top: 2px solid var(--color-accent, #3b82f6);
		font-size: 0.85rem;
	}
	.count {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		color: var(--color-accent, #6cb6ff);
	}
	.count strong {
		font-weight: 600;
	}
	.count-label {
		color: var(--color-text-muted);
	}
	.btn-apply {
		margin-left: auto;
		background: var(--color-accent, #3b82f6);
		color: white;
		border: 0;
		padding: 0.4rem 1rem;
		border-radius: 4px;
		font: inherit;
		font-size: 0.85rem;
		cursor: pointer;
	}
	.btn-apply:disabled {
		background: rgba(96, 96, 96, 0.5);
		color: var(--color-text-muted);
		cursor: not-allowed;
	}
	.btn-clear {
		background: transparent;
		color: var(--color-text-muted);
		border: 0;
		font-size: 1rem;
		cursor: pointer;
		padding: 0.2rem 0.45rem;
	}
	.btn-clear:hover {
		color: var(--color-text-primary);
	}
	@media (max-width: 760px) {
		.count-label {
			display: none;
		}
		.bar {
			padding: 0.5rem 0.6rem;
		}
	}
</style>
