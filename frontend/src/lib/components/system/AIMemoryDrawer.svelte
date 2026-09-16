<script lang="ts">
	import { SideDrawer, Button } from '$lib/components/ui';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import type { AIMemoryData, AIMemoryFact, AILearnedPlaybook, AILearningJournalEntry, AISentinelSettings } from '$lib/types';
	import {
		Brain,
		Sparkles,
		ShieldCheck,
		ShieldAlert,
		BookOpen,
		History,
		Trash2,
		Plus,
		RefreshCw,
		CheckCircle2,
		AlertTriangle,
		Bot,
		Cpu,
		Clock,
		ExternalLink,
	} from 'lucide-svelte';

	interface Props {
		open: boolean;
		onClose: () => void;
	}

	let { open, onClose }: Props = $props();

	let activeTab = $state<'facts' | 'playbooks' | 'sentinel' | 'journal'>('facts');
	let loading = $state(false);
	let error = $state<string | null>(null);
	let memoryData = $state<AIMemoryData | null>(null);

	// New fact form
	let newFactCategory = $state('network');
	let newFactContent = $state('');
	let addingFact = $state(false);

	// Sentinel settings edit
	let sentinelEnabled = $state(true);
	let sentinelAutonomy = $state<'disabled' | 'notify_only' | 'safe_auto'>('notify_only');
	let sentinelInterval = $state(60);
	let savingSentinel = $state(false);

	$effect(() => {
		if (open) {
			loadData();
		}
	});

	async function loadData() {
		loading = true;
		error = null;
		try {
			const res = await api.systemAIMemory();
			memoryData = res;
			if (res.settings) {
				sentinelEnabled = res.settings.enabled;
				sentinelAutonomy = (res.settings.autonomyLevel as any) || 'notify_only';
				sentinelInterval = res.settings.intervalSeconds || 60;
			}
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function addFact() {
		if (!newFactContent.trim()) return;
		addingFact = true;
		try {
			await api.systemAIAddFact({
				category: newFactCategory,
				content: newFactContent.trim(),
				source: 'user',
			});
			newFactContent = '';
			notifications.success('Факт успешно сохранён в память ассистента');
			await loadData();
		} catch (e) {
			notifications.error('Ошибка сохранения: ' + (e instanceof Error ? e.message : String(e)));
		} finally {
			addingFact = false;
		}
	}

	async function deleteFact(id: string) {
		try {
			await api.systemAIDeleteFact(id);
			notifications.success('Факт удалён');
			await loadData();
		} catch (e) {
			notifications.error('Ошибка удаления: ' + (e instanceof Error ? e.message : String(e)));
		}
	}

	async function deletePlaybook(id: string) {
		try {
			await api.systemAIDeletePlaybook(id);
			notifications.success('Playbook удалён');
			await loadData();
		} catch (e) {
			notifications.error('Ошибка удаления: ' + (e instanceof Error ? e.message : String(e)));
		}
	}

	async function saveSentinel() {
		savingSentinel = true;
		try {
			await api.systemAISaveSentinel({
				enabled: sentinelEnabled,
				autonomyLevel: sentinelAutonomy,
				intervalSeconds: Number(sentinelInterval) || 60,
			});
			notifications.success('Настройки Sentinel сохранены');
			await loadData();
		} catch (e) {
			notifications.error('Ошибка сохранения: ' + (e instanceof Error ? e.message : String(e)));
		} finally {
			savingSentinel = false;
		}
	}

	function formatDate(d: string | undefined): string {
		if (!d) return '—';
		try {
			const date = new Date(d);
			return date.toLocaleString('ru-RU', {
				day: 'numeric',
				month: 'short',
				hour: '2-digit',
				minute: '2-digit',
			});
		} catch {
			return d;
		}
	}
</script>

<SideDrawer {open} {onClose} title="Память и самообучение ИИ" width={680}>
	<div class="space-y-4 text-sm text-foreground">
		<!-- Top Sentinel Status Banner -->
		<div class="p-3.5 rounded-xl border border-primary/20 bg-primary/5 flex items-center justify-between gap-3">
			<div class="flex items-center gap-2.5">
				<div class="w-8 h-8 rounded-lg bg-primary/20 text-primary flex items-center justify-center">
					<Brain class="w-4 h-4" />
				</div>
				<div>
					<div class="flex items-center gap-2">
						<span class="font-semibold text-foreground text-xs uppercase tracking-wider">Фоновый Sentinel</span>
						{#if memoryData?.sentinel?.running && memoryData?.sentinel?.enabled && memoryData?.sentinel?.autonomyLevel !== 'disabled'}
							<span class="px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20 flex items-center gap-1">
								<span class="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
								Активен ({sentinelAutonomy === 'safe_auto' ? 'Авто-исправление' : 'Рекомендации'})
							</span>
						{:else}
							<span class="px-2 py-0.5 rounded-full text-[11px] font-medium bg-muted text-muted-foreground border border-border">
								Выключен
							</span>
						{/if}
					</div>
					<div class="text-xs text-muted-foreground mt-0.5">
						{#if memoryData?.sentinel?.lastCheck}
							Последняя проверка: {formatDate(memoryData.sentinel.lastCheck)}
						{:else}
							Ожидание первого цикла проверки
						{/if}
						{#if memoryData?.sentinel?.lastAction}
							• Действие: <span class="font-mono text-[11px] text-foreground">{memoryData.sentinel.lastAction}</span>
						{/if}
					</div>
				</div>
			</div>
			<Button variant="ghost" size="sm" onclick={loadData} disabled={loading}>
				<RefreshCw class="w-3.5 h-3.5 {loading ? 'animate-spin' : ''}" />
			</Button>
		</div>

		<!-- Navigation Tabs -->
		<div class="flex items-center gap-1 border-b border-border pb-1">
			<button
				type="button"
				class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 {activeTab === 'facts' ? 'bg-secondary text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
				onclick={() => (activeTab = 'facts')}
			>
				<BookOpen class="w-3.5 h-3.5" />
				База фактов
				<span class="px-1.5 py-0.2 rounded-full text-[10px] bg-primary/10 text-primary">{memoryData?.facts?.length || 0}</span>
			</button>

			<button
				type="button"
				class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 {activeTab === 'playbooks' ? 'bg-secondary text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
				onclick={() => (activeTab = 'playbooks')}
			>
				<Sparkles class="w-3.5 h-3.5" />
				Playbooks
				<span class="px-1.5 py-0.2 rounded-full text-[10px] bg-primary/10 text-primary">{memoryData?.playbooks?.length || 0}</span>
			</button>

			<button
				type="button"
				class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 {activeTab === 'sentinel' ? 'bg-secondary text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
				onclick={() => (activeTab = 'sentinel')}
			>
				<ShieldCheck class="w-3.5 h-3.5" />
				Автономия
			</button>

			<button
				type="button"
				class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 {activeTab === 'journal' ? 'bg-secondary text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
				onclick={() => (activeTab = 'journal')}
			>
				<History class="w-3.5 h-3.5" />
				Журнал обучения
				<span class="px-1.5 py-0.2 rounded-full text-[10px] bg-primary/10 text-primary">{memoryData?.journal?.length || 0}</span>
			</button>
		</div>

		{#if error}
			<div class="p-3 rounded-lg bg-destructive/10 border border-destructive/20 text-destructive text-xs">
				{error}
			</div>
		{/if}

		<!-- TAB 1: FACTS -->
		{#if activeTab === 'facts'}
			<div class="space-y-3">
				<!-- Add fact form -->
				<div class="p-3 rounded-xl border border-border bg-card/60 space-y-2.5">
					<div class="flex items-center justify-between">
						<span class="font-medium text-xs text-foreground">Запомнить новый факт о роутере или сети</span>
					</div>
					<div class="flex gap-2">
						<select
							bind:value={newFactCategory}
							class="h-8 px-2 rounded-lg border border-border bg-background text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
						>
							<option value="network">Сеть / LAN</option>
							<option value="routing">Маршрутизация</option>
							<option value="device">Устройство / MWS</option>
							<option value="user_pref">Предпочтение</option>
							<option value="troubleshooting">Диагностика</option>
						</select>
						<input
							type="text"
							placeholder="Например: Провайдер блокирует UDP 443 на внешнем шлюзе"
							bind:value={newFactContent}
							onkeydown={(e) => e.key === 'Enter' && addFact()}
							class="flex-1 h-8 px-3 rounded-lg border border-border bg-background text-xs text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
						/>
						<Button variant="primary" size="sm" onclick={addFact} disabled={addingFact || !newFactContent.trim()}>
							<Plus class="w-3.5 h-3.5 mr-1" />
							Запомнить
						</Button>
					</div>
					<p class="text-[11px] text-muted-foreground">
						Факты автоматически внедряются в системный контекст модели при каждом запросе, позволяя ассистенту учитывать топологию и индивидуальные особенности вашей сети.
					</p>
				</div>

				<!-- Facts list -->
				{#if !memoryData?.facts || memoryData.facts.length === 0}
					<div class="p-6 text-center text-xs text-muted-foreground border border-dashed border-border rounded-xl">
						В памяти пока нет сохранённых фактов. Ассистент запоминает их автоматически при диалоге или вы можете добавить факт выше.
					</div>
				{:else}
					<div class="space-y-2">
						{#each memoryData.facts as fact (fact.id)}
							<div class="p-3 rounded-xl border border-border bg-card hover:border-border/80 transition-colors flex items-start justify-between gap-3">
								<div class="space-y-1">
									<div class="flex items-center gap-2">
										<span class="px-2 py-0.5 rounded-md text-[10px] font-semibold tracking-wider uppercase bg-primary/10 text-primary border border-primary/20">
											{fact.category}
										</span>
										<span class="text-[11px] text-muted-foreground">
											{formatDate(fact.updatedAt || fact.createdAt)}
										</span>
										{#if fact.source === 'user'}
											<span class="text-[10px] text-muted-foreground bg-secondary px-1.5 py-0.2 rounded">пользователь</span>
										{:else}
											<span class="text-[10px] text-muted-foreground bg-secondary px-1.5 py-0.2 rounded">ассистент</span>
										{/if}
									</div>
									<p class="text-xs text-foreground font-medium">{fact.content}</p>
								</div>
								<button
									type="button"
									class="text-muted-foreground hover:text-destructive p-1 rounded transition-colors"
									title="Удалить факт"
									onclick={() => deleteFact(fact.id)}
								>
									<Trash2 class="w-3.5 h-3.5" />
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		<!-- TAB 2: PLAYBOOKS -->
		{#if activeTab === 'playbooks'}
			<div class="space-y-3">
				{#if !memoryData?.playbooks || memoryData.playbooks.length === 0}
					<div class="p-6 text-center text-xs text-muted-foreground border border-dashed border-border rounded-xl space-y-2">
						<p>Пока нет выученных сценариев решений (Playbooks).</p>
						<p class="text-[11px]">
							Когда ИИ-ассистент или фоновый Sentinel успешно устраняют сбой (например, перезапуск упавшего туннеля или релоад Mihomo), сценарий сохраняется сюда и используется для мгновенного исправления похожих сбоев в будущем.
						</p>
					</div>
				{:else}
					<div class="space-y-2">
						{#each memoryData.playbooks as pb (pb.id)}
							<div class="p-3.5 rounded-xl border border-border bg-card space-y-2">
								<div class="flex items-start justify-between gap-2">
									<div class="space-y-1">
										<div class="flex items-center gap-2">
											<span class="font-semibold text-xs text-foreground">{pb.title}</span>
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-secondary text-secondary-foreground font-mono">
												{pb.category}
											</span>
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-emerald-500/10 text-emerald-600 font-medium">
												Применено: {pb.successCount} раз
											</span>
										</div>
										<div class="text-xs text-muted-foreground">
											<span class="font-medium text-foreground/80">Симптом:</span>
											<span class="font-mono text-[11px] bg-muted px-1.5 py-0.2 rounded ml-1">{pb.trigger}</span>
										</div>
									</div>
									<button
										type="button"
										class="text-muted-foreground hover:text-destructive p-1 rounded transition-colors"
										title="Удалить playbook"
										onclick={() => deletePlaybook(pb.id)}
									>
										<Trash2 class="w-3.5 h-3.5" />
									</button>
								</div>

								<div class="p-2.5 rounded-lg bg-muted/40 border border-border/60 text-xs space-y-1">
									{#if pb.diagnosis}
										<div>
											<span class="text-muted-foreground">Диагноз:</span> {pb.diagnosis}
										</div>
									{/if}
									<div class="flex items-center gap-2">
										<span class="text-muted-foreground">Действие:</span>
										<span class="font-mono text-[11px] text-primary font-semibold">
											{pb.action}{pb.target ? ` (${pb.target})` : ''}
										</span>
									</div>
								</div>

								<div class="flex items-center justify-between text-[11px] text-muted-foreground pt-0.5">
									<span>Источник: {pb.learnedFrom}</span>
									{#if pb.lastUsedAt}
										<span>Использован: {formatDate(pb.lastUsedAt)}</span>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		<!-- TAB 3: SENTINEL SETTINGS -->
		{#if activeTab === 'sentinel'}
			<div class="space-y-4">
				<div class="p-4 rounded-xl border border-border bg-card space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<div class="font-medium text-xs text-foreground">Включить фонового часового (Sentinel)</div>
							<div class="text-[11px] text-muted-foreground mt-0.5">Периодический мониторинг туннелей, MWS и маршрутизации</div>
						</div>
						<input
							type="checkbox"
							bind:checked={sentinelEnabled}
							class="w-4 h-4 rounded border-border text-primary focus:ring-primary"
						/>
					</div>

					<div class="space-y-2 border-t border-border pt-3">
						<div class="font-medium text-xs text-foreground">Режим автономии</div>
						<div class="grid grid-cols-1 gap-2">
							<label class="p-2.5 rounded-lg border border-border bg-card/60 flex items-start gap-2.5 cursor-pointer hover:bg-secondary/40 transition-colors">
								<input
									type="radio"
									name="autonomy"
									value="notify_only"
									bind:group={sentinelAutonomy}
									class="mt-0.5 text-primary focus:ring-primary"
								/>
								<div>
									<div class="font-medium text-xs text-foreground">Только уведомления (Рекомендуется)</div>
									<div class="text-[11px] text-muted-foreground">При обнаружении сбоя Sentinel подбирает решение и создаёт кнопку исправления в интерфейсе, ожидая вашего клика.</div>
								</div>
							</label>

							<label class="p-2.5 rounded-lg border border-border bg-card/60 flex items-start gap-2.5 cursor-pointer hover:bg-secondary/40 transition-colors">
								<input
									type="radio"
									name="autonomy"
									value="safe_auto"
									bind:group={sentinelAutonomy}
									class="mt-0.5 text-primary focus:ring-primary"
								/>
								<div>
									<div class="font-medium text-xs text-foreground">Безопасное авто-исправление (Safe Auto)</div>
									<div class="text-[11px] text-muted-foreground">Автоматически перезапускает упавшие туннели и движки, если для этого сбоя уже есть проверенный успешный Playbook.</div>
								</div>
							</label>

							<label class="p-2.5 rounded-lg border border-border bg-card/60 flex items-start gap-2.5 cursor-pointer hover:bg-secondary/40 transition-colors">
								<input
									type="radio"
									name="autonomy"
									value="disabled"
									bind:group={sentinelAutonomy}
									class="mt-0.5 text-primary focus:ring-primary"
								/>
								<div>
									<div class="font-medium text-xs text-foreground">Отключено</div>
									<div class="text-[11px] text-muted-foreground">Sentinel не выполняет фоновых проверок.</div>
								</div>
							</label>
						</div>
					</div>

					<div class="space-y-1.5 border-t border-border pt-3">
						<label class="font-medium text-xs text-foreground block">
							Интервал фоновых проверок (секунд)
						</label>
						<input
							type="number"
							min="10"
							max="600"
							bind:value={sentinelInterval}
							class="w-32 h-8 px-2.5 rounded-lg border border-border bg-background text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
						/>
						<p class="text-[11px] text-muted-foreground">По умолчанию 60 секунд. Проверки легковесны и не нагружают процессор роутера.</p>
					</div>

					<div class="pt-2">
						<Button variant="primary" size="sm" onclick={saveSentinel} disabled={savingSentinel}>
							<CheckCircle2 class="w-3.5 h-3.5 mr-1.5" />
							Сохранить настройки Sentinel
						</Button>
					</div>
				</div>
			</div>
		{/if}

		<!-- TAB 4: JOURNAL -->
		{#if activeTab === 'journal'}
			<div class="space-y-3">
				{#if !memoryData?.journal || memoryData.journal.length === 0}
					<div class="p-6 text-center text-xs text-muted-foreground border border-dashed border-border rounded-xl">
						Журнал самообучения пока пуст. События мониторинга, консультаций с облаком и авто-исправлений будут появляться здесь.
					</div>
				{:else}
					<div class="space-y-2">
						{#each memoryData.journal as entry (entry.id)}
							<div class="p-3 rounded-xl border border-border bg-card text-xs space-y-1.5">
								<div class="flex items-center justify-between">
									<div class="flex items-center gap-2">
										{#if entry.outcome === 'success'}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-emerald-500/10 text-emerald-600 font-medium">УСПЕХ</span>
										{:else if entry.outcome === 'failed'}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-destructive/10 text-destructive font-medium">СБОЙ</span>
										{:else if entry.outcome === 'learned'}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-primary/10 text-primary font-medium">ВЫУЧЕНО</span>
										{:else}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-secondary text-secondary-foreground font-medium">ОЖИДАНИЕ</span>
										{/if}
										<span class="font-mono text-[11px] text-foreground font-medium">{entry.trigger}</span>
									</div>
									<span class="text-[11px] text-muted-foreground">{formatDate(entry.timestamp)}</span>
								</div>

								{#if entry.query}
									<div class="text-[11px] text-muted-foreground">
										{entry.query}
									</div>
								{/if}

								{#if entry.cloudAdvice}
									<div class="p-2 rounded bg-muted/40 border border-border/50 text-[11px] text-foreground/90">
										<span class="text-muted-foreground">ИИ-диагноз:</span> {entry.cloudAdvice}
									</div>
								{/if}

								{#if entry.actionTaken}
									<div class="flex items-center gap-1.5 text-[11px]">
										<span class="text-muted-foreground">Действие:</span>
										<span class="font-mono text-primary font-medium">{entry.actionTaken}</span>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>
</SideDrawer>
