<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { AlertTriangle, BrainCircuit, CheckCircle2, ChevronDown, Cpu, Eye, EyeOff, HardDrive, LockKeyhole, Power, RefreshCw, Search, Settings2, ShieldCheck, Sparkles } from 'lucide-svelte';
	import { api } from '$lib/api/client';
	import type { AIAssistantState, AIEmbeddedStatus, AIModelConfig, DownloadOutbound } from '$lib/types';
	import { Button } from '$lib/components/ui';

	let question = $state('Почему на устройствах нет интернета через выбранную политику?');
	let assistantState = $state<AIAssistantState | null>(null);
	let loading = $state(false);
	let loadError = $state('');
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let modelConfig = $state<AIModelConfig | null>(null);
	let embeddedStatus = $state<AIEmbeddedStatus | null>(null);
	let configOpen = $state(false);
	let configEnabled = $state(false);
	let configAutoFix = $state(false);
	let configProvider = $state('openai');
	let configBaseURL = $state('');
	let configModel = $state('');
	let configAPIKey = $state('');
	let configRouteTag = $state('direct');
	let configRouteKind = $state('direct');
	let routeOutbounds = $state<DownloadOutbound[]>([]);
	let showAPIKey = $state(false);
	let savingConfig = $state(false);
	let configMessage = $state('');
	let stoppingEmbedded = $state(false);
	let applyingAction = $state(false);
	let localBinaryPath = $state('');
	let localModelPath = $state('');
	let localContextSize = $state(1536);
	let localThreads = $state(2);
	let localPort = $state(11435);
	let localAutoStopMinutes = $state(10);

	const running = $derived(assistantState?.status === 'running' || loading);
	const activeProviderKeySet = $derived(modelConfig?.provider === configProvider && modelConfig.apiKeySet);

	const providerLabels: Record<string, string> = {
		openai: 'OpenAI (официальный)',
		google: 'Google Gemini',
		deepseek: 'DeepSeek',
		openrouter: 'OpenRouter',
		ollama: 'Ollama (ПК / NAS в сети)',
		local_embedded: 'Встроенный на роутере (llama-server GGUF)',
		custom: 'Пользовательский URL (OpenAI-совместимый)',
	};

	const providerDefaults: Record<string, { model: string; baseUrl: string; hint: string }> = {
		openai: { model: 'gpt-5.6-terra', baseUrl: '', hint: 'Официальный OpenAI Responses API (/v1/responses), запросы отправляются с store: false.' },
		google: { model: 'gemini-3.6-flash', baseUrl: '', hint: 'Стабильная Google Gemini Flash для generateContent API. При временной перегрузке AWG Manager автоматически повторяет запрос.' },
		deepseek: { model: 'deepseek-chat', baseUrl: 'https://api.deepseek.com/v1', hint: 'Быстрая и экономичная модель DeepSeek V3.' },
		openrouter: { model: 'openai/gpt-4o-mini', baseUrl: 'https://openrouter.ai/api/v1', hint: 'Агрегатор моделей OpenRouter.' },
		ollama: { model: 'qwen2.5:0.5b', baseUrl: 'http://192.168.90.50:11434/v1', hint: 'Локальный сервер Ollama на компьютере или домашнем сервере.' },
		local_embedded: { model: 'qwen-0.5b', baseUrl: 'http://127.0.0.1:11435/v1', hint: 'Запуск квантованной GGUF модели прямо на Keenetic ARM64 (Qwen 0.5B / 1.5B).' },
		custom: { model: '', baseUrl: '', hint: 'Любой сервер с поддержкой OpenAI /v1/chat/completions.' },
	};

	function stopPolling() {
		if (pollTimer) clearInterval(pollTimer);
		pollTimer = null;
	}

	async function refresh() {
		try {
			assistantState = await api.systemAIStatus();
			loadError = '';
			if (assistantState && assistantState.status !== 'running') stopPolling();
		} catch (error) {
			loadError = error instanceof Error ? error.message : 'Не удалось получить состояние помощника';
			stopPolling();
		}
	}

	function startPolling() {
		stopPolling();
		pollTimer = setInterval(() => void refresh(), 1200);
	}

	async function diagnose() {
		const submitted = question.trim();
		if (!submitted || running) return;
		loading = true;
		loadError = '';
		try {
			assistantState = await api.systemAIDiagnose(submitted);
			question = '';
			startPolling();
		} catch (error) {
			loadError = error instanceof Error ? error.message : 'Не удалось запустить диагностику';
		} finally {
			loading = false;
		}
	}

	async function applyProposal() {
		if (!assistantState?.proposal || assistantState.proposal.status !== 'pending') return;
		applyingAction = true;
		loadError = '';
		try {
			assistantState = await api.systemAIApplyAction(assistantState.proposal.id);
		} catch (error) {
			loadError = error instanceof Error ? error.message : 'Не удалось применить исправление';
			await refresh();
		} finally {
			applyingAction = false;
		}
	}

	function onComposerKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			event.preventDefault();
			void diagnose();
		}
	}

	function chooseQuestion(value: string) {
		question = value;
	}

	function onProviderChange(p: string) {
		configProvider = p;
		const def = providerDefaults[p];
		if (def) {
			configModel = def.model;
			configBaseURL = def.baseUrl;
		}
		configAPIKey = '';
		showAPIKey = false;
		configMessage = '';
	}

	async function loadConfig() {
		try {
			modelConfig = await api.systemAIConfig();
			if (modelConfig) {
				configEnabled = modelConfig.enabled;
				configAutoFix = modelConfig.autoFix ?? false;
				configProvider = modelConfig.provider || 'openai';
				configBaseURL = modelConfig.baseUrl || '';
				configModel = modelConfig.model || '';
				configRouteTag = modelConfig.routeTag || 'direct';
				configRouteKind = modelConfig.routeKind || 'direct';
				localBinaryPath = modelConfig.localEngine?.binaryPath || '';
				localModelPath = modelConfig.localEngine?.modelPath || '';
				localContextSize = modelConfig.localEngine?.contextSize || 1536;
				localThreads = modelConfig.localEngine?.threads || 2;
				localPort = modelConfig.localEngine?.port || 11435;
				localAutoStopMinutes = modelConfig.localEngine?.autoStopMinutes || 10;
			}
			await loadEmbeddedStatus();
		} catch (error) {
			configMessage = error instanceof Error ? error.message : 'Не удалось загрузить настройки модели';
		}
	}

	async function loadRouteOutbounds() {
		try {
			routeOutbounds = await api.listDownloadOutbounds();
		} catch {
			routeOutbounds = [{ tag: 'direct', kind: 'direct', label: 'Direct (WAN)', available: true }];
		}
	}

	function routeKey(ob: Pick<DownloadOutbound, 'tag' | 'kind'>): string {
		return `${ob.kind}\u0000${ob.tag}`;
	}

	function selectRoute(value: string) {
		const split = value.indexOf('\u0000');
		configRouteKind = split >= 0 ? value.slice(0, split) : 'direct';
		configRouteTag = split >= 0 ? value.slice(split + 1) : 'direct';
	}

	async function loadEmbeddedStatus() {
		try {
			embeddedStatus = await api.systemAIEmbedded();
		} catch {
			// ignore if not available
		}
	}

	async function stopEmbedded() {
		stoppingEmbedded = true;
		try {
			embeddedStatus = await api.systemAIEmbeddedStop();
			configMessage = '';
		} catch (error) {
			configMessage = error instanceof Error ? error.message : 'Не удалось остановить локальную модель';
		} finally {
			stoppingEmbedded = false;
		}
	}

	async function saveConfig() {
		savingConfig = true;
		configMessage = '';
		try {
			modelConfig = await api.systemAISaveConfig({
				enabled: configEnabled,
				autoFix: configAutoFix,
				provider: configProvider,
				baseUrl: configBaseURL.trim(),
				model: configModel.trim(),
				routeTag: configRouteTag,
				routeKind: configRouteKind,
				...(configAPIKey.trim() ? { apiKey: configAPIKey.trim() } : {}),
				...(configProvider === 'local_embedded' ? {
					localEngine: {
						enabled: true,
						binaryPath: localBinaryPath.trim(),
						modelPath: localModelPath.trim(),
						contextSize: localContextSize,
						threads: localThreads,
						port: localPort,
						autoStopMinutes: localAutoStopMinutes,
					},
				} : {}),
			});
			configAPIKey = '';
			configMessage = 'Настройки сохранены. Ключ хранится на роутере в отдельном файле с правами 0600.';
			await loadEmbeddedStatus();
		} catch (error) {
			configMessage = error instanceof Error ? error.message : 'Не удалось сохранить настройки';
		} finally {
			savingConfig = false;
		}
	}

	onMount(() => {
		void loadConfig();
		void loadRouteOutbounds();
		void refresh().then(() => {
			if (assistantState?.status === 'running') startPolling();
		});
	});
	onDestroy(stopPolling);
