<script lang="ts">
	import { onMount } from 'svelte';
	import { Button, Card, FieldHint, IconButton, Input, SegmentedControl } from '$lib/components/ui';
	import { Copy, Eye, EyeOff, KeyRound, QrCode, RefreshCw, Sparkles } from 'lucide-svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { errText } from '$lib/utils/errorMessage';
	import type { WdttServerConfig } from '$lib/types';
	import LinkBox from './LinkBox.svelte';
	import { wdttServerPorts, type ShareConfig } from './shareConfig';

	interface Props {
		serverId: string;
		serverName: string;
		server: WdttServerConfig;
		running: boolean;
		busy?: boolean;
		onsave: () => Promise<void>;
		onpasswordchange?: (newPass: string) => void;
	}

	let {
		serverId,
		serverName,
		server,
		running,
		busy = false,
		onsave,
		onpasswordchange,
	}: Props = $props();

	let password = $state(server.sharedPassword ?? '');
	let showPassword = $state(false);
	let savingPassword = $state(false);

	let peer = $state(server.linkPeer ?? '');
	let vkHashes = $state(server.linkVkHashes ?? '');

	let link = $state('');
	let linkQwdtt = $state('');
	let linkBusy = $state(false);
	let wanBusy = $state(false);

	const dtlsPort = $derived(wdttServerPorts(server)[0]?.port ?? 0);
	const hasChanges = $derived(password.trim() !== (server.sharedPassword ?? '').trim());

	onMount(() => {
		if (!password.trim()) {
			password = generateRandomHex(16);
		}
		if (password.trim()) {
			void generateLink();
		}
	});

	function generateRandomHex(bytes = 16): string {
		const arr = new Uint8Array(bytes);
		crypto.getRandomValues(arr);
		return Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	function generateNewPassword() {
		password = generateRandomHex(16);
	}

	async function saveLinkParams() {
		if (busy) return;
		const trimmedPeer = peer.trim();
		const trimmedHashes = vkHashes.trim();
		if (server.linkPeer === trimmedPeer && server.linkVkHashes === trimmedHashes) {
			return;
		}
		try {
			server.linkPeer = trimmedPeer;
			server.linkVkHashes = trimmedHashes;
			await onsave();
		} catch (e) {
			notifications.error(errText(e));
		}
	}

	async function savePassword() {
		if (savingPassword || busy) return;
		const trimmed = password.trim();
		if (!trimmed) {
			notifications.error('Пароль не может быть пустым');
			return;
		}
		savingPassword = true;
		try {
			server.clientAuthMode = 'shared';
			server.sharedPassword = trimmed;
			server.linkPeer = peer.trim();
			server.linkVkHashes = vkHashes.trim();
			await onsave();
			notifications.success('Общий пароль сохранён');
			onpasswordchange?.(trimmed);
			await generateLink();
		} catch (e) {
			notifications.error(errText(e));
		} finally {
			savingPassword = false;
		}
	}

	async function copyPassword() {
		if (!password.trim()) return;
		if (await copyToClipboard(password.trim())) {
			notifications.success('Пароль скопирован');
		}
	}

	async function generateLink() {
		if (linkBusy) return;
		linkBusy = true;
		try {
			let peerParam = peer.trim();
			if (peerParam && !peerParam.includes(':') && dtlsPort) {
				peerParam = `${peerParam}:${dtlsPort}`;
			}
			const hashes = vkHashes
				.split(/[,;\s]+/)
				.map((h) => h.trim())
				.filter(Boolean);
			const res = await api.generateWdttServerLink(serverId, {
				peer: peerParam || undefined,
				vkHashes: hashes.length ? hashes : undefined,
				name: serverName,
				password: password.trim() || undefined,
			});
			link = res.link ?? '';
			linkQwdtt = res.linkQwdtt ?? '';
			if (res.peer && !peer.trim()) {
				peer = res.peer;
			}
		} catch (e) {
			if (password.trim()) {
				notifications.error(errText(e));
			}
		} finally {
			linkBusy = false;
		}
	}

	async function fillWan() {
		wanBusy = true;
		try {
			const ip = await api.getWANIP();
			peer = ip.includes(':') || !dtlsPort ? ip : `${ip}:${dtlsPort}`;
			await saveLinkParams();
			await generateLink();
		} catch (e) {
			notifications.error(errText(e));
		} finally {
			wanBusy = false;
		}
	}
</script>

<div class="shared-auth-container">
	<div class="info-banner">
		<div class="info-title">
			<KeyRound size={16} class="info-icon" />
			<span>Авторизация по общему паролю (PSK)</span>
		</div>
		<p class="info-text">
			Все абоненты подключаются по единому паролю без создания отдельных учётных записей.
			Сервер автоматически выделяет IP-адрес каждому подключённому устройству.
		</p>
	</div>

	<!-- Блок пароля -->
	<div class="card-section">
		<div class="section-header">
			<span class="section-title">Общий пароль сервера</span>
			<FieldHint text="Ключ WRAP для подключения всех клиентов к серверу" />
		</div>

		<div class="password-field-row">
			<div class="input-wrap">
				<input
					type={showPassword ? 'text' : 'password'}
					bind:value={password}
					class="password-input"
					placeholder="Задайте пароль или сгенерируйте"
					disabled={busy || savingPassword}
					onkeydown={(e) => {
						if (e.key === 'Enter') void savePassword();
					}}
				/>
				<button
					type="button"
					class="icon-inline-btn"
					onclick={() => (showPassword = !showPassword)}
					title={showPassword ? 'Скрыть' : 'Показать'}
				>
					{#if showPassword}
						<EyeOff size={16} />
					{:else}
						<Eye size={16} />
					{/if}
				</button>
			</div>

			<Button
				variant="secondary"
				size="sm"
				onclick={copyPassword}
				disabled={!password.trim()}
				title="Скопировать пароль"
			>
				<Copy size={14} />
				<span>Копировать</span>
			</Button>

			<Button
				variant="secondary"
				size="sm"
				onclick={generateNewPassword}
				disabled={busy || savingPassword}
				title="Сгенерировать случайный пароль"
			>
				<Sparkles size={14} />
				<span>Сгенерировать</span>
			</Button>

			{#if hasChanges}
				<Button
					variant="primary"
					size="sm"
					onclick={savePassword}
					disabled={busy || savingPassword || !password.trim()}
				>
					{savingPassword ? 'Сохранение...' : 'Сохранить'}
				</Button>
			{/if}
		</div>
	</div>

	<!-- Блок параметров ссылки -->
	<div class="card-section">
		<div class="section-header">
			<span class="section-title">Ссылка для подключения</span>
			<FieldHint text="Ссылка содержит адрес роутера, порты, общий пароль и VK-хеши" />
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
						bind:value={peer}
						class="text-input"
						placeholder="IP:порт или домен"
						onchange={() => { void saveLinkParams(); void generateLink(); }}
						onblur={() => { void saveLinkParams(); void generateLink(); }}
					/>
				</div>
			</div>

			<div class="param-item param-span">
				<span class="param-label">VK-хеши (через запятую)</span>
				<input
					type="text"
					bind:value={vkHashes}
					class="text-input"
					placeholder="VK звонки URL или хеши"
					onchange={() => { void saveLinkParams(); void generateLink(); }}
					onblur={() => { void saveLinkParams(); void generateLink(); }}
				/>
			</div>
		</div>

		<div class="actions-bar">
			<Button
				variant="secondary"
				size="sm"
				onclick={() => { void saveLinkParams(); void generateLink(); }}
				disabled={linkBusy || !password.trim()}
			>
				<RefreshCw size={14} class={linkBusy ? 'spin' : ''} />
				<span>Обновить ссылку</span>
			</Button>
		</div>

		{#if link}
			<div class="links-display">
				<LinkBox {link} title="wdtt:// (для Keenetic и роутеров)" />
				{#if linkQwdtt}
					<LinkBox link={linkQwdtt} title="qwdtt:// (для мобильных телефонов и qWDTT)" />
				{/if}
			</div>
		{/if}
	</div>
</div>

<style>
	.shared-auth-container {
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

	.password-field-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.input-wrap {
		position: relative;
		flex: 1;
		min-width: 220px;
		display: flex;
		align-items: center;
	}

	.password-input,
	.text-input {
		width: 100%;
		height: 34px;
		padding: 0 2.25rem 0 0.75rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		outline: none;
		transition: border-color 0.15s;
	}

	.text-input {
		padding: 0 0.75rem;
	}

	.password-input:focus,
	.text-input:focus {
		border-color: var(--color-primary, #3b82f6);
	}

	.icon-inline-btn {
		position: absolute;
		right: 0.5rem;
		display: flex;
		align-items: center;
		justify-content: center;
		background: none;
		border: none;
		color: var(--color-text-secondary);
		cursor: pointer;
		padding: 0.25rem;
		border-radius: 4px;
	}

	.icon-inline-btn:hover {
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
