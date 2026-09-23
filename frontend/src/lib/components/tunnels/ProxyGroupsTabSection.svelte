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

<div class="proxy-groups-page space-y-3">
	<!-- Toolbar -->
	<div class="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-2.5 bg-[var(--color-bg-secondary)] px-3.5 py-2.5 rounded-xl border border-[var(--color-border)]">
		<div class="flex items-center gap-2.5 flex-1 min-w-0">
			<div class="relative flex-1 max-w-sm">
				<Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--color-text-muted)]" />
				<input
					type="text"
					bind:value={searchQuery}
					placeholder="Поиск групп или узлов..."
					class="w-full pl-8 pr-3 py-1.5 text-xs bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent)]"
				/>
			</div>
			<span class="text-xs text-[var(--color-text-muted)] whitespace-nowrap hidden sm:inline">
				Групп: <strong class="text-[var(--color-text-primary)]">{groups.length}</strong>
			</span>
		</div>

		<div class="flex items-center gap-2 shrink-0">
			<Button variant="primary" size="sm" onclick={openCreate}>
				<Plus class="w-3.5 h-3.5 mr-1" />
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
					<Button variant="primary" size="sm" onclick={openCreate}>
						<Plus class="w-3.5 h-3.5 mr-1" />
						Создать группу
					</Button>
				{/snippet}
			</EmptyState>
		{/if}
	{:else}
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-3">
			{#each filteredGroups as group (group.id || group.name)}
				{@const rt = runtimeProxies[group.name]}
				{@const activeNow = rt?.now || (group.proxies?.[0] ?? '')}
				{@const isSusaninTarget = susaninEgressId === group.id || susaninEgressId === group.name}
				<div class="group-card bg-[var(--color-bg-secondary)] rounded-xl border border-[var(--color-border)] p-3.5 flex flex-col justify-between hover:border-[var(--color-border-hover)] transition-all shadow-xs">
					<div class="space-y-2.5">
						<!-- Compact Header Row -->
						<div class="flex items-start justify-between gap-2">
							<div class="flex items-center gap-1.5 flex-wrap min-w-0">
								<span class="w-2 h-2 rounded-full shrink-0 {group.enabled !== false ? 'bg-[var(--color-success)]' : 'bg-gray-400'}" title={group.enabled !== false ? 'Включена' : 'Отключена'}></span>
								<Layers class="w-4 h-4 text-[var(--color-accent)] shrink-0" />
								<h3 class="font-semibold text-sm text-[var(--color-text-primary)] leading-tight truncate">
									{group.name}
								</h3>
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

							<!-- Action Buttons -->
							<div class="flex items-center gap-1 shrink-0">
								<button
									class="p-1.5 rounded-lg hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-accent)] transition-colors cursor-pointer"
									title="Проверить задержку группы"
									disabled={testingDelay[group.name]}
									onclick={() => testGroupDelay(group.name)}
								>
									<Activity class="w-3.5 h-3.5 {testingDelay[group.name] ? 'animate-spin text-[var(--color-accent)]' : ''}" />
								</button>
								<button
									class="p-1.5 rounded-lg hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-accent)] transition-colors cursor-pointer"
									title="Редактировать группу"
									onclick={() => openEdit(group)}
								>
									<Pencil class="w-3.5 h-3.5" />
								</button>
								<button
									class="p-1.5 rounded-lg hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-muted)] hover:text-[var(--color-error)] transition-colors cursor-pointer"
									title="Удалить группу"
									onclick={() => (deletingId = group.id)}
								>
									<Trash2 class="w-3.5 h-3.5" />
								</button>
							</div>
						</div>

						<!-- Compact Active Node Bar -->
						<div class="flex items-center justify-between gap-2 px-2.5 py-1.5 rounded-lg bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] text-xs">
							<div class="flex items-center gap-1.5 min-w-0">
								<Zap class="w-3.5 h-3.5 text-amber-500 shrink-0" />
								<span class="text-[10px] uppercase tracking-wider font-semibold text-[var(--color-text-muted)] shrink-0">Активен:</span>
								<span class="font-mono text-xs text-[var(--color-text-primary)] font-medium truncate" title={activeNow}>
									{memberLabel(activeNow) || 'Не определён'}
								</span>
							</div>

							<div class="flex items-center gap-2 shrink-0">
								{#if delays[group.name]}
									<span class="text-[11px] font-mono font-medium text-[var(--color-success)] bg-[var(--color-success)]/10 px-1.5 py-0.5 rounded">
										{delays[group.name]} мс
									</span>
								{/if}
								{#if group.type === 'select'}
									<span class="text-[10px] text-[var(--color-accent)] hidden sm:inline">кликните узел для выбора</span>
								{/if}
							</div>
						</div>

						<!-- Member Chips -->
						{#if (group.proxies || []).length > 0}
							<div class="space-y-1">
								<div class="flex items-center justify-between text-[10px] uppercase tracking-wider font-semibold text-[var(--color-text-muted)]">
									<span>Узлы ({group.proxies?.length || 0})</span>
									{#if (group.use || []).length > 0}
										<span>+ {group.use.length} подписок</span>
									{/if}
								</div>
								<div class="flex flex-wrap gap-1.5 max-h-28 overflow-y-auto py-0.5">
									{#each group.proxies || [] as member}
										{@const isSelected = activeNow === member}
										{#if group.type === 'select'}
											<button
												class="px-2 py-1 text-xs rounded-md border transition-all flex items-center gap-1 cursor-pointer {isSelected ? 'bg-[var(--color-accent-tint)] border-[var(--color-accent)] text-[var(--color-accent)] font-medium shadow-xs' : 'bg-[var(--color-bg-tertiary)] border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:text-[var(--color-text-primary)]'}"
												disabled={switchingMember[group.name]}
												onclick={() => selectMember(group.name, member)}
												title="Сделать узел активным"
											>
												{#if isSelected}
													<Check class="w-3 h-3 text-[var(--color-accent)] shrink-0" />
												{/if}
												<span class="truncate max-w-[200px]" title={member}>{memberLabel(member)}</span>
											</button>
										{:else}
											<span class="px-2 py-0.5 text-xs rounded-md border flex items-center gap-1 {isSelected ? 'bg-[var(--color-bg-tertiary)] border-[var(--color-success)] text-[var(--color-success)] font-medium' : 'bg-[var(--color-bg-tertiary)] border-[var(--color-border)] text-[var(--color-text-secondary)]'}">
												{#if isSelected}
													<Check class="w-3 h-3 text-[var(--color-success)] shrink-0" />
												{/if}
												<span class="truncate max-w-[200px]" title={member}>{memberLabel(member)}</span>
											</span>
										{/if}
									{/each}
								</div>
							</div>
						{/if}
					</div>

					<!-- Card Footer info -->
					{#if group.interval || group.tolerance || group.lazy}
						<div class="mt-2.5 pt-2 border-t border-[var(--color-border)] text-[11px] text-[var(--color-text-muted)] flex items-center justify-between">
							<div class="flex items-center gap-2">
								{#if group.interval}
									<span>Интервал: <strong class="text-[var(--color-text-primary)]">{group.interval} с</strong></span>
								{/if}
								{#if group.tolerance}
									<span>Допуск: <strong class="text-[var(--color-text-primary)]">{group.tolerance} мс</strong></span>
								{/if}
							</div>
							{#if group.lazy}
								<span class="text-[10px] font-mono bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 rounded border border-[var(--color-border)]">lazy check</span>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

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
