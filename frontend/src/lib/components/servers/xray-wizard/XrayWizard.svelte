<script lang="ts">
	import { api } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';
	import WizardShell from '../wizard/WizardShell.svelte';
	import PreflightList from '../wizard/PreflightList.svelte';
	import PlanSummary from '../wizard/PlanSummary.svelte';
	import ApplyProgress from '../wizard/ApplyProgress.svelte';
	import ConnectionResult from '../wizard/ConnectionResult.svelte';
	import type {
		CapabilitiesResponse,
		PreflightResponse,
		ServerPlanRecord,
		JobStatusResponse,
		RevealCredentials,
		WizardPlanRequest
	} from '$lib/types/serverWizard';
	import { Smartphone, Monitor, Router as RouterIcon } from 'lucide-svelte';

	interface Props {
		open: boolean;
		onclose: () => void;
		onapplied?: () => void;
	}

	let { open = $bindable(false), onclose, onapplied }: Props = $props();

	const steps = [
		{ id: 'device', label: 'Устройство' },
		{ id: 'cdn', label: 'CDN и Домен' },
		{ id: 'preflight', label: 'Проверка' },
		{ id: 'plan', label: 'План' },
		{ id: 'apply', label: 'Применение' },
		{ id: 'result', label: 'Подключение' }
	];

	let currentStep = $state(0);
	let loading = $state(false);

	// Capabilities
	let caps = $state<CapabilitiesResponse | null>(null);
	let capsError = $state<string | null>(null);

	// Form State
	let deviceType = $state<'phone' | 'pc' | 'router'>('phone');
	let clientRemark = $state('Телефон (Happ)');
	let publicDomain = $state('');
	let path = $state('/cdn-bridge/');
	let mode = $state<'xhttp_get' | 'ws'>('xhttp_get');
	let cdnProfileId = $state('cdn_get');
	let upstreamDevice = $state('socks');

	// Wizard Process State
	let preflight = $state<PreflightResponse | null>(null);
	let planRecord = $state<ServerPlanRecord | null>(null);
	let currentJob = $state<JobStatusResponse | null>(null);
	let revealedCreds = $state<RevealCredentials | null>(null);
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let cancelling = $state(false);

	$effect(() => {
		if (open) {
			void initWizard();
		} else {
			cleanup();
		}
	});

	function cleanup() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
		currentStep = 0;
		loading = false;
		capsError = null;
		preflight = null;
		planRecord = null;
		currentJob = null;
		revealedCreds = null;
		cancelling = false;
	}

	async function initWizard() {
		loading = true;
		capsError = null;
		try {
			caps = await api.getWizardCapabilities('xray');
			if (caps && Array.isArray(caps.egress_options) && caps.egress_options.length > 0) {
				const hasDefault = caps.egress_options.some((o) => o.id === upstreamDevice);
				if (!hasDefault) {
					upstreamDevice = caps.egress_options[0].id;
				}
			}
		} catch (e) {
			const msg = e instanceof Error ? e.message : 'Не удалось загрузить параметры мастера';
			capsError = msg;
			notifications.error(msg);
		} finally {
			loading = false;
		}
	}

	function onSelectDevice(type: 'phone' | 'pc' | 'router') {
		deviceType = type;
		if (type === 'phone') {
			clientRemark = 'Телефон (Happ)';
			mode = 'xhttp_get';
			cdnProfileId = 'cdn_get';
		} else if (type === 'pc') {
			clientRemark = 'Компьютер (Sing-box)';
			mode = 'xhttp_get';
			cdnProfileId = 'cdn_get';
		} else {
			clientRemark = 'Удаленный роутер (Keenetic)';
			mode = 'xhttp_get';
			cdnProfileId = 'cdn_get';
		}
	}

	function buildRequest(): WizardPlanRequest {
		return {
			kind: 'xray',
			device_type: deviceType,
			client_remark: clientRemark.trim(),
			public_domain: publicDomain.trim(),
			path: path.trim(),
			mode,
			cdn_profile_id: cdnProfileId,
			public_port: 443,
			listen_port: 9008,
			upstream_device: upstreamDevice
		};
	}

	async function goNext() {
		if (currentStep === 1) {
			// Step 1 -> 2: Run preflight
			loading = true;
			try {
				preflight = await api.runWizardPreflight('xray', buildRequest());
				currentStep = 2;
			} catch (e) {
				notifications.error(e instanceof Error ? e.message : 'Ошибка проверки готовности');
			} finally {
				loading = false;
			}
		} else if (currentStep === 2) {
			// Step 2 -> 3: Generate plan
			if (!preflight?.can_proceed) {
				notifications.error('Исправьте блокирующие ошибки перед продолжением');
				return;
			}
			loading = true;
			try {
				planRecord = await api.createWizardPlan('xray', buildRequest());
				currentStep = 3;
			} catch (e) {
				notifications.error(e instanceof Error ? e.message : 'Ошибка создания плана изменений');
			} finally {
				loading = false;
			}
		} else if (currentStep === 3) {
			// Step 3 -> 4: Apply plan
			if (!planRecord) return;
			loading = true;
			try {
				const res = await api.applyWizardPlan('xray', planRecord.plan_id);
				currentStep = 4;
				startPolling(res.job_id);
			} catch (e: unknown) {
				const errMsg = e instanceof Error ? e.message : String(e);
				if (errMsg.includes('PLAN_STALE') || errMsg.includes('устарел')) {
					notifications.error('Конфигурация роутера изменилась, план устарел. Пожалуйста, выполните повторную проверку.');
					currentStep = 1;
				} else {
					notifications.error(errMsg || 'Ошибка применения плана');
				}
			} finally {
				loading = false;
			}
		} else if (currentStep === 4) {
			// Finished apply -> result
			currentStep = 5;
		} else {
			currentStep++;
		}
	}

	function goBack() {
		if (currentStep > 0 && currentStep < 4) {
			currentStep--;
		}
	}

	function startPolling(jobId: string) {
		if (pollTimer) clearInterval(pollTimer);
		pollTimer = setInterval(async () => {
			try {
				const job = await api.getWizardJob('xray', jobId);
				currentJob = job;
				if (job.phase === 'succeeded') {
					if (pollTimer) clearInterval(pollTimer);
					pollTimer = null;
					currentStep = 5;
					onapplied?.();
					void revealSecrets(jobId);
				} else if (job.phase === 'failed' || job.phase === 'cancelled' || job.phase === 'recovery_required') {
					if (pollTimer) clearInterval(pollTimer);
					pollTimer = null;
				}
			} catch {
				// Ignore transient network errors
			}
		}, 800);
	}

	async function cancelApply() {
		if (!currentJob?.id) return;
		cancelling = true;
		try {
			await api.cancelWizardJob('xray', currentJob.id);
			notifications.info('Запрос на отмену отправлен');
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Не удалось отменить операцию');
		} finally {
			cancelling = false;
		}
	}

	async function revealSecrets(jobId?: string) {
		const targetId = jobId || currentJob?.id;
		if (!targetId) return;
		loading = true;
		try {
			revealedCreds = await api.revealWizardCredentials('xray', targetId);
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка получения ключей');
		} finally {
			loading = false;
		}
	}

	const canNext = $derived.by(() => {
		if (loading || capsError != null || !caps) return false;
		if (currentStep === 0) return Boolean(clientRemark.trim());
		if (currentStep === 1) return Boolean(publicDomain.trim() && path.trim().startsWith('/'));
		if (currentStep === 2) return preflight?.can_proceed ?? false;
		if (currentStep === 3) return planRecord != null;
		if (currentStep === 4) return currentJob?.phase === 'succeeded';
		return true;
	});

	const nextLabel = $derived.by(() => {
		if (currentStep === 2) return 'Показать план';
		if (currentStep === 3) return 'Применить настройки';
		if (currentStep === 4) return 'К подключению';
		if (currentStep === 5) return 'Завершить';
		return 'Далее';
	});