</script>

<div class="assistant-shell">
	<section class="hero">
		<div class="hero-icon"><BrainCircuit size={24} /></div>
		<div class="hero-copy">
			<h3>ИИ-помощник</h3>
			<p>Ищет неисправности в маршрутах, туннелях, DNS и системных службах по обезличенному диагностическому снимку.</p>
		</div>
		<span class="safety"><ShieldCheck size={14} /> Только чтение</span>
	</section>

	<section class="model-settings card">
		<button class="settings-head" type="button" onclick={() => (configOpen = !configOpen)} aria-expanded={configOpen}>
			<Settings2 size={17} />
			<span>
				<strong>Модельный анализ</strong>
				<small>
					{#if modelConfig?.enabled}
						{providerLabels[modelConfig.provider] || modelConfig.provider} · {modelConfig.model}
					{:else}
						Не подключён · используется встроенный локальный анализ
					{/if}
				</small>
			</span>
			{#if modelConfig?.apiKeySet}<em>Ключ сохранён</em>{/if}
			{#if embeddedStatus?.running}<em class="running-tag">llama-server PID {embeddedStatus.pid}</em>{/if}
			<ChevronDown size={16} class={configOpen ? 'opened' : ''} />
		</button>
		{#if configOpen}
			<div class="settings-body">
				<div class="privacy-warning">
					<ShieldCheck size={16} />
					<span>В модель отправляется сокращённая диагностическая выжимка без конфигов и журналов. IP-адреса и распространённые форматы ключей маскируются; не вставляйте секреты в текст вопроса.</span>
				</div>

				<label class="enable-row">
					<input type="checkbox" bind:checked={configEnabled} />
					<span>
						<strong>Использовать модель после локальной проверки</strong>
						<small>При сбое или недоступности API локальные результаты всегда сохраняются.</small>
					</span>
				</label>

				<label class="enable-row autofix-row">
					<input type="checkbox" bind:checked={configAutoFix} />
					<span>
						<strong>Автономное устранение проблем (Auto-Fix)</strong>
						<small>Разрешить ИИ автоматически применять безопасные исправления (перезапуск упавших туннелей/служб, сброс DNS) сразу после выявления сбоя.</small>
					</span>
				</label>

				<div class="provider-select-row">
					<span class="label">Провайдер</span>
					<div class="provider-chips">
						{#each Object.entries(providerLabels) as [key, label]}
							<button
								type="button"
								class="provider-chip"
								class:active={configProvider === key}
								onclick={() => onProviderChange(key)}
							>
								{label}
							</button>
						{/each}
					</div>
					{#if providerDefaults[configProvider]?.hint}
						<div class="provider-hint">{providerDefaults[configProvider].hint}</div>
					{/if}
				</div>

				<div class="config-grid">
					<label>
						<span>Модель (Model ID)</span>
						<input bind:value={configModel} placeholder="Например: gpt-5.6-terra или gemini-3.6-flash" autocomplete="off" />
					</label>

					{#if configProvider === 'ollama' || configProvider === 'custom'}
						<label>
							<span>Базовый URL (Base URL)</span>
							<input bind:value={configBaseURL} placeholder={providerDefaults[configProvider]?.baseUrl || 'По умолчанию'} autocomplete="off" />
						</label>
					{/if}

					{#if configProvider !== 'ollama' && configProvider !== 'local_embedded'}
						<label class="full-width">
							<span>API Key {activeProviderKeySet ? '(оставьте пустым, чтобы сохранить текущий)' : ''}</span>
							<div class="secret-input">
								<input
									type={showAPIKey ? 'text' : 'password'}
									bind:value={configAPIKey}
									placeholder={activeProviderKeySet ? 'Ключ уже сохранён на роутере' : (configProvider === 'google' ? 'Google AI API key' : 'sk-…')}
									autocomplete="new-password"
								/>
								<button type="button" onclick={() => (showAPIKey = !showAPIKey)} aria-label={showAPIKey ? 'Скрыть ключ' : 'Показать ключ'}>
									{#if showAPIKey}<EyeOff size={15} />{:else}<Eye size={15} />{/if}
								</button>
							</div>
						</label>
					{/if}
				</div>

				{#if configProvider !== 'local_embedded'}
					<label class="route-select">
						<span>Маршрут запросов к модели</span>
						<select
							value={`${configRouteKind}\u0000${configRouteTag}`}
							onchange={(event) => selectRoute(event.currentTarget.value)}
						>
							{#each routeOutbounds as outbound}
								<option value={routeKey(outbound)} disabled={!outbound.available}>
									{outbound.label}{outbound.detail ? ` — ${outbound.detail}` : ''}{outbound.available ? '' : ' (недоступен)'}
								</option>
							{/each}
						</select>
						<small>Запрос не пойдёт напрямую, если выбранный туннель недоступен.</small>
					</label>
				{/if}

				{#if configProvider === 'local_embedded' && embeddedStatus}
					<div class="embedded-card">
						<div class="embedded-head">
							<Cpu size={16} />
							<strong>Встроенный движок роутера (NC-1812)</strong>
							{#if embeddedStatus.running}
								<span class="badge-running">Активен (PID {embeddedStatus.pid})</span>
							{:else}
								<span class="badge-idle">Остановлен (запуск по требованию)</span>
							{/if}
						</div>
						<div class="embedded-stats">
							<div>
								<span>Доступно RAM:</span>
								<b>{embeddedStatus.memAvailableMB} МБ</b>
							</div>
							<div>
								<span>Бинарник:</span>
								<b>{embeddedStatus.binaryExists ? (embeddedStatus.binaryPath || 'Найден') : 'Не найден в /opt/bin/llama-server'}</b>
							</div>
							<div>
								<span>GGUF модель:</span>
								<b>{embeddedStatus.modelExists ? (embeddedStatus.modelPath || 'Найдена') : 'Не найдена в /opt/storage/ai/*.gguf'}</b>
							</div>
						</div>
						<div class="local-config-grid">
							<label><span>Путь к llama-server</span><input bind:value={localBinaryPath} placeholder="Автопоиск в /opt/bin" /></label>
							<label><span>Путь к GGUF</span><input bind:value={localModelPath} placeholder="Автопоиск в /opt/storage/ai" /></label>
							<label><span>Контекст</span><input type="number" min="256" max="4096" step="128" bind:value={localContextSize} /></label>
							<label><span>Потоки CPU</span><input type="number" min="1" max="3" bind:value={localThreads} /></label>
							<label><span>Порт</span><input type="number" min="1024" max="65535" bind:value={localPort} /></label>
							<label><span>Автостоп, мин.</span><input type="number" min="1" max="60" bind:value={localAutoStopMinutes} /></label>
						</div>
						{#if embeddedStatus.running && embeddedStatus.managed}
							<div class="embedded-actions">
								<Button variant="danger" size="sm" loading={stoppingEmbedded} onclick={stopEmbedded}>
									<Power size={14} /> Выгрузить модель из RAM
								</Button>
							</div>
						{:else if embeddedStatus.running}
							<div class="provider-hint">Сервер запущен внешним процессом. AWG Manager использует его, но не будет принудительно останавливать.</div>
						{/if}
					</div>
				{/if}

				<div class="settings-actions">
					<span class:error-text={configMessage.toLowerCase().includes('ошиб') || configMessage.toLowerCase().includes('required')}>{configMessage}</span>
					<Button variant="primary" loading={savingConfig} onclick={saveConfig}>Сохранить подключение</Button>
				</div>
			</div>
		{/if}
	</section>

	<section class="chat card" aria-live="polite">
		{#if !assistantState?.messages?.length}
			<div class="chat-empty">
				<BrainCircuit size={22} />
				<strong>Чем помочь с роутером?</strong>
				<span>Задайте вопрос обычными словами. Помощник сам выберет безопасные проверки.</span>
			</div>
		{:else}
			{#each assistantState.messages as message}
				<div class="chat-row" class:user={message.role === 'user'}>
					<div class="chat-bubble">
						<span>{message.role === 'user' ? 'Вы' : 'ИИ-помощник'}</span>
						<p>{message.content}</p>
					</div>
				</div>
			{/each}
			{#if running}
				<div class="chat-row"><div class="chat-bubble thinking"><span class="spinner"></span><p>{assistantState?.progress || 'Анализирую…'}</p></div></div>
			{/if}
		{/if}
	</section>

	<section class="composer card">
		<label for="ai-question">Сообщение</label>
		<textarea id="ai-question" bind:value={question} maxlength="1000" rows="2" disabled={running} onkeydown={onComposerKeydown} placeholder="Напишите вопрос об интернете, DNS, туннелях или маршрутах…"></textarea>
		<div class="suggestions">
			<button type="button" onclick={() => chooseQuestion('Почему на устройствах нет интернета через выбранную политику?')}>Нет интернета</button>
			<button type="button" onclick={() => chooseQuestion('Проверь маршруты и возможные утечки трафика мимо VPN.')}>Маршруты и утечки</button>
			<button type="button" onclick={() => chooseQuestion('Проверь работу DNS и возможную утечку DNS.')}>DNS</button>
			<button type="button" onclick={() => chooseQuestion('Почему не запускается выбранный прокси-движок?')}>Прокси-движок</button>
		</div>
		<div class="composer-footer">
			<span><LockKeyhole size={14} /> Диагностика сокращается и маскируется; не указывайте секреты в вопросе.</span>
			<Button variant="primary" size="md" loading={running} onclick={diagnose} disabled={!question.trim()}>
				<Search size={16} /> {running ? 'Проверяю…' : 'Отправить'}
			</Button>
		</div>
	</section>

	{#if loadError || assistantState?.status === 'error'}
		<div class="message error" role="alert">
			<AlertTriangle size={18} />
			<div><strong>Диагностика не завершена</strong><span>{loadError || assistantState?.error}</span></div>
		</div>
	{:else if assistantState?.status === 'running'}
		<div class="message running" aria-live="polite">
			<span class="spinner"></span>
			<div><strong>Собираю диагностический снимок</strong><span>{assistantState.progress || 'Выполняются безопасные проверки…'}</span></div>
		</div>
	{:else if assistantState?.status === 'done' && assistantState.intent?.kind !== 'chat'}
		<section class="results">
			{#if assistantState.proposal}
				<section class="remediation card">
					<div>
						<AlertTriangle size={18} />
						<span>
							<strong>{assistantState.proposal.title}</strong>
							<small>{assistantState.proposal.description}</small>
							{#if assistantState.proposal.verification}
								<div class="verification-badge" class:passed={assistantState.proposal.verification.status === 'passed'} class:warning={assistantState.proposal.verification.status === 'warning'}>
									<CheckCircle2 size={13} /> {assistantState.proposal.verification.summary}
								</div>
							{/if}
						</span>
					</div>
					{#if assistantState.proposal.status === 'pending'}
						<Button variant="primary" loading={applyingAction} onclick={applyProposal}>Применить исправление</Button>
					{:else}
						<em class:failed={assistantState.proposal.status === 'failed'}>
							{assistantState.proposal.autoApplied ? 'Применено автоматически (Auto-Fix)' : assistantState.proposal.status === 'applied' ? 'Применено' : assistantState.proposal.error || assistantState.proposal.status}
						</em>
					{/if}
				</section>
			{/if}
			{#if assistantState.toolSteps?.length}
				<section class="tool-timeline card">
					<div class="tool-timeline-head">
						<Search size={17} />
						<div><strong>Адресные проверки</strong><span>Выбраны по смыслу вопроса · {assistantState.intent?.kind}</span></div>
					</div>
					{#each assistantState.toolSteps as step}
						<article class="tool-step" class:tool-error={step.status === 'error'} class:tool-warning={step.status === 'warning'}>
							<div class="tool-step-title"><span class="tool-dot"></span><strong>{step.title}</strong><em>{step.status === 'passed' ? 'Готово' : step.status === 'warning' ? 'Внимание' : 'Ошибка'}</em><small>{step.durationMs} мс</small></div>
							<p>{step.summary}</p>
							{#if step.evidence?.length}
								<div class="tool-evidence">{#each step.evidence as evidence}<code>{evidence}</code>{/each}</div>
							{/if}
						</article>
					{/each}
				</section>
			{/if}
			<div class="summary card">
				<div>
					<span class="eyebrow">Результат локального анализа</span>
					<strong>{assistantState.summary}</strong>
				</div>
				<div class="stats">
					<span class="ok"><b>{assistantState.stats.passed}</b> успешно</span>
					<span class:bad={assistantState.stats.failed > 0}><b>{assistantState.stats.failed}</b> отклонений</span>
					<span><b>{assistantState.stats.skipped}</b> пропущено</span>
				</div>
			</div>

			{#if assistantState.modelAnswer}
				<article class="model-answer card">
					<div>
						<Sparkles size={18} />
						<strong>Ответ модели</strong>
						<span>{providerLabels[assistantState.engine] || assistantState.engine}</span>
					</div>
					<p>{assistantState.modelAnswer}</p>
				</article>
			{:else if assistantState.modelError}
				<div class="message error">
					<AlertTriangle size={18} />
					<div><strong>Модельный анализ недоступен</strong><span>{assistantState.modelError}. Локальные результаты показаны ниже.</span></div>
				</div>
			{/if}

			{#if assistantState.findings.length === 0}
				<div class="message success">
					<CheckCircle2 size={18} />
					<div><strong>Явных неисправностей не найдено</strong><span>Если проблема сохраняется, откройте «Соединения» и повторите проверку во время активного трафика.</span></div>
				</div>
			{:else}
				<div class="finding-list">
					{#each assistantState.findings as finding}
						<article class="finding card" class:critical={finding.severity === 'critical'}>
							<div class="finding-head">
								<AlertTriangle size={17} />
								<strong>{finding.title}</strong>
								<span>{finding.severity === 'critical' ? 'Критично' : 'Внимание'}</span>
							</div>
							<p>{finding.detail || 'Проверка завершилась с отклонением.'}</p>
							<div class="recommendation"><b>Что проверить:</b> {finding.recommendation}</div>
						</article>
					{/each}
				</div>
			{/if}

			<div class="readonly-note">
				<ShieldCheck size={16} /> Помощник ничего не изменил на роутере. Диагностика строго безопасна и не изменяет настройки без вашего подтверждения.
			</div>
		</section>
	{/if}
</div>

<style>
	.assistant-shell{display:grid;gap:14px}.card{border:1px solid var(--border);border-radius:10px;background:var(--bg-secondary);box-shadow:var(--shadow-sm)}
	.chat{display:grid;gap:10px;min-height:190px;max-height:480px;overflow:auto;padding:16px}.chat-empty{display:grid;place-items:center;align-content:center;gap:7px;min-height:155px;color:var(--text-muted);text-align:center}.chat-empty strong{color:var(--text-primary);font-size:14px}.chat-empty span{max-width:430px;font-size:11px}.chat-row{display:flex;justify-content:flex-start}.chat-row.user{justify-content:flex-end}.chat-bubble{max-width:min(78%,680px);padding:10px 12px;border:1px solid var(--border);border-radius:4px 12px 12px 12px;background:var(--bg-primary)}.chat-row.user .chat-bubble{border-color:color-mix(in srgb,var(--accent) 35%,var(--border));border-radius:12px 4px 12px 12px;background:var(--accent-soft)}.chat-bubble>span{color:var(--text-muted);font-size:9px;font-weight:600;text-transform:uppercase;letter-spacing:.04em}.chat-bubble p{margin:5px 0 0;color:var(--text-secondary);font-size:12px;line-height:1.6;white-space:pre-wrap}.chat-bubble.thinking{display:flex;align-items:center;gap:9px}.chat-bubble.thinking p{margin:0;color:var(--text-muted)}
	.hero{display:flex;align-items:center;gap:13px;padding:4px 2px}.hero-icon{display:grid;place-items:center;width:44px;height:44px;border-radius:12px;background:var(--accent-soft);color:var(--accent)}.hero-copy{min-width:0;flex:1}.hero h3,.hero p{margin:0}.hero h3{font-size:17px}.hero p{margin-top:4px;color:var(--text-muted);font-size:12px;line-height:1.5}.safety{display:inline-flex;align-items:center;gap:5px;padding:5px 8px;border:1px solid color-mix(in srgb,var(--success) 40%,var(--border));border-radius:999px;color:var(--success);font-size:11px;white-space:nowrap}
	.composer{display:grid;gap:9px;padding:16px}.composer label{font-size:12px;font-weight:600}.composer textarea{box-sizing:border-box;width:100%;resize:vertical;min-height:82px;padding:10px 12px;border:1px solid var(--border);border-radius:8px;background:var(--bg-primary);color:var(--text-primary);font:inherit;line-height:1.45}.composer textarea:focus{outline:2px solid color-mix(in srgb,var(--accent) 25%,transparent);border-color:var(--accent)}.suggestions{display:flex;flex-wrap:wrap;gap:6px}.suggestions button{padding:5px 8px;border:1px solid var(--border);border-radius:999px;background:var(--bg-primary);color:var(--text-secondary);font:inherit;font-size:11px;cursor:pointer}.suggestions button:hover{border-color:var(--accent);color:var(--accent)}.composer-footer{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:3px}.composer-footer>span{display:flex;align-items:center;gap:5px;color:var(--text-muted);font-size:11px}
	.settings-head{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto auto;align-items:center;gap:10px;width:100%;padding:12px 14px;border:0;background:transparent;color:var(--text-primary);text-align:left;cursor:pointer}.settings-head>span{display:grid;gap:2px}.settings-head small{color:var(--text-muted);font-size:11px;font-weight:400}.settings-head em{padding:3px 6px;border-radius:5px;background:var(--success-soft);color:var(--success);font-size:10px;font-style:normal}.settings-head .running-tag{background:var(--accent-soft);color:var(--accent)}.settings-head :global(svg:last-child){transition:transform .15s}.settings-head :global(svg.opened){transform:rotate(180deg)}.settings-body{display:grid;gap:12px;padding:0 14px 14px;border-top:1px solid var(--border)}.privacy-warning{display:flex;align-items:flex-start;gap:7px;margin-top:12px;padding:9px;border-radius:7px;background:var(--bg-primary);color:var(--text-muted);font-size:11px;line-height:1.45}.enable-row{display:flex;align-items:flex-start;gap:9px}.enable-row>span{display:grid;gap:2px}.enable-row small{color:var(--text-muted);font-size:11px}
	.provider-select-row{display:grid;gap:6px}.provider-select-row .label{font-size:11px;color:var(--text-secondary)}.provider-chips{display:flex;flex-wrap:wrap;gap:6px}.provider-chip{padding:5px 9px;border:1px solid var(--border);border-radius:7px;background:var(--bg-primary);color:var(--text-secondary);font:inherit;font-size:11px;cursor:pointer;transition:all .15s}.provider-chip:hover{border-color:var(--accent);color:var(--text-primary)}.provider-chip.active{border-color:var(--accent);background:var(--accent-soft);color:var(--accent);font-weight:600}.provider-hint{padding:4px 2px;color:var(--text-muted);font-size:11px}
	.config-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.config-grid label{display:grid;gap:5px;color:var(--text-secondary);font-size:11px}.config-grid label.full-width{grid-column:1 / -1}.config-grid input{box-sizing:border-box;width:100%;padding:8px 9px;border:1px solid var(--border);border-radius:7px;background:var(--bg-primary);color:var(--text-primary);font:inherit}.secret-input{display:grid;grid-template-columns:1fr auto}.secret-input input{border-radius:7px 0 0 7px}.secret-input button{display:grid;place-items:center;padding:0 9px;border:1px solid var(--border);border-left:0;border-radius:0 7px 7px 0;background:var(--bg-primary);color:var(--text-muted);cursor:pointer}
	.embedded-card{padding:12px;border:1px solid var(--border);border-radius:8px;background:var(--bg-primary);display:grid;gap:8px}.embedded-head{display:flex;align-items:center;gap:8px;font-size:12px}.embedded-head strong{flex:1}.badge-running{padding:2px 6px;border-radius:4px;background:var(--success-soft);color:var(--success);font-size:10px;font-weight:600}.badge-idle{padding:2px 6px;border-radius:4px;background:var(--bg-secondary);color:var(--text-muted);font-size:10px}.embedded-stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:6px;font-size:11px;color:var(--text-muted)}.embedded-stats b{color:var(--text-primary)}.embedded-actions{display:flex;justify-content:flex-end;margin-top:4px}
	.local-config-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;padding-top:8px;border-top:1px solid var(--border)}.local-config-grid label{display:grid;gap:4px;color:var(--text-secondary);font-size:11px}.local-config-grid input{box-sizing:border-box;width:100%;padding:7px 8px;border:1px solid var(--border);border-radius:7px;background:var(--bg-secondary);color:var(--text-primary);font:inherit}
	.settings-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px}.settings-actions>span{flex:1;color:var(--success);font-size:11px}.settings-actions .error-text{color:var(--danger)}
	.message{display:flex;align-items:flex-start;gap:10px;padding:13px 15px;border:1px solid var(--border);border-radius:9px;background:var(--bg-secondary)}.message>div{display:grid;gap:3px}.message strong{font-size:13px}.message span{color:var(--text-muted);font-size:12px}.message.error{border-color:color-mix(in srgb,var(--danger) 45%,var(--border));color:var(--danger)}.message.running{color:var(--accent)}.message.success{color:var(--success)}.spinner{width:16px;height:16px;border:2px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:spin .8s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
	.results,.finding-list{display:grid;gap:10px}.summary{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:14px 16px}.summary>div:first-child{display:grid;gap:5px}.eyebrow{color:var(--text-muted);font-size:10px;text-transform:uppercase;letter-spacing:.05em}.summary strong{font-size:13px}.stats{display:flex;gap:8px;flex-wrap:wrap}.stats span{padding:6px 8px;border-radius:7px;background:var(--bg-primary);color:var(--text-muted);font-size:11px}.stats .ok{color:var(--success)}.stats .bad{color:var(--danger)}
	.finding{padding:14px 16px;border-left:3px solid var(--warning)}.finding.critical{border-left-color:var(--danger)}.finding-head{display:flex;align-items:center;gap:8px;color:var(--warning)}.finding.critical .finding-head{color:var(--danger)}.finding-head strong{flex:1;color:var(--text-primary);font-size:13px}.finding-head span{padding:3px 6px;border-radius:5px;background:var(--bg-primary);font-size:10px}.finding p{margin:9px 0;color:var(--text-secondary);font-size:12px;line-height:1.5}.recommendation{padding:9px 10px;border-radius:7px;background:var(--bg-primary);color:var(--text-secondary);font-size:11px;line-height:1.5}.readonly-note{display:flex;align-items:flex-start;gap:7px;padding:9px 2px;color:var(--text-muted);font-size:11px;line-height:1.45}
	.model-answer{padding:15px 16px}.model-answer>div{display:flex;align-items:center;gap:8px;color:var(--accent)}.model-answer>div strong{color:var(--text-primary);font-size:13px}.model-answer>div span{margin-left:auto;color:var(--text-muted);font-size:10px}.model-answer p{margin:11px 0 0;color:var(--text-secondary);font-size:12px;line-height:1.65;white-space:pre-wrap}
	.tool-timeline{padding:14px 16px;display:grid;gap:10px}.tool-timeline-head{display:flex;align-items:center;gap:9px;color:var(--accent)}.tool-timeline-head>div{display:grid;gap:2px}.tool-timeline-head strong{color:var(--text-primary);font-size:13px}.tool-timeline-head span{color:var(--text-muted);font-size:10px}.tool-step{display:grid;gap:6px;padding:10px 11px;border:1px solid var(--border);border-radius:8px;background:var(--bg-primary)}.tool-step-title{display:flex;align-items:center;gap:7px}.tool-step-title strong{font-size:12px}.tool-step-title em{padding:2px 5px;border-radius:4px;background:var(--success-soft);color:var(--success);font-size:9px;font-style:normal}.tool-step-title small{margin-left:auto;color:var(--text-muted);font-size:9px}.tool-dot{width:7px;height:7px;border-radius:50%;background:var(--success)}.tool-step.tool-warning .tool-dot{background:var(--warning)}.tool-step.tool-warning em{background:var(--warning-soft);color:var(--warning)}.tool-step.tool-error .tool-dot{background:var(--danger)}.tool-step.tool-error em{background:var(--danger-soft);color:var(--danger)}.tool-step p{margin:0;color:var(--text-secondary);font-size:11px}.tool-evidence{display:grid;gap:4px}.tool-evidence code{overflow-wrap:anywhere;padding:6px 7px;border-radius:5px;background:var(--bg-secondary);color:var(--text-muted);font-size:10px}
	.remediation{display:flex;align-items:center;justify-content:space-between;gap:14px;padding:14px 16px;border-color:color-mix(in srgb,var(--warning) 45%,var(--border))}.remediation>div{display:flex;align-items:flex-start;gap:9px;color:var(--warning)}.remediation span{display:grid;gap:4px}.remediation strong{color:var(--text-primary);font-size:13px}.remediation small{max-width:650px;color:var(--text-muted);font-size:11px;line-height:1.45}.remediation em{color:var(--success);font-size:11px;font-style:normal}.remediation em.failed{color:var(--danger)}
	.verification-badge{display:inline-flex;align-items:center;gap:5px;margin-top:5px;padding:3px 8px;border-radius:6px;background:var(--success-soft);color:var(--success);font-size:11px;font-weight:500}.verification-badge.warning{background:var(--warning-soft);color:var(--warning)}
	.autofix-row{border-top:1px dashed var(--border);padding-top:8px}
	@media(max-width:720px){.hero,.composer-footer,.summary{align-items:stretch;flex-direction:column}.safety{align-self:flex-start}.composer-footer :global(.btn){width:100%}.stats{justify-content:flex-start}.config-grid,.local-config-grid{grid-template-columns:1fr}.settings-actions{align-items:stretch;flex-direction:column}.settings-actions :global(.btn){width:100%}}
</style>
