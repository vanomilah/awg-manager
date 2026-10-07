<script lang="ts">
	import type { AccessPolicy, PolicyDevice, PolicyGlobalInterface } from '$lib/types';
	import { isHydraRouteAccessPolicy, isDeviceOnline } from '$lib/utils/accessPolicy';
	import { getDeviceTypeInfo } from '$lib/utils/device-icon';
	import { cleanPolicyInterfaceLabel } from '$lib/utils/routingTunnelOptions';
	import { Button } from '$lib/components/ui';
	import PolicyIcon from './PolicyIcon.svelte';
	import {
		Search,
		X,
		SquarePen,
		Plus,
		Check,
		Layers,
		CheckCircle2,
		Copy,
		Globe,
		ChevronDown,
		LayoutList,
		LayoutGrid,
		RotateCcw,
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

	// View mode: 'list' (clean modern device list) vs 'matrix' (compact crosshair grid)
	let viewLayout = $state<'list' | 'matrix'>(
		typeof localStorage !== 'undefined'
			? (localStorage.getItem('awgm_device_view_layout') as 'list' | 'matrix') || 'list'
			: 'list'
	);

	function setViewLayout(mode: 'list' | 'matrix') {
		viewLayout = mode;
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem('awgm_device_view_layout', mode);
		}
	}

	let search = $state('');
	let filter = $state<'all' | 'online' | 'custom'>('all');
	let selectedMacs = $state<Set<string>>(new Set());
	let pendingMacs = $state<Set<string>>(new Set());
	let bulkTargetPolicy = $state<string>('default');
	let bulkSubmitting = $state(false);
	let copiedIp = $state<string | null>(null);

	// Dropdown menu state for policy selector
	let openDropdownMac = $state<string | null>(null);
	let dropdownCoords = $state<{ top: number; left: number; width: number }>({
		top: 0,
		left: 0,
		width: 280,
	});

	// Standard policies only (HydraRoute cannot have host assignments via NDMS)
	let standardPolicies = $derived(
		accessPolicies.filter((p) => !isHydraRouteAccessPolicy(p))
	);

	function isOnline(d: PolicyDevice): boolean {
		return isDeviceOnline(d);
	}

	function hasCustomPolicy(d: PolicyDevice): boolean {
		return d.policy !== '' && d.policy !== 'default';
	}

	function formatIfaceBadge(iface: { name: string; label?: string }): string {
		const raw = iface.label || iface.name;
		const cleaned = cleanPolicyInterfaceLabel(raw);
		return cleaned || iface.name;
	}

	let filteredDevices = $derived.by(() => {
		let list = policyDevices;

		// Status filter
		if (filter === 'online') {
			list = list.filter(isOnline);
		} else if (filter === 'custom') {
			list = list.filter(hasCustomPolicy);
		}

		// Text search
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
				notifications.success(`${device.name || device.hostname || device.mac} переключен на «Основная (провайдер)»`);
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
					? 'Основная (по умолчанию)'
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

	function togglePolicyDropdown(e: MouseEvent, device: PolicyDevice) {
		e.stopPropagation();
		if (openDropdownMac === device.mac) {
			openDropdownMac = null;
			return;
		}

		const btn = e.currentTarget as HTMLElement;
		const rect = btn.getBoundingClientRect();
		const windowWidth = typeof window !== 'undefined' ? window.innerWidth : 800;
		const windowHeight = typeof window !== 'undefined' ? window.innerHeight : 800;
		const isMobile = windowWidth <= 768;
		const menuWidth = isMobile ? Math.min(320, windowWidth - 20) : Math.min(300, windowWidth - 24);
		const alignRight = rect.left + menuWidth > windowWidth - 16;
		const left = isMobile
			? 10
			: alignRight
				? Math.max(12, rect.right - menuWidth)
				: Math.max(12, rect.left);

		const spaceBelow = windowHeight - rect.bottom - 10;
		const approxHeight = Math.min(360, (standardPolicies.length + 1) * 48 + 50);
		const flipUp = spaceBelow < approxHeight && rect.top > approxHeight;
		let top = flipUp ? Math.max(10, rect.top - approxHeight - 4) : rect.bottom + 4;
		if (top + approxHeight > windowHeight - 8) {
			top = Math.max(10, windowHeight - approxHeight - 8);
		}

		dropdownCoords = { top, left, width: menuWidth };
		openDropdownMac = device.mac;
	}

	// Portal helper to mount dropdown directly to document.body and bypass table overflow
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() {
				if (node.parentNode) {
					node.parentNode.removeChild(node);
				}
			},
		};
	}

	// Dismiss dropdown on outside click or Escape
	$effect(() => {
		if (!openDropdownMac) return;

		function handleDocClick(e: MouseEvent) {
			const menuEl = document.getElementById('device-policy-dropdown-portal');
			if (menuEl && menuEl.contains(e.target as Node)) return;
			openDropdownMac = null;
		}

		function handleDocKey(e: KeyboardEvent) {
			if (e.key === 'Escape') openDropdownMac = null;
		}

		document.addEventListener('click', handleDocClick);
		document.addEventListener('keydown', handleDocKey);
		return () => {
			document.removeEventListener('click', handleDocClick);
			document.removeEventListener('keydown', handleDocKey);
		};
	});
</script>

