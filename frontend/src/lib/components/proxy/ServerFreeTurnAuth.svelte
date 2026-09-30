<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { Button, FieldHint } from '$lib/components/ui';
	import { RefreshCw, Share2 } from 'lucide-svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { errText } from '$lib/utils/errorMessage';
	import { servers, type ServersSnapshot } from '$lib/stores/servers';
	import {
		buildRunningServerPeerDropdownOptions,
		encodeServerPeerValue,
	} from '$lib/utils/serverPeerOptions';
	import type { FreeTurnServerConfig } from '$lib/types';
	import LinkBox from './LinkBox.svelte';
	import ServerAllowlist from './ServerAllowlist.svelte';
	import ServerWgBind from '../freeturn/ServerWgBind.svelte';

	interface Props {
		serverId: string;
		serverName: string;
		server: FreeTurnServerConfig;
		running: boolean;
		busy?: boolean;
		locked: (fn: () => Promise<void>) => Promise<void>;
		onsave: () => Promise<void>;
		peer?: string;
		peerConf?: string;
	}

	let {
		serverId,
		serverName,
		server,
		running,
		busy = false,
		locked,
		onsave,
		peer = $bindable(''),
		peerConf = $bindable(''),
	}: Props = $props();

	let wanPeer = $state(server.linkPeer ?? '');
	let link = $state('');
	let linkBusy = $state(false);
	let wanBusy = $state(false);
	let keeneticPeerSelected = $state(false);
	let lastGeneratedConf = $state('');
	let peerSnap = $state<ServersSnapshot | null>(null);

	const listenPort = $derived(String(server.listen?.split(':').pop() || '56000'));
	const peerOptions = $derived(buildRunningServerPeerDropdownOptions(peerSnap));

	$effect(() => {
		const unsub = servers.subscribe((st) => {
			peerSnap = st.data;
		});
		return unsub;
	});

	// Автоматический подбор пира, если ещё не выбран
	$effect(() => {
		if (peer || !peerSnap) return;
		untrack(() => {
			autoSelectPeer();
		});
	});

	function autoSelectPeer() {
		if (peer || !peerSnap) return;
		const storageKey = 'awgm_ft_peer_' + serverId;
		const saved = localStorage.getItem(storageKey);
		if (saved && peerOptions.some((o) => o.value === saved)) {
			peer = saved;
			return;
		}

		// Поиск по server.connect (например 127.0.0.1:51820 -> порт 51820)
		if (server.connect) {
			const m = server.connect.match(/:(\d+)$/);
			if (m) {
				const port = Number(m[1]);
				// Ищем в managed
				for (const s of peerSnap.managed ?? []) {
					if (s.listenPort === port && (s.peers?.length ?? 0) > 0) {
						peer = encodeServerPeerValue('managed', s.interfaceName, s.peers![0].publicKey);
						return;
					}
				}
				// Ищем в system
				for (const s of peerSnap.servers ?? []) {
					if (s.listenPort === port && (s.peers?.length ?? 0) > 0) {
						peer = encodeServerPeerValue('system', s.id, s.peers![0].publicKey);
						return;
					}
				}
			}
		}

		// Если всего один пир доступен — выбираем его
		if (peerOptions.length === 1) {
			peer = peerOptions[0].value;
		}
	}

	$effect(() => {
		if (peer) {
			localStorage.setItem('awgm_ft_peer_' + serverId, peer);
		}
	});

	let queuedRegenerate = false;

	$effect(() => {
		const current = peerConf;
		if (current && current !== lastGeneratedConf) {
			lastGeneratedConf = current;
			void generateLink();
		}
	});

	onMount(() => {
		autoSelectPeer();
		if (!peer) {
			void generateLink();
		}
	});

	async function saveWanPeer() {
		if (busy) return;
		const trimmed = wanPeer.trim();
		if (server.linkPeer === trimmed) return;
		try {
			server.linkPeer = trimmed;
			await onsave();
		} catch (e) {
			notifications.error(errText(e));
		}
	}

	async function fillWan() {
		wanBusy = true;
		try {
			const ip = await api.getWANIP();
			wanPeer = ip.includes(':') || !listenPort ? ip : `${ip}:${listenPort}`;
			server.linkPeer = wanPeer;
			await onsave();
			await generateLink();
		} catch (e) {
			notifications.error(errText(e));
		} finally {
			wanBusy = false;
		}
	}

	async function generateLink() {
		if (linkBusy) {
			queuedRegenerate = true;
			return;
		}
		linkBusy = true;
		try {
			let peerParam = wanPeer.trim();
			if (peerParam && !peerParam.includes(':') && listenPort) {
				peerParam = `${peerParam}:${listenPort}`;
			}
			const res = await api.generateFreeTurnLink({
				serverId,
				peer: peerParam || undefined,
				name: serverName,
				wg: peerConf.trim() || undefined,
			});
			link = res.link ?? '';
			if (res.peer && !wanPeer.trim()) {
				wanPeer = res.peer;
			}
		} catch (e) {
			if (peerConf.trim()) {
				notifications.error(errText(e));
			}
		} finally {
			linkBusy = false;
			if (queuedRegenerate) {
				queuedRegenerate = false;
				void generateLink();
			}
		}
	}
