<script lang="ts">
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { Button } from '$lib/components/ui';
	import type { DeviceProxyOutbound, DeviceProxyRuntime } from '$lib/types';

	interface Props {
		outbounds: DeviceProxyOutbound[];
		runtime: DeviceProxyRuntime;
		onSwitched: () => void;
		onSelectRuntime?: (tag: string) => Promise<void>;
		onApplyNow?: () => Promise<void>;
		radioName?: string;
	}

	let {
		outbounds,
		runtime,
		onSwitched,
		onSelectRuntime = async (tag: string) => { await api.selectDeviceProxyRuntime(tag); },
		onApplyNow = async () => { await api.applyDeviceProxy(); },
		radioName = 'device-proxy-active-tunnel'
	}: Props = $props();

	let switching = $state(false);
	let applying = $state(false);
	let revealedDetails = $state<Record<string, boolean>>({});

	function isDetailRevealed(tag: string): boolean {
		return !!revealedDetails[tag];
	}

	function toggleDetailReveal(event: MouseEvent, tag: string) {
		event.preventDefault();
		event.stopPropagation();
		revealedDetails = { ...revealedDetails, [tag]: !revealedDetails[tag] };
	}

	function isSensitiveOutbound(ob: DeviceProxyOutbound): boolean {
		return ob.kind === 'singbox' && ob.tag.startsWith('sub-') && hasEndpoint(ob.detail);
	}

	function hasEndpoint(detail: string): boolean {
		return detail.split(' · ').some(isEndpointPart);
	}

	function maskDetail(detail: string): string {
		if (!detail) return '';
		return detail
			.split(' · ')
			.map((part) => (isEndpointPart(part) ? maskEndpoint(part) : part))
			.join(' · ');
	}

	function isEndpointPart(part: string): boolean {
		const trimmed = part.trim();
		if (!trimmed) return false;

		// host:port, domain:port, IPv4:port, [IPv6]:port
		if (/^\[[0-9a-fA-F:]+\]:\d+$/.test(trimmed)) return true;
		if (/^[a-zA-Z0-9.-]+\:\d+$/.test(trimmed)) return true;
		if (/^\d{1,3}(\.\d{1,3}){3}:\d+$/.test(trimmed)) return true;

		return false;
	}

	function maskEndpoint(endpoint: string): string {
		const trimmed = endpoint.trim();

		const ipv6 = trimmed.match(/^(\[[0-9a-fA-F:]+\])(:\d+)$/);
		if (ipv6) {
			return `[••••]${ipv6[2]}`;
		}

		const hostPort = trimmed.match(/^(.+?)(:\d+)$/);
		if (!hostPort) return endpoint;

		return `${maskHost(hostPort[1])}${hostPort[2]}`;
	}

	function maskHost(host: string): string {
		if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) {
			const parts = host.split('.');
			return `${parts[0]}.•••.•••.${parts[3]}`;
		}

		const parts = host.split('.');
		if (parts.length < 2) {
			return maskHostLabel(host);
		}

		const tld = parts.pop();
		return `${parts.map(maskHostLabel).join('.')}.${tld}`;
	}

	function maskHostLabel(label: string): string {
		if (label.length <= 2) return '••';
		return `${label[0]}••${label[label.length - 1]}`;
	}

	async function handleSelect(tag: string) {
		if (switching || !runtime.alive) return;
		const current = runtime.activeTag || runtime.defaultTag;
		if (tag === current) return;

		switching = true;
		try {
			await onSelectRuntime(tag);
			notifications.success(`Активный туннель: ${labelFor(tag)}`);
			onSwitched();
		} catch (e) {
			notifications.error(`Не удалось переключить: ${(e as Error).message}`);
		} finally {
			switching = false;
		}
	}

	async function applyNow() {
		if (applying) return;
		applying = true;
		try {
			await onApplyNow();
			notifications.success('Перезапуск выполнен, новая конфигурация активна');
			onSwitched();
		} catch (e) {
			notifications.error(`Не удалось применить: ${(e as Error).message}`);
		} finally {
			applying = false;
		}
	}

	function labelFor(tag: string): string {
		const ob = outbounds.find((o) => o.tag === tag);
		return ob?.label ?? tag;
	}

	const currentTag = $derived(runtime.alive ? (runtime.activeTag || runtime.defaultTag) : runtime.defaultTag);
	const isTemporary = $derived(runtime.alive && runtime.activeTag !== '' && runtime.activeTag !== runtime.defaultTag);
	const defaultLabel = $derived(labelFor(runtime.defaultTag));

	type Group = { title: string; items: DeviceProxyOutbound[] };

	const groups = $derived.by<Group[]>(() => {
		const direct = outbounds.filter((o) => o.kind === 'direct');
		const sb = outbounds.filter((o) => o.kind === 'singbox');
		const awg = outbounds.filter((o) => o.kind === 'awg');
		const out: Group[] = [];
		if (direct.length > 0) out.push({ title: '', items: direct });
		if (awg.length > 0) out.push({ title: 'Туннели', items: awg });
		if (sb.length > 0) out.push({ title: 'Sing-box туннели', items: sb });
		return out;
	});