<div class="device-matrix-wrapper">
	<!-- Toolbar: Search, quick filters, policy selector filter, view switcher -->
	<div class="matrix-toolbar">
		<div class="toolbar-left">
			<div class="search-box">
				<Search size={15} class="search-icon" />
				<input
					type="text"
					placeholder="Поиск по названию, хосту, IP или MAC..."
					bind:value={search}
					class="search-input"
				/>
				{#if search}
					<button type="button" class="clear-btn" onclick={() => (search = '')} title="Очистить">
						<X size={13} />
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
					<span class="online-lamp"></span>
					Онлайн <span class="pill-count pill-count--online">{onlineCount}</span>
				</button>
				<button
					type="button"
					class="filter-pill"
					class:active={filter === 'custom'}
					onclick={() => (filter = 'custom')}
				>
					С политикой <span class="pill-count pill-count--custom">{customPolicyCount}</span>
				</button>
			</div>
		</div>

		<div class="toolbar-right">
			<!-- Layout View Toggle: List (Option 2) vs Matrix (Option 1) -->
			<div class="layout-toggle-group">
				<button
					type="button"
					class="layout-btn"
					class:active={viewLayout === 'list'}
					onclick={() => setViewLayout('list')}
					title="Список устройств с селектором политики"
				>
					<LayoutList size={15} />
					<span class="layout-btn-label">Список</span>
				</button>
				<button
					type="button"
					class="layout-btn"
					class:active={viewLayout === 'matrix'}
					onclick={() => setViewLayout('matrix')}
					title="Матрица устройств"
				>
					<LayoutGrid size={15} />
					<span class="layout-btn-label">Сетка</span>
				</button>
			</div>
		</div>
	</div>

	<!-- Empty State -->
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
				<Button
					variant="ghost"
					size="sm"
					onclick={() => {
						search = '';
						filter = 'all';
					}}
				>
					Сбросить фильтры
				</Button>
			{/if}
		</div>
	{:else if viewLayout === 'list'}
		<!-- ─── ВАРИАНТ 2: Список устройств ─── -->

		<!-- 1. Десктопная / планшетная таблица (гармоничные компактные колонки без дыр) -->
		<div class="matrix-table-container layout-list desktop-only">
			<table class="device-list-table">
				<thead>
					<tr>
						<th class="th-checkbox">
							<input
								type="checkbox"
								class="matrix-checkbox"
								checked={selectedMacs.size === filteredDevices.length && filteredDevices.length > 0}
								indeterminate={selectedMacs.size > 0 && selectedMacs.size < filteredDevices.length}
								onchange={toggleSelectAll}
								title="Выбрать все"
							/>
						</th>
						<th class="th-device">Устройство ({filteredDevices.length})</th>
						<th class="th-network">Сеть (IP / MAC)</th>
						<th class="th-policy">Политика доступа</th>
						<th class="th-actions">Действия</th>
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
						{@const currentPolObj = standardPolicies.find((p) => p.name === currentPol)}

						<tr class="device-row" class:device-row--selected={isSelected}>
							<!-- Checkbox -->
							<td class="td-checkbox">
								<input
									type="checkbox"
									class="matrix-checkbox"
									checked={isSelected}
									onchange={() => toggleSelect(device.mac)}
								/>
							</td>

							<!-- Device Info -->
							<td class="td-device">
								<div class="device-cell">
									<!-- Индикатор активности устройства (как в режиме списка политик) -->
									<span
										class="device-led"
										class:device-led--online={online}
										class:device-led--offline={!online}
										title={online ? 'В сети (онлайн)' : 'Офлайн'}
									></span>
									<div class="device-avatar" title={typeInfo.label}>
										<DeviceIcon size={18} />
									</div>

									<div class="device-details">
										<div class="device-title-row">
											<span class="device-name" title={device.name || device.hostname || device.mac}>
												{device.name || device.hostname || device.mac}
											</span>
											{#if device.link}
												<span
													class="device-link-tag"
													class:link-up={device.link.toLowerCase() === 'up'}
													class:link-down={device.link.toLowerCase() === 'down'}
												>
													{device.link}
												</span>
											{/if}
										</div>

										{#if device.hostname && device.name && device.hostname !== device.name}
											<span class="device-hostname" title="Сетевое имя">{device.hostname}</span>
										{/if}
									</div>
								</div>
							</td>

							<!-- Network Info (IP & MAC) -->
							<td class="td-network">
								<div class="network-cell">
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
							</td>

							<!-- Access Policy Selector Button («кнопочка») -->
							<td class="td-policy">
								<div class="policy-selector-wrap">
									<button
										type="button"
										class="policy-selector-btn"
										class:policy-selector-btn--custom={!isDefaultActive}
										class:policy-selector-btn--open={openDropdownMac === device.mac}
										class:policy-selector-btn--pending={isPending}
										disabled={isPending}
										onclick={(e) => togglePolicyDropdown(e, device)}
										title="Нажмите для смены политики доступа"
									>
										<div class="policy-btn-left">
											{#if isPending}
												<div class="btn-spinner"></div>
											{:else if isDefaultActive}
												<Globe size={15} class="default-icon" />
											{:else}
												<PolicyIcon
													label={currentPolObj?.description || currentPolObj?.name || currentPol}
													policyName={currentPol}
													isHydraRoute={false}
												/>
											{/if}

											<div class="policy-btn-content">
												<span class="policy-btn-name">
													{isDefaultActive ? 'Основная' : currentPolObj?.description || currentPol}
												</span>
												{#if isDefaultActive}
													<span class="policy-btn-sub">Провайдер</span>
												{:else if currentPolObj?.interfaces?.length}
													<span class="policy-btn-iface">
														{formatIfaceBadge(currentPolObj.interfaces[0])}
														{#if currentPolObj.interfaces.length > 1}
															<span class="iface-count">+{currentPolObj.interfaces.length - 1}</span>
														{/if}
													</span>
												{/if}
											</div>
										</div>

										<span
											class="policy-chevron-wrap"
											class:chevron-rotated={openDropdownMac === device.mac}
										>
											<ChevronDown size={13} />
										</span>
									</button>
								</div>
							</td>

							<!-- Actions -->
							<td class="td-actions">
								<div class="row-actions">
									{#if !isDefaultActive}
										{#if oneditpolicy && currentPolObj}
											<button
												type="button"
												class="action-icon-btn"
												title={`Настроить выходы политики «${currentPolObj.description || currentPol}»`}
												onclick={() => oneditpolicy(currentPol)}
											>
												<SquarePen size={14} />
											</button>
										{/if}
										<button
											type="button"
											class="action-icon-btn action-icon-btn--reset"
											title="Сбросить на Основную (провайдер)"
											disabled={isPending}
											onclick={() => handleSetPolicy(device, 'default')}
										>
											<RotateCcw size={13} />
										</button>
									{:else}
										<span class="action-muted">—</span>
									{/if}
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		<!-- 2. Мобильный адаптивный список карточек (< 768px): всё видно без горизонтального скролла -->
		<div class="mobile-card-list-view mobile-cards-list mobile-only">
			{#each filteredDevices as device (device.mac)}
				{@const online = isOnline(device)}
				{@const isPending = pendingMacs.has(device.mac)}
				{@const isSelected = selectedMacs.has(device.mac)}
				{@const typeInfo = getDeviceTypeInfo(device)}
				{@const DeviceIcon = typeInfo.icon}
				{@const currentPol = device.policy || 'default'}
				{@const isDefaultActive = currentPol === '' || currentPol === 'default'}
				{@const currentPolObj = standardPolicies.find((p) => p.name === currentPol)}

				<div class="mobile-device-card" class:mobile-device-card--selected={isSelected}>
					<!-- Верхняя строка карточки: Чекбокс, Иконка, Имя, Статус, IP и MAC -->
					<div class="mobile-card-top">
						<div class="mobile-card-identity">
							<input
								type="checkbox"
								class="matrix-checkbox"
								checked={isSelected}
								onchange={() => toggleSelect(device.mac)}
							/>

							<span
								class="device-led"
								class:device-led--online={online}
								class:device-led--offline={!online}
								title={online ? 'В сети' : 'Офлайн'}
							></span>
							<div class="device-avatar" title={typeInfo.label}>
								<DeviceIcon size={15} />
							</div>

							<div class="mobile-device-details">
								<div class="device-title-row">
									<span class="device-name" title={device.name || device.hostname || device.mac}>
										{device.name || device.hostname || device.mac}
									</span>
									{#if device.link}
										<span
											class="device-link-tag"
											class:link-up={device.link.toLowerCase() === 'up'}
											class:link-down={device.link.toLowerCase() === 'down'}
										>
											{device.link}
										</span>
									{/if}
								</div>

								<div class="mobile-network-row">
									{#if device.ip && device.ip !== '0.0.0.0'}
										<button
											type="button"
											class="ip-chip"
											onclick={() => copyIp(device.ip)}
											title="Скопировать IP"
										>
											<code>{device.ip}</code>
											{#if copiedIp === device.ip}
												<Check size={10} class="text-green" />
											{:else}
												<Copy size={10} class="ip-copy-icon" />
											{/if}
										</button>
									{:else}
										<span class="device-no-ip">IP нет</span>
									{/if}
									<span class="mobile-mac">{device.mac}</span>
								</div>
							</div>
						</div>
					</div>

					<!-- Нижняя строка карточки: Кнопка политики на всю ширину + кнопки действий -->
					<div class="mobile-card-bottom">
						<button
							type="button"
							class="policy-selector-btn policy-selector-btn--mobile"
							class:policy-selector-btn--custom={!isDefaultActive}
							class:policy-selector-btn--open={openDropdownMac === device.mac}
							class:policy-selector-btn--pending={isPending}
							disabled={isPending}
							onclick={(e) => togglePolicyDropdown(e, device)}
						>
							<div class="policy-btn-left">
								{#if isPending}
									<div class="btn-spinner"></div>
								{:else if isDefaultActive}
									<Globe size={14} class="default-icon" />
								{:else}
									<PolicyIcon
										label={currentPolObj?.description || currentPolObj?.name || currentPol}
										policyName={currentPol}
										isHydraRoute={false}
									/>
								{/if}

								<div class="policy-btn-content">
									<span class="policy-btn-name">
										{isDefaultActive ? 'Основная' : currentPolObj?.description || currentPol}
									</span>
									{#if isDefaultActive}
										<span class="policy-btn-sub">Провайдер</span>
									{:else if currentPolObj?.interfaces?.length}
										<span class="policy-btn-iface">
											{formatIfaceBadge(currentPolObj.interfaces[0])}
											{#if currentPolObj.interfaces.length > 1}
												<span class="iface-count">+{currentPolObj.interfaces.length - 1}</span>
											{/if}
										</span>
									{/if}
								</div>
							</div>

							<span
								class="policy-chevron-wrap"
								class:chevron-rotated={openDropdownMac === device.mac}
							>
								<ChevronDown size={12} />
							</span>
						</button>

						{#if !isDefaultActive}
							<div class="mobile-card-actions">
								{#if oneditpolicy && currentPolObj}
									<button
										type="button"
										class="action-icon-btn"
										title="Настроить выходы"
										onclick={() => oneditpolicy(currentPol)}
									>
										<SquarePen size={13} />
									</button>
								{/if}
								<button
									type="button"
									class="action-icon-btn action-icon-btn--reset"
									title="Сбросить на Основную"
									disabled={isPending}
									onclick={() => handleSetPolicy(device, 'default')}
								>
									<RotateCcw size={13} />
								</button>
							</div>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{:else}
		<!-- ─── ВАРИАНТ 1: Обновленная чистая матрица с горизонтальным скроллом и sticky-колонкой ─── -->
		<div class="matrix-table-container matrix-table-container--grid">
			<table class="matrix-table">
				<thead>
					<tr>
						<!-- Device column header (Sticky left) -->
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
									<Globe size={15} class="default-icon" />
									<span class="policy-th-name">Основная</span>
								</div>
								<span class="policy-th-sub">Провайдер</span>
							</div>
						</th>

						<!-- Custom Policies column headers -->
						{#each standardPolicies as policy}
							<th class="col-policy">
								<div class="policy-th-header">
									<div class="policy-th-title">
										<PolicyIcon
											label={policy.description || policy.name || ''}
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
												<span class="matrix-header-badge" title={formatIfaceBadge(iface)}>
													{formatIfaceBadge(iface)}
												</span>
											{/each}
											{#if policy.interfaces.length > 2}
												<span
													class="more-ifaces"
													title={policy.interfaces.map(formatIfaceBadge).join(', ')}
												>
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
							<!-- Device info cell (Sticky left) -->
							<td class="col-device">
								<div class="device-cell">
									<input
										type="checkbox"
										class="matrix-checkbox"
										checked={isSelected}
										onchange={() => toggleSelect(device.mac)}
									/>

									<!-- Индикатор активности устройства (как в режиме списка политик) -->
									<span
										class="device-led"
										class:device-led--online={online}
										class:device-led--offline={!online}
										title={online ? 'В сети' : 'Офлайн'}
									></span>
									<div class="device-avatar" title={typeInfo.label}>
										<DeviceIcon size={18} />
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
												<span class="device-no-ip">IP нет</span>
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
								{#if isPending && isDefaultActive}
									<div class="cell-spinner"></div>
								{:else if isDefaultActive}
									<span class="matrix-active-pill" title="Текущая политика: Основная">
										<Check size={12} strokeWidth={2.5} />
									</span>
								{:else}
									<button
										type="button"
										class="matrix-assign-hint"
										title="Назначить в Основную"
										disabled={isPending}
									>
										+
									</button>
								{/if}
							</td>

							<!-- Custom policy cells -->
							{#each standardPolicies as policy (policy.name)}
								{@const isActive = currentPol === policy.name}
								<td
									class="col-policy-cell"
									class:active-cell={isActive}
									onclick={() => !isPending && handleSetPolicy(device, policy.name)}
								>
									{#if isPending && isActive}
										<div class="cell-spinner"></div>
									{:else if isActive}
										<span class="matrix-active-pill" title={`Текущая политика: ${policy.description || policy.name}`}>
											<Check size={12} strokeWidth={2.5} />
										</span>
									{:else}
										<button
											type="button"
											class="matrix-assign-hint"
											title={`Назначить в «${policy.description || policy.name}»`}
											disabled={isPending}
										>
											+
										</button>
									{/if}
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
				<span class="bulk-text">Выбрано: <strong>{selectedMacs.size}</strong></span>
			</div>

			<div class="bulk-controls">
				<label class="bulk-label" for="bulk-policy-select">Назначить:</label>
				<select
					id="bulk-policy-select"
					bind:value={bulkTargetPolicy}
					class="bulk-select"
					disabled={bulkSubmitting}
				>
					<option value="default">Основная (по умолчанию)</option>
					{#each standardPolicies as p}
						<option value={p.name}>{p.description || p.name}</option>
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
					Сбросить
				</Button>
			</div>
		</div>
	{/if}
</div>

<!-- ─── Portaled Dropdown Popover Menu for Policy Selector ─── -->
{#if openDropdownMac}
	{@const targetDev = policyDevices.find((d) => d.mac === openDropdownMac)}
	{#if targetDev}
		{@const isDefault = targetDev.policy === '' || targetDev.policy === 'default'}
		<div
			id="device-policy-dropdown-portal"
			use:portal
			class="device-policy-dropdown-menu"
			style="top: {dropdownCoords.top}px; left: {dropdownCoords.left}px; min-width: {dropdownCoords.width}px;"
		>
			<div class="dropdown-header">
				<span class="dropdown-header-title">
					Политика для <strong>{targetDev.name || targetDev.hostname || targetDev.mac}</strong>
				</span>
			</div>

			<div class="dropdown-items">
				<!-- Option: Default (Provider) -->
				<button
					type="button"
					class="dropdown-item"
					class:dropdown-item--active={isDefault}
					onclick={() => {
						const dev = targetDev;
						openDropdownMac = null;
						if (dev) handleSetPolicy(dev, 'default');
					}}
				>
					<div class="dropdown-item-left">
						<div class="dropdown-item-icon default-icon-wrap">
							<Globe size={16} class="default-icon" />
						</div>
						<div class="dropdown-item-texts">
							<span class="dropdown-item-name">Основная</span>
							<span class="dropdown-item-sub">Провайдер (прямой выход)</span>
						</div>
					</div>
					{#if isDefault}
						<Check size={16} class="text-accent dropdown-item-check" />
					{/if}
				</button>

				{#if standardPolicies.length > 0}
					<div class="dropdown-divider"></div>
					<div class="dropdown-section-label">Пользовательские политики</div>

					{#each standardPolicies as policy (policy.name)}
						{@const isCur = targetDev.policy === policy.name}
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div
							class="dropdown-item"
							class:dropdown-item--active={isCur}
							role="button"
							tabindex="0"
							onclick={() => {
								const dev = targetDev;
								openDropdownMac = null;
								if (dev) handleSetPolicy(dev, policy.name);
							}}
						>
							<div class="dropdown-item-left">
								<div class="dropdown-item-icon">
									<PolicyIcon
										label={policy.description || policy.name || ''}
										policyName={policy.name}
										isHydraRoute={false}
									/>
								</div>
								<div class="dropdown-item-texts">
									<span class="dropdown-item-name">{policy.description || policy.name}</span>
									{#if policy.interfaces?.length}
										<div class="dropdown-item-ifaces">
											{#each policy.interfaces.slice(0, 2) as iface}
												<span class="item-iface-tag">{formatIfaceBadge(iface)}</span>
											{/each}
											{#if policy.interfaces.length > 2}
												<span class="item-iface-more">+{policy.interfaces.length - 2}</span>
											{/if}
										</div>
									{:else}
										<span class="dropdown-item-sub text-warning">Без выходов</span>
									{/if}
								</div>
							</div>

							<div class="dropdown-item-right">
								{#if oneditpolicy}
									<button
										type="button"
										class="dropdown-item-edit-btn"
										title={`Настроить выходы политики «${policy.description || policy.name}»`}
										onclick={(e) => {
											e.stopPropagation();
											openDropdownMac = null;
											oneditpolicy(policy.name);
										}}
									>
										<SquarePen size={13} />
									</button>
								{/if}
								{#if isCur}
									<Check size={16} class="text-accent dropdown-item-check" />
								{/if}
							</div>
						</div>
					{/each}
				{/if}
			</div>

			{#if oncreatepolicy}
				<div class="dropdown-footer">
					<button
						type="button"
						class="dropdown-create-btn"
						onclick={() => {
							openDropdownMac = null;
							oncreatepolicy();
						}}
					>
						<Plus size={13} /> Создать новую политику
					</button>
				</div>
			{/if}
		</div>
	{/if}
{/if}

<style>
	.device-matrix-wrapper {
		display: flex;
		flex-direction: column;
		gap: 12px;
		height: 100%;
		position: relative;
	}

	/* ─── Toolbar ─── */
	.matrix-toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 10px;
	}

	.toolbar-left {
		display: flex;
		align-items: center;
		flex-wrap: nowrap;
		gap: 10px;
		flex: 1;
		min-width: 0;
	}

	.toolbar-right {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-shrink: 0;
	}

	.search-box {
		position: relative;
		display: flex;
		align-items: center;
		min-width: 280px;
		max-width: 400px;
		flex: 1 1 340px;
	}

	.search-box :global(.search-icon) {
		position: absolute;
		left: 10px;
		top: 50%;
		transform: translateY(-50%);
		color: var(--text-muted);
		pointer-events: none;
		z-index: 2;
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}

	.search-box input.search-input,
	.search-box input {
		width: 100%;
		height: 34px;
		padding-left: 36px !important;
		padding-right: 28px !important;
		padding-top: 0 !important;
		padding-bottom: 0 !important;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 6px);
		color: var(--text-primary);
		font-size: 0.8125rem;
		outline: none;
		transition: border-color 0.15s ease, box-shadow 0.15s ease;
		box-sizing: border-box;
	}

	.search-box input.search-input:focus,
	.search-box input:focus {
		border-color: var(--accent);
		box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 20%, transparent);
	}

	.search-box .clear-btn {
		position: absolute;
		right: 8px;
		top: 50%;
		transform: translateY(-50%);
		background: none;
		border: none;
		color: var(--text-muted);
		cursor: pointer;
		padding: 2px;
		border-radius: 4px;
		display: flex;
		align-items: center;
		z-index: 1;
	}

	.search-box .clear-btn:hover {
		color: var(--text-primary);
	}

	.filter-group {
		display: flex;
		align-items: center;
		gap: 4px;
		background: var(--bg-secondary);
		padding: 2px;
		border-radius: var(--radius-sm, 6px);
		border: 1px solid var(--border);
		flex-shrink: 0;
	}

	.filter-pill {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 4px 9px;
		border: none;
		border-radius: var(--radius-xs, 4px);
		background: transparent;
		color: var(--text-muted);
		font-size: 0.75rem;
		font-weight: 500;
		cursor: pointer;
		transition: all 0.12s ease;
		user-select: none;
		white-space: nowrap;
	}

	.filter-pill:hover {
		color: var(--text-primary);
		background: var(--bg-tertiary);
	}

	.filter-pill.active {
		background: var(--bg-tertiary);
		color: var(--text-primary);
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
	}

	.online-lamp {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--color-success, #22c55e);
		box-shadow: 0 0 5px rgba(34, 197, 94, 0.8);
	}

	.pill-count {
		font-size: 0.6875rem;
		padding: 1px 5px;
		border-radius: 999px;
		background: var(--bg-primary);
		color: var(--text-muted);
	}

	.pill-count--online {
		color: var(--color-success, #22c55e);
	}

	.pill-count--custom {
		color: var(--accent);
	}

	/* Layout Switcher */
	.layout-toggle-group {
		display: flex;
		align-items: center;
		background: var(--bg-secondary);
		padding: 2px;
		border-radius: var(--radius-sm, 6px);
		border: 1px solid var(--border);
	}

	.layout-btn {
		display: flex;
		align-items: center;
		gap: 5px;
		padding: 4px 8px;
		background: none;
		border: none;
		border-radius: var(--radius-xs, 4px);
		color: var(--text-muted);
		font-size: 0.75rem;
		cursor: pointer;
		transition: all 0.12s ease;
	}

	.layout-btn:hover {
		color: var(--text-primary);
		background: var(--bg-tertiary);
	}

	.layout-btn.active {
		background: var(--bg-tertiary);
		color: var(--text-primary);
		font-weight: 600;
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
	}

	.layout-btn-label {
		display: inline-block;
	}

	/* ─── Table Container (Desktop) ─── */
	.matrix-table-container {
		flex: 1;
		min-height: 0;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius, 8px);
		background: var(--bg-secondary);
	}

	/* Таблица занимает 100% ширины, строго выравниваясь с тулбаром и шапкой */
	.matrix-table-container.layout-list {
		width: 100%;
	}

	/* Checkbox common */
	.matrix-checkbox {
		cursor: pointer;
		accent-color: var(--accent);
		width: 15px;
		height: 15px;
		border-radius: 3px;
	}

	/* ─── ВАРИАНТ 2: Десктопная таблица ─── */
	.device-list-table {
		width: 100%;
		border-collapse: collapse;
		text-align: left;
		font-size: 0.8125rem;
	}

	.device-list-table thead th {
		position: sticky;
		top: 0;
		z-index: 10;
		background: var(--bg-tertiary);
		border-bottom: 1px solid var(--border);
		padding: 9px 12px;
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
	}

	.th-checkbox,
	.td-checkbox {
		width: 44px;
		min-width: 44px;
		text-align: center;
		padding: 8px 10px;
	}

	.th-device,
	.td-device {
		width: 32%;
		min-width: 220px;
	}

	.th-network,
	.td-network {
		width: 24%;
		min-width: 180px;
		white-space: nowrap;
	}

	.th-policy,
	.td-policy {
		width: 36%;
		min-width: 260px;
	}

	.th-actions,
	.td-actions {
		width: 80px;
		min-width: 80px;
		text-align: right;
		padding-right: 14px;
		white-space: nowrap;
	}

	.device-row {
		border-bottom: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
		transition: background 0.12s ease;
	}

	.device-row:hover {
		background: var(--bg-hover);
	}

	.device-row--selected {
		background: color-mix(in srgb, var(--accent) 8%, var(--bg-secondary));
	}

	.device-list-table tbody td {
		padding: 8px 12px;
		vertical-align: middle;
	}

	/* Device Cell */
	.device-cell {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.device-avatar {
		width: 34px;
		height: 34px;
		border-radius: var(--radius-sm, 6px);
		background: var(--bg-tertiary);
		border: 1px solid var(--border);
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--text-secondary);
		flex-shrink: 0;
	}

	/* Индикатор активности устройства (как в режиме списка политик) */
	.device-led {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
		transition: all 0.2s ease;
	}

	.device-led--online {
		background: var(--success, #22c55e);
		box-shadow: 0 0 6px var(--success, #22c55e);
	}

	.device-led--offline {
		background: var(--text-muted, #9ca3af);
		opacity: 0.55;
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
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.device-hostname {
		font-size: 0.6875rem;
		color: var(--text-muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.device-link-tag {
		font-size: 0.625rem;
		font-weight: 600;
		padding: 1px 5px;
		border-radius: 3px;
		text-transform: uppercase;
		letter-spacing: 0.02em;
		background: var(--bg-tertiary);
		color: var(--text-muted);
		border: 1px solid var(--border);
	}

	.device-link-tag.link-up {
		background: color-mix(in srgb, var(--color-success, #22c55e) 14%, transparent);
		color: var(--color-success, #22c55e);
		border-color: color-mix(in srgb, var(--color-success, #22c55e) 30%, transparent);
	}

	.device-link-tag.link-down {
		opacity: 0.7;
	}

	/* Network Cell */
	.network-cell {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.device-meta-row {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	.ip-chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		background: none;
		border: none;
		padding: 0;
		color: var(--text-primary);
		cursor: pointer;
		font-family: inherit;
		font-size: 0.8125rem;
		text-align: left;
	}

	.ip-chip code {
		font-family: var(--font-mono, monospace);
		font-size: 0.75rem;
		color: var(--text-primary);
	}

	.ip-chip:hover code {
		color: var(--accent);
	}

	:global(.ip-copy-icon) {
		opacity: 0;
		color: var(--text-muted);
		transition: opacity 0.1s ease;
	}

	.ip-chip:hover :global(.ip-copy-icon) {
		opacity: 1;
	}

	.device-mac {
		font-family: var(--font-mono, monospace);
		font-size: 0.6875rem;
		color: var(--text-muted);
		opacity: 0.8;
	}

	.device-no-ip {
		font-size: 0.75rem;
		font-style: italic;
		color: var(--text-muted);
	}

	/* ─── Access Policy Selector Button («кнопочка») ─── */
	.policy-selector-wrap {
		display: flex;
		align-items: center;
		width: 100%;
		max-width: 400px;
	}

	.policy-selector-btn {
		width: 100%;
		display: inline-flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 5px 9px;
		background: var(--bg-tertiary);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 6px);
		color: var(--text-primary);
		cursor: pointer;
		font-size: 0.8125rem;
		font-family: inherit;
		transition: all 0.14s ease;
		box-sizing: border-box;
	}

	.policy-selector-btn:hover {
		background: var(--bg-hover);
		border-color: var(--accent);
	}

	.policy-selector-btn--open {
		border-color: var(--accent);
		background: var(--bg-hover);
		box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 25%, transparent);
	}

	.policy-selector-btn--custom {
		background: color-mix(in srgb, var(--accent) 10%, var(--bg-tertiary));
		border-color: color-mix(in srgb, var(--accent) 35%, var(--border));
	}

	.policy-selector-btn--custom:hover {
		border-color: var(--accent);
	}

	.policy-btn-left {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
		flex: 1;
	}

	.policy-btn-content {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		min-width: 0;
		gap: 1px;
		text-align: left;
	}

	.policy-btn-name {
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 240px;
	}

	.policy-btn-sub {
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	.policy-btn-iface {
		font-size: 0.6875rem;
		color: var(--text-secondary);
		display: flex;
		align-items: center;
		gap: 3px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 150px;
	}

	.iface-count {
		font-size: 0.625rem;
		padding: 0 3px;
		border-radius: 2px;
		background: var(--bg-primary);
		color: var(--text-muted);
	}

	.policy-chevron-wrap {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		color: var(--text-muted);
		transition: transform 0.15s ease;
		flex-shrink: 0;
	}

	.policy-chevron-wrap.chevron-rotated {
		transform: rotate(180deg);
		color: var(--accent);
	}

	:global(.default-icon) {
		color: var(--accent);
		flex-shrink: 0;
	}

	.btn-spinner {
		width: 14px;
		height: 14px;
		border: 2px solid transparent;
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}

	/* Row Actions */
	.td-actions {
		text-align: right;
		padding-right: 14px;
	}

	.row-actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 4px;
	}

	.action-icon-btn {
		width: 28px;
		height: 28px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: var(--bg-tertiary);
		border: 1px solid var(--border);
		border-radius: var(--radius-xs, 4px);
		color: var(--text-muted);
		cursor: pointer;
		transition: all 0.12s ease;
		flex-shrink: 0;
	}

	.action-icon-btn:hover {
		color: var(--text-primary);
		border-color: var(--accent);
		background: var(--bg-hover);
	}

	.action-icon-btn--reset:hover {
		color: var(--color-warning, #eab308);
	}

	.action-muted {
		color: var(--text-muted);
		opacity: 0.5;
	}

	/* ─── МОБИЛЬНЫЙ ВИД: Карточки устройств (< 768px) ─── */
	.desktop-only {
		display: block;
	}

	.mobile-only,
	.mobile-cards-list {
		display: none !important;
	}

	.mobile-card-list-view {
		display: none;
		flex-direction: column;
		gap: 6px;
		overflow-y: auto;
		flex: 1;
		min-height: 0;
		-webkit-overflow-scrolling: touch;
	}

	.mobile-device-card {
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 6px);
		padding: 5px 8px;
		display: flex;
		flex-direction: column;
		gap: 3px;
		transition: background 0.1s ease, border-color 0.1s ease;
	}

	.mobile-device-card--selected {
		background: color-mix(in srgb, var(--accent) 8%, var(--bg-secondary));
		border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
	}

	.mobile-card-top {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.mobile-card-identity {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		flex: 1;
	}

	.mobile-card-identity .device-avatar {
		width: 22px;
		height: 22px;
		border-radius: 4px;
	}

	.mobile-card-identity .device-avatar :global(svg) {
		width: 13px;
		height: 13px;
	}

	.mobile-card-identity .device-led {
		width: 7px;
		height: 7px;
	}

	.mobile-device-details {
		display: flex;
		flex-direction: column;
		min-width: 0;
		gap: 1px;
		flex: 1;
	}

	.mobile-device-details .device-name {
		font-size: 0.8125rem;
		font-weight: 600;
		line-height: 1.15;
	}

	.mobile-device-details .device-link-tag {
		font-size: 0.5625rem;
		padding: 0 3px;
	}

	.mobile-network-row {
		display: flex;
		align-items: center;
		gap: 5px;
		font-size: 0.6875rem;
		flex-wrap: wrap;
	}

	.mobile-mac {
		font-family: var(--font-mono, monospace);
		font-size: 0.625rem;
		color: var(--text-muted);
		opacity: 0.75;
	}

	.mobile-card-bottom {
		display: flex;
		align-items: center;
		gap: 5px;
		padding-top: 3px;
		border-top: 1px solid color-mix(in srgb, var(--border) 35%, transparent);
	}

	.policy-selector-btn--mobile {
		flex: 1;
		min-width: 0;
		padding: 2px 7px;
		min-height: 28px;
		height: 28px;
	}

	.policy-selector-btn--mobile .policy-btn-name {
		font-size: 0.75rem;
		max-width: 160px;
	}

	.policy-selector-btn--mobile .policy-btn-sub {
		font-size: 0.625rem;
	}

	.mobile-card-actions {
		display: flex;
		align-items: center;
		gap: 3px;
		flex-shrink: 0;
	}

	.mobile-card-actions .action-icon-btn {
		width: 26px;
		height: 26px;
	}

	/* ─── ВАРИАНТ 1: Matrix Table (Compact, Polished, Horizontally scrollable) ─── */
	.matrix-table-container--grid {
		overflow-x: auto;
		-webkit-overflow-scrolling: touch;
	}

	.matrix-table {
		width: 100%;
		min-width: max-content;
		border-collapse: separate;
		border-spacing: 0;
		text-align: left;
		font-size: 0.8125rem;
	}

	.matrix-table thead th {
		position: sticky;
		top: 0;
		z-index: 10;
		background: var(--bg-tertiary);
		border-bottom: 1px solid var(--border);
		padding: 8px 12px;
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
	}

	.col-device {
		min-width: 220px;
		position: sticky;
		left: 0;
		z-index: 11;
		background: var(--bg-secondary);
		border-right: 1px solid var(--border);
	}

	.matrix-table thead th.col-device {
		z-index: 20;
		background: var(--bg-tertiary);
		border-right: 1px solid var(--border);
	}

	.th-device-content {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.col-policy {
		min-width: 96px;
		max-width: 130px;
		text-align: center;
		border-left: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
	}

	.col-policy--default {
		background: color-mix(in srgb, var(--accent) 4%, transparent);
	}

	.policy-th-header {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
	}

	.policy-th-title {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 5px;
	}

	.policy-th-name {
		font-size: 0.8125rem;
		font-weight: 600;
		max-width: 100px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
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
		background: var(--bg-hover);
	}

	.policy-iface-badges {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 3px;
		margin-top: 2px;
	}

	.matrix-header-badge {
		font-size: 0.625rem;
		padding: 1px 4px;
		border-radius: 3px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		color: var(--text-secondary);
		max-width: 80px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.more-ifaces {
		font-size: 0.625rem;
		color: var(--text-muted);
	}

	.col-policy-cell {
		text-align: center;
		cursor: pointer;
		border-left: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
		transition: background 0.12s ease;
		padding: 6px 4px;
	}

	.col-policy-cell:hover {
		background: color-mix(in srgb, var(--accent) 15%, transparent);
	}

	.col-policy-cell.active-cell {
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}

	.matrix-active-pill {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		background: var(--accent);
		color: #ffffff;
		box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 50%, transparent);
	}

	.matrix-assign-hint {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		border-radius: 4px;
		background: transparent;
		border: 1px dashed transparent;
		color: var(--text-muted);
		opacity: 0;
		cursor: pointer;
		font-size: 0.875rem;
		transition: all 0.12s ease;
	}

	.col-policy-cell:hover .matrix-assign-hint {
		opacity: 0.8;
		border-color: var(--border);
		background: var(--bg-secondary);
	}

	.cell-spinner {
		display: inline-block;
		width: 16px;
		height: 16px;
		border: 2px solid transparent;
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}

	/* ─── Portaled Policy Selector Dropdown Menu ─── */
	:global(.device-policy-dropdown-menu) {
		position: fixed;
		z-index: 10000;
		background: var(--bg-tertiary);
		border: 1px solid var(--border);
		border-radius: var(--radius, 8px);
		box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.4), 0 4px 6px -2px rgba(0, 0, 0, 0.2);
		overflow: hidden;
		display: flex;
		flex-direction: column;
		max-height: 400px;
		animation: popoverFadeIn 0.12s ease-out;
	}

	@keyframes popoverFadeIn {
		from {
			opacity: 0;
			transform: translateY(-4px);
		}
		to {
			opacity: 1;
			transform: translateY(0);
		}
	}

	:global(.dropdown-header) {
		padding: 8px 12px;
		background: var(--bg-secondary);
		border-bottom: 1px solid var(--border);
		font-size: 0.75rem;
		color: var(--text-muted);
	}

	:global(.dropdown-header-title strong) {
		color: var(--text-primary);
	}

	:global(.dropdown-items) {
		overflow-y: auto;
		max-height: 300px;
		padding: 4px;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	:global(.dropdown-item) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 4px;
		padding: 3px 6px;
		background: none;
		border: none;
		border-radius: var(--radius-xs, 4px);
		color: var(--text-primary);
		font-family: inherit;
		font-size: 0.8125rem;
		text-align: left;
		transition: background 0.1s ease;
		width: 100%;
		box-sizing: border-box;
	}

	:global(button.dropdown-item) {
		cursor: pointer;
		padding: 6px 10px;
	}

	:global(.dropdown-item:hover) {
		background: var(--bg-hover);
	}

	:global(.dropdown-item--active) {
		background: color-mix(in srgb, var(--accent) 12%, var(--bg-tertiary));
		font-weight: 500;
	}

	:global(.dropdown-item-main) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 3px 4px;
		background: none;
		border: none;
		border-radius: var(--radius-xs, 4px);
		color: inherit;
		cursor: pointer;
		font-family: inherit;
		font-size: inherit;
		text-align: left;
		flex: 1;
		min-width: 0;
	}

	:global(.dropdown-item-left) {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
		flex: 1;
	}

	:global(.dropdown-item-icon) {
		width: 22px;
		height: 22px;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}

	:global(.dropdown-item-texts) {
		display: flex;
		flex-direction: column;
		min-width: 0;
		gap: 1px;
	}

	:global(.dropdown-item-name) {
		font-weight: 600;
		color: var(--text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	:global(.dropdown-item-sub) {
		font-size: 0.6875rem;
		color: var(--text-muted);
	}

	:global(.dropdown-item-ifaces) {
		display: flex;
		align-items: center;
		gap: 4px;
		margin-top: 1px;
	}

	:global(.item-iface-tag) {
		font-size: 0.625rem;
		padding: 1px 4px;
		border-radius: 3px;
		background: var(--bg-secondary);
		border: 1px solid var(--border);
		color: var(--text-secondary);
		max-width: 110px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	:global(.item-iface-more) {
		font-size: 0.625rem;
		color: var(--text-muted);
	}

	:global(.dropdown-item-right) {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-shrink: 0;
	}

	:global(.dropdown-item-edit-btn) {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		padding: 0;
		border: 1px solid transparent;
		background: transparent;
		color: var(--text-muted);
		border-radius: 4px;
		cursor: pointer;
		transition: all 0.12s ease;
	}

	:global(.dropdown-item-edit-btn:hover) {
		color: var(--accent);
		background: var(--bg-hover);
		border-color: var(--border);
	}

	:global(.dropdown-divider) {
		height: 1px;
		background: var(--border);
		margin: 4px 6px;
	}

	:global(.dropdown-section-label) {
		font-size: 0.6875rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--text-muted);
		padding: 4px 10px 2px 10px;
	}

	:global(.dropdown-footer) {
		padding: 4px;
		border-top: 1px solid var(--border);
		background: var(--bg-secondary);
	}

	:global(.dropdown-create-btn) {
		width: 100%;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		padding: 6px 10px;
		background: none;
		border: none;
		border-radius: var(--radius-xs, 4px);
		color: var(--accent);
		font-size: 0.75rem;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.1s ease;
	}

	:global(.dropdown-create-btn:hover) {
		background: var(--bg-hover);
	}

	/* ─── Floating Bulk Action Bar ─── */
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
		border: 1px solid var(--border);
		border-radius: 30px;
		box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.4), 0 4px 6px -2px rgba(0, 0, 0, 0.15);
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
		border: 1px solid var(--border);
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
		border: 1px solid var(--border);
		border-radius: var(--radius, 8px);
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
		color: var(--color-success, #22c55e);
	}

	.text-warning {
		color: var(--color-warning, #eab308);
	}

	:global(.text-accent) {
		color: var(--accent);
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	/* ─── АДАПТИВНОСТЬ ПОД МОБИЛЬНЫЕ УСТРОЙСТВА (< 768px) ─── */
	@media (max-width: 768px) {
		.desktop-only {
			display: none !important;
		}

		.mobile-only,
		.mobile-cards-list,
		.mobile-card-list-view {
			display: flex !important;
		}

		.device-matrix-wrapper {
			gap: 8px;
		}

		.matrix-toolbar {
			display: flex;
			flex-direction: column;
			align-items: stretch;
			gap: 6px;
			width: 100%;
		}

		.toolbar-left {
			display: flex;
			flex-direction: column;
			align-items: stretch;
			gap: 6px;
			width: 100%;
		}

		.search-box {
			max-width: none;
			width: 100%;
			min-width: 0;
		}

		.search-box input.search-input,
		.search-box input {
			height: 32px;
			font-size: 0.75rem;
			padding-left: 36px !important;
			padding-right: 28px !important;
		}

		.filter-group {
			display: flex;
			width: 100%;
			min-width: 0;
			gap: 3px;
			box-sizing: border-box;
		}

		.filter-pill {
			flex: 1 1 0;
			min-width: 0;
			padding: 4px 2px;
			font-size: 0.6875rem;
			justify-content: center;
			text-align: center;
		}

		.toolbar-right {
			display: flex;
			align-items: center;
			justify-content: flex-end;
			width: 100%;
			gap: 6px;
		}

		.layout-toggle-group {
			flex-shrink: 0;
		}

		.layout-btn {
			padding: 3px 6px;
			font-size: 0.6875rem;
		}

		/* Компактная матрица на мобильных устройствах (< 768px) */
		.matrix-table thead th.col-device,
		.matrix-table tbody td.col-device {
			width: 140px;
			min-width: 140px;
			max-width: 140px;
			padding: 6px 6px;
			box-sizing: border-box;
		}

		.matrix-table .th-device-content {
			gap: 5px;
			font-size: 0.75rem;
			max-width: 128px;
			overflow: hidden;
		}

		.matrix-table .th-device-content span {
			font-size: 0.6875rem;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		.matrix-table .device-cell {
			gap: 5px;
			min-width: 0;
			max-width: 128px;
			overflow: hidden;
		}

		.matrix-table .device-avatar {
			width: 24px;
			height: 24px;
			border-radius: 4px;
			flex-shrink: 0;
		}

		.matrix-table .device-avatar :global(svg) {
			width: 14px;
			height: 14px;
		}

		.matrix-table .device-led {
			width: 7px;
			height: 7px;
		}

		.matrix-table .device-details {
			min-width: 0;
			overflow: hidden;
			flex: 1;
			gap: 1px;
		}

		.matrix-table .device-title-row {
			gap: 3px;
			min-width: 0;
			max-width: 100%;
			overflow: hidden;
		}

		.matrix-table .device-name {
			font-size: 0.75rem;
			flex: 1;
			min-width: 0;
			max-width: 68px;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		.matrix-table .device-link-tag {
			font-size: 0.5625rem;
			padding: 0 3px;
			flex-shrink: 0;
		}

		.matrix-table .device-meta-row {
			flex-direction: column;
			align-items: flex-start;
			gap: 0;
			min-width: 0;
			max-width: 100%;
			overflow: hidden;
		}

		.matrix-table .ip-chip {
			font-size: 0.6875rem;
			gap: 2px;
			max-width: 72px;
			overflow: hidden;
		}

		.matrix-table .ip-chip code {
			font-size: 0.6875rem;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		.matrix-table .device-mac {
			font-size: 0.5625rem;
			max-width: 72px;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
			opacity: 0.7;
		}

		.matrix-table thead th.col-policy {
			min-width: 70px;
			max-width: 82px;
			padding: 5px 3px;
		}

		.matrix-table .policy-th-title {
			gap: 2px;
		}

		.matrix-table .policy-th-name {
			font-size: 0.6875rem;
			max-width: 52px;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		.matrix-table .policy-th-sub {
			font-size: 0.625rem;
		}

		.matrix-table .matrix-header-badge {
			font-size: 0.5625rem;
			padding: 0 2px;
			max-width: 48px;
		}

		.matrix-table .col-policy-cell {
			min-width: 70px;
			padding: 6px 2px;
		}

		.matrix-table .matrix-active-pill {
			width: 20px;
			height: 20px;
		}

		/* Выпадающее меню политики на мобильном (растягивается как Action Sheet) */
		:global(.device-policy-dropdown-menu) {
			left: 10px !important;
			right: 10px !important;
			width: auto !important;
			min-width: 0 !important;
			max-width: calc(100vw - 20px) !important;
		}

		/* Плавающая панель групповых действий на мобилке */
		.bulk-floating-bar {
			width: calc(100% - 24px);
			flex-direction: column;
			border-radius: 14px;
			gap: 8px;
			bottom: 8px;
			padding: 10px 14px;
		}

		.bulk-controls {
			width: 100%;
			flex-wrap: wrap;
			justify-content: space-between;
			gap: 6px;
		}

		.bulk-select {
			flex: 1;
			min-width: 140px;
		}
	}
</style>
