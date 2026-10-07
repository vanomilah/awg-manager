<script lang="ts">
	import { onMount } from 'svelte';
	import type { AccessPolicy, PolicyDevice, PolicyGlobalInterface } from '$lib/types';
	import { api } from '$lib/api/client';
	import { errorMessage } from '$lib/utils/errorMessage';
	import { notifications } from '$lib/stores/notifications';
	import { Modal, Toggle, Badge, Button } from '$lib/components/ui';
	import { X, Check } from 'lucide-svelte';
	import InterfaceList from './InterfaceList.svelte';
	import PolicyIcon from './PolicyIcon.svelte';
	import { isHydraRouteAccessPolicy, isDeviceOnline } from '$lib/utils/accessPolicy';

	interface Props {
		open: boolean;
		policy: AccessPolicy;
		devices: PolicyDevice[];
		globalInterfaces: PolicyGlobalInterface[];
		onclose: () => void;
		onupdate: () => Promise<void>;
		ondeviceunassigned?: (mac: string, fromPolicy: string) => void;
	}

	let {
		open = $bindable(false),
		policy,
		devices,
		globalInterfaces,
		onclose,
		onupdate,
		ondeviceunassigned,
	}: Props = $props();

	onMount(() => {
		void onupdate();
	});

	let isHrPolicy = $derived(isHydraRouteAccessPolicy(policy));

	let description = $state('');
	const VALID_PATTERN = /^[a-zA-Z0-9_-]*$/;
	const MAX_LEN = 256;

	$effect(() => {
		description = policy.description ?? '';
	});

	let assignedDevices = $derived(devices.filter((d) => d.policy === policy.name));
	let descriptionValid = $derived(
		description.trim().length > 0 &&
		description.trim().length <= MAX_LEN &&
		VALID_PATTERN.test(description.trim())
	);

	const standaloneHint = $derived(
		policy.standalone
			? 'Политика действует самостоятельно, без привязки к глобальным правилам. Статические маршруты из настроек роутера не копируются (маршруты из вкладки "IP-адреса" работать не будут).'
			: 'Если standalone отключено — в политике действуют глобальные правила, статические маршруты из настроек роутера копируются.'
	);

	let savingDesc = $state(false);

	async function saveDescription() {
		if (isHrPolicy) return;
		if (description.trim() === policy.description) return;
		if (!descriptionValid) {
			notifications.error('Описание: только латинские буквы, цифры, дефисы и подчёркивания');
			description = policy.description ?? '';
			return;
		}
		savingDesc = true;
		try {
			await api.setAccessPolicyDescription(policy.name, description.trim());
			await onupdate();
			notifications.success('Описание обновлено');
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		} finally {
			savingDesc = false;
		}
	}

	function handleDescriptionKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			(e.target as HTMLElement)?.blur();
		}
	}

	async function toggleStandalone(checked: boolean) {
		if (isHrPolicy) return;
		try {
			await api.setAccessPolicyStandalone(policy.name, checked);
			await onupdate();
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		}
	}

	async function handlePermit(iface: string, order: number) {
		try {
			await api.permitPolicyInterface(policy.name, iface, order);
			await onupdate();
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		}
	}

	async function handleDeny(iface: string) {
		try {
			await api.denyPolicyInterface(policy.name, iface);
			await onupdate();
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		}
	}

	async function handleReorder(iface: string, newOrder: number) {
		try {
			await api.permitPolicyInterface(policy.name, iface, newOrder);
			await onupdate();
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		}
	}

	async function unassignDevice(mac: string) {
		try {
			await api.unassignDeviceFromPolicy(mac);
			ondeviceunassigned?.(mac, policy.name);
			await onupdate();
			notifications.success('Устройство отвязано от политики');
		} catch (e) {
			notifications.error(`Ошибка: ${errorMessage(e)}`);
		}
	}
</script>

<Modal
	{open}
	title={`Выходы политики: ${policy.description || policy.name}`}
	size="lg"
	{onclose}
