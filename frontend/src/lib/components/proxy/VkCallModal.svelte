<script lang="ts">
	import { onMount } from 'svelte';
	import {
		Button,
		Dropdown,
		Input,
		Modal,
		SegmentedControl,
		Toggle
	} from '$lib/components/ui';
	import {
		Check,
		Copy,
		ExternalLink,
		Info,
		KeyRound,
		RefreshCw,
		ShieldCheck,
		Sparkles,
		XCircle
	} from 'lucide-svelte';
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { errText } from '$lib/utils/errorMessage';
	import type { VKCallsCheckItem } from '$lib/types';

	interface Props {
		open: boolean;
		initialValue?: string;
		targetFormat?: 'links' | 'hashes';
		onApply: (value: string) => void;
		onclose?: () => void;
	}

	let {
		open = $bindable(false),
		initialValue = '',
		targetFormat = 'links',
		onApply,
		onclose
	}: Props = $props();

	type ModalTab = 'generate' | 'check' | 'help';
	let activeTab = $state<ModalTab>('generate');

	// Config / Token state
	let hasSavedToken = $state(false);
	let maskedToken = $state('');
	let savedGroupId = $state<number | undefined>(undefined);
	let inputToken = $state('');
	let inputGroupId = $state<string>('');
	let saveTokenOnServer = $state(true);

	// Generate state
	let generateCount = $state<string>('2');
	let isGenerating = $state(false);
	let generatedLinks = $state<string[]>([]);
	let generatedHashes = $state<string[]>([]);
	let outputFormat = $state<'links' | 'hashes'>('links');
	let generateError = $state<string | null>(null);

	// Check state
	let checkInput = $state('');
	let isChecking = $state(false);
	let checkResults = $state<VKCallsCheckItem[]>([]);
	let checkError = $state<string | null>(null);

	const TAB_OPTIONS = [
		{ value: 'generate' as const, label: 'Генерация ссылок' },
		{ value: 'check' as const, label: 'Проверка ссылок' },
		{ value: 'help' as const, label: 'Инструкция' }
	];

	const COUNT_OPTIONS = [
		{ value: '1', label: '1' },
		{ value: '2', label: '2' },
		{ value: '3', label: '3' },
		{ value: '4', label: '4' },
		{ value: '5', label: '5' }
	];

	const FORMAT_OPTIONS = [
		{ value: 'links' as const, label: 'Ссылки (FreeTurn)' },
		{ value: 'hashes' as const, label: 'Хеши (WDTT)' }
	];

	onMount(() => {
		outputFormat = targetFormat;
		void loadConfig();
		if (initialValue.trim()) {
			checkInput = initialValue.trim();
		}
	});

	$effect(() => {
		if (open) {
			outputFormat = targetFormat;
			if (initialValue.trim() && !checkInput.trim()) {
				checkInput = initialValue.trim();
			}
			void loadConfig();
		}
	});

	async function loadConfig() {
		try {
			const cfg = await api.getVKCallsConfig();
			hasSavedToken = cfg.hasToken;
			maskedToken = cfg.maskedToken || '';
			savedGroupId = cfg.groupId || undefined;
			if (savedGroupId && !inputGroupId) {
				inputGroupId = String(savedGroupId);
			}
		} catch (e) {
			// ignore on load
		}
	}

	async function handleGenerate() {
		isGenerating = true;
		generateError = null;
		try {
			const count = parseInt(generateCount, 10) || 2;
			const grp = inputGroupId.trim() ? parseInt(inputGroupId.trim(), 10) : undefined;
			const res = await api.generateVKCalls({
				token: inputToken.trim() || undefined,
				groupId: grp,
				count,
				saveToken: saveTokenOnServer && !!inputToken.trim()
			});

			if (!res.success) {
				throw new Error(res.error || 'Не удалось сгенерировать ссылки VK Calls');
			}

			generatedLinks = res.links || [];
			generatedHashes = res.hashes || [];
			notifications.success(`Сгенерировано ссылок: ${res.links.length}`);
			await loadConfig();
		} catch (e) {
			generateError = errText(e);
			notifications.error(generateError);
		} finally {
			isGenerating = false;
		}
	}

	async function handleCheck(itemsToCheck?: string[]) {
		let rawList: string[] = [];
		if (itemsToCheck && itemsToCheck.length > 0) {
			rawList = itemsToCheck;
		} else {
			rawList = checkInput
				.split(/[\n,\s]+/)
				.map((s) => s.trim())
				.filter(Boolean);
		}

		if (rawList.length === 0) {
			notifications.warning('Введите хотя бы одну ссылку или хеш для проверки');
			return;
		}

		isChecking = true;
		checkError = null;
		try {
			const res = await api.checkVKCalls(rawList);
			checkResults = res.results || [];
			const aliveCount = checkResults.filter((r) => r.alive).length;
			notifications.info(`Проверено: ${checkResults.length}. Активны: ${aliveCount}`);
		} catch (e) {
			checkError = errText(e);
			notifications.error(checkError);
		} finally {
			isChecking = false;
		}
	}

	async function copyItem(text: string) {
		if (await copyToClipboard(text)) {
			notifications.success('Скопировано в буфер обмена');
		}
	}

	async function copyAll() {
		const items = outputFormat === 'links' ? generatedLinks : generatedHashes;
		const separator = outputFormat === 'links' ? '\n' : ',';
		const text = items.join(separator);
		if (await copyToClipboard(text)) {
			notifications.success('Все элементы скопированы');
		}
	}

	function handleApply() {
		const items = outputFormat === 'links' ? generatedLinks : generatedHashes;
		if (items.length === 0) {
			notifications.warning('Сначала сгенерируйте ссылки');
			return;
		}
		const separator = outputFormat === 'links' ? ',' : ',';
		const value = items.join(separator);
		onApply(value);
		notifications.success('Вставлено в конфигурацию клиента');
		open = false;
		onclose?.();
	}

	function handleClose() {
		open = false;
		onclose?.();
	}
