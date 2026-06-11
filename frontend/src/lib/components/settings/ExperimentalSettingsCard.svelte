<script lang="ts">
	import SettingsSectionLabel from './SettingsSectionLabel.svelte';
	import { Badge, Button, Toggle } from '$lib/components/ui';
	import { summonPukhosos } from '$lib/stores/pukhososSummon';
	import {
		uiElementHiderEnabled,
		uiElementHiderRules,
		type HiddenElementRule,
	} from '$lib/stores/uiElementHider';
	import { PUKHOSOS_PATROL_MS } from '$lib/utils/pukhososPatrol';
	import { FlaskConical } from 'lucide-svelte';

	let summoning = $state(false);

	const hiddenRules = $derived($uiElementHiderRules);

	function handleSummon() {
		if (summoning) return;
		summoning = true;
		summonPukhosos();
		window.setTimeout(() => {
			summoning = false;
		}, PUKHOSOS_PATROL_MS);
	}

	function badgeLabel(rule: HiddenElementRule): string {
		const text = rule.label.trim() || rule.selector;
		return text.length > 42 ? `${text.slice(0, 39)}…` : text;
	}

	function badgeTitle(rule: HiddenElementRule): string {
		return `${rule.label}\n${rule.path}\n${rule.selector}`;
	}
</script>

<div class="settings-block" id="experimental-settings" data-awg-ui-protected>
	<div class="card">
		<SettingsSectionLabel label="Экспериментальное" icon={FlaskConical} tone="info" header cycleInVivid />
		<div class="setting-row">
			<div class="flex flex-col gap-1">
				<span class="font-medium">Пухосос</span>
				<span class="setting-description">
					Вызвать пухосос, он пару раз проедет по блоку ссылок и благодарностей.
				</span>
			</div>
			<Button variant="secondary" size="md" onclick={handleSummon} disabled={summoning}>
				{summoning ? 'Едет…' : 'Вызвать пухосос'}
			</Button>
		</div>
		<div class="setting-row toggle-inline-row">
			<div class="flex flex-col gap-1">
				<span class="font-medium">Скрытие элементов</span>
				<span class="setting-description">
					Кнопка с ластиком в левом нижнем углу — кликайте по блокам, которые хотите убрать.
					Удержание ластика выключает режим.
				</span>
			</div>
			<Toggle
				checked={$uiElementHiderEnabled}
				onchange={(v) => uiElementHiderEnabled.set(v)}
			/>
		</div>
		{#if hiddenRules.length > 0}
			<div class="hidden-rules-row">
				<span class="hidden-rules-label">Скрыто</span>
				<div class="hidden-rules-badges">
					{#each hiddenRules as rule (rule.id)}
						<button
							type="button"
							class="hidden-rule-badge"
							title={badgeTitle(rule)}
							aria-label={`Вернуть: ${rule.label}`}
							onclick={() => uiElementHiderRules.remove(rule.id)}
						>
							<Badge variant="muted" size="sm">{badgeLabel(rule)}</Badge>
						</button>
					{/each}
				</div>
			</div>
		{/if}
	</div>
</div>

<style>
	.hidden-rules-row {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.75rem 0 0;
		border-top: 1px solid var(--color-border);
	}

	.hidden-rules-label {
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--color-text-muted);
	}

	.hidden-rules-badges {
		display: flex;
		flex-wrap: wrap;
		gap: 0.375rem;
	}

	.hidden-rule-badge {
		border: 0;
		padding: 0;
		margin: 0;
		background: transparent;
		cursor: pointer;
		max-width: 100%;
	}

	.hidden-rule-badge:hover :global(.badge) {
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.hidden-rule-badge :global(.badge) {
		max-width: 100%;
		overflow: hidden;
		text-overflow: ellipsis;
	}
</style>
