import type {
	ASCParams,
	AddManagedPeerRequest,
	CreateManagedServerRequest,
	ManagedPeer,
	ManagedServer,
	ManagedServerBackupFile,
	ManagedServerDriftResponse,
	ManagedServerRestoreResponse,
	ManagedServerStats,
	RestoreOptions,
	UpdateManagedPeerRequest,
	UpdateManagedServerRequest,
	WireguardServerConfig,
	XrayClient,
	XrayConfig,
	XrayStatus,
	XrayShareLinks,
	XrayMigrationStatus,
	TgWebProxyConfig,
	TgWebProxyStatus,
	TgWebProxyRevealData,
} from '$lib/types';
import type {
	XrayProfileSummaryDTO,
	XrayProfileDetailDTO,
	XrayManagedConfig,
	XrayConfigDiff,
	XrayCapabilities,
	XrayProfileRole,
} from '$lib/types/xray';
import type {
	WizardKind,
	CapabilitiesResponse,
	PreflightResponse,
	WizardPlanRequest,
	ServerPlanRecord,
	JobStatusResponse,
	RevealCredentials,
} from '$lib/types/serverWizard';
import { SystemClient } from './clientSystem';

let cachedCsrfToken: string | null = null;
let csrfTokenPromise: Promise<string> | null = null;

export function clearTgWebProxyCSRFCache(): void {
	cachedCsrfToken = null;
	csrfTokenPromise = null;
}

export class ServersClient extends SystemClient {
	override async logout(): Promise<void> {
		clearTgWebProxyCSRFCache();
		await super.logout();
	}

	async getTgWebProxyCSRF(forceRefresh = false): Promise<string> {
		if (!forceRefresh && cachedCsrfToken) {
			return cachedCsrfToken;
		}
		if (csrfTokenPromise) {
			return csrfTokenPromise;
		}
		csrfTokenPromise = (async () => {
			try {
				const res = await this.request<{ csrf_token: string }>('/servers/tgwebproxy/csrf', {
					method: 'GET'
				});
				cachedCsrfToken = res.csrf_token;
				return res.csrf_token;
			} finally {
				csrfTokenPromise = null;
			}
		})();
		return csrfTokenPromise;
	}

	private async requestWithCSRF<T>(
		endpoint: string,
		options: RequestInit = {}
	): Promise<T> {
		let token = await this.getTgWebProxyCSRF();
		const doReq = (t: string) => {
			return this.request<T>(endpoint, {
				...options,
				headers: {
					...(options.headers || {}),
					'X-CSRF-Token': t,
					'X-Requested-With': 'XMLHttpRequest',
				}
			});
		};

		try {
			return await doReq(token);
		} catch (err: any) {
			const msg = String(err?.message || '');
			if (msg.includes('CSRF') || msg.includes('csrf') || msg.includes('Forbidden') || msg.includes('403')) {
				clearTgWebProxyCSRFCache();
				token = await this.getTgWebProxyCSRF(true);
				return await doReq(token);
			}
			throw err;
		}
	}
	// ─────────────────────────────────────────────
	// #region VPN Servers — list, config, mark
	// ─────────────────────────────────────────────

	async getServerConfig(name: string): Promise<WireguardServerConfig> {
		return this.request(`/servers/config?name=${encodeURIComponent(name)}`);
	}