>
	<div class="policy-modal-body">
		<!-- Верхний баннер политики с иконкой и статусом -->
		<div class="policy-banner">
			<div class="policy-banner-left">
				<PolicyIcon
					label={policy.description || policy.name}
					policyName={policy.name}
					isHydraRoute={isHrPolicy}
				/>
				<div class="policy-banner-meta">
					<div class="policy-banner-title-row">
						<span class="policy-banner-name">{policy.description || policy.name}</span>
						{#if isHrPolicy}
							<Badge variant="warning" uppercase size="xs" pill>HydraRoute</Badge>
						{/if}
						{#if policy.standalone}
							<Badge variant="accent" uppercase size="xs" pill>Standalone</Badge>
						{/if}
					</div>
					<span class="policy-banner-sub">
						ID: <code>{policy.name}</code> • Привязано устройств: {assignedDevices.length}
					</span>
				</div>
			</div>
		</div>

		{#if isHrPolicy}
			<div class="hr-banner">
				<p>
					Маршрутизация HydraRoute Neo распространяется только на политику по умолчанию.
					Интерфейсы настраиваются тем же способом, что и на вкладке HR Neo.
				</p>
			</div>
		{/if}

		{#if !isHrPolicy}
			<!-- Редактирование названия и standalone -->
			<div class="policy-settings-card">
				<div class="field-group">
					<label class="field-label" for="policy-desc-input">
						Описание политики
					</label>
					<div class="field-input-wrap">
						<input
							id="policy-desc-input"
							type="text"
							class="field-input"
							bind:value={description}
							onblur={saveDescription}
							onkeydown={handleDescriptionKeydown}
							disabled={savingDesc}
							maxlength={MAX_LEN}
							placeholder="Название политики..."
						/>
						{#if savingDesc}
							<span class="desc-spinner"></span>
						{/if}
					</div>
					<span class="field-hint">Латинские буквы, цифры, дефисы, подчёркивания (сохраняется при смене фокуса или Enter)</span>
				</div>

				<div class="standalone-toggle-wrap">
					<Toggle
						checked={policy.standalone}
						onchange={toggleStandalone}
						spinner="after"
						label="Standalone (автономный режим)"
						hint={standaloneHint}
					/>
				</div>
			</div>
		{/if}

		<!-- Список интерфейсов с приоритетом -->
		<div class="interfaces-card">
			<InterfaceList
				interfaces={policy.interfaces ?? []}
				availableInterfaces={globalInterfaces}
				addPickerVariant="panel"
				onpermit={handlePermit}
				ondeny={handleDeny}
				onreorder={handleReorder}
				onupdate={onupdate}
			/>
		</div>

		{#if !isHrPolicy}
			<!-- Список назначенных устройств с возможностью быстрой отвязки -->
			<div class="devices-card">
				<div class="devices-card-header">
					<h4 class="devices-card-title">Привязанные устройства ({assignedDevices.length})</h4>
					<span class="devices-card-hint">
						Назначать устройства можно прямо в таблице или сетке устройств
					</span>
				</div>

				{#if assignedDevices.length === 0}
					<div class="devices-empty">
						<span>Нет привязанных устройств</span>
					</div>
				{:else}
					<div class="device-chips-grid">
						{#each assignedDevices as device (device.mac)}
							{@const online = isDeviceOnline(device)}
							<div class="device-chip">
								<span
									class="device-chip-led"
									class:online
									class:offline={!online}
									title={online ? 'Онлайн' : 'Офлайн'}
								></span>
								<div class="device-chip-info">
									<span class="device-chip-name" title={device.name || device.hostname || device.mac}>
										{device.name || device.hostname || device.mac}
									</span>
									{#if device.ip && device.ip !== '0.0.0.0'}
										<span class="device-chip-ip">{device.ip}</span>
									{/if}
								</div>
								<button
									type="button"
									class="device-chip-remove"
									title={`Отвязать «${device.name || device.hostname || device.mac}» от политики`}
									onclick={() => unassignDevice(device.mac)}
								>
									<X size={13} />
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>

	{#snippet actions()}
		<Button variant="primary" onclick={onclose}>
			Готово
		</Button>
	{/snippet}
</Modal>

<style>
	.policy-modal-body {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.policy-banner {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 12px 14px;
		background: var(--bg-tertiary);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 8px);
	}

	.policy-banner-left {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
	}

	.policy-banner-meta {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}

	.policy-banner-title-row {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}

	.policy-banner-name {
		font-size: 0.9375rem;
		font-weight: 600;
		color: var(--text-primary);
	}

	.policy-banner-sub {
		font-size: 0.75rem;
		color: var(--text-muted);
	}

	.policy-banner-sub code {
		font-family: monospace;
		padding: 1px 4px;
		border-radius: 4px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
	}

	.hr-banner {
		padding: 10px 12px;
		border-radius: var(--radius-sm, 6px);
		background: color-mix(in srgb, var(--warning, #eab308) 12%, transparent);
		border: 1px solid color-mix(in srgb, var(--warning, #eab308) 30%, transparent);
		font-size: 0.8125rem;
		color: var(--text-primary);
	}

	.hr-banner p {
		margin: 0;
	}

	.policy-settings-card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 8px);
	}

	.field-group {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.field-label {
		font-size: 0.8125rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.field-input-wrap {
		position: relative;
		display: flex;
		align-items: center;
	}

	.field-input {
		width: 100%;
		height: 34px;
		padding: 0 10px;
		background: var(--bg-primary);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 6px);
		color: var(--text-primary);
		font-size: 0.8125rem;
		outline: none;
		transition: border-color 0.15s ease;
	}

	.field-input:focus {
		border-color: var(--accent);
	}

	.desc-spinner {
		position: absolute;
		right: 10px;
		width: 14px;
		height: 14px;
		border: 2px solid var(--border);
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 0.8s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.field-hint {
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	.standalone-toggle-wrap {
		padding-top: 8px;
		border-top: 1px solid var(--border);
	}

	.interfaces-card {
		padding: 14px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 8px);
	}

	.devices-card {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 8px);
	}

	.devices-card-header {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.devices-card-title {
		font-size: 0.8125rem;
		font-weight: 600;
		color: var(--text-primary);
		margin: 0;
	}

	.devices-card-hint {
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	.devices-empty {
		padding: 12px;
		text-align: center;
		font-size: 0.75rem;
		color: var(--text-muted);
		background: var(--bg-primary);
		border-radius: var(--radius-sm, 6px);
		border: 1px dashed var(--border);
	}

	.device-chips-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: 6px;
		max-height: 200px;
		overflow-y: auto;
		padding-right: 4px;
	}

	.device-chip {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 8px;
		background: var(--bg-primary);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 6px);
		min-width: 0;
	}

	.device-chip-led {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.device-chip-led.online {
		background: var(--color-success, #22c55e);
		box-shadow: 0 0 5px rgba(34, 197, 94, 0.8);
	}

	.device-chip-led.offline {
		background: var(--text-muted);
	}

	.device-chip-info {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
	}

	.device-chip-name {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.device-chip-ip {
		font-size: 0.6875rem;
		color: var(--text-muted);
		font-family: monospace;
	}

	.device-chip-remove {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		border: none;
		background: transparent;
		color: var(--text-muted);
		cursor: pointer;
		border-radius: 4px;
		flex-shrink: 0;
		transition: all 0.12s ease;
	}

	.device-chip-remove:hover {
		background: var(--bg-hover);
		color: var(--error, #ef4444);
	}
</style>