</script>

<Modal
	bind:open
	title="VK Calls — ссылки и хеши"
	size="lg"
	onclose={handleClose}
>
	<div class="vk-modal">
		<div class="vk-tabs-wrap">
			<SegmentedControl
				options={TAB_OPTIONS}
				bind:value={activeTab}
				fullWidth
			/>
		</div>

		{#if activeTab === 'generate'}
			<div class="vk-tab-content">
				<div class="vk-config-card">
					{#if hasSavedToken}
						<div class="vk-saved-banner">
							<ShieldCheck size={18} class="vk-icon-ok" />
							<div class="vk-saved-info">
								<span class="vk-saved-title">Сохранён токен:</span>
								<code class="vk-masked">{maskedToken}</code>
							</div>
							<span class="vk-saved-hint">Можно не указывать токен заново</span>
						</div>
					{/if}

					<div class="vk-form-grid">
						<div class="vk-form-col">
							<Input
								label="VK Access Token"
								placeholder={hasSavedToken ? 'Оставьте пустым для сохранённого токена' : 'vk1.a.xxxx...'}
								bind:value={inputToken}
								hint="Токен с правами создания звонков (calls.start)"
								fullWidth
							/>
						</div>
						<div class="vk-form-col-sm">
							<Input
								label="ID группы (опционально)"
								placeholder="club12345"
								bind:value={inputGroupId}
								hint="Для звонков от имени сообщества"
								fullWidth
							/>
						</div>
					</div>

					<div class="vk-options-row">
						<div class="vk-count-picker">
							<span class="vk-label">Количество ссылок:</span>
							<SegmentedControl
								options={COUNT_OPTIONS}
								bind:value={generateCount}
							/>
						</div>
						<Toggle
							label="Запомнить токен на роутере"
							bind:checked={saveTokenOnServer}
						/>
					</div>

					<div class="vk-action-bar">
						<Button
							variant="primary"
							loading={isGenerating}
							disabled={!hasSavedToken && !inputToken.trim()}
							onclick={handleGenerate}
						>
							<Sparkles size={16} />
							Сгенерировать ссылки VK Calls
						</Button>
					</div>

					{#if generateError}
						<div class="vk-error-box">
							<XCircle size={16} />
							<span>{generateError}</span>
						</div>
					{/if}
				</div>

				{#if generatedLinks.length > 0}
					<div class="vk-results-card">
						<div class="vk-results-header">
							<span class="vk-results-title">Сгенерированные результаты ({generatedLinks.length}):</span>
							<SegmentedControl
								options={FORMAT_OPTIONS}
								bind:value={outputFormat}
								size="sm"
							/>
						</div>

						<div class="vk-items-list">
							{#each (outputFormat === 'links' ? generatedLinks : generatedHashes) as item, i}
								<div class="vk-item-row">
									<span class="vk-item-idx">{i + 1}</span>
									<span class="vk-item-text" title={item}>{item}</span>
									<div class="vk-item-actions">
										<button
											type="button"
											class="vk-btn-icon"
											title="Копировать"
											onclick={() => copyItem(item)}
										>
											<Copy size={15} />
										</button>
										{#if outputFormat === 'links'}
											<a
												href={item}
												target="_blank"
												rel="noopener noreferrer"
												class="vk-btn-icon"
												title="Открыть звонок в VK"
											>
												<ExternalLink size={15} />
											</a>
										{/if}
									</div>
								</div>
							{/each}
						</div>

						<div class="vk-results-footer">
							<Button variant="secondary" size="sm" onclick={copyAll}>
								<Copy size={15} />
								Копировать всё
							</Button>
							<Button
								variant="secondary"
								size="sm"
								loading={isChecking}
								onclick={() => handleCheck(generatedLinks)}
							>
								<RefreshCw size={15} />
								Проверить доступность
							</Button>
							<Button variant="primary" size="sm" onclick={handleApply}>
								<Check size={15} />
								Вставить в клиент
							</Button>
						</div>
					</div>
				{/if}
			</div>
		{:else if activeTab === 'check'}
			<div class="vk-tab-content">
				<div class="vk-check-card">
					<p class="vk-desc">
						Проверка валидности существующих ссылок или хешей через публичный API VK (без авторизации).
					</p>
					<textarea
						class="vk-textarea"
						rows="4"
						bind:value={checkInput}
						placeholder="https://vk.ru/call/join/...&#10;или хеши через запятую"
					></textarea>

					<div class="vk-action-bar">
						<Button
							variant="primary"
							loading={isChecking}
							disabled={!checkInput.trim()}
							onclick={() => handleCheck()}
						>
							<RefreshCw size={16} />
							Проверить ссылки
						</Button>
					</div>

					{#if checkError}
						<div class="vk-error-box">
							<XCircle size={16} />
							<span>{checkError}</span>
						</div>
					{/if}

					{#if checkResults.length > 0}
						<div class="vk-check-results">
							<span class="vk-results-title">Результаты проверки:</span>
							<div class="vk-items-list">
								{#each checkResults as res, i}
									<div class="vk-item-row" class:vk-item-alive={res.alive} class:vk-item-dead={!res.alive}>
										<span class="vk-item-idx">{i + 1}</span>
										<span class="vk-item-text" title={res.link}>
											<code>{res.hash}</code>
										</span>
										<div class="vk-status-badge" class:vk-badge-ok={res.alive} class:vk-badge-err={!res.alive}>
											{#if res.alive}
												<ShieldCheck size={14} />
												<span>Активен</span>
											{:else}
												<XCircle size={14} />
												<span>{res.error || 'Завершён'}</span>
											{/if}
										</div>
									</div>
								{/each}
							</div>
						</div>
					{/if}
				</div>
			</div>
		{:else if activeTab === 'help'}
			<div class="vk-tab-content vk-help-content">
				<div class="vk-help-card">
					<h4><KeyRound size={18} /> Как получить VK Access Token?</h4>
					<p>Для создания ссылок на звонки требуется токен пользователя ВКонтакте с правом доступа к звонкам (метод <code>calls.start</code>):</p>
					<ol>
						<li>
							<strong>Способ 1 (VK Admin / Kate Mobile):</strong>
							Получите <code>access_token</code> через любое VK OAuth-приложение (например, Kate Mobile, VK Admin или собственное Standalone-приложение ВКонтакте) с разрешением <code>video</code>, <code>calls</code> или <code>offline</code>.
						</li>
						<li>
							<strong>Способ 2 (Токен сообщества):</strong>
							Если вы хотите, чтобы звонки создавались от имени вашей группы: перейдите в «Управление сообществом» &rarr; «Работа с API» &rarr; «Создать ключ доступа» с правами управления звонками. Укажите ID группы в поле «ID группы».
						</li>
						<li>
							<strong>Безопасность:</strong>
							Токен сохраняется локально на вашем роутере в зашифрованном / закрытом файле настроек AWGM и используется только для запросов к официальному API ВКонтакте.
						</li>
					</ol>
				</div>

				<div class="vk-help-card">
					<h4><Info size={18} /> Зачем нужны ссылки VK Calls?</h4>
					<p>
						Клиенты <strong>FreeTurn</strong> (флаг <code>-links</code>) и <strong>WDTT</strong> (флаг <code>vk-hashes</code>) используют ссылки на звонки ВКонтакте для обхода блокировок: трафик инкапсулируется в медиа-потоки звонков VK, маскируясь под обычные групповые видеозвонки.
					</p>
				</div>
			</div>
		{/if}
	</div>

	{#snippet actions()}
		<Button variant="secondary" onclick={handleClose}>Закрыть</Button>
	{/snippet}
</Modal>

<style>
	.vk-modal {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.vk-tabs-wrap {
		margin-bottom: 4px;
	}

	.vk-tab-content {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.vk-config-card,
	.vk-results-card,
	.vk-check-card,
	.vk-help-card {
		background: var(--bg-card, rgba(255, 255, 255, 0.04));
		border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
		border-radius: 8px;
		padding: 16px;
		display: flex;
		flex-direction: column;
		gap: 14px;
	}

	.vk-saved-banner {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 14px;
		background: rgba(34, 197, 94, 0.12);
		border: 1px solid rgba(34, 197, 94, 0.25);
		border-radius: 6px;
		font-size: 13px;
	}

	:global(.vk-icon-ok) {
		color: #22c55e;
		flex-shrink: 0;
	}

	.vk-saved-info {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.vk-saved-title {
		font-weight: 500;
	}

	.vk-masked {
		font-family: monospace;
		background: rgba(0, 0, 0, 0.2);
		padding: 2px 6px;
		border-radius: 4px;
	}

	.vk-saved-hint {
		margin-left: auto;
		color: var(--text-muted, #888);
		font-size: 12px;
	}

	.vk-form-grid {
		display: flex;
		gap: 12px;
		align-items: flex-start;
	}

	.vk-form-col {
		flex: 1;
		min-width: 0;
	}

	.vk-form-col-sm {
		width: 220px;
		flex-shrink: 0;
	}

	@media (max-width: 640px) {
		.vk-form-grid {
			flex-direction: column;
		}
		.vk-form-col-sm {
			width: 100%;
		}
	}

	.vk-options-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
	}

	.vk-count-picker {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.vk-label {
		font-size: 13px;
		color: var(--text-muted, #aaa);
	}

	.vk-action-bar {
		display: flex;
		justify-content: flex-end;
		margin-top: 4px;
	}

	.vk-error-box {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		background: rgba(239, 68, 68, 0.12);
		border: 1px solid rgba(239, 68, 68, 0.3);
		border-radius: 6px;
		color: #f87171;
		font-size: 13px;
	}

	.vk-results-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
	}

	.vk-results-title {
		font-weight: 600;
		font-size: 14px;
	}

	.vk-items-list {
		display: flex;
		flex-direction: column;
		gap: 6px;
		max-height: 240px;
		overflow-y: auto;
	}

	.vk-item-row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 12px;
		background: rgba(0, 0, 0, 0.15);
		border: 1px solid var(--border-color, rgba(255, 255, 255, 0.05));
		border-radius: 6px;
		font-size: 13px;
	}

	.vk-item-idx {
		color: var(--text-muted, #777);
		font-size: 12px;
		width: 16px;
	}

	.vk-item-text {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-family: monospace;
	}

	.vk-item-actions {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.vk-btn-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		border-radius: 4px;
		border: 1px solid var(--border-color, rgba(255, 255, 255, 0.1));
		background: transparent;
		color: var(--text-color, #eee);
		cursor: pointer;
		transition: background 0.15s, border-color 0.15s;
	}

	.vk-btn-icon:hover {
		background: rgba(255, 255, 255, 0.1);
		border-color: rgba(255, 255, 255, 0.25);
	}

	.vk-results-footer {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 10px;
		margin-top: 6px;
		flex-wrap: wrap;
	}

	.vk-desc {
		font-size: 13px;
		color: var(--text-muted, #aaa);
		margin: 0;
	}

	.vk-textarea {
		width: 100%;
		padding: 10px 12px;
		border-radius: 6px;
		background: var(--bg-input, rgba(0, 0, 0, 0.25));
		border: 1px solid var(--border-color, rgba(255, 255, 255, 0.15));
		color: inherit;
		font-family: monospace;
		font-size: 13px;
		resize: vertical;
		box-sizing: border-box;
	}

	.vk-status-badge {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 2px 8px;
		border-radius: 4px;
		font-size: 12px;
		font-weight: 500;
	}

	.vk-badge-ok {
		background: rgba(34, 197, 94, 0.15);
		color: #22c55e;
		border: 1px solid rgba(34, 197, 94, 0.3);
	}

	.vk-badge-err {
		background: rgba(239, 68, 68, 0.15);
		color: #f87171;
		border: 1px solid rgba(239, 68, 68, 0.3);
	}

	.vk-help-content {
		line-height: 1.5;
	}

	.vk-help-card h4 {
		margin: 0;
		font-size: 15px;
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.vk-help-card p {
		margin: 0;
		font-size: 13px;
		color: var(--text-muted, #bbb);
	}

	.vk-help-card ol {
		margin: 0;
		padding-left: 20px;
		display: flex;
		flex-direction: column;
		gap: 8px;
		font-size: 13px;
		color: var(--text-muted, #bbb);
	}

	.vk-help-card code {
		font-family: monospace;
		background: rgba(0, 0, 0, 0.2);
		padding: 1px 4px;
		border-radius: 3px;
	}
</style>
