<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { tunnels } from '$lib/stores/tunnels';
	import { notifications } from '$lib/stores/notifications';
	import { PageContainer } from '$lib/components/layout';
	import { Button, ConfirmModal } from '$lib/components/ui';
	import AmneziaConfEditor from '$lib/components/tunnels/AmneziaConfEditor.svelte';
	import { classifyVpnLink, decodeVpnLink, isVpnLink } from '$lib/utils/vpnlink';
	import { api } from '$lib/api/client';
	import type { AmneziaPremiumCountry, AmneziaPremiumIssuedConfig, SystemInfo } from '$lib/types';

	type TunnelImportTab = 'file' | 'paste' | 'vpn';

	function normalizeTunnelImportTab(raw: string | null): TunnelImportTab {
		if (raw === 'file' || raw === 'paste' || raw === 'vpn') return raw;
		if (raw === 'link' || raw === 'premium') return 'vpn';
		return 'file';
	}

	const initialTab = normalizeTunnelImportTab($page.url.searchParams.get('tab'));

	let loading = $state(false);
	let importContent = $state('');
	let importName = $state('');
	let fileInput = $state<HTMLInputElement>();
	let dragOver = $state(false);
	let activeTab = $state<TunnelImportTab>(initialTab);
	/** Единое поле vpn:// для клиентской ссылки и Amnezia Premium */
	let vpnPasteInput = $state('');
	let linkPreview = $state('');
	let linkError = $state('');
	let vpnDebounceTimer: ReturnType<typeof setTimeout> | null = null;
	/** Инвалидация устаревших async-веток при новом вводе / переключении вкладки */
	let vpnAnalysisGen = 0;
	let systemInfo = $state<SystemInfo | null>(null);
	let selectedBackend = $state<'nativewg' | 'kernel'>('nativewg');

	let premiumSid = $state('');
	let premiumCountries = $state<AmneziaPremiumCountry[]>([]);
	let premiumIssuedConfigs = $state<AmneziaPremiumIssuedConfig[]>([]);
	let premiumBusy = $state(false);
	let premiumBusyDepth = 0;
	let premiumPickBusy = $state('');
	let premiumError = $state('');
	let premiumReissueConfirm = $state<{ code: string; label: string } | null>(null);
	let premiumConfirmBusy = $state(false);

	type VpnPastePresentation =
		| { kind: 'neutral'; label: string }
		| { kind: 'regular'; label: string }
		| { kind: 'premium'; label: string };

	let vpnPastePresentation = $derived.by((): VpnPastePresentation => {
		const raw = vpnPasteInput.trim();
		if (!raw || !isVpnLink(raw)) {
			return { kind: 'neutral', label: 'Вставить ссылку' };
		}
		if (classifyVpnLink(raw) === 'regular') {
			return { kind: 'regular', label: 'Вставить ссылку' };
		}
		return { kind: 'premium', label: 'Amnezia Premium' };
	});

	let showPremiumChrome = $derived.by(() => {
		const raw = vpnPasteInput.trim();
		if (!raw || !isVpnLink(raw)) return false;
		return classifyVpnLink(raw) !== 'regular';
	});

	// Sync activeTab → URL (?tab=). Mirrors what canonical Tabs urlParam
	// does — kept inline because this page uses a custom div-based tab UI.
	$effect(() => {
		const t = activeTab;
		const sp = new URLSearchParams($page.url.search);
		if (t === 'file') {
			sp.delete('tab');
		} else {
			sp.set('tab', t === 'vpn' ? 'vpn' : t);
		}
		const nextSearch = sp.toString();
		if (nextSearch === $page.url.searchParams.toString()) return;
		const target = $page.url.pathname + (nextSearch ? `?${nextSearch}` : '') + $page.url.hash;
		void goto(target, { replaceState: true, keepFocus: true, noScroll: true });
	});

	$effect(() => {
		api.getSystemInfo().then(info => {
			systemInfo = info;
			if (info.backendAvailability && !info.backendAvailability.nativewg && info.backendAvailability.kernel) {
				selectedBackend = 'kernel';
			}
		}).catch(() => {});
	});

	function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		if (input.files && input.files[0]) {
			readFile(input.files[0]);
		}
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		dragOver = false;
		if (event.dataTransfer?.files && event.dataTransfer.files[0]) {
			readFile(event.dataTransfer.files[0]);
		}
	}

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
		dragOver = true;
	}

	function handleDragLeave() {
		dragOver = false;
	}

	function readFile(file: File) {
		if (!importName) {
			const baseName = file.name.replace(/\.conf$/i, '');
			importName = baseName;
		}

		const reader = new FileReader();
		reader.onload = (e) => {
			const content = e.target?.result as string;
			if (content) {
				importContent = content;
				notifications.success(`Файл "${file.name}" загружен`);
			}
		};
		reader.onerror = () => {
			notifications.error('Не удалось прочитать файл');
		};
		reader.readAsText(file);
	}

	function resetPremiumCatalogState() {
		premiumSid = '';
		premiumCountries = [];
		premiumIssuedConfigs = [];
	}

	function beginPremiumBusy() {
		premiumBusyDepth++;
		premiumBusy = true;
	}

	function endPremiumBusy() {
		premiumBusyDepth = Math.max(0, premiumBusyDepth - 1);
		premiumBusy = premiumBusyDepth > 0;
	}

	function scheduleVpnPasteAnalysis() {
		if (vpnDebounceTimer) clearTimeout(vpnDebounceTimer);
		vpnDebounceTimer = setTimeout(() => void runVpnPasteAnalysis(), 420);
	}

	function activateVpnTab() {
		activeTab = 'vpn';
		if (vpnDebounceTimer) clearTimeout(vpnDebounceTimer);
		vpnDebounceTimer = null;
		void runVpnPasteAnalysis();
	}

	async function fetchPremiumCatalogForGen(gen: number) {
		const key = vpnPasteInput.trim();
		if (!key || !isVpnLink(key)) return;
		if (classifyVpnLink(key) === 'regular') return;

		beginPremiumBusy();
		premiumError = '';
		try {
			const { sid } = await api.amneziaPremiumLogin(key);
			if (gen !== vpnAnalysisGen) return;
			premiumSid = sid;
			const info = await api.amneziaPremiumAccountInfo(sid);
			if (gen !== vpnAnalysisGen) return;
			premiumCountries = info.available_countries ?? [];
			premiumIssuedConfigs = info.issued_configs ?? [];
			if (!premiumCountries.length) {
				notifications.warning('Список стран пуст — проверьте подписку');
			}
		} catch (e) {
			if (gen !== vpnAnalysisGen) return;
			if (e instanceof DOMException && e.name === 'AbortError') return;
			resetPremiumCatalogState();
			const msg = e instanceof Error ? e.message : 'Ошибка запроса к cp.amnezia.org';
			premiumError = msg;
			notifications.error(msg);
		} finally {
			endPremiumBusy();
		}
	}

	async function fetchPremiumCatalog() {
		const gen = ++vpnAnalysisGen;
		await fetchPremiumCatalogForGen(gen);
	}

	async function runVpnPasteAnalysis() {
		if (vpnDebounceTimer !== null) {
			clearTimeout(vpnDebounceTimer);
			vpnDebounceTimer = null;
		}

		const gen = ++vpnAnalysisGen;

		const raw = vpnPasteInput.trim();
		linkError = '';
		linkPreview = '';

		if (!raw) {
			resetPremiumCatalogState();
			importContent = '';
			premiumError = '';
			premiumBusyDepth = 0;
			premiumBusy = false;
			return;
		}

		if (!isVpnLink(raw)) {
			resetPremiumCatalogState();
			importContent = '';
			linkError = 'Ожидается ссылка вида vpn://…';
			return;
		}

		const flavor = classifyVpnLink(raw);

		if (flavor === 'regular') {
			resetPremiumCatalogState();
			importContent = '';
			try {
				const result = decodeVpnLink(raw);
				if (gen !== vpnAnalysisGen) return;
				linkPreview = result.config;
				importContent = result.config;
				if (result.name && !importName) importName = result.name;
			} catch (e) {
				importContent = '';
				linkError = e instanceof Error ? e.message : 'Ошибка декодирования';
			}
			return;
		}

		linkPreview = '';
		importContent = '';
		resetPremiumCatalogState();
		await fetchPremiumCatalogForGen(gen);
	}

	/** Импорт из сырого текста (.conf или vpn:// с клиентским конфигом). Обновляет importContent после успешного декода vpn:// */
	async function executeImport(rawContent: string) {
		let content = rawContent.trim();
		if (!content) {
			notifications.error('Вставьте содержимое конфигурации, загрузите файл или вставьте vpn:// ссылку');
			return;
		}

		if (isVpnLink(content)) {
			try {
				const result = decodeVpnLink(content);
				content = result.config;
				if (result.name && !importName) {
					importName = result.name;
				}
			} catch (e) {
				notifications.error(e instanceof Error ? e.message : 'Ошибка декодирования vpn:// ссылки');
				return;
			}
		}

		importContent = content;

		loading = true;
		try {
			const tunnel = await tunnels.importConfig(content, importName, selectedBackend);
			if (tunnel.warnings?.length) {
				tunnel.warnings.forEach(w => notifications.warning(w));
			}
			notifications.success('Туннель успешно импортирован');
			goto(`/tunnels/${tunnel.id}`);
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка импорта');
		} finally {
			loading = false;
		}
	}

	async function handleImport() {
		await executeImport(importContent);
	}

	function isPremiumCountryIssued(code: string): boolean {
		const cc = code.trim().toLowerCase();
		return premiumIssuedConfigs.some(
			(ic) => String(ic.server_country_code ?? '').trim().toLowerCase() === cc
		);
	}

	function requestPremiumCountryPick(code: string, label: string) {
		if (!premiumSid) {
			notifications.error('Сначала загрузите список стран');
			return;
		}
		if (isPremiumCountryIssued(code)) {
			premiumReissueConfirm = { code, label };
			return;
		}
		void downloadAndImportPremiumCountry(code, label);
	}

	async function confirmPremiumReissue() {
		const ctx = premiumReissueConfirm;
		if (!ctx || !premiumSid) return;
		premiumConfirmBusy = true;
		try {
			await downloadAndImportPremiumCountry(ctx.code, ctx.label);
			premiumReissueConfirm = null;
		} finally {
			premiumConfirmBusy = false;
		}
	}

	async function downloadAndImportPremiumCountry(code: string, label: string) {
		premiumPickBusy = code;
		try {
			const { config } = await api.amneziaPremiumDownloadConfig(premiumSid, code);
			if (!importName) importName = label || code.toUpperCase();
			await executeImport(config);
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Не удалось скачать конфигурацию');
		} finally {
			premiumPickBusy = '';
		}
	}