</script>

<section class="card">
	<div class="card-header">
		<h2 class="section-title">Активный туннель</h2>
		<span class={runtime.alive ? 'badge badge-success' : 'badge badge-muted'}>
			{runtime.alive ? 'Работает сейчас' : 'Применится при запуске'}
		</span>
	</div>

	<div class="radio-list" class:disabled={switching || !runtime.alive}>
		{#each groups as group}
			{#if group.title}
				<div class="group-title">{group.title}</div>
			{/if}
			{#each group.items as ob}
				{@const checked = currentTag === ob.tag}
				<label class="option" class:checked>
					<input
						type="radio"
						name={radioName}
						value={ob.tag}
						checked={checked}
						disabled={switching || !runtime.alive}
						onchange={() => handleSelect(ob.tag)}
					/>
					<span class="option-content">
						<span class="option-name">{ob.label || ob.tag}</span>
						{#if ob.detail || (ob.label && ob.label !== ob.tag)}
							{@const sensitive = isSensitiveOutbound(ob)}
							<span class="option-meta">
								<span class="option-meta-text">
									{ob.tag}{ob.detail ? ' · ' + (sensitive && !isDetailRevealed(ob.tag) ? maskDetail(ob.detail) : ob.detail) : ''}
								</span>
								{#if sensitive}
									<button
										type="button"
										class="detail-eye"
										aria-label={isDetailRevealed(ob.tag) ? 'Скрыть адрес сервера' : 'Показать адрес сервера'}
										title={isDetailRevealed(ob.tag) ? 'Скрыть адрес сервера' : 'Показать адрес сервера'}
										onclick={(event) => toggleDetailReveal(event, ob.tag)}
									>
										{#if isDetailRevealed(ob.tag)}
											<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
												<path d="M17.94 17.94A10.94 10.94 0 0 1 12 20C7 20 2.73 16.89 1 12a19.2 19.2 0 0 1 5.06-6.94"/>
												<path d="M10.58 10.58A2 2 0 0 0 12 14a2 2 0 0 0 1.42-.58"/>
												<path d="M9.9 4.24A10.75 10.75 0 0 1 12 4c5 0 9.27 3.11 11 8a19.2 19.2 0 0 1-2.22 3.59"/>
												<line x1="1" y1="1" x2="23" y2="23"/>
											</svg>
										{:else}
											<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
												<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
												<circle cx="12" cy="12" r="3"/>
											</svg>
										{/if}
									</button>
								{/if}
							</span>
						{/if}
					</span>
					<span class="option-check" aria-hidden="true">
						{#if checked}
							<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
								<polyline points="20 6 9 17 4 12"/>
							</svg>
						{/if}
					</span>
				</label>
			{/each}
		{/each}
	</div>

	{#if isTemporary}
		<div class="hint-row">
			<div class="hint-text">
				<span class="badge badge-warning">временно</span>
				После перезапуска вернётся к "{defaultLabel}"
			</div>
			<Button
				variant="ghost"
				size="sm"
				loading={applying}
				onclick={applyNow}
			>
				Применить сейчас
			</Button>
		</div>
	{:else if !runtime.alive}
		<div class="hint-row">
			<div class="hint-text">Запустите sing-box, чтобы переключать вживую.</div>
		</div>
	{/if}
</section>

<style>
	.section-title { font-size: 1rem; font-weight: 600; margin: 0; }

	.card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		margin-bottom: 0.75rem;
	}

	.radio-list {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
		max-height: 360px;
		overflow-y: auto;
	}

	.radio-list.disabled {
		opacity: 0.6;
	}

	.group-title {
		font-size: 0.6875rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
		padding: 0.5rem 0 0.125rem;
	}

	.option {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.625rem 0.875rem;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
		transition: background 0.15s ease, border-color 0.15s ease;
		min-width: 0;
	}

	.option:hover:not(.checked) {
		border-color: var(--color-border-hover);
		background: var(--color-bg-hover);
	}

	.option.checked {
		border-color: var(--color-accent);
		background: rgba(122, 162, 247, 0.08);
	}

	.option input[type='radio'] {
		position: absolute;
		opacity: 0;
		pointer-events: none;
		width: 0;
		height: 0;
	}

	.option-content {
		display: flex;
		flex-direction: column;
		gap: 0.125rem;
		flex: 1;
		min-width: 0;
		overflow: hidden;
	}

	.option-name {
		font-size: 0.875rem;
		color: var(--color-text-primary);
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.option-meta {
		display: flex;
		align-items: center;
		gap: 0.375rem;
		min-width: 0;
		font-family: var(--font-mono);
		font-size: 0.6875rem;
		color: var(--color-text-muted);
	}

	.option-meta-text {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.detail-eye {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 18px;
		height: 18px;
		padding: 0;
		border: none;
		background: transparent;
		color: var(--color-text-muted);
		cursor: pointer;
		flex-shrink: 0;
		border-radius: 4px;
	}

	.detail-eye:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-hover);
	}

	.option-check {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 18px;
		height: 18px;
		flex-shrink: 0;
		color: var(--color-accent);
	}

	.hint-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		margin-top: 0.5rem;
		padding: 0.5rem 0.75rem;
		background: var(--color-bg-tertiary, var(--color-bg-secondary));
		border-radius: 6px;
	}

	.hint-text {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
		font-size: 0.8125rem;
		color: var(--color-text-secondary);
	}

	.badge-muted {
		background: rgba(107, 114, 128, 0.15);
		color: var(--color-text-muted);
	}

	@media (max-width: 640px) {
		.card {
			padding: 0.75rem;
		}

		.card-header {
			align-items: flex-start;
			flex-direction: column;
			gap: 0.5rem;
		}

		.section-title {
			font-size: 0.9375rem;
		}

		.radio-list {
			max-height: none;
		}

		.group-title {
			font-size: 0.625rem;
			padding-top: 0.375rem;
		}

		.option {
			padding: 0.5rem 0.625rem;
			gap: 0.5rem;
			align-items: flex-start;
		}

		.option-name {
			font-size: 0.8125rem;
			white-space: normal;
			overflow-wrap: anywhere;
		}

		.option-meta {
			font-size: 0.625rem;
			gap: 0.25rem;
			align-items: flex-start;
		}

		.option-meta-text {
			white-space: normal;
			overflow: visible;
			text-overflow: initial;
			overflow-wrap: anywhere;
			word-break: break-word;
			line-height: 1.25;
		}

		.option-check {
			width: 16px;
			height: 16px;
			margin-top: 0.125rem;
		}

		.detail-eye {
			width: 16px;
			height: 16px;
			margin-top: 0.125rem;
		}

		.hint-row {
			flex-direction: column;
			align-items: flex-start;
			padding: 0.5rem;
		}

		.hint-text {
			font-size: 0.75rem;
		}
	}
</style>