	async markServerInterface(name: string): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/mark?name=${encodeURIComponent(name)}`, {
			method: 'POST'
		});
	}

	async unmarkServerInterface(name: string): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/mark?name=${encodeURIComponent(name)}`, {
			method: 'DELETE'
		});
	}

	async getMarkedServerInterfaces(): Promise<string[]> {
		return this.request('/servers/marked');
	}

	async getWANIP(): Promise<string> {
		const res = await this.request<{ ip: string }>('/servers/wan-ip');
		return res.ip;
	}

	async restartManagedServer(serverId: string): Promise<{ id: string; accepted: boolean }> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/restart`, {
			method: 'POST'
		});
	}

	async restartWireguardServer(name: string): Promise<{ id: string; accepted: boolean }> {
		return this.request(`/servers/restart?name=${encodeURIComponent(name)}`, {
			method: 'POST'
		});
	}

	async setWireguardServerEnabled(
		name: string,
		enabled: boolean
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/enabled?name=${encodeURIComponent(name)}`, {
			method: 'POST',
			body: JSON.stringify({ enabled })
		});
	}

	async setWireguardServerNATMode(
		name: string,
		mode: 'full' | 'internet-only' | 'none'
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(name)}/nat`, {
			method: 'POST',
			body: JSON.stringify({ mode })
		});
	}

	async setWireguardServerNATEnabled(
		name: string,
		enabled: boolean
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(name)}/nat`, {
			method: 'POST',
			body: JSON.stringify({ enabled })
		});
	}

	async setWireguardServerPolicy(
		name: string,
		policy: string
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(name)}/policy`, {
			method: 'POST',
			body: JSON.stringify({ policy })
		});
	}

	async setWireguardServerEndpoint(
		name: string,
		endpoint: string
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(name)}/endpoint`, {
			method: 'POST',
			body: JSON.stringify({ endpoint })
		});
	}

	async addSystemServerPeer(
		serverId: string,
		data: { description: string; tunnelIP: string }
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(serverId)}/peers`, {
			method: 'POST',
			body: JSON.stringify(data)
		});
	}

	async updateSystemServerPeer(
		serverId: string,
		pubkey: string,
		data: { description: string; tunnelIP: string }
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		});
	}

	async deleteSystemServerPeer(
		serverId: string,
		pubkey: string
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}`, {
			method: 'DELETE'
		});
	}

	async toggleSystemServerPeer(
		serverId: string,
		publicKey: string,
		enabled: boolean
	): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(publicKey)}/toggle`, {
			method: 'POST',
			body: JSON.stringify({ enabled })
		});
	}

	async getSystemServerPeerConf(serverId: string, pubkey: string): Promise<string> {
		const res = await this.request<{ conf: string }>(
			`/servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}/conf`
		);
		return res.conf;
	}

	// #endregion


	// ─────────────────────────────────────────────
	// #region Managed WireGuard Server — CRUD, peers, ASC
	// ─────────────────────────────────────────────

	async getManagedServers(): Promise<ManagedServer[]> {
		return this.request('/managed-servers');
	}

	async getManagedServer(serverId: string): Promise<ManagedServer> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}`);
	}

	async createManagedServer(req: CreateManagedServerRequest): Promise<ManagedServer> {
		return this.request('/managed-servers', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async suggestManagedServerAddress(): Promise<{ address: string; mask: string }> {
		return this.request('/managed-servers/suggest-address');
	}

	async getManagedServerPolicies(): Promise<{ id: string; description: string }[]> {
		return this.request('/managed-servers/policies');
	}

	async getManagedServerStats(serverId: string): Promise<ManagedServerStats> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/stats`);
	}

	async setManagedServerPolicy(serverId: string, policy: string): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/policy`, {
			method: 'POST',
			body: JSON.stringify({ policy })
		});
	}

	async updateManagedServer(serverId: string, req: UpdateManagedServerRequest): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	}

	async setManagedServerEnabled(serverId: string, enabled: boolean): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/enabled`, {
			method: 'POST',
			body: JSON.stringify({ enabled })
		});
	}

	async setManagedServerNATMode(serverId: string, mode: 'full' | 'internet-only' | 'none'): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/nat`, {
			method: 'POST',
			body: JSON.stringify({ mode })
		});
	}

	async setManagedServerLANSegments(serverId: string, segments: string[]): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/lan-segments`, {
			method: 'POST',
			body: JSON.stringify({ segments })
		});
	}

	async listManagedLANSegments(): Promise<{ name: string; label: string; subnet: string }[]> {
		return this.request('/managed-servers/lan-segments');
	}

	async deleteManagedServer(serverId: string): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}`, {
			method: 'DELETE'
		});
	}

	async addManagedPeer(serverId: string, req: AddManagedPeerRequest): Promise<ManagedPeer> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/peers`, {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async updateManagedPeer(serverId: string, pubkey: string, req: UpdateManagedPeerRequest): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	}

	async deleteManagedPeer(serverId: string, pubkey: string): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}`, {
			method: 'DELETE'
		});
	}

	async toggleManagedPeer(serverId: string, publicKey: string, enabled: boolean): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(publicKey)}/toggle`, {
			method: 'POST',
			body: JSON.stringify({ enabled })
		});
	}

	async getManagedPeerConf(serverId: string, pubkey: string): Promise<string> {
		const res = await this.request<{ conf: string }>(`/managed-servers/${encodeURIComponent(serverId)}/peers/${encodeURIComponent(pubkey)}/conf`);
		return res.conf;
	}

	async getManagedServerASC(serverId: string): Promise<ASCParams> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/asc`);
	}

	async setManagedServerASC(serverId: string, params: ASCParams): Promise<import('$lib/stores/servers').ServersSnapshot> {
		return this.request(`/managed-servers/${encodeURIComponent(serverId)}/asc`, {
			method: 'PUT',
			body: JSON.stringify(params)
		});
	}

	// #endregion


	// ─────────────────────────────────────────────
	// #region Managed Server Backup / Restore
	// ─────────────────────────────────────────────

	async managedServerExport(): Promise<ManagedServerBackupFile> {
		return this.request<ManagedServerBackupFile>('/managed/export');
	}

	async managedServerImport(
		payload: ManagedServerBackupFile & { options: RestoreOptions },
	): Promise<ManagedServerRestoreResponse> {
		return this.request<ManagedServerRestoreResponse>('/managed/import', {
			method: 'POST',
			body: JSON.stringify(payload),
		});
	}

	async managedServerDrift(): Promise<ManagedServerDriftResponse> {
		return this.request<ManagedServerDriftResponse>('/managed/drift');
	}

	async managedServerRestoreDrift(opts: RestoreOptions): Promise<ManagedServerRestoreResponse> {
		return this.request<ManagedServerRestoreResponse>('/managed/restore-drift', {
			method: 'POST',
			body: JSON.stringify({ options: opts }),
		});
	}

	// #endregion

	// ─────────────────────────────────────────────
	// #region Xray Server & Telegram Web Proxy
	// ─────────────────────────────────────────────

	async getXrayServerConfig(): Promise<XrayConfig> {
		return this.request<XrayConfig>('/servers/xray');
	}

	async updateXrayServerConfig(cfg: Partial<XrayConfig>): Promise<XrayConfig> {
		return this.request<XrayConfig>('/servers/xray', {
			method: 'PUT',
			body: JSON.stringify(cfg),
		});
	}

	async getXrayServerStatus(): Promise<XrayStatus> {
		return this.request<XrayStatus>('/servers/xray/status');
	}

	async xrayServerAction(action: 'start' | 'stop' | 'restart'): Promise<XrayStatus> {
		return this.request<XrayStatus>('/servers/xray/action', {
			method: 'POST',
			body: JSON.stringify({ action }),
		});
	}

	async addXrayClient(remark: string): Promise<XrayClient> {
		return this.request<XrayClient>('/servers/xray/clients', {
			method: 'POST',
			body: JSON.stringify({ remark }),
		});
	}

	async deleteXrayClient(id: string): Promise<{ success: boolean }> {
		return this.request<{ success: boolean }>(`/servers/xray/clients/${encodeURIComponent(id)}`, {
			method: 'DELETE',
		});
	}

	async toggleXrayClient(id: string, enabled: boolean): Promise<{ success: boolean }> {
		return this.request<{ success: boolean }>(`/servers/xray/clients/${encodeURIComponent(id)}/toggle`, {
			method: 'POST',
			body: JSON.stringify({ enabled }),
		});
	}

	async getXrayClientLinks(id: string): Promise<XrayShareLinks> {
		return this.request<XrayShareLinks>(`/servers/xray/clients/${encodeURIComponent(id)}/link`);
	}

	async getXrayMigrationStatus(): Promise<XrayMigrationStatus> {
		return this.request<XrayMigrationStatus>('/servers/xray/migration');
	}

	async resolveXrayConflict(action: 'keep_new' | 'keep_legacy' | 'import_legacy_draft'): Promise<{ success: boolean; decision?: any }> {
		return this.request<{ success: boolean; decision?: any }>('/servers/xray/migration/resolve', {
			method: 'POST',
			body: JSON.stringify({ action }),
		});
	}

	async getTgWebProxyConfig(): Promise<TgWebProxyConfig> {
		return this.request<TgWebProxyConfig>('/servers/tgwebproxy');
	}

	async updateTgWebProxyConfig(cfg: Partial<TgWebProxyConfig>): Promise<TgWebProxyConfig> {
		return this.requestWithCSRF<TgWebProxyConfig>('/servers/tgwebproxy', {
			method: 'PUT',
			body: JSON.stringify(cfg),
		});
	}

	async getTgWebProxyStatus(): Promise<TgWebProxyStatus> {
		return this.request<TgWebProxyStatus>('/servers/tgwebproxy/status');
	}

	async revealTgWebProxySecret(): Promise<TgWebProxyRevealData> {
		return this.requestWithCSRF<TgWebProxyRevealData>('/servers/tgwebproxy/reveal', {
			method: 'POST',
		});
	}

	async tgWebProxyAction(action: 'start' | 'stop' | 'restart' | 'rotate_secret' | 'revoke_legacy' | 'clear_scanner_cache'): Promise<TgWebProxyStatus> {
		return this.requestWithCSRF<TgWebProxyStatus>('/servers/tgwebproxy/action', {
			method: 'POST',
			body: JSON.stringify({ action }),
		});
	}

	// #endregion

	// ─────────────────────────────────────────────
	// #region Server First-Run Wizards
	// ─────────────────────────────────────────────

	async getWizardCSRF(kind: WizardKind): Promise<string> {
		const res = await this.request<{ token: string }>(`/servers/${kind}/wizard/csrf`, {
			method: 'GET'
		});
		return res.token;
	}

	async getWizardCapabilities(kind: WizardKind): Promise<CapabilitiesResponse> {
		const res = await this.request<CapabilitiesResponse>(`/servers/${kind}/wizard/capabilities`, {
			method: 'GET'
		});
		return {
			...res,
			kind: res?.kind || kind,
			profiles: Array.isArray(res?.profiles) ? res.profiles : [],
			egress_options: Array.isArray(res?.egress_options) ? res.egress_options : [],
			modes: Array.isArray(res?.modes) ? res.modes : [],
			scenarios: Array.isArray(res?.scenarios) ? res.scenarios : [],
			configured: Boolean(res?.configured),
			running: Boolean(res?.running),
			recovery_required: Boolean(res?.recovery_required),
		};
	}

	async runWizardPreflight(kind: WizardKind, req: WizardPlanRequest): Promise<PreflightResponse> {
		return this.request<PreflightResponse>(`/servers/${kind}/wizard/preflight`, {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async createWizardPlan(kind: WizardKind, req: WizardPlanRequest): Promise<ServerPlanRecord> {
		return this.request<ServerPlanRecord>(`/servers/${kind}/wizard/plan`, {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async applyWizardPlan(kind: WizardKind, planId: string): Promise<{ job_id: string }> {
		const token = await this.getWizardCSRF(kind);
		return this.request<{ job_id: string }>(`/servers/${kind}/wizard/apply`, {
			method: 'POST',
			headers: {
				'X-CSRF-Token': token,
				'X-Requested-With': 'XMLHttpRequest',
			},
			body: JSON.stringify({ plan_id: planId })
		});
	}

	async getWizardJob(kind: WizardKind, jobId: string): Promise<JobStatusResponse> {
		return this.request<JobStatusResponse>(`/servers/${kind}/wizard/jobs/${jobId}`, {
			method: 'GET'
		});
	}

	async cancelWizardJob(kind: WizardKind, jobId: string): Promise<{ status: string }> {
		const token = await this.getWizardCSRF(kind);
		return this.request<{ status: string }>(`/servers/${kind}/wizard/jobs/${jobId}/cancel`, {
			method: 'POST',
			headers: {
				'X-CSRF-Token': token,
				'X-Requested-With': 'XMLHttpRequest',
			}
		});
	}

	async revealWizardCredentials(kind: WizardKind, jobId: string): Promise<RevealCredentials> {
		const token = await this.getWizardCSRF(kind);
		return this.request<RevealCredentials>(`/servers/${kind}/wizard/jobs/${jobId}/reveal`, {
			method: 'POST',
			headers: {
				'X-CSRF-Token': token,
				'X-Requested-With': 'XMLHttpRequest',
			}
		});
	}

	// #endregion

	// ─────────────────────────────────────────────
	// #region Xray Full Management Profiles & Transactions
	// ─────────────────────────────────────────────

	async xrayListProfiles(): Promise<XrayProfileSummaryDTO[]> {
		return this.request<XrayProfileSummaryDTO[]>('/servers/xray/profiles');
	}

	async xrayCreateProfile(req: {
		id?: string;
		name: string;
		role?: XrayProfileRole;
		enabled?: boolean;
		config: XrayManagedConfig;
		raw_overlay?: string;
	}): Promise<XrayProfileDetailDTO> {
		return this.request<XrayProfileDetailDTO>('/servers/xray/profiles', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async xrayGetProfile(id: string): Promise<XrayProfileDetailDTO> {
		return this.request<XrayProfileDetailDTO>(`/servers/xray/profiles/${encodeURIComponent(id)}`);
	}

	async xrayUpdateProfile(
		id: string,
		req: {
			name: string;
			role?: XrayProfileRole;
			enabled: boolean;
			config: XrayManagedConfig;
			raw_overlay?: string;
		}
	): Promise<XrayProfileDetailDTO> {
		return this.request<XrayProfileDetailDTO>(`/servers/xray/profiles/${encodeURIComponent(id)}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	}

	async xrayDeleteProfile(id: string): Promise<void> {
		await this.request<void>(`/servers/xray/profiles/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});
	}

	async xrayImportProfile(req: {
		name?: string;
		role?: XrayProfileRole;
		content: string;
	}): Promise<XrayProfileDetailDTO> {
		return this.request<XrayProfileDetailDTO>('/servers/xray/profiles/import', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async xrayExportRedactedProfile(id: string): Promise<Record<string, unknown>> {
		return this.request<Record<string, unknown>>(`/servers/xray/profiles/${encodeURIComponent(id)}/export`);
	}

	/**
	 * Mandatory Security Constraint: POST export-private never exposes credentials in query params or logs.
	 */
	async xrayExportPrivateProfile(id: string): Promise<Record<string, unknown>> {
		return this.request<Record<string, unknown>>(`/servers/xray/profiles/${encodeURIComponent(id)}/export-private`, {
			method: 'POST'
		});
	}

	async xrayPreviewProfileDiff(id: string): Promise<XrayConfigDiff> {
		return this.request<XrayConfigDiff>(`/servers/xray/profiles/${encodeURIComponent(id)}/preview`, {
			method: 'POST'
		});
	}

	async xrayApplyProfile(
		id: string
	): Promise<{ success: boolean; profile_id: string; generation_id: string }> {
		return this.request<{ success: boolean; profile_id: string; generation_id: string }>(
			`/servers/xray/profiles/${encodeURIComponent(id)}/apply`,
			{
				method: 'POST'
			}
		);
	}

	async xrayListGenerations(id: string): Promise<any[]> {
		return this.request<any[]>(`/servers/xray/profiles/${encodeURIComponent(id)}/generations`);
	}

	async xrayRollbackProfile(
		id: string,
		generationId: string
	): Promise<{ success: boolean; generation_id: string }> {
		return this.request<{ success: boolean; generation_id: string }>(
			`/servers/xray/profiles/${encodeURIComponent(id)}/rollback`,
			{
				method: 'POST',
				body: JSON.stringify({ generation_id: generationId })
			}
		);
	}

	async xrayGetCapabilities(): Promise<XrayCapabilities> {
		return this.request<XrayCapabilities>('/servers/xray/capabilities');
	}

	async xrayResolveRecovery(strategy = 'clear'): Promise<{ success: boolean }> {
		return this.request<{ success: boolean }>('/servers/xray/recovery/resolve', {
			method: 'POST',
			body: JSON.stringify({ strategy })
		});
	}

	// #endregion

}
