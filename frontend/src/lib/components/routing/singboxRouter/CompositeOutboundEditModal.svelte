<script lang="ts">
	import { Button, Dropdown, SegmentedControl, type DropdownOption } from '$lib/components/ui';
	import { X, ChevronUp, ChevronDown } from 'lucide-svelte';
	import type { SegmentedOption } from '$lib/components/ui/segmentedControl';
	import SingboxSettingsModal from './SingboxSettingsModal.svelte';
	import ForeignIfacePanel from './ForeignIfacePanel.svelte';
	import type { SingboxRouterOutbound, SingboxRouterWANInterface } from '$lib/types';
	import type { OutboundGroup } from './outboundOptions';
	import { isSubscriptionOutbound } from '$lib/components/sb-router/outboundLabel';
	import { subscriptionsStore } from '$lib/stores/subscriptions';
	import { resolveMemberLabel } from '$lib/utils/memberLabel';
	import { api } from '$lib/api/client';
	import { bindInterfaceLabel, directBindChoices } from '$lib/utils/bindInterface';

	// Group types supported: urltest, fallback, load-balance, selector, direct.
	// Sing-box 1.15+ custom binary includes native fallback and load-balance.

	interface Props {
		outbound?: SingboxRouterOutbound;
		/** Outbound'ы того же конфига (tproxy или fakeip) — чьи привязки прятать. */
		outbounds: SingboxRouterOutbound[];
		outboundOptions: OutboundGroup[];
		onClose: () => void;
		onSave: (o: SingboxRouterOutbound) => Promise<void> | void;
	}
	let { outbound, outbounds, outboundOptions, onClose, onSave }: Props = $props();

	type OutboundType = 'urltest' | 'fallback' | 'load-balance' | 'selector' | 'direct';

	// svelte-ignore state_referenced_locally
	let type: OutboundType = $state(
		outbound?.type === 'selector' ? 'selector' :
		outbound?.type === 'direct' ? 'direct' :
		outbound?.type === 'fallback' ? 'fallback' :
		(outbound?.type === 'load-balance' || outbound?.type === 'loadbalance') ? 'load-balance' :
		'urltest'
	);
	// svelte-ignore state_referenced_locally
	let tag = $state(outbound?.tag ?? '');
	// svelte-ignore state_referenced_locally
	let members = $state<string[]>([...(outbound?.outbounds ?? [])]);
	// svelte-ignore state_referenced_locally
	let url = $state(outbound?.url ?? 'https://www.gstatic.com/generate_204');
	// svelte-ignore state_referenced_locally
	let interval = $state(outbound?.interval ?? '3m');
	// svelte-ignore state_referenced_locally
	let tolerance = $state(outbound?.tolerance ?? 50);
	// svelte-ignore state_referenced_locally
	let strategy = $state(outbound?.strategy ?? 'round-robin');
	// svelte-ignore state_referenced_locally
	let defaultOutbound = $state(outbound?.default ?? '');
	// svelte-ignore state_referenced_locally
	let bindInterface = $state(outbound?.bind_interface ?? '');
	let bindables = $state<SingboxRouterWANInterface[]>([]);
	let bindablesLoading = $state(true);
	function loadBindables(): Promise<void> {
		return api.singboxRouterListBindableInterfaces()
			.then((l) => { bindables = l; })
			.catch(() => { bindables = []; })
			.finally(() => { bindablesLoading = false; });
	}
	$effect(() => { void loadBindables(); });
	// До ответа роутера — пусто: заглушка «нет в системе» для своей привязки
	// заслонила бы плейсхолдер «Загрузка интерфейсов…».
	const bindChoices = $derived(bindablesLoading ? [] : directBindChoices(bindables, outbounds, outbound));
	const bindableOptions = $derived<DropdownOption[]>(
		bindChoices.map((i) => ({ value: i.name, label: bindInterfaceLabel(i), group: i.foreign ? 'Сторонние' : undefined }))
	);
	async function onForeignPicked(name: string): Promise<void> {
		await loadBindables();
		if (bindChoices.some((i) => i.name === name)) {
			bindInterface = name;
			error = '';
		} else {
			error = 'Интерфейс отмечен, но не появился в списке — обновите диалог';
		}
	}

	let busy = $state(false);
	let error = $state('');
	let memberPicker = $state('');

	// Snapshot initial state for isDirty detection
	let initialType: OutboundType = $state('urltest');
	let initialTag = $state('');
	let initialMembers = $state<string[]>([]);
	let initialUrl = $state('https://www.gstatic.com/generate_204');
	let initialInterval = $state('3m');
	let initialTolerance = $state(50);
	let initialStrategy = $state('round-robin');
	let initialDefaultOutbound = $state('');
	let initialBind = $state('');

	// Initialize snapshot when modal opens
	$effect(() => {
		if (outbound) {
			initialType = outbound.type === 'selector' ? 'selector' :
				outbound.type === 'direct' ? 'direct' :
				outbound.type === 'fallback' ? 'fallback' :
				(outbound.type === 'load-balance' || outbound.type === 'loadbalance') ? 'load-balance' :
				'urltest';
			initialTag = outbound.tag;
			initialMembers = [...(outbound.outbounds ?? [])];
			initialUrl = outbound.url ?? 'https://www.gstatic.com/generate_204';
			initialInterval = outbound.interval ?? '3m';
			initialTolerance = outbound.tolerance ?? 50;
			initialStrategy = outbound.strategy ?? 'round-robin';
			initialDefaultOutbound = outbound.default ?? '';
			initialBind = outbound.bind_interface ?? '';
		} else {
			initialType = 'urltest';
			initialTag = '';
			initialMembers = [];
			initialUrl = 'https://www.gstatic.com/generate_204';
			initialInterval = '3m';
			initialTolerance = 50;
			initialStrategy = 'round-robin';
			initialDefaultOutbound = '';
			initialBind = '';
		}
	});

	const subsData = $derived($subscriptionsStore?.data ?? []);
	const isSubscription = $derived(
		outbound ? isSubscriptionOutbound(outbound, subsData) : false,
	);

	const isDirty = $derived.by(() => {
		const membersChanged =
			!isSubscription && [...members].join(',') !== [...initialMembers].join(',');
		return (
			type !== initialType ||
			tag !== initialTag ||
			membersChanged ||
			url !== initialUrl ||
			interval !== initialInterval ||
			tolerance !== initialTolerance ||
			strategy !== initialStrategy ||
			defaultOutbound !== initialDefaultOutbound ||
			bindInterface !== initialBind
		);
	});

	// Flat options with group labels for the Dropdown native grouping.
	// Filter out tags already added so the user can't pick duplicates, and
	// the group's own tag so it can't reference itself (self-reference
	// FATALs sing-box with a circular-dependency error).
	// Also filter out "direct" for urltest and load-balance (disallowed by validator).
	const memberDropdownOptions = $derived<DropdownOption[]>(
		outboundOptions.flatMap((g) =>
			g.items
				.filter((i) => {
					if (members.includes(i.value) || i.value === tag.trim()) return false;
					if ((type === 'urltest' || type === 'load-balance') && i.value === 'direct') return false;
					return true;
				})
				.map((i) => ({ value: i.value, label: i.label, group: g.group }))
		)
	);

	// Advisory: warn when the group's tag matches an existing outbound's
	// display name (e.g. a composite named "DE" like the AWG tunnel "DE").
	// Tags are the real identifier, so this is not an error — but the name
	// clash is exactly what leads users to add the wrong "DE" as a member.
	const tagCollision = $derived.by(() => {
		const t = tag.trim();
		if (!t) return false;
		return outboundOptions.some((g) =>
			g.items.some((i) => i.value !== t && i.label.replace(/\s*\(.*\)\s*$/, '') === t)
		);
	});

	// Default-picker options: only members already chosen. Подписочные
	// тэги (sub-XXX-YYY) и awg-XXX тэги резолвим в человеческие labels —
	// тот же UX, что и на карточке composite outbound (issue #214).
	function memberLabel(tag: string): string {
		return resolveMemberLabel(tag, subsData, outboundOptions);
	}
	const defaultOptions = $derived<DropdownOption[]>(
		members.map((m) => ({ value: m, label: memberLabel(m) }))
	);

	function addMember(v: string): void {
		if (isSubscription) return;
		if (!v) return;
		if (members.includes(v)) return;
		members = [...members, v];
		// Reset picker so the same slot can be reused for the next addition.
		memberPicker = '';
	}

	function removeMember(v: string): void {
		if (isSubscription) return;
		members = members.filter((m) => m !== v);
		if (defaultOutbound === v) defaultOutbound = '';
	}

	function moveMember(index: number, direction: -1 | 1): void {
		if (isSubscription) return;
		const targetIndex = index + direction;
		if (targetIndex < 0 || targetIndex >= members.length) return;
		const copy = [...members];
		const temp = copy[index];
		copy[index] = copy[targetIndex];
		copy[targetIndex] = temp;
		members = copy;
	}

	async function save(): Promise<void> {
		busy = true;
		error = '';
		try {
			if (!tag.trim()) {
				error = 'Tag обязателен';
				busy = false;
				return;
			}

			let built: SingboxRouterOutbound;
			if (type === 'direct') {
				if (!bindInterface) {
					error = 'Выберите интерфейс';
					busy = false;
					return;
				}
				built = { type: 'direct', tag: tag.trim(), bind_interface: bindInterface };
			} else {
				const memberList = isSubscription && outbound ? [...(outbound.outbounds ?? [])] : [...members];
				if (memberList.length < 2) {
					error = 'Нужно минимум 2 члена';
					busy = false;
					return;
				}
				built = {
					type,
					tag: tag.trim(),
					outbounds: memberList,
				};
				if (outbound?.source === 'subscription') {
					built.source = 'subscription';
				}
				if (type === 'urltest') {
					built.url = url;
					built.interval = interval;
					built.tolerance = tolerance;
				} else if (type === 'fallback') {
					built.url = url;
					built.interval = interval;
					built.interrupt_exist_connections = true;
				} else if (type === 'load-balance') {
					built.strategy = strategy || 'round-robin';
					built.url = url;
					built.interval = interval;
				} else if (type === 'selector') {
					built.default = defaultOutbound || members[0];
				}
			}

			await onSave(built);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	const typeOptions = $derived<SegmentedOption<OutboundType>[]>([
		{ value: 'urltest', label: 'URLTest', disabled: !!outbound && outbound.type === 'direct' },
		{ value: 'fallback', label: 'Fallback', disabled: !!outbound && outbound.type === 'direct' },
		{ value: 'load-balance', label: 'Load-Balance', disabled: !!outbound && outbound.type === 'direct' },
		{ value: 'selector', label: 'Selector', disabled: !!outbound && outbound.type === 'direct' },
		{ value: 'direct', label: 'Интерфейс', disabled: !!outbound && outbound.type !== 'direct' },
	]);

	const strategyOptions: DropdownOption[] = [
		{ value: 'round-robin', label: 'Round-Robin (поочерёдно)' },
		{ value: 'consistent-hashing', label: 'Consistent Hashing (по сессиям / хешу)' },
	];

	const typeDescription = $derived(
		type === 'direct'
			? 'Прямое соединение через выбранный интерфейс (IPSec/IKEv2/др. VPN). Трафик правила пойдёт через этот интерфейс.'
			: type === 'urltest'
				? 'Периодически пингует каждого члена и автоматически направляет через самого быстрого.'
				: type === 'fallback'
					? 'Резервирование: направляет трафик через первого доступного участника по порядку списка.'
					: type === 'load-balance'
						? 'Балансировка: распределяет подключения между всеми участниками по выбранной стратегии.'
						: 'Ручное переключение через Clash API. Новые подключения идут через выбранный default.'
	);
</script>

<SingboxSettingsModal
	title={outbound ? 'Редактировать outbound' : 'Новый outbound'}
	onClose={onClose}
	size="lg"
	hasUnsavedChanges={() => isDirty}
>
	<div class="form">
		<div class="field type-field">
			<div class="lbl">Тип</div>
			<SegmentedControl
				value={type}
				options={typeOptions}
				ariaLabel="Тип outbound"
				onchange={(next) => (type = next)}
			/>
			<div class="type-hint">{typeDescription}</div>
		</div>

		<label class="field">
			<div class="lbl">Tag (имя)</div>
			<input bind:value={tag} placeholder="fast-de" />
			{#if tagCollision}
				<div class="tag-warn">⚠ Имя совпадает с именем существующего туннеля — их легко перепутать в списке участников. Лучше дать группе отличающееся имя.</div>
			{/if}
		</label>

		{#if type !== 'direct'}
			<div class="field">
				<div class="lbl">{isSubscription ? 'Участники' : 'Members (минимум 2)'}</div>	
				<div class="member-chips" class:empty={members.length === 0}>
					{#if members.length === 0}
						<span class="chips-placeholder">Участники не выбраны</span>
					{:else}
						{#each members as m, i (m)}
							<span class="member-chip" title={m}>
								<span class="member-chip-order" class:primary={i === 0}>
									{#if type === 'fallback'}
										{i === 0 ? '#1 (Основной)' : `#${i + 1} (Резерв ${i})`}
									{:else}
										#{i + 1}
									{/if}
								</span>
								<span class="member-chip-label">{memberLabel(m)}</span>
								{#if !isSubscription}
									<div class="member-chip-actions">
										{#if i > 0}
											<button
												type="button"
												class="member-chip-nav"
												aria-label="Вверх"
												title="Поднять выше (к #1)"
												onclick={() => moveMember(i, -1)}
											>
												<ChevronUp size={13} />
											</button>
										{/if}
										{#if i < members.length - 1}
											<button
												type="button"
												class="member-chip-nav"
												aria-label="Вниз"
												title="Опустить ниже"
												onclick={() => moveMember(i, 1)}
											>
												<ChevronDown size={13} />
											</button>
										{/if}
										<button
											type="button"
											class="member-chip-remove"
											aria-label={`Удалить ${m}`}
											title="Удалить"
											onclick={() => removeMember(m)}
										>
											<X size={14} strokeWidth={2.5} aria-hidden="true" />
										</button>
									</div>
								{/if}
							</span>
						{/each}
					{/if}
				</div>
				{#if isSubscription}
					<div class="type-hint">Список редактируется в разделе «Туннели» → «Прокси» → «Подписки»</div>
				{/if}
				{#if !isSubscription}
					<Dropdown
						value={memberPicker}
						options={memberDropdownOptions}
						placeholder="Добавить участника"
						onchange={addMember}
						fullWidth
					/>
				{/if}
			</div>

			{#if type === 'urltest'}
				<label class="field">
					<div class="lbl">Test URL</div>
					<input bind:value={url} />
				</label>
				<div class="row2">
					<label class="field">
						<div class="lbl">Interval</div>
						<input bind:value={interval} placeholder="3m" />
					</label>
					<label class="field">
						<div class="lbl">Tolerance (ms)</div>
						<input type="number" bind:value={tolerance} />
					</label>
				</div>
			{:else if type === 'fallback'}
				<label class="field">
					<div class="lbl">Test URL</div>
					<input bind:value={url} />
				</label>
				<label class="field">
					<div class="lbl">Interval</div>
					<input bind:value={interval} placeholder="30s" />
				</label>
			{:else if type === 'load-balance'}
				<div class="field">
					<div class="lbl">Стратегия балансировки</div>
					<Dropdown
						bind:value={strategy}
						options={strategyOptions}
						fullWidth
					/>
				</div>
				<label class="field">
					<div class="lbl">Test URL</div>
					<input bind:value={url} />
				</label>
				<label class="field">
					<div class="lbl">Interval</div>
					<input bind:value={interval} placeholder="3m" />
				</label>
			{:else if type === 'selector'}
				<div class="field">
					<div class="lbl">Default (один из members)</div>
					<Dropdown
						bind:value={defaultOutbound}
						options={defaultOptions}
						placeholder={members.length === 0 ? 'Сначала добавьте участников' : '— выбрать —'}
						disabled={members.length === 0}
						fullWidth
					/>
				</div>
			{/if}
		{/if}

		{#if type === 'direct'}
			<div class="field">
				<div class="lbl">Интерфейс</div>
				<Dropdown
					bind:value={bindInterface}
					options={bindableOptions}
					placeholder={bindablesLoading ? 'Загрузка интерфейсов…' : bindChoices.length === 0 ? 'Нет доступных интерфейсов' : '— выбрать интерфейс —'}
					disabled={bindablesLoading || bindChoices.length === 0}
					fullWidth
				/>
				<ForeignIfacePanel onpicked={onForeignPicked} />
			</div>
		{/if}

		{#if error}<div class="error">{error}</div>{/if}
	</div>

	{#snippet actions()}
		<Button variant="ghost" size="md" onclick={onClose} type="button">Отмена</Button>
		<Button variant="primary" size="md" onclick={save} disabled={busy} loading={busy} type="button">
			Сохранить
		</Button>
	{/snippet}
</SingboxSettingsModal>