</script>

<svelte:head>
	<title>Новый туннель - AWG Manager</title>
</svelte:head>

<PageContainer>
<div class="page-header">
	<a href="/" class="back-link">
		<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
			<line x1="19" y1="12" x2="5" y2="12"/>
			<polyline points="12 19 5 12 12 5"/>
		</svg>
		Назад
	</a>
	<h1 class="page-title">Новый туннель</h1>
</div>

<div class="import-container">
	<label class="field-label" for="import-name">Название туннеля</label>
	<div class="top-row">
		<input type="text" id="import-name" class="name-input" bind:value={importName} placeholder="Мой VPN">
		<!-- TODO Phase 1: button needs to match 42px name-input height (size="md" is 32px) -->
		<div class="btn-import-wrap">
			<Button variant="primary" size="md" onclick={handleImport} disabled={!importContent.trim()} loading={loading}>
				Импортировать
			</Button>
		</div>
	</div>

	<div class="backend-selector">
		<span class="field-label">Режим работы</span>
		<div class="backend-options">
			<button
				type="button"
				class="backend-option"
				class:selected={selectedBackend === 'nativewg'}
				class:disabled={systemInfo !== null && !systemInfo.backendAvailability?.nativewg}
				disabled={systemInfo !== null && !systemInfo.backendAvailability?.nativewg}
				title={systemInfo !== null && !systemInfo.backendAvailability?.nativewg ? 'Прошивка не поддерживает нативный WireGuard' : ''}
				onclick={() => selectedBackend = 'nativewg'}
			>
				<span class="backend-name">NativeWG</span>
				<span class="backend-desc">DNS/IP маршрутизация, failover, виден в UI роутера</span>
			</button>
			<button
				type="button"
				class="backend-option"
				class:selected={selectedBackend === 'kernel'}
				class:disabled={systemInfo !== null && !systemInfo.backendAvailability?.kernel}
				disabled={systemInfo !== null && !systemInfo.backendAvailability?.kernel}
				title={systemInfo !== null && !systemInfo.backendAvailability?.kernel ? 'Модуль ядра не загружен' : ''}
				onclick={() => selectedBackend = 'kernel'}
			>
				<span class="backend-name">Kernel</span>
				<span class="backend-desc">Без интеграции в роутер, для сторонних проектов</span>
			</button>
		</div>
	</div>

	<div class="tabs">
		<button
			class="tab"
			class:tab-active={activeTab === 'file'}
			onclick={() => activeTab = 'file'}
		>
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
				<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
				<polyline points="17 8 12 3 7 8"/>
				<line x1="12" y1="3" x2="12" y2="15"/>
			</svg>
			Загрузить файл
		</button>
		<button
			class="tab"
			class:tab-active={activeTab === 'paste'}
			onclick={() => activeTab = 'paste'}
		>
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
				<path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/>
				<rect x="8" y="2" width="8" height="4" rx="1" ry="1"/>
			</svg>
			Вставить текст
		</button>
		<button type="button" class="tab" class:tab-active={activeTab === 'vpn'} onclick={activateVpnTab}>
			{#if vpnPastePresentation.kind === 'premium'}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="16" height="16" aria-hidden="true">
					<path d="M11.562 3.266a.5.5 0 0 1 .876 0L15.39 8.87a1 1 0 0 0 1.516.294L21.183 5.5a.5.5 0 0 1 .798.519l-2.834 10.246a1 1 0 0 1-.956.734H5.81a1 1 0 0 1-.957-.734L2.078 6.02a.5.5 0 0 1 .798-.519l4.276 3.664a1 1 0 0 0 1.516-.294z" />
				</svg>
			{:else}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16" aria-hidden="true">
					<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
					<path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
				</svg>
			{/if}
			{vpnPastePresentation.label}
		</button>
	</div>

	<div class="tab-content">
		{#if activeTab === 'file'}
			<div
				class="file-drop-zone"
				class:drag-over={dragOver}
				class:has-content={!!importContent.trim()}
				ondrop={handleDrop}
				ondragover={handleDragOver}
				ondragleave={handleDragLeave}
				role="button"
				tabindex="0"
				onclick={() => fileInput?.click()}
				onkeydown={(e) => e.key === 'Enter' && fileInput?.click()}
			>
				<input
					type="file"
					accept=".conf"
					bind:this={fileInput}
					onchange={handleFileSelect}
					style="display: none"
				>
				{#if importContent.trim()}
					<div class="drop-content">
						<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="48" height="48">
							<polyline points="20 6 9 17 4 12"/>
						</svg>
						<div class="drop-text">
							<p class="drop-title">Файл загружен</p>
							<p class="drop-hint">Нажмите чтобы заменить</p>
						</div>
					</div>
				{:else}
					<div class="drop-content">
						<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="48" height="48">
							<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
							<polyline points="17 8 12 3 7 8"/>
							<line x1="12" y1="3" x2="12" y2="15"/>
						</svg>
						<div class="drop-text">
							<p class="drop-title">Перетащите .conf файл сюда</p>
							<p class="drop-hint">или нажмите для выбора</p>
						</div>
					</div>
				{/if}
			</div>
		{:else if activeTab === 'paste'}
			<AmneziaConfEditor
				bind:value={importContent}
				variant="page"
				placeholder="[Interface]
PrivateKey = ...
Address = 10.0.0.2/32

[Peer]
PublicKey = ...
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0"
			/>
		{:else if activeTab === 'vpn'}
			<div class="vpn-import-stack">
				<textarea
					id="vpn-paste"
					class="config-textarea vpn-paste-input"
					bind:value={vpnPasteInput}
					oninput={scheduleVpnPasteAnalysis}
					onpaste={() => queueMicrotask(() => void runVpnPasteAnalysis())}
					placeholder="Вставьте vpn:// — клиентский конфиг или ключ Amnezia Premium"
					spellcheck="false"
				></textarea>
				{#if showPremiumChrome}
					<p class="premium-cp-note">
						Если распознан ключ для получения параметров подписки, приложение может обратиться к внешнему сервису по вашей инициативе.<br>
						AWG Manager не связан с операторами таких сервисов, не проверяет и не гарантирует ключи, доступность их API а так же стабильность работы данного функционала.<br>
						В рамках использования приложения и связанных решений вы принимаете на себя ответственность за соблюдение законодательства страны, в которой находитесь.<br>
						Данный функционал не является официальной интеграцией и не подлежит технической поддержке.
					</p>
					<div class="premium-actions">
						<Button variant="secondary" size="md" onclick={() => fetchPremiumCatalog()} loading={premiumBusy} disabled={premiumBusy}>
							Обновить список стран
						</Button>
					</div>
				{/if}
				{#if premiumError}
					<p class="link-error">{premiumError}</p>
				{/if}
				{#if premiumCountries.length}
					<p class="field-label premium-countries-label">Страны</p>
					<ul class="premium-country-list" role="listbox">
						{#each premiumCountries as c (c.server_country_code)}
							{@const issued = isPremiumCountryIssued(c.server_country_code)}
							<li>
								<button
									type="button"
									class="premium-country-row"
									class:premium-country-row--issued={issued}
									disabled={!!premiumPickBusy}
									onclick={() => requestPremiumCountryPick(c.server_country_code, c.server_country_name)}
								>
									<span class="premium-country-name">{c.server_country_name}</span>
									{#if issued}
										<span class="premium-country-issued-badge">Выдано</span>
									{/if}
									<span class="premium-country-code">{c.server_country_code.toUpperCase()}</span>
									{#if premiumPickBusy === c.server_country_code}
										<span class="premium-country-spinner">…</span>
									{/if}
								</button>
							</li>
						{/each}
					</ul>
				{/if}
				{#if linkError}
					<p class="link-error">{linkError}</p>
				{/if}
				{#if linkPreview}
					<AmneziaConfEditor bind:value={linkPreview} variant="preview" readonly />
				{/if}
			</div>
		{/if}
	</div>

	<p class="form-hint">
		Поддерживаются WireGuard и AmneziaWG конфигурации с параметрами Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5; вкладка vpn:// распознаёт клиентский конфиг в ссылке или ключ Premium (запрос списка стран через прокси cp.amnezia.org).
	</p>
</div>
</PageContainer>

{#if premiumReissueConfirm}
	<ConfirmModal
		open={true}
		title="Новый файл конфигурации"
		message={`Сгенерировать новый файл конфигурации — «${premiumReissueConfirm.label}»?`}
		secondary="Ранее созданный файл перестанет действовать. Подключиться с его помощью будет невозможно. Сгенерированные файлы конфигурации также учитываются в лимите устройств."
		confirmLabel="Сгенерировать"
		cancelLabel="Отмена"
		variant="danger"
		busy={premiumConfirmBusy}
		onClose={() => {
			if (!premiumConfirmBusy) premiumReissueConfirm = null;
		}}
		onConfirm={confirmPremiumReissue}
	/>
{/if}

<style>
	.back-link {
		display: flex;
		align-items: center;
		gap: 4px;
		color: var(--color-text-secondary);
		font-size: 13px;
		padding: 6px 10px;
		border-radius: 6px;
		transition: all 0.15s;
	}

	.back-link:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.import-container {
		max-width: 700px;
		margin: 0 auto;
		padding: 0 1rem;
	}

	.field-label {
		display: block;
		font-size: 13px;
		font-weight: 500;
		color: var(--color-text-secondary);
		margin-bottom: 6px;
	}

	.top-row {
		display: flex;
		align-items: stretch;
		gap: 12px;
		margin-bottom: 1.5rem;
	}

	.name-input {
		flex: 1;
		min-width: 0;
		height: 42px;
		padding: 0 12px;
		font-size: 14px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: 6px;
		color: var(--color-text-primary);
		transition: border-color 0.15s;
	}

	.name-input:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.btn-import-wrap {
		display: flex;
		align-items: stretch;
		flex-shrink: 0;
	}

	.btn-import-wrap :global(.btn) {
		height: 42px;
		padding: 0 24px;
		font-size: 14px;
	}

	.tabs {
		display: flex;
		border-bottom: 1px solid var(--color-border);
		gap: 0;
	}

	.tab {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 10px 16px;
		font-size: 13px;
		font-weight: 500;
		color: var(--color-text-muted);
		background: none;
		border: none;
		border-bottom: 2px solid transparent;
		cursor: pointer;
		transition: all 0.15s;
		margin-bottom: -1px;
	}

	.tab:hover {
		color: var(--color-text-secondary);
	}

	.tab-active {
		color: var(--color-accent);
		border-bottom-color: var(--color-accent);
	}

	.tab-content {
		margin-top: 0;
	}

	.config-textarea {
		width: 100%;
		min-height: 300px;
		padding: 14px;
		font-family: monospace;
		font-size: 13px;
		line-height: 1.5;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-top: none;
		border-radius: 0 0 8px 8px;
		color: var(--color-text-primary);
		resize: vertical;
		transition: border-color 0.15s;
		white-space: pre;
		overflow-x: auto;
	}

	.config-textarea:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.config-textarea::placeholder {
		color: var(--color-text-muted);
	}

	.file-drop-zone {
		min-height: 220px;
		border: 2px dashed var(--color-border);
		border-top: 2px dashed var(--color-border);
		border-radius: 0 0 8px 8px;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		transition: all 0.15s ease;
		background: transparent;
	}

	.file-drop-zone:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
	}

	.file-drop-zone.drag-over {
		border-color: var(--color-accent);
		background: rgba(122, 162, 247, 0.1);
	}

	.file-drop-zone.has-content {
		border-color: var(--color-success);
		border-style: solid;
	}

	.file-drop-zone.has-content svg {
		color: var(--color-success);
	}

	.drop-content {
		display: flex;
		align-items: center;
		gap: 16px;
	}

	.drop-content svg {
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.drop-title {
		font-size: 17px;
		font-weight: 500;
		color: var(--color-text-primary);
		margin-bottom: 4px;
	}

	.drop-hint {
		font-size: 14px;
		color: var(--color-text-muted);
	}

	.vpn-import-stack {
		display: flex;
		flex-direction: column;
		gap: 0;
	}

	.vpn-paste-input {
		min-height: 100px;
		border-radius: 8px;
		margin-bottom: 0;
	}

	.link-error {
		font-size: 13px;
		color: var(--color-error);
		margin: 8px 0 0;
		padding: 0 2px;
	}

	.form-hint {
		font-size: 12px;
		color: var(--color-text-muted);
		margin-top: 1rem;
	}

	.backend-selector {
		margin-bottom: 1.5rem;
	}

	.backend-options {
		display: flex;
		gap: 12px;
		margin-top: 6px;
	}

	.backend-option {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 4px;
		padding: 14px;
		border: 1px solid var(--color-border);
		border-radius: 8px;
		cursor: pointer;
		transition: all 0.15s;
		background: var(--color-bg-secondary);
		text-align: center;
	}

	.backend-option:hover:not(.disabled) {
		border-color: var(--color-accent);
	}

	.backend-option.selected {
		border-color: var(--color-accent);
		background: rgba(122, 162, 247, 0.08);
	}

	.backend-option.disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.backend-name {
		font-size: 14px;
		font-weight: 500;
		color: var(--color-text-primary);
	}

	.backend-desc {
		font-size: 12px;
		color: var(--color-text-muted);
	}

	.premium-cp-note {
		font-size: 12px;
		line-height: 1.45;
		color: var(--color-text-muted);
		margin: 10px 0 0;
		padding: 8px 12px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: 8px;
	}

	.premium-cp-note a {
		color: var(--color-accent);
		text-decoration: underline;
		text-underline-offset: 2px;
	}

	.premium-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		padding: 12px 0;
	}

	.premium-countries-label {
		margin-top: 8px;
	}

	.premium-country-list {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 280px;
		overflow-y: auto;
		border: 1px solid var(--color-border);
		border-radius: 8px;
		background: var(--color-bg-secondary);
	}

	.premium-country-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		width: 100%;
		padding: 10px 14px;
		border: none;
		border-bottom: 1px solid var(--color-border);
		background: transparent;
		cursor: pointer;
		text-align: left;
		font-size: 14px;
		color: var(--color-text-primary);
		transition: background 0.12s;
	}

	.premium-country-row--issued {
		background: var(--color-warning-tint);
		box-shadow: inset 3px 0 0 var(--color-warning);
	}

	.premium-country-row--issued:hover:not(:disabled) {
		background: color-mix(in srgb, var(--color-warning) 20%, var(--color-bg-tertiary));
	}

	.premium-country-issued-badge {
		flex-shrink: 0;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.02em;
		color: var(--color-warning);
		padding: 2px 8px;
		border-radius: 4px;
		background: color-mix(in srgb, var(--color-warning) 14%, transparent);
		border: 1px solid var(--color-warning-border);
	}

	.premium-country-list li:last-child .premium-country-row {
		border-bottom: none;
	}

	.premium-country-row:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
	}

	.premium-country-row:disabled {
		opacity: 0.65;
		cursor: wait;
	}

	.premium-country-name {
		flex: 1;
		min-width: 0;
	}

	.premium-country-code {
		font-family: monospace;
		font-size: 12px;
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.premium-country-spinner {
		font-size: 13px;
		color: var(--color-accent);
		flex-shrink: 0;
	}

	@media (max-width: 500px) {
		.top-row {
			flex-direction: column;
			align-items: stretch;
		}

		.name-input {
			max-width: none;
		}

		.btn-import-wrap {
			align-self: flex-start;
		}

		.backend-options {
			flex-direction: column;
		}
	}
</style>
