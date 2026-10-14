import type {
	OpenFluxClientConfig,
	OpenFluxClientStatus,
	OpenFluxConfig,
	OpenFluxShareConfig,
	OpenFluxStatus,
} from '$lib/types';
import { TelemtClient } from './clientTelemt';

export class OpenFluxClient extends TelemtClient {
	// ─────────────────────────────────────────────
	// #region OpenFlux Exit Node
	// ─────────────────────────────────────────────

	async openfluxStatus(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/status');
	}

	async openfluxSaveConfig(cfg: OpenFluxConfig): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/config', {
			method: 'POST',
			body: JSON.stringify(cfg),
		});
	}

	async openfluxInstall(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/install', { method: 'POST' });
	}

	async openfluxStart(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/start', { method: 'POST' });
	}

	async openfluxStop(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/stop', { method: 'POST' });
	}

	async openfluxRestart(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/restart', { method: 'POST' });
	}

	async openfluxLink(): Promise<{ link: string }> {
		return this.request<{ link: string }>('/openflux/link');
	}

	async openfluxLogs(lines = 100): Promise<{ logs: string }> {
		return this.request<{ logs: string }>(`/openflux/logs?lines=${lines}`);
	}

	// #endregion

	// ─────────────────────────────────────────────
	// #region OpenFlux Client (Outbound Proxy)
	// ─────────────────────────────────────────────

	async openfluxClientStatus(): Promise<OpenFluxClientStatus> {
		return this.request<OpenFluxClientStatus>('/openflux/client/status');
	}

	async openfluxSaveClientConfig(cfg: OpenFluxClientConfig): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/client/config', {
			method: 'POST',
			body: JSON.stringify(cfg),
		});
	}

	async openfluxClientSaveConfig(cfg: OpenFluxClientConfig): Promise<OpenFluxStatus> {
		return this.openfluxSaveClientConfig(cfg);
	}

	async openfluxClientStart(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/client/start', { method: 'POST' });
	}

	async openfluxClientStop(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/client/stop', { method: 'POST' });
	}

	async openfluxClientRestart(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/client/restart', { method: 'POST' });
	}

	async openfluxParseLink(
		link: string,
	): Promise<{ share: OpenFluxShareConfig; config: OpenFluxClientConfig }> {
		return this.request<{ share: OpenFluxShareConfig; config: OpenFluxClientConfig }>(
			'/openflux/client/parse-link',
			{
				method: 'POST',
				body: JSON.stringify({ link }),
			},
		);
	}

	async openfluxClientLogs(lines = 100): Promise<{ logs: string }> {
		return this.request<{ logs: string }>(`/openflux/client/logs?lines=${lines}`);
	}

	async openfluxClientDelete(): Promise<OpenFluxStatus> {
		return this.request<OpenFluxStatus>('/openflux/client/delete', { method: 'POST' });
	}

	// #endregion
}

