export interface OpenFluxConfig {
	enabled: boolean;
	mode: 'l4' | 'l3';
	transportMode: 'session' | 'single';
	transports: string[];
	mailruUrl?: string;
	yandexUrl?: string;
	directListen?: string;
	directHost?: string;
	secret?: string;
	codec?: 'batched' | 'legacy';
	openFirewall: boolean;
	debug: number;
	name?: string;
}

export interface OpenFluxStatus {
	installed: boolean;
	running: boolean;
	pid?: number;
	uptimeS?: number;
	version?: string;
	latestVersion?: string;
	archSupported: boolean;
	binary: string;
	arch: string;
	link?: string;
	error?: string;
	config?: OpenFluxConfig;
	logTail?: string;
}