</script>

<WizardShell
	bind:open
	title="Мастер настройки Xray VLESS"
	subtitle="Пошаговая настройка сервера VLESS через CDN для смартфонов, ПК и роутеров"
	{steps}
	currentStepIndex={currentStep}
	{canNext}
	canBack={currentStep > 0 && currentStep < 4}
	{nextLabel}
	{loading}
	hideFooter={currentStep === 5}
	onnext={currentStep === 5 ? onclose : goNext}
	onback={goBack}
	{onclose}
>
	{#if currentStep === 0}
		<!-- Step 0: Device Selection & Name -->
		<div class="wizard-step-content">
			{#if capsError}
				<div class="caps-error-banner">
					<div class="caps-error-icon">⚠️</div>
					<div class="caps-error-body">
						<div class="caps-error-title">Ошибка загрузки параметров мастера</div>
						<div class="caps-error-msg">{capsError}</div>
					</div>
					<button type="button" class="btn-retry" onclick={() => void initWizard()}>
						Повторить
					</button>
				</div>
			{/if}

			<div>
				<div class="field-section-title">Для какого устройства настраиваем?</div>
				<div class="device-grid">
					<!-- Phone -->
					<button
						type="button"
						class="device-card"
						class:active={deviceType === 'phone'}
						onclick={() => onSelectDevice('phone')}
					>
						<div class="device-icon">
							<Smartphone size={20} />
						</div>
						<div class="device-title">Смартфон (iOS / Android)</div>
						<div class="device-desc">
							Готовые профили для приложения Happ, v2rayNG или Streisand с поддержкой XHTTP.
						</div>
					</button>

					<!-- PC -->
					<button
						type="button"
						class="device-card"
						class:active={deviceType === 'pc'}
						onclick={() => onSelectDevice('pc')}
					>
						<div class="device-icon">
							<Monitor size={20} />
						</div>
						<div class="device-title">Компьютер (Windows / Mac / Linux)</div>
						<div class="device-desc">
							Экспорт в формате sing-box, Mihomo (Clash Meta) и Nekoray.
						</div>
					</button>

					<!-- Keenetic Router -->
					<button
						type="button"
						class="device-card"
						class:active={deviceType === 'router'}
						onclick={() => onSelectDevice('router')}
					>
						<div class="device-icon">
							<RouterIcon size={20} />
						</div>
						<div class="device-title">Второй роутер (Дача / Офис)</div>
						<div class="device-desc">
							Создание туннеля для объединения двух сетей Keenetic через CDN.
						</div>
					</button>
				</div>
			</div>

			<!-- Client Name Input -->
			<div class="form-group divider-top">
				<label for="xray-client-remark" class="field-label">
					Имя клиента (метка для ссылки) <span class="required">*</span>:
				</label>
				<input
					id="xray-client-remark"
					bind:value={clientRemark}
					placeholder="Например: Мой iPhone"
					class="field-input"
				/>
				<p class="field-hint">
					Это имя будет отображаться в списке пользователей и в названии конфигурации в клиенте.
				</p>
			</div>
		</div>

	{:else if currentStep === 1}
		<!-- Step 1: Ingress Domain & CDN Mode -->
		<div class="wizard-step-content">
			<div class="form-group">
				<label for="xray-public-domain" class="field-label">
					Публичный домен CDN ингресса <span class="required">*</span>:
				</label>
				<input
					id="xray-public-domain"
					bind:value={publicDomain}
					placeholder="cdn.example.com"
					class="field-input mono"
				/>
				<p class="field-hint">
					Домен, направленный через CDN на белый IP или DDNS вашего роутера.
				</p>
			</div>

			<div class="form-row-2">
				<div class="form-group">
					<label for="xray-path" class="field-label">Служебный путь (Path):</label>
					<input
						id="xray-path"
						bind:value={path}
						class="field-input mono"
					/>
					<p class="field-hint">
						Должен начинаться со слэша, например: <code>/cdn-bridge/</code>.
					</p>
				</div>

				<div class="form-group">
					<label for="xray-mode" class="field-label">Транспортный режим:</label>
					<select
						id="xray-mode"
						bind:value={mode}
						class="field-select"
					>
						<option value="xhttp_get">XHTTP GET (Рекомендуется для CDN)</option>
						<option value="ws">WebSocket (WS)</option>
					</select>
					<p class="field-hint">
						XHTTP GET устойчив к сбросу соединений и агрессивному кешированию CDN.
					</p>
				</div>
			</div>

			<!-- Egress Selection -->
			<div class="form-group divider-top">
				<label for="xray-egress" class="field-label">
					Маршрутизация исходящего трафика (Egress):
				</label>
				<select
					id="xray-egress"
					bind:value={upstreamDevice}
					class="field-select"
				>
					{#if caps?.egress_options}
						{#each caps.egress_options as opt}
							<option value={opt.id}>
								{opt.name} {opt.available ? '' : '(недоступен)'}
							</option>
						{/each}
					{:else}
						<option value="socks">Локальный прокси Mihomo (:1099)</option>
					{/if}
				</select>
				<p class="field-hint">
					Выберите «Локальный прокси Mihomo (:1099)» для применения умных правил роутера или конкретный сетевой интерфейс/туннель.
				</p>
			</div>
		</div>

	{:else if currentStep === 2}
		<!-- Step 2: Preflight Checks -->
		<div class="wizard-step-content">
			<p class="step-intro-text">
				Проверка наличия бинарных файлов Xray, свободных локальных портов и шлюза Mihomo:
			</p>
			{#if preflight}
				<PreflightList checks={preflight.checks} />
			{/if}
		</div>

	{:else if currentStep === 3}
		<!-- Step 3: Plan Preview -->
		<div class="wizard-step-content">
			<p class="step-intro-text">
				Проверьте планируемую конфигурацию Xray и диспетчера маршрутизации:
			</p>
			{#if planRecord?.plan}
				<PlanSummary plan={planRecord.plan} />
			{/if}
		</div>

	{:else if currentStep === 4}
		<!-- Step 4: Applying Progress -->
		{#if currentJob}
			<ApplyProgress job={currentJob} {cancelling} oncancel={cancelApply} />
		{/if}

	{:else if currentStep === 5}
		<!-- Step 5: Connection Result -->
		<div class="wizard-step-content">
			<ConnectionResult
				kind="xray"
				creds={revealedCreds}
				{loading}
				onreveal={() => void revealSecrets()}
			/>
			<div class="finish-bar">
				<button
					type="button"
					class="btn-finish"
					onclick={onclose}
				>
					Завершить работу мастера
				</button>
			</div>
		</div>
	{/if}
</WizardShell>

<style>
	.wizard-step-content {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	.field-section-title {
		font-size: 0.75rem;
		font-weight: 600;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-bottom: 0.625rem;
	}

	.step-intro-text {
		font-size: 0.8125rem;
		color: var(--color-text-secondary);
		margin: 0 0 0.5rem;
		line-height: 1.4;
	}

	.device-grid {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 0.75rem;
	}

	@media (max-width: 640px) {
		.device-grid {
			grid-template-columns: 1fr;
		}
	}

	.device-card {
		display: flex;
		flex-direction: column;
		padding: 1rem;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		text-align: left;
		cursor: pointer;
		transition: all var(--t-fast) ease;
	}

	.device-card:hover {
		background: var(--color-bg-hover);
		border-color: var(--color-border-hover);
	}

	.device-card.active {
		background: var(--color-accent-tint);
		border-color: var(--color-accent);
		box-shadow: 0 0 0 1px var(--color-accent);
	}

	.device-icon {
		width: 36px;
		height: 36px;
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		display: flex;
		align-items: center;
		justify-content: center;
		margin-bottom: 0.75rem;
		transition: all var(--t-fast) ease;
	}

	.device-card.active .device-icon {
		background: var(--color-accent);
		color: #ffffff;
	}

	.device-title {
		font-size: 0.8125rem;
		font-weight: 600;
		color: var(--color-text-primary);
		line-height: 1.3;
	}

	.device-desc {
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		margin-top: 0.375rem;
		line-height: 1.4;
	}

	.form-group {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}

	.form-row-2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.875rem;
	}

	@media (max-width: 640px) {
		.form-row-2 {
			grid-template-columns: 1fr;
		}
	}

	.divider-top {
		padding-top: 1rem;
		border-top: 1px solid var(--color-border);
	}

	.field-label {
		font-size: 0.75rem;
		font-weight: 600;
		color: var(--color-text-secondary);
	}

	.field-label .required {
		color: var(--color-error);
		margin-left: 2px;
	}

	.field-input,
	.field-select {
		width: 100%;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-primary);
		font-family: inherit;
		font-size: 0.8125rem;
		padding: 0.5rem 0.75rem;
		line-height: 1.4;
		box-sizing: border-box;
		transition: border-color var(--t-fast) ease;
	}

	.field-input.mono {
		font-family: var(--font-mono);
	}

	.field-input:focus,
	.field-select:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 2px var(--color-accent-tint);
	}

	.field-hint {
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		margin: 0;
		line-height: 1.4;
	}

	.field-hint code {
		font-family: var(--font-mono);
		background: var(--color-bg-tertiary);
		padding: 1px 4px;
		border-radius: 3px;
	}

	.finish-bar {
		display: flex;
		justify-content: flex-end;
		padding-top: 1rem;
		border-top: 1px solid var(--color-border);
	}

	.btn-finish {
		padding: 0.5rem 1.25rem;
		border-radius: var(--radius-sm);
		background: var(--color-accent);
		color: #ffffff;
		font-size: 0.8125rem;
		font-weight: 600;
		border: none;
		cursor: pointer;
		transition: background var(--t-fast) ease;
	}

	.btn-finish:hover {
		background: var(--color-accent-hover);
	}

	.caps-error-banner {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.75rem 1rem;
		background: rgba(239, 68, 68, 0.1);
		border: 1px solid rgba(239, 68, 68, 0.3);
		border-radius: var(--radius-sm);
		margin-bottom: 1rem;
	}

	.caps-error-icon {
		font-size: 1.25rem;
		flex-shrink: 0;
	}

	.caps-error-body {
		flex: 1;
	}

	.caps-error-title {
		font-size: 0.8125rem;
		font-weight: 600;
		color: var(--color-danger, #ef4444);
		margin-bottom: 0.125rem;
	}

	.caps-error-msg {
		font-size: 0.75rem;
		color: var(--color-text-muted);
		line-height: 1.3;
	}

	.btn-retry {
		padding: 0.35rem 0.75rem;
		font-size: 0.75rem;
		font-weight: 500;
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		color: var(--color-text-primary);
		cursor: pointer;
		white-space: nowrap;
		transition: background var(--t-fast) ease;
	}

	.btn-retry:hover {
		background: var(--color-bg-hover);
	}
</style>
