<script lang="ts">
	import { Badge, Button, Modal } from '$lib/components/ui';
	import { EmptyState } from '$lib/components/layout';
	import { Plus, Trash2, Pencil, Activity, Check, Zap, Search, Layers, Radio } from 'lucide-svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { awgTags as awgTagsStore } from '$lib/stores/awgTags';
	import { subscriptionsStore } from '$lib/stores/subscriptions';
	import { formatOutboundHumanName } from '$lib/utils/outboundHumanName';
	import type { MihomoNativeGroup, MihomoNativeProxy, MihomoNativeSubscription, MihomoRuntimeProxy } from '$lib/types';
	import MihomoGroupEditModal from '$lib/components/sb-router/mihomo/MihomoGroupEditModal.svelte';

	interface Props {
		loading?: boolean;
		groups: MihomoNativeGroup[];
		proxies?: MihomoNativeProxy[];
		subscriptions?: MihomoNativeSubscription[];
		runtimeProxies?: Record<string, MihomoRuntimeProxy>;
		susaninEgressId?: string;
		onGroupChanged?: () => void;
	}

	let {
		loading = false,
		groups = [],
		proxies = [],
		subscriptions = [],
		runtimeProxies = {},
		susaninEgressId = '',
		onGroupChanged,
	}: Props = $props();

	let searchQuery = $state('');
	let editModalOpen = $state(false);
	let selectedGroup = $state<MihomoNativeGroup | null>(null);
	let deletingId = $state<string | null>(null);
	let switchingMember = $state<Record<string, boolean>>({});
	let testingDelay = $state<Record<string, boolean>>({});
	let delays = $state<Record<string, number>>({});

	const nameContext = $derived({
		awgTags: $awgTagsStore.data,
		subscriptions: $subscriptionsStore.data,
		mihomoSubscriptions: subscriptions,
	});

	function memberLabel(member: string): string {
		const proxy = proxies.find((item) => item.id === member || item.name === member);
		if (proxy?.name) return proxy.name;
		return formatOutboundHumanName(member, nameContext);
	}

	const filteredGroups = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		if (!q) return groups;
		return groups.filter((g) =>
			g.name.toLowerCase().includes(q) ||
			g.type.toLowerCase().includes(q) ||
			(g.proxies || []).some((p) => p.toLowerCase().includes(q))
		);
	});

	function openCreate() {
		selectedGroup = null;
		editModalOpen = true;
	}

	function openEdit(group: MihomoNativeGroup) {
		selectedGroup = group;
		editModalOpen = true;
	}

	async function selectMember(groupName: string, memberName: string) {
		if (switchingMember[groupName]) return;
		switchingMember[groupName] = true;
		try {
			await api.mihomoSelectProxy(groupName, memberName);
			notifications.success(`Активный узел переключён на ${memberName}`);
			onGroupChanged?.();
		} catch (err) {
			notifications.error(`Не удалось переключить узел: ${err instanceof Error ? err.message : String(err)}`);
		} finally {
			switchingMember[groupName] = false;
		}
	}

	async function testGroupDelay(groupName: string) {
		if (testingDelay[groupName]) return;
		testingDelay[groupName] = true;
		try {
			const delay = await api.mihomoProxyDelay(groupName);
			if (delay > 0) {
				delays[groupName] = delay;
				notifications.success(`Задержка ${groupName}: ${delay} мс`);
			} else {
				notifications.warning(`Тест группы ${groupName}: недоступна или таймаут`);
			}
		} catch (err) {
			notifications.warning(`Тест группы ${groupName}: недоступна или таймаут`);
		} finally {
			testingDelay[groupName] = false;
		}
	}

	async function handleDelete(id: string) {
		try {
			await api.mihomoNativeDeleteGroup(id);
			notifications.success('Группа удалена');
			deletingId = null;
			onGroupChanged?.();
		} catch (err) {
			notifications.error(`Ошибка при удалении: ${err instanceof Error ? err.message : String(err)}`);
		}
	}

	function groupTypeLabel(type: string): string {
		switch (type) {
			case 'select':
				return 'Ручной выбор (Select)';
			case 'url-test':
				return 'Авто (наименьшая задержка)';
			case 'fallback':
				return 'Отказоустойчивость (Fallback)';
			case 'load-balance':
				return 'Балансировка (Load Balance)';
			case 'relay':
				return 'Цепочка (Relay)';
			default:
				return type;
		}
	}

	function typeBadgeVariant(type: string): 'info' | 'success' | 'warning' | 'purple' | 'muted' {
		switch (type) {
			case 'select':
				return 'info';
			case 'url-test':
				return 'success';
			case 'fallback':
				return 'warning';
			case 'load-balance':
				return 'purple';
			default:
				return 'muted';
		}
	}
