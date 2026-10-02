<script lang="ts">
	import type { AccessPolicy, PolicyDevice, PolicyGlobalInterface } from '$lib/types';
	import { isHydraRouteAccessPolicy } from '$lib/utils/accessPolicy';
	import { getDeviceTypeInfo } from '$lib/utils/device-icon';
	import { Badge, Button } from '$lib/components/ui';
	import PolicyIcon from './PolicyIcon.svelte';
	import {
		Search,
		X,
		SquarePen,
		Plus,
		Check,
		Radio,
		Layers,
		CheckCircle2,
		Copy,
		Globe,
	} from 'lucide-svelte';
	import { notifications } from '$lib/stores/notifications';

	interface Props {
		accessPolicies: AccessPolicy[];
		policyDevices: PolicyDevice[];
		policyInterfaces?: PolicyGlobalInterface[];
		onassign: (mac: string, policy: string) => Promise<void>;
		onunassign: (mac: string) => Promise<void>;
		onbatchassign?: (macs: string[], policy: string) => Promise<void>;
		oncreatepolicy?: () => void;
		oneditpolicy?: (policyName: string) => void;
	}

	let {
		accessPolicies,
		policyDevices,
		policyInterfaces = [],
		onassign,
		onunassign,
		onbatchassign,
		oncreatepolicy,
		oneditpolicy,
	}: Props = $props();

	let search = $state('');
	let filter = $state<'all' | 'online' | 'custom'>('all');
	let selectedMacs = $state<Set<string>>(new Set());
	let pendingMacs = $state<Set<string>>(new Set());
	let bulkTargetPolicy = $state<string>('default');
	let bulkSubmitting = $state(false);
	let copiedIp = $state<string | null>(null);

	// Standard policies only (HydraRoute cannot have host assignments via NDMS)
	let standardPolicies = $derived(
		accessPolicies.filter((p) => !isHydraRouteAccessPolicy(p))
	);

	function isOnline(d: PolicyDevice): boolean {
		return d.active && d.link === 'up';
	}

	function hasCustomPolicy(d: PolicyDevice): boolean {
		return d.policy !== '' && d.policy !== 'default';
	}

	let filteredDevices = $derived.by(() => {
		let list = policyDevices;
		if (filter === 'online') {
			list = list.filter(isOnline);
		} else if (filter === 'custom') {
			list = list.filter(hasCustomPolicy);
		}

		if (!search.trim()) return list;
		const q = search.trim().toLowerCase();
		return list.filter((d) => {
			const name = (d.name || '').toLowerCase();
			const host = (d.hostname || '').toLowerCase();
			const ip = (d.ip || '').toLowerCase();
			const mac = (d.mac || '').toLowerCase();
			return name.includes(q) || host.includes(q) || ip.includes(q) || mac.includes(q);
		});
	});

	let onlineCount = $derived(policyDevices.filter(isOnline).length);
	let customPolicyCount = $derived(policyDevices.filter(hasCustomPolicy).length);

	function toggleSelect(mac: string) {
		const next = new Set(selectedMacs);
		if (next.has(mac)) {
			next.delete(mac);
		} else {
			next.add(mac);
		}
		selectedMacs = next;
	}

	function toggleSelectAll() {
		if (selectedMacs.size === filteredDevices.length && filteredDevices.length > 0) {
			selectedMacs = new Set();
		} else {
			selectedMacs = new Set(filteredDevices.map((d) => d.mac));
		}
	}

	async function handleSetPolicy(device: PolicyDevice, targetPolicy: string) {
		const current = device.policy;
		if (current === targetPolicy) return;
		if (targetPolicy === 'default' && (current === '' || current === 'default')) return;

		const nextPending = new Set(pendingMacs);
		nextPending.add(device.mac);
		pendingMacs = nextPending;

		try {
			if (targetPolicy === 'default' || targetPolicy === '') {
				await onunassign(device.mac);
				notifications.success(`${device.name || device.hostname || device.mac} переключен на «По умолчанию»`);
			} else {
				await onassign(device.mac, targetPolicy);
				const pol = standardPolicies.find((p) => p.name === targetPolicy);
				const polLabel = pol?.description || targetPolicy;
				notifications.success(`${device.name || device.hostname || device.mac} переключен на «${polLabel}»`);
			}
		} catch (e) {
			notifications.error(`Ошибка переключения: ${(e as Error).message}`);
		} finally {
			const donePending = new Set(pendingMacs);
			donePending.delete(device.mac);
			pendingMacs = donePending;
		}
	}

	async function handleBulkApply() {
		if (selectedMacs.size === 0 || bulkSubmitting) return;
		bulkSubmitting = true;

		const macs = Array.from(selectedMacs);
		try {
			if (onbatchassign) {
				await onbatchassign(macs, bulkTargetPolicy);
			} else {
				// Fallback to sequential calls if batch handler is omitted
				for (const mac of macs) {
					if (bulkTargetPolicy === 'default' || bulkTargetPolicy === '') {
						await onunassign(mac);
					} else {
						await onassign(mac, bulkTargetPolicy);
					}
				}
			}
			const polLabel =
				bulkTargetPolicy === 'default'
					? 'По умолчанию'
					: standardPolicies.find((p) => p.name === bulkTargetPolicy)?.description || bulkTargetPolicy;
			notifications.success(`${macs.length} устройств переведено в политику «${polLabel}»`);
			selectedMacs = new Set();
		} catch (e) {
			notifications.error(`Ошибка группового назначения: ${(e as Error).message}`);
		} finally {
			bulkSubmitting = false;
		}
	}

	function copyIp(ip: string) {
		if (!ip || ip === '0.0.0.0') return;
		navigator.clipboard.writeText(ip);
		copiedIp = ip;
		setTimeout(() => {
			if (copiedIp === ip) copiedIp = null;
		}, 1800);
	}
