export interface OpenFluxConfig {
	enabled: boolean;
	mode: 'l4' | 'l3';
	transportMode: 'session' | 'single';
	transports: string[];
	mailruUrl?: string;
	yandexUrl?: string;
	vyandexUrl?: string;
	cupsonlineUrl?: string;
	telemostUrl?: string;
	directListen?: string;
	directHost?: string;
	secret?: string;
	codec?: 'batched' | 'legacy';
	openFirewall: boolean;
	debug: number;
	name?: string;

	// Routing, NAT and Egress matching WDTT server
	natMode?: 'full' | 'internet-only' | 'none';
	natStaticWan?: string;
	natStaticWans?: string[];
	policy?: string;
	lanSegments?: string[];
	exposeToPolicies?: boolean;
	egressInterface?: string;
	ndmsIface?: string;
	tunIface?: string;
}

export interface OpenFluxClientConfig {
	enabled: boolean;
	name?: string;
	inboundMode?: 'tun' | 'socks5';
	ndmsIface?: string;
	tunIface?: string;
	tunAddr?: string;
	tunMtu?: number;
	policy?: string;
	exposeToPolicies?: boolean;
	defaultRoute?: boolean;
	transportMode: 'session' | 'single' | 'stream';
	transports: string[];
	directDial?: string;
	mailruUrl?: string;
	yandexUrl?: string;
	vyandexUrl?: string;
	cupsonlineUrl?: string;
	telemostUrl?: string;
	secret?: string;
	context?: string;
	codec?: 'batched' | 'legacy';
	socks5Listen?: string;
	httpProxyListen?: string;
	autoReconnect: boolean;
	debug: number;
}

export interface OpenFluxShareTransport {
	type: string;
	name?: string;
	url?: string;
	priority?: number;
	dial?: string;
}

export interface OpenFluxShareConfig {
	name?: string;
	negotiate?: boolean;
	codec?: string;
	secret?: string;
	context?: string;
	mode?: string;
	transports: OpenFluxShareTransport[];
}

export interface OpenFluxClientStatus {
	installed?: boolean;
	running: boolean;
	pid?: number;
	uptimeS?: number;
	inboundMode?: 'tun' | 'socks5';
	ndmsIface?: string;
	tunIface?: string;
	tunAddr?: string;
	policy?: string;
	socks5Port?: number;
	socks5Uri?: string;
	httpUri?: string;
	error?: string;
	config?: OpenFluxClientConfig;
	logTail?: string;
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
	client?: OpenFluxClientStatus;
}