</script>

<div class="proxy-groups-page space-y-4">
	<!-- Toolbar -->
	<div class="groups-toolbar flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 bg-[var(--color-bg-secondary)] p-3 rounded-xl border border-[var(--color-border)]">
		<div class="groups-search-row flex items-center gap-2 flex-1">
			<div class="groups-search relative flex-1 max-w-sm">
				<Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--color-text-muted)]" />
				<input
					type="text"
					bind:value={searchQuery}
					placeholder="Поиск по имени или узлам..."
					class="w-full pl-9 pr-3 py-1.5 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent)]"
				/>
			</div>
			<div class="text-xs text-[var(--color-text-muted)] whitespace-nowrap">
				Всего групп: <span class="font-medium text-[var(--color-text-primary)]">{groups.length}</span>
			</div>
		</div>

		<div class="flex items-center gap-2">
			<Button variant="primary" onclick={openCreate}>
				<Plus class="w-4 h-4 mr-1.5" />
				Создать группу
			</Button>
		</div>
	</div>

	<!-- Groups Grid -->
	{#if filteredGroups.length === 0}
		{#if searchQuery}
			<EmptyState
				title="Ничего не найдено"
				description="По запросу «{searchQuery}» группы не найдены."
			/>
		{:else}
			<EmptyState
				title="Нет прокси-групп"
				description="Создайте первую группу для объединения прокси, подписок и туннелей с автоматическим выбором или балансировкой."
			>
				{#snippet action()}
					<Button variant="primary" onclick={openCreate}>
						<Plus class="w-4 h-4 mr-1.5" />
						Создать группу
					</Button>
				{/snippet}
			</EmptyState>
		{/if}
	{:else}
		<div class="groups-grid grid grid-cols-1 lg:grid-cols-2 gap-4">
			{#each filteredGroups as group (group.id || group.name)}
				{@const rt = runtimeProxies[group.name]}
				{@const activeNow = rt?.now || (group.proxies?.[0] ?? '')}
				{@const isSusaninTarget = susaninEgressId === group.id || susaninEgressId === group.name}
				<div class="group-card bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-4 flex flex-col justify-between hover:border-[var(--color-border-hover)] transition-colors shadow-sm">
					<div>
						<!-- Header Row -->
						<div class="group-card-header flex items-start justify-between gap-2 mb-2">
							<div class="flex items-center gap-2 flex-wrap">
								<Layers class="w-5 h-5 text-[var(--color-accent)] shrink-0" />
								<h3 class="font-semibold text-base text-[var(--color-text-primary)] leading-tight">
									{group.name}
								</h3>
								<Badge variant="muted" size="sm">Mihomo</Badge>
								<Badge variant={typeBadgeVariant(group.type)} size="sm">
									{groupTypeLabel(group.type)}
								</Badge>
								{#if isSusaninTarget}
									<Badge variant="purple" size="sm">
										<Radio class="w-3 h-3 mr-1" />
										Susanin Egress
									</Badge>
								{/if}
							</div>

							<div class="flex items-center gap-1 shrink-0">
								<button
									class="p-1.5 rounded-md hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-accent)] transition-colors"
									title="Проверить задержку"
									disabled={testingDelay[group.name]}
									onclick={() => testGroupDelay(group.name)}
								>
									<Activity class="w-4 h-4 {testingDelay[group.name] ? 'animate-spin' : ''}" />
								</button>
								<button
									class="p-1.5 rounded-md hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-accent)] transition-colors"
									title="Редактировать группу"
									onclick={() => openEdit(group)}
								>
									<Pencil class="w-4 h-4" />
								</button>
								<button
									class="p-1.5 rounded-md hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-error)] transition-colors"
									title="Удалить группу"
									onclick={() => (deletingId = group.id)}
								>
									<Trash2 class="w-4 h-4" />
								</button>
							</div>
						</div>

						<!-- Status & Latency -->
						<div class="group-summary flex items-center gap-3 text-xs text-[var(--color-text-muted)] mb-3">
							<div class="flex items-center gap-1.5">
								<span class="w-2 h-2 rounded-full {group.enabled !== false ? 'bg-[var(--color-success)]' : 'bg-gray-500'}"></span>
								<span>{group.enabled !== false ? 'Активна' : 'Отключена'}</span>
							</div>
							{#if delays[group.name]}
								<div class="text-[var(--color-success)] font-mono font-medium">
									{delays[group.name]} мс
								</div>
							{/if}
							<div>
								Узлов: <span class="font-medium text-[var(--color-text-primary)]">{(group.proxies || []).length}</span>
								{#if (group.use || []).length > 0}
									+ <span class="font-medium text-[var(--color-text-primary)]">{(group.use || []).length}</span> подписок
								{/if}
							</div>
						</div>

						<!-- Active Member Row -->
						<div class="active-node bg-[var(--color-bg-tertiary)] p-2.5 rounded-lg border border-[var(--color-border)] mb-3">
							<div class="text-[11px] font-medium text-[var(--color-text-muted)] mb-1 flex items-center justify-between">
								<span>АКТИВНЫЙ УЗЕЛ СЕЙЧАС</span>
								{#if group.type === 'select'}
									<span class="text-[var(--color-accent)] text-[10px]">Кликните узел для переключения</span>
								{/if}
							</div>
							<div class="flex items-center gap-2">
								<Zap class="w-4 h-4 text-[var(--color-warning)] shrink-0" />
								<span class="font-mono text-sm text-[var(--color-text-primary)] font-medium truncate">
									{memberLabel(activeNow) || 'Не определён'}
								</span>
							</div>
						</div>

						<!-- Members Chips -->
						{#if (group.proxies || []).length > 0}
							<div class="space-y-1.5">
								<div class="text-[11px] font-medium text-[var(--color-text-muted)] uppercase tracking-wider">
									Состав группы
								</div>
								<div class="member-list flex flex-wrap gap-1.5 max-h-32 overflow-y-auto p-0.5">
									{#each group.proxies || [] as member}
										{@const isSelected = activeNow === member}
										{#if group.type === 'select'}
											<button
												class="px-2 py-0.5 text-xs rounded-md border transition-colors flex items-center gap-1 {isSelected ? 'bg-[var(--color-accent-tint)] border-[var(--color-accent)] text-[var(--color-accent)] font-medium' : 'bg-[var(--color-bg-tertiary)] border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)]'}"
												disabled={switchingMember[group.name]}
												onclick={() => selectMember(group.name, member)}
												title="Сделать активным"
											>
												{#if isSelected}
													<Check class="w-3 h-3 text-[var(--color-accent)]" />
												{/if}
												<span class="truncate max-w-[220px]" title={member}>{memberLabel(member)}</span>
											</button>
										{:else}
											<span class="px-2 py-0.5 text-xs rounded-md border bg-[var(--color-bg-tertiary)] border-[var(--color-border)] text-[var(--color-text-secondary)] flex items-center gap-1">
												{#if isSelected}
													<Check class="w-3 h-3 text-[var(--color-success)]" />
												{/if}
												<span class="truncate max-w-[220px]" title={member}>{memberLabel(member)}</span>
											</span>
										{/if}
									{/each}
								</div>
							</div>
						{/if}
					</div>

					<!-- Card Footer info -->
					{#if group.url || group.interval}
						<div class="mt-3 pt-2.5 border-t border-[var(--color-border)] text-[11px] text-[var(--color-text-muted)] flex items-center justify-between">
							{#if group.interval}
								<span>Интервал: {group.interval} с</span>
							{/if}
							{#if group.tolerance}
								<span>Допуск: {group.tolerance} мс</span>
							{/if}
							{#if group.lazy}
								<span>Lazy check</span>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.proxy-groups-page { display: flex; flex-direction: column; gap: 18px; width: 100%; }
	.groups-toolbar {
		display: flex; align-items: center; justify-content: space-between; gap: 16px;
		padding: 16px; border: 1px solid var(--color-border); border-radius: 12px;
		background: var(--color-bg-secondary);
	}
	.groups-search-row { display: flex; align-items: center; gap: 12px; min-width: 0; }
	.groups-search { position: relative; width: min(420px, 100%); }
	.groups-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
	.group-card {
		display: flex; min-width: 0; min-height: 250px; flex-direction: column; justify-content: space-between;
		padding: 18px; border: 1px solid var(--color-border); border-radius: 12px;
		background: var(--color-bg-secondary); box-shadow: 0 1px 2px rgb(15 23 42 / 5%);
	}
	.group-card:hover { border-color: var(--color-border-hover); }
	.group-card-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 12px; }
	.group-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; margin-bottom: 14px; }
	.active-node {
		margin-bottom: 14px; padding: 12px 14px; border: 1px solid var(--color-border);
		border-radius: 9px; background: var(--color-bg-tertiary);
	}
	.member-list { display: flex; flex-wrap: wrap; gap: 7px; max-height: 132px; overflow-y: auto; padding: 2px; }
	@media (max-width: 980px) { .groups-grid { grid-template-columns: 1fr; } }
	@media (max-width: 640px) {
		.groups-toolbar, .groups-search-row { align-items: stretch; flex-direction: column; }
		.groups-search { width: 100%; max-width: none; }
		.group-card { min-height: 0; padding: 14px; }
	}
</style>

<!-- Edit / Create Modal -->
<MihomoGroupEditModal
	open={editModalOpen}
	group={selectedGroup}
	{groups}
	{proxies}
	{subscriptions}
	runtimeProxies={Object.values(runtimeProxies)}
	onClose={() => (editModalOpen = false)}
	onSaved={() => {
		editModalOpen = false;
		onGroupChanged?.();
	}}
	onDelete={(id) => {
		editModalOpen = false;
		handleDelete(id);
	}}
/>

<!-- Delete Confirm Modal -->
{#if deletingId}
	{@const target = groups.find((g) => g.id === deletingId)}
	<Modal
		open={Boolean(deletingId)}
		title="Удаление прокси-группы"
		onclose={() => (deletingId = null)}
	>
		<div class="p-4 space-y-4">
			<p class="text-sm text-[var(--color-text-primary)]">
				Вы действительно хотите удалить группу <strong class="text-[var(--color-error)]">{target?.name}</strong>?
			</p>
			<p class="text-xs text-[var(--color-text-muted)]">
				Правила маршрутизации, ссылавшиеся на эту группу, будут перенаправлены на DIRECT.
			</p>
			<div class="flex justify-end gap-2 pt-2">
				<Button variant="secondary" onclick={() => (deletingId = null)}>Отмена</Button>
				<Button variant="danger" onclick={() => deletingId && handleDelete(deletingId)}>
					Удалить
				</Button>
			</div>
		</div>
	</Modal>
{/if}
