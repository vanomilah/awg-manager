<script lang="ts">
	// Шаг 2 мастера «Выхода» — параметры (WE-29..WE-37). Поля правятся на месте
	// в объекте мастера; пароль есть только у WDTT-клиента, у FreeTurn его нет.
	import { Button, Dropdown, Input, Toggle } from '$lib/components/ui';
	import { Sparkles } from 'lucide-svelte';
	import SensitiveInput from '../proxy-panel/SensitiveInput.svelte';
	import { autoReconnectIntervalOptions } from '../freeturn/options';
	import type { ExitProtocol, ExitWizardFields } from './exitWizard';
	import VkCallModal from './VkCallModal.svelte';

	interface Props {
		protocol: ExitProtocol;
		/** Поля мастера правятся здесь же: владелец значения — мастер. */
		fields: ExitWizardFields;
	}

	let { protocol, fields = $bindable() }: Props = $props();
	let vkModalOpen = $state(false);
</script>

<p class="lead">Значения из ссылки — поправьте, если нужно.</p>

<div class="grid">
	<Input label="Имя" bind:value={fields.name} fullWidth />
	<Input label="Адрес сервера" bind:value={fields.peer} fullWidth />
	{#if protocol === 'wdtt'}
		<SensitiveInput label="Пароль" bind:value={fields.password} />
	{/if}
	<!-- WE-50/WE-51: поле обязательное у обоих протоколов (`exitStep2Ready`), и
	     без подписи «Дальше» гасла бы молча. Значение у них разное: у WDTT это
	     VK-хеши, у FreeTurn — ссылки VK Calls (`links`), отсюда две строки и две
	     подписи: WE-35 у WDTT и EX-59 у FreeTurn (та же, что на детали). -->
	<div class="field-with-btn">
		<Input
			label={protocol === 'wdtt' ? 'VK-хеши' : 'Ссылки VK Calls'}
			bind:value={fields.vkHashes}
			hint={protocol === 'wdtt'
				? 'Обязательно — без VK-хешей клиент не запустится'
				: 'Обязательно — без ссылок VK Calls клиент не запустится'}
			fullWidth
		/>
		<Button
			variant="secondary"
			size="sm"
			class="vk-btn"
			title="Сгенерировать или проверить ссылки VK Calls"
			onclick={() => (vkModalOpen = true)}
		>
			<Sparkles size={14} />
			VK Calls
		</Button>
	</div>
	<!-- WE-37 — про округление в wdtt-клиенте; у freeturn правила кратности нет. -->
	<Input
		label="Потоков"
		type="number"
		value={fields.workers}
		oninput={(v) => (fields.workers = v)}
		hint={protocol === 'wdtt' ? 'Клиент округлит вниз до кратного 9 (минимум 9)' : ''}
		fullWidth
	/>
	<div class="reconnect-box">
		<Toggle
			label="Автопереподключение"
			description="Перезапуск при 401 Unauthorized / сбое TURN или по интервалу"
			checked={fields.autoReconnect ?? false}
			onchange={(v) => {
				fields.autoReconnect = v;
				if (v && !fields.autoReconnectInterval) {
					fields.autoReconnectInterval = '1h';
				}
			}}
		/>
		{#if fields.autoReconnect}
			<div class="reconnect-interval">
				<Dropdown
					label="Интервал"
					bind:value={fields.autoReconnectInterval}
					options={autoReconnectIntervalOptions}
					fullWidth
				/>
			</div>
		{/if}
	</div>
</div>

{#if vkModalOpen}
	<VkCallModal
		bind:open={vkModalOpen}
		initialValue={fields.vkHashes}
		targetFormat={protocol === 'wdtt' ? 'hashes' : 'links'}
		onApply={(val) => {
			fields.vkHashes = val;
		}}
	/>
{/if}

<style>
	.field-with-btn {
		grid-column: 1 / -1;
		display: flex;
		align-items: flex-end;
		gap: 8px;
	}

	:global(.vk-btn) {
		margin-bottom: 2px;
		white-space: nowrap;
	}

	.lead {
		margin: 0 0 0.875rem;
		font-size: 0.875rem;
		color: var(--color-text-secondary);
	}

	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 0.75rem;
	}

	.reconnect-box {
		grid-column: 1 / -1;
		padding-top: 0.5rem;
	}

	.reconnect-interval {
		max-width: 240px;
		margin-top: 0.5rem;
	}
</style>