</script>

<div class="freeturn-auth-container">
	<div class="info-banner">
		<div class="info-title">
			<Share2 size={16} class="info-icon" />
			<span>Раздача подключения FreeTurn</span>
		</div>
		<p class="info-text">
			Клиенты подключаются к серверу FreeTurn по обфусцированному TURN-каналу и выходят в выбранный WireGuard-пир роутера. Ссылка содержит параметры подключения и готовый клиентский конфиг WireGuard.
		</p>
	</div>

	<!-- Блок привязки к WG-пиру -->
	<div class="card-section">
		<div class="section-header">
			<span class="section-title">WG-пир роутера</span>
			<FieldHint text="Вход для трафика клиентов. Конфиг выбранного пира автоматически встраивается в ссылку freeturn://" />
		</div>

		<ServerWgBind
			autoApply
			compact
			bind:selected={peer}
			bind:wgConf={peerConf}
			bind:keeneticSelected={keeneticPeerSelected}
			clientListenPort={9000}
			peerLabel="WG-пир"
			onConnect={async (addr) => {
				if (server.connect !== addr) {
					server.connect = addr;
					await onsave();
				}
			}}
			onPeerConf={(conf) => {
				peerConf = conf;
				void generateLink();
			}}
		/>
	</div>

	<!-- Блок параметров ссылки и LinkBox -->
	<div class="card-section">
		<div class="section-header">
			<span class="section-title">Ссылка для подключения</span>
			<FieldHint text="Ссылка freeturn:// содержит внешний адрес сервера, порт, ключ обфускации и WireGuard-конфиг клиента" />
		</div>

		<div class="link-params-grid">
			<div class="param-item param-span">
				<div class="param-header-row">
					<span class="param-label">Адрес сервера (Peer / WAN IP)</span>
					<button
						type="button"
						class="text-action-btn"
						onclick={fillWan}
						disabled={wanBusy}
					>
						{wanBusy ? 'Определение...' : 'Определить WAN IP'}
					</button>
				</div>
				<div class="input-action-row">
					<input
						type="text"
						bind:value={wanPeer}
						class="text-input"
						placeholder={`IP:порт или домен (авто: :${listenPort})`}
						onchange={() => { void saveWanPeer(); void generateLink(); }}
						onblur={() => { void saveWanPeer(); void generateLink(); }}
					/>
				</div>
			</div>
		</div>

		<div class="actions-bar">
			<Button
				variant="secondary"
				size="sm"
				onclick={() => { void saveWanPeer(); void generateLink(); }}
				disabled={linkBusy}
			>
				<RefreshCw size={14} class={linkBusy ? 'spin' : ''} />
				<span>Обновить ссылку</span>
			</Button>
		</div>

		{#if link}
			<div class="links-display">
				<LinkBox {link} freeturn={true} title="Ссылка freeturn:// (для FreeTurn-клиента и Android)" />
			</div>
		{:else if !peer}
			<p class="empty-hint">
				Выберите WG-пир выше — ссылка freeturn:// сгенерируется автоматически.
			</p>
		{/if}
	</div>

	<!-- Опциональный белый список Client ID -->
	<details class="allowlist-collapse">
		<summary class="allowlist-summary">
			<span class="allowlist-title">Белый список Client ID (опционально)</span>
			<span class="allowlist-badge">По умолчанию выключен</span>
		</summary>
		<div class="allowlist-body">
			<p class="allowlist-info">
				По умолчанию FreeTurn принимает подключения клиентов без проверки по белому списку. Если вы хотите разрешить доступ строго определённым Client ID, добавьте их ниже.
			</p>
			<ServerAllowlist
				{serverId}
				{serverName}
				{server}
				{busy}
				{locked}
			/>
		</div>
	</details>
</div>

<style>
	.freeturn-auth-container {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.info-banner {
		padding: 0.875rem 1rem;
		background: var(--color-surface-raised, rgba(255, 255, 255, 0.04));
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 6px);
	}

	.info-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--color-text);
		margin-bottom: 0.25rem;
	}

	:global(.info-icon) {
		color: var(--color-primary, #3b82f6);
	}

	.info-text {
		margin: 0;
		font-size: 0.8125rem;
		color: var(--color-text-secondary);
		line-height: 1.4;
	}

	.card-section {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		padding: 1rem;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 6px);
		background: var(--color-surface);
	}

	.section-header {
		display: flex;
		align-items: center;
		gap: 0.375rem;
	}

	.section-title {
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--color-text);
	}

	.link-params-grid {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 0.75rem 1rem;
		align-items: start;
	}

	.param-item {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}

	.param-span {
		grid-column: 1 / -1;
	}

	.param-header-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.param-label {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--color-text-secondary);
	}

	.text-action-btn {
		background: none;
		border: none;
		padding: 0;
		font-size: 0.75rem;
		color: var(--color-primary, #3b82f6);
		cursor: pointer;
		text-decoration: underline;
	}

	.text-action-btn:hover:not(:disabled) {
		color: var(--color-primary-hover, #60a5fa);
	}

	.text-action-btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.text-input {
		width: 100%;
		height: 34px;
		padding: 0 0.75rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		outline: none;
		transition: border-color 0.15s;
	}

	.text-input:focus {
		border-color: var(--color-primary, #3b82f6);
	}

	.actions-bar {
		display: flex;
		justify-content: flex-end;
		margin-top: 0.25rem;
	}

	.links-display {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		margin-top: 0.5rem;
	}

	.empty-hint {
		margin: 0.5rem 0 0;
		font-size: 0.8125rem;
		color: var(--color-text-secondary);
		font-style: italic;
	}

	.allowlist-collapse {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 6px);
		background: var(--color-surface, rgba(255, 255, 255, 0.02));
		overflow: hidden;
	}

	.allowlist-summary {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.625rem 0.875rem;
		cursor: pointer;
		user-select: none;
		font-size: 0.8125rem;
		background: var(--color-surface);
		transition: background-color 0.15s;
	}

	.allowlist-summary:hover {
		background: var(--color-surface-raised, rgba(255, 255, 255, 0.04));
	}

	.allowlist-title {
		font-weight: 600;
		color: var(--color-text-secondary);
	}

	.allowlist-badge {
		font-size: 0.6875rem;
		color: var(--color-text-tertiary, #888);
		padding: 0.125rem 0.375rem;
		border-radius: 3px;
		background: var(--color-surface-raised, rgba(255, 255, 255, 0.05));
	}

	.allowlist-body {
		padding: 0.875rem;
		border-top: 1px solid var(--color-border);
	}

	.allowlist-info {
		margin: 0 0 0.75rem;
		font-size: 0.75rem;
		color: var(--color-text-secondary);
		line-height: 1.4;
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
