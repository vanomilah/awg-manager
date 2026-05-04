import type { WizardState, SingboxRouterPreset, SingboxRouterDNSRule, WizardResult } from '$lib/types';

export interface OrchestratorApi {
	singboxRouterListPolicies(): Promise<{ name: string }[]>;
	singboxRouterCreatePolicy(description: string): Promise<{ name: string }>;
	assignDeviceToPolicy(mac: string, policy: string): Promise<void>;
	singboxRouterListDNSServers(): Promise<{ tag: string }[]>;
	singboxRouterAddDNSServer(server: {
		tag: string;
		type: string;
		server: string;
		detour: string;
		domain_strategy?: string;
	}): Promise<void>;
	singboxRouterApplyPreset(id: string, outbound: string): Promise<void>;
	singboxRouterListDNSRules(): Promise<SingboxRouterDNSRule[]>;
	singboxRouterAddDNSRule(rule: SingboxRouterDNSRule): Promise<void>;
	singboxRouterUpdateDNSRule(index: number, rule: SingboxRouterDNSRule): Promise<void>;
	singboxRouterEnable(): Promise<void>;
	singboxRouterStatus(): Promise<{ running: boolean }>;
}

export interface OrchestratorOptions {
	api: OrchestratorApi;
	presets: SingboxRouterPreset[];
	onProgress: (label: string, status: 'running' | 'ok' | 'err') => void;
	statusTimeoutMs?: number;
}

export class WizardError extends Error {
	phase: string;
	constructor(phase: string, message: string) {
		super(message);
		this.phase = phase;
	}
}

const WIZARD_DNS_TAG = 'wizard-upstream';

function geositeTagsFromPresets(presetIds: string[], presets: SingboxRouterPreset[]): string[] {
	const out: string[] = [];
	for (const id of presetIds) {
		const p = presets.find((x) => x.id === id);
		if (!p) continue;
		for (const rs of p.ruleSets) {
			if (!out.includes(rs.tag)) out.push(rs.tag);
		}
	}
	return out;
}

async function step<T>(
	label: string,
	phase: string,
	onProgress: OrchestratorOptions['onProgress'],
	fn: () => Promise<T>,
): Promise<T> {
	onProgress(label, 'running');
	try {
		const r = await fn();
		onProgress(label, 'ok');
		return r;
	} catch (e) {
		onProgress(label, 'err');
		const msg = e instanceof Error ? e.message : String(e);
		throw new WizardError(phase, msg);
	}
}

async function waitForRunning(api: OrchestratorApi, timeoutMs: number): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		const status = await api.singboxRouterStatus();
		if (status.running) return;
		await new Promise((r) => setTimeout(r, 250));
	}
	throw new Error('sing-box не подтвердил запуск за отведённое время');
}

export async function runWizard(
	state: WizardState,
	opts: OrchestratorOptions,
): Promise<WizardResult> {
	const { api, presets, onProgress } = opts;
	const timeoutMs = opts.statusTimeoutMs ?? 5000;

	if (!state.tunnelTag) throw new WizardError('precondition', 'tunnel not selected');
	if (state.presetIds.length === 0) throw new WizardError('precondition', 'no presets selected');
	if (state.deviceMacs.length === 0) throw new WizardError('precondition', 'no devices selected');

	const tunnelTag = state.tunnelTag;

	const result: WizardResult = {
		policyCreated: false,
		devicesBound: 0,
		presetsApplied: 0,
		dnsServerCreated: false,
		dnsRuleApplied: false,
		engineStarted: false,
	};

	// Phase 1: createPolicy (idempotent — skip if already exists)
	const existingPolicies = await step(
		`Policy ${state.policyName}`,
		'createPolicy',
		onProgress,
		() => api.singboxRouterListPolicies(),
	);
	if (!existingPolicies.find((p) => p.name === state.policyName)) {
		await step(`Создаём policy ${state.policyName}`, 'createPolicy', onProgress, () =>
			api.singboxRouterCreatePolicy(state.policyName),
		);
		result.policyCreated = true;
	}

	// Phase 2: bind devices
	for (const mac of state.deviceMacs) {
		await step(`Привязка ${mac}`, 'bindDevice', onProgress, () =>
			api.assignDeviceToPolicy(mac, state.policyName),
		);
		result.devicesBound++;
	}

	// Phase 3: addDNSServer (idempotent — skip if wizard-upstream tag exists)
	const dnsServers = await step('Список DNS-серверов', 'listDNSServers', onProgress, () =>
		api.singboxRouterListDNSServers(),
	);
	if (!dnsServers.find((d) => d.tag === WIZARD_DNS_TAG)) {
		await step(
			`DNS-сервер ${state.dnsServer ?? '1.1.1.1'}`,
			'addDNSServer',
			onProgress,
			() =>
				api.singboxRouterAddDNSServer({
					tag: WIZARD_DNS_TAG,
					type: 'udp',
					server: state.dnsServer ?? '1.1.1.1',
					detour: tunnelTag,
					domain_strategy: 'ipv4_only',
				}),
		);
		result.dnsServerCreated = true;
	}

	// Phase 4: applyPresets
	for (const id of state.presetIds) {
		await step(`Применяем preset ${id}`, 'applyPreset', onProgress, () =>
			api.singboxRouterApplyPreset(id, tunnelTag),
		);
		result.presetsApplied++;
	}

	// Phase 5: addOrUpdateDNSRule (append-mode idempotency)
	const tags = geositeTagsFromPresets(state.presetIds, presets);
	const existingRules = await step('Список DNS-правил', 'listDNSRules', onProgress, () =>
		api.singboxRouterListDNSRules(),
	);
	const existingIdx = existingRules.findIndex((r) => r.server === WIZARD_DNS_TAG);
	if (existingIdx >= 0) {
		const merged = [...new Set([...(existingRules[existingIdx].rule_set ?? []), ...tags])];
		await step('Обновляем DNS-правило', 'updateDNSRule', onProgress, () =>
			api.singboxRouterUpdateDNSRule(existingIdx, {
				...existingRules[existingIdx],
				rule_set: merged,
				server: WIZARD_DNS_TAG,
			}),
		);
	} else {
		await step('Создаём DNS-правило', 'addDNSRule', onProgress, () =>
			api.singboxRouterAddDNSRule({ rule_set: tags, server: WIZARD_DNS_TAG }),
		);
	}
	result.dnsRuleApplied = true;

	// Phase 6+7: enable engine + wait for running
	await step('Запуск sing-box', 'enableEngine', onProgress, async () => {
		await api.singboxRouterEnable();
		await waitForRunning(api, timeoutMs);
	});
	result.engineStarted = true;

	return result;
}