</script>

<div class="device-matrix-wrapper">
	<!-- Toolbar: Search, quick filters, create policy -->
	<div class="matrix-toolbar">
		<div class="search-box">
			<Search size={16} class="search-icon" />
			<input
				type="text"
				placeholder="Поиск по названию, хосту, IP или MAC..."
				bind:value={search}
				class="search-input"
			/>
			{#if search}
				<button type="button" class="clear-btn" onclick={() => (search = '')} title="Очистить">
					<X size={14} />
				</button>
			{/if}
		</div>

		<div class="filter-group">
			<button
				type="button"
				class="filter-pill"
				class:active={filter === 'all'}
				onclick={() => (filter = 'all')}
			>
				Все <span class="pill-count">{policyDevices.length}</span>
			</button>
			<button
				type="button"
				class="filter-pill"
				class:active={filter === 'online'}
				onclick={() => (filter = 'online')}
			>
				Онлайн <span class="pill-count pill-count--online">{onlineCount}</span>
			</button>
			<button
				type="button"
				class="filter-pill"
				class:active={filter === 'custom'}
				onclick={() => (filter = 'custom')}
			>
				С политикой <span class="pill-count">{customPolicyCount}</span>
			</button>
		</div>

		{#if oncreatepolicy}
			<Button variant="secondary" size="sm" onclick={oncreatepolicy}>
				<Plus size={14} /> Новая политика
			</Button>
		{/if}
	</div>

	<!-- Empty state -->
	{#if filteredDevices.length === 0}
		<div class="matrix-empty">
			{#if policyDevices.length === 0}
				<Layers size={36} class="empty-icon" />
				<h3>Устройства не обнаружены</h3>
				<p>Подключите устройства к роутеру по Wi-Fi или проводу, чтобы настроить маршруты.</p>
			{:else}
				<Search size={36} class="empty-icon" />
				<h3>Ничего не найдено</h3>
				<p>По фильтрам и поисковому запросу нет подходящих устройств.</p>
				<Button variant="ghost" size="sm" onclick={() => { search = ''; filter = 'all'; }}>
					Сбросить фильтры
				</Button>
			{/if}
		</div>
	{:else}
		<!-- Desktop / Tablet Matrix Table -->
		<div class="matrix-table-container">
			<table class="matrix-table">
				<thead>
					<tr>
						<!-- Device column header -->
						<th class="col-device">
							<div class="th-device-content">
								<input
									type="checkbox"
									class="matrix-checkbox"
									checked={selectedMacs.size === filteredDevices.length && filteredDevices.length > 0}
									indeterminate={selectedMacs.size > 0 && selectedMacs.size < filteredDevices.length}
									onchange={toggleSelectAll}
									title="Выбрать все"
								/>
								<span>Устройство ({filteredDevices.length})</span>
							</div>
						</th>

						<!-- Default Policy column header -->
						<th class="col-policy col-policy--default">
							<div class="policy-th-header">
								<div class="policy-th-title">
									<Globe size={16} class="default-icon" />
									<span class="policy-th-name">Основная</span>
								</div>
								<span class="policy-th-sub">Провайдер (прямой)</span>
							</div>
						</th>

						<!-- Custom Policies column headers -->
						{#each standardPolicies as policy}
							<th class="col-policy">
								<div class="policy-th-header">
									<div class="policy-th-title">
										<PolicyIcon
											label={policy.description}
											policyName={policy.name}
											isHydraRoute={false}
										/>
										<span class="policy-th-name" title={policy.description || policy.name}>
											{policy.description || policy.name}
										</span>
										{#if oneditpolicy}
											<button
												type="button"
												class="policy-edit-btn"
												title={`Настроить политику «${policy.description || policy.name}»`}
												onclick={() => oneditpolicy(policy.name)}
											>
												<SquarePen size={12} />
											</button>
										{/if}
									</div>

									{#if policy.interfaces?.length}
										<div class="policy-iface-badges">
											{#each policy.interfaces.slice(0, 2) as iface}
												<Badge variant="muted" size="xs">
													{iface.label || iface.name}
												</Badge>
											{/each}
											{#if policy.interfaces.length > 2}
												<span class="more-ifaces" title={policy.interfaces.map(i => i.label || i.name).join(', ')}>
													+{policy.interfaces.length - 2}
												</span>
											{/if}
										</div>
									{:else}
										<span class="policy-th-sub text-warning">Нет выходов</span>
									{/if}
								</div>
							</th>
						{/each}
					</tr>
				</thead>

				<tbody>
					{#each filteredDevices as device (device.mac)}
						{@const online = isOnline(device)}
						{@const isPending = pendingMacs.has(device.mac)}
						{@const isSelected = selectedMacs.has(device.mac)}
						{@const typeInfo = getDeviceTypeInfo(device)}
						{@const DeviceIcon = typeInfo.icon}
						{@const currentPol = device.policy || 'default'}
						{@const isDefaultActive = currentPol === '' || currentPol === 'default'}

						<tr class="device-row" class:device-row--selected={isSelected}>
							<!-- Device info cell -->
							<td class="col-device">
								<div class="device-cell">
									<input
										type="checkbox"
										class="matrix-checkbox"
										checked={isSelected}
										onchange={() => toggleSelect(device.mac)}
									/>

									<div class="device-avatar-wrap">
										<div class="device-avatar" title={typeInfo.label}>
											<DeviceIcon size={18} />
										</div>
										<span
											class="device-led"
											class:device-led--online={online}
											class:device-led--offline={!online}
											title={online ? 'В сети' : 'Офлайн'}
										></span>
									</div>

									<div class="device-details">
										<div class="device-title-row">
											<span class="device-name" title={device.name || device.hostname || device.mac}>
												{device.name || device.hostname || device.mac}
											</span>
											{#if device.link}
												<span class="device-link-tag">{device.link}</span>
											{/if}
										</div>

										<div class="device-meta-row">
											{#if device.ip && device.ip !== '0.0.0.0'}
												<button
													type="button"
													class="ip-chip"
													title="Нажмите, чтобы скопировать IP"
													onclick={() => copyIp(device.ip)}
												>
													<code>{device.ip}</code>
													{#if copiedIp === device.ip}
														<Check size={11} class="text-green" />
													{:else}
														<Copy size={11} class="ip-copy-icon" />
													{/if}
												</button>
											{:else}
												<span class="device-no-ip">IP не назначен</span>
											{/if}
											<span class="device-mac" title="MAC-адрес">{device.mac}</span>
										</div>
									</div>
								</div>
							</td>

							<!-- Default policy cell -->
							<td
								class="col-policy-cell"
								class:active-cell={isDefaultActive}
								onclick={() => !isPending && handleSetPolicy(device, 'default')}
							>
								<div class="radio-wrap">
									<input
										type="radio"
										name={`policy-${device.mac}`}
										checked={isDefaultActive}
										disabled={isPending}
										class="matrix-radio"
										onchange={() => handleSetPolicy(device, 'default')}
									/>
									{#if isPending && isDefaultActive}
										<div class="cell-spinner"></div>
									{/if}
								</div>
							</td>

							<!-- Custom policy cells -->
							{#each standardPolicies as policy (policy.name)}
								{@const isActive = currentPol === policy.name}
								<td
									class="col-policy-cell"
									class:active-cell={isActive}
									onclick={() => !isPending && handleSetPolicy(device, policy.name)}
								>
									<div class="radio-wrap">
										<input
											type="radio"
											name={`policy-${device.mac}`}
											checked={isActive}
											disabled={isPending}
											class="matrix-radio"
											onchange={() => handleSetPolicy(device, policy.name)}
										/>
										{#if isPending && isActive}
											<div class="cell-spinner"></div>
										{/if}
									</div>
								</td>
							{/each}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}

	<!-- Floating Bulk Action Bar -->
	{#if selectedMacs.size > 0}
		<div class="bulk-floating-bar">
			<div class="bulk-info">
				<CheckCircle2 size={18} class="text-accent" />
				<span class="bulk-text">Выбрано устройств: <strong>{selectedMacs.size}</strong></span>
			</div>

			<div class="bulk-controls">
				<label class="bulk-label" for="bulk-policy-select">Назначить:</label>
				<select
					id="bulk-policy-select"
					bind:value={bulkTargetPolicy}
					class="bulk-select"
					disabled={bulkSubmitting}
				>
					<option value="default">Основная (Провайдер без VPN)</option>
					{#each standardPolicies as policy}
						<option value={policy.name}>
							{policy.description || policy.name}
						</option>
					{/each}
				</select>

				<Button
					variant="primary"
					size="sm"
					onclick={handleBulkApply}
					disabled={bulkSubmitting}
					loading={bulkSubmitting}
				>
					Применить
				</Button>

				<Button
					variant="ghost"
					size="sm"
					onclick={() => (selectedMacs = new Set())}
					disabled={bulkSubmitting}
				>
					Снять
				</Button>
			</div>
		</div>
	{/if}
</div>

<style>
	.device-matrix-wrapper {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		gap: 12px;
		position: relative;
	}

	/* Toolbar */
	.matrix-toolbar {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
		padding: 4px 0;
	}

	.search-box {
		position: relative;
		flex: 1 1 240px;
		min-width: 200px;
	}

	.search-box :global(.search-icon) {
		position: absolute;
		left: 10px;
		top: 50%;
		transform: translateY(-50%);
		color: var(--text-muted);
		pointer-events: none;
	}

	.search-input {
		width: 100%;
		height: 34px;
		padding: 0 30px 0 32px;
		background: var(--bg-secondary);
		border: 1px solid var(--border-primary);
		border-radius: var(--radius-sm, 6px);
		color: var(--text-primary);
		font-size: 0.8125rem;
		outline: none;
		transition: border-color 0.15s ease;
	}

	.search-input:focus {
		border-color: var(--accent);
	}

	.clear-btn {
		position: absolute;
		right: 8px;
		top: 50%;
		transform: translateY(-50%);
		background: none;
		border: none;
		color: var(--text-muted);
		cursor: pointer;
		padding: 2px;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.clear-btn:hover {
		color: var(--text-primary);
	}

	.filter-group {
		display: flex;
		gap: 4px;
		background: var(--bg-secondary);
		padding: 2px;
		border: 1px solid var(--border-primary);
		border-radius: var(--radius-sm, 6px);
	}

	.filter-pill {
		display: flex;
		align-items: center;
		gap: 6px;
		height: 28px;
		padding: 0 10px;
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--text-secondary);
		background: none;
		border: none;
		border-radius: var(--radius-xs, 4px);
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.filter-pill:hover {
		color: var(--text-primary);
		background: var(--bg-hover, rgba(255, 255, 255, 0.05));
	}

	.filter-pill.active {
		color: var(--text-primary);
		background: var(--bg-tertiary);
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
	}

	.pill-count {
		font-size: 0.6875rem;
		padding: 1px 5px;
		border-radius: 999px;
		background: var(--bg-primary);
		color: var(--text-muted);
	}

	.pill-count--online {
		color: #22c55e;
	}

	/* Matrix Table Container */
	.matrix-table-container {
		flex: 1;
		min-height: 0;
		overflow: auto;
		border: 1px solid var(--border-primary);
		border-radius: var(--radius-md, 8px);
		background: var(--bg-secondary);
	}

	.matrix-table {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		text-align: left;
		font-size: 0.8125rem;
	}

	/* Table Header */
	.matrix-table thead th {
		position: sticky;
		top: 0;
		z-index: 10;
		background: var(--bg-tertiary);
		border-bottom: 1px solid var(--border-primary);
		padding: 10px 14px;
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
	}

	.col-device {
		min-width: 250px;
		width: 35%;
		position: sticky;
		left: 0;
		z-index: 11;
		background: inherit;
	}

	.matrix-table thead th.col-device {
		z-index: 20;
		background: var(--bg-tertiary);
	}

	.th-device-content {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.col-policy {
		min-width: 140px;
		text-align: center;
		border-left: 1px solid var(--border-subtle, rgba(255, 255, 255, 0.05));
	}

	.col-policy--default {
		background: rgba(var(--accent-rgb, 59, 130, 246), 0.03);
	}

	.policy-th-header {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 3px;
	}

	.policy-th-title {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
	}

	.policy-th-name {
		font-size: 0.8125rem;
		font-weight: 600;
		max-width: 140px;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	:global(.default-icon) {
		color: var(--accent);
	}

	.policy-th-sub {
		font-size: 0.6875rem;
		font-weight: 400;
		color: var(--text-muted);
	}

	.policy-edit-btn {
		background: none;
		border: none;
		color: var(--text-muted);
		cursor: pointer;
		padding: 2px;
		border-radius: 3px;
		display: flex;
		align-items: center;
	}

	.policy-edit-btn:hover {
		color: var(--accent);
		background: var(--bg-hover, rgba(255, 255, 255, 0.08));
	}

	.policy-iface-badges {
		display: flex;
		align-items: center;
		gap: 4px;
		margin-top: 2px;
	}

	.more-ifaces {
		font-size: 0.6875rem;
		color: var(--text-muted);
		cursor: help;
	}

	/* Table Body Rows */
	.device-row {
		border-bottom: 1px solid var(--border-subtle, rgba(255, 255, 255, 0.04));
		transition: background 0.1s ease;
	}

	.device-row:hover {
		background: var(--bg-hover, rgba(255, 255, 255, 0.03));
	}

	.device-row--selected {
		background: rgba(var(--accent-rgb, 59, 130, 246), 0.06);
	}

	.matrix-table tbody td {
		padding: 8px 12px;
		vertical-align: middle;
	}

	.matrix-table tbody td.col-device {
		background: var(--bg-secondary);
	}

	.device-row:hover td.col-device {
		background: var(--bg-hover, var(--bg-secondary));
	}

	.device-row--selected td.col-device {
		background: rgba(var(--accent-rgb, 59, 130, 246), 0.08);
	}

	/* Device Cell */
	.device-cell {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.matrix-checkbox {
		cursor: pointer;
		accent-color: var(--accent);
		width: 15px;
		height: 15px;
		border-radius: 3px;
	}

	.device-avatar-wrap {
		position: relative;
		flex-shrink: 0;
	}

	.device-avatar {
		width: 32px;
		height: 32px;
		border-radius: 8px;
		background: var(--bg-tertiary);
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--text-secondary);
	}

	.device-led {
		position: absolute;
		bottom: -1px;
		right: -1px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		border: 1.5px solid var(--bg-secondary);
	}

	.device-led--online {
		background: #22c55e;
		box-shadow: 0 0 5px rgba(34, 197, 94, 0.6);
	}

	.device-led--offline {
		background: #9ca3af;
		opacity: 0.6;
	}

	.device-details {
		display: flex;
		flex-direction: column;
		min-width: 0;
		gap: 2px;
	}

	.device-title-row {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.device-name {
		font-weight: 500;
		color: var(--text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.device-link-tag {
		font-size: 0.625rem;
		padding: 1px 4px;
		background: var(--bg-tertiary);
		color: var(--text-muted);
		border-radius: 3px;
		text-transform: uppercase;
		letter-spacing: 0.02em;
	}

	.device-meta-row {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	.ip-chip {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		background: none;
		border: none;
		padding: 0;
		color: var(--text-secondary);
		cursor: pointer;
		font-family: inherit;
		border-radius: 2px;
	}

	.ip-chip:hover {
		color: var(--accent);
	}

	:global(.ip-copy-icon) {
		opacity: 0;
		transition: opacity 0.1s ease;
	}

	.ip-chip:hover :global(.ip-copy-icon) {
		opacity: 1;
	}

	.device-mac {
		font-family: monospace;
		opacity: 0.7;
	}

	.device-no-ip {
		font-style: italic;
		color: var(--text-muted);
	}

	/* Policy Cells & Radio buttons */
	.col-policy-cell {
		text-align: center;
		cursor: pointer;
		border-left: 1px solid var(--border-subtle, rgba(255, 255, 255, 0.04));
		transition: background 0.12s ease;
	}

	.col-policy-cell:hover {
		background: rgba(var(--accent-rgb, 59, 130, 246), 0.08);
	}

	.col-policy-cell.active-cell {
		background: rgba(var(--accent-rgb, 59, 130, 246), 0.1);
	}

	.radio-wrap {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		position: relative;
		width: 24px;
		height: 24px;
	}

	.matrix-radio {
		cursor: pointer;
		width: 17px;
		height: 17px;
		accent-color: var(--accent);
		margin: 0;
	}

	.cell-spinner {
		position: absolute;
		width: 16px;
		height: 16px;
		border: 2px solid transparent;
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	/* Floating Bulk Action Bar */
	.bulk-floating-bar {
		position: absolute;
		bottom: 16px;
		left: 50%;
		transform: translateX(-50%);
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		padding: 8px 16px;
		background: var(--bg-tertiary);
		border: 1px solid var(--border-primary);
		border-radius: 30px;
		box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3), 0 4px 6px -2px rgba(0, 0, 0, 0.1);
		z-index: 50;
		animation: slideUp 0.15s ease-out;
	}

	@keyframes slideUp {
		from {
			opacity: 0;
			transform: translate(-50%, 15px);
		}
		to {
			opacity: 1;
			transform: translate(-50%, 0);
		}
	}

	.bulk-info {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 0.8125rem;
		white-space: nowrap;
	}

	.bulk-controls {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.bulk-label {
		font-size: 0.75rem;
		color: var(--text-muted);
	}

	.bulk-select {
		height: 30px;
		padding: 0 8px;
		background: var(--bg-secondary);
		border: 1px solid var(--border-primary);
		border-radius: var(--radius-xs, 4px);
		color: var(--text-primary);
		font-size: 0.8125rem;
		outline: none;
	}

	/* Empty state */
	.matrix-empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: 48px 16px;
		text-align: center;
		gap: 10px;
		background: var(--bg-secondary);
		border: 1px solid var(--border-primary);
		border-radius: var(--radius-md, 8px);
	}

	:global(.empty-icon) {
		color: var(--text-muted);
		opacity: 0.6;
	}

	.matrix-empty h3 {
		font-size: 1rem;
		font-weight: 600;
		color: var(--text-primary);
		margin: 0;
	}

	.matrix-empty p {
		font-size: 0.8125rem;
		color: var(--text-muted);
		max-width: 400px;
		margin: 0;
	}

	:global(.text-green) {
		color: #22c55e;
	}

	.text-warning {
		color: #eab308;
	}

	:global(.text-accent) {
		color: var(--accent);
	}

	@media (max-width: 640px) {
		.bulk-floating-bar {
			width: calc(100% - 32px);
			flex-direction: column;
			border-radius: 12px;
			gap: 10px;
			bottom: 8px;
		}

		.bulk-controls {
			width: 100%;
			justify-content: space-between;
		}

		.bulk-select {
			flex: 1;
			min-width: 0;
		}
	}
</style>
