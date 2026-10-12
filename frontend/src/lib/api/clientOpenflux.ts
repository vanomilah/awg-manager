import type { OpenFluxConfig, OpenFluxStatus } from '$lib/types';
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
}
