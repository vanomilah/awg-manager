// ─────────────────────────────────────────────
// #region Servers — WireGuard, managed server
// ─────────────────────────────────────────────

export interface WireguardServer {
	id: string;
	interfaceName: string;
	description: string;
	status: 'up' | 'down';
	connected: boolean;
	mtu: number;
	address: string;
	mask: string;
	publicKey: string;
	listenPort: number;
	peers: WireguardServerPeer[];
	natEnabled?: boolean;
	natMode?: 'full' | 'internet-only' | 'none';
	policy?: string;
	keenDnsDomain?: string;
	/** User-configured connect host for client .conf; empty = WAN IP at generation. */
	endpoint?: string;
	builtIn?: boolean;
	/**
	 * False when the backend failed to read NAT mode / policy from NDMS
	 * (e.g. transient router error). When false, natMode/policy are NOT
	 * trustworthy and the UI must show an "unknown" state rather than the
	 * zero-valued 'none'. Absent (legacy/managed) is treated as known.
	 */
	natModeKnown?: boolean;
	policyKnown?: boolean;
	/** NDMS admin intent (conf layer running). Prefer over status/connected for toggles. */
	enabled?: boolean;
	enabledKnown?: boolean;
}

export interface WireguardServerPeer {
	publicKey: string;
	description: string;
	endpoint: string;
	allowedIPs?: string[];
	rxBytes: number;
	txBytes: number;
	lastHandshake: string;
	online: boolean;
	enabled: boolean;
	confAvailable?: boolean;
}

export interface WireguardServerConfig {
	publicKey: string;
	listenPort: number;
	mtu: number;
	address: string;
	peers: WireguardServerPeerConfig[];
}

export interface WireguardServerPeerConfig {
	publicKey: string;
	description: string;
	presharedKey: string;
	allowedIPs: string[];
	address: string;
}

export interface ManagedServer {
	interfaceName: string;
	description?: string;
	address: string;
	mask: string;
	listenPort: number;
	endpoint?: string;
	dns?: string;
	mtu?: number;
	natEnabled?: boolean;
	natMode?: 'full' | 'internet-only' | 'none';
	lanSegments?: string[];
	policy: string;
	peers: ManagedPeer[];
}

export interface ManagedPeer {
	publicKey: string;
	privateKey: string;
	presharedKey: string;
	description: string;
	tunnelIP: string;
	dns?: string;
	enabled: boolean;
}

export interface ManagedServerStats {
	status: string;
	peers: ManagedPeerStats[];
}

export interface ManagedPeerStats {
	publicKey: string;
	endpoint: string;
	rxBytes: number;
	txBytes: number;
	lastHandshake: string;
	online: boolean;
}

export interface CreateManagedServerRequest {
	address: string;
	mask: string;
	listenPort: number;
	description?: string;
	endpoint?: string;
	dns?: string;
	mtu?: number;
	generateAsc?: boolean;
}

// UpdateManagedServerRequest matches the Go-side pointer-field semantics:
// - omit a field entirely (do not include it in the body) to PRESERVE the existing value
// - include a field (even with empty string / 0) to SET it (empty string CLEARS)
// Build the payload conditionally on the call site so a value the user
// didn't touch never appears in the request.
export interface UpdateManagedServerRequest {
	address: string;
	mask: string;
	listenPort: number;
	description?: string;
	endpoint?: string;
	dns?: string;
	mtu?: number;
}

export interface AddManagedPeerRequest {
	description: string;
	tunnelIP: string;
	dns?: string;
}

export interface UpdateManagedPeerRequest {
	description: string;
	tunnelIP: string;
	dns?: string;
}

// #endregion

// #region Managed Server Backup / Restore
// ─────────────────────────────────────────────

/**
 * Single managed server entry as exported to a backup file.
 * Shape mirrors storage.ManagedServer JSON; policy is optional
 * because newly-created servers may not have one assigned yet.
 */
export interface ManagedServerExport {
	interfaceName: string;
	description?: string;
	address: string;
	mask: string;
	listenPort: number;
	endpoint?: string;
	dns?: string;
	mtu?: number;
	natEnabled?: boolean;
	policy?: string;
	privateKey?: string;
	i1?: string;
	i2?: string;
	i3?: string;
	i4?: string;
	i5?: string;
	peers: ManagedPeer[];
}

export interface ManagedServerBackupFile {
	version: number;
	type: string;
	exportedAt: string;
	managedServers: ManagedServerExport[];
	warnings?: Array<{
		interfaceName?: string;
		message: string;
	}>;
}

export interface RestoreOptions {
	allowRenumber: boolean;
}

export interface RestoreOutcome {
	name: string;
	newName?: string;
	action: 'created' | 'merged' | 'renamed' | 'conflict' | 'failed';
	addedPeers?: number;
	conflicts?: string[];
	error?: string;
}

export interface ManagedServerRestoreResponse {
	outcomes: RestoreOutcome[];
}

export interface ManagedServerDriftResponse {
	drift: ManagedServerExport[];
}

// #endregion

// ─────────────────────────────────────────────
// #region Xray Server & Telegram Web Proxy
// ─────────────────────────────────────────────

export interface XrayClient {
	id: string;
	remark: string;
	enabled: boolean;
	created_at: string;
}

export interface XrayConfig {
	enabled: boolean;
	listen_port: number;
	dispatcher_port: number;
	public_domain: string;
	public_port: number;
	path: string;
	mode: string;
	uplink_method: string;
	xmux_max_connections: number;
	outbound_socks_port: number;
	clients: XrayClient[];
}

export interface XrayShareLinks {
	vless_url: string;
	happ_json: string;
	singbox_json: string;
	mihomo_yaml: string;
	remark: string;
	uuid: string;
}

export interface TgWebProxyConfig {
	schema_version?: number;
	enabled: boolean;
	listen_port: number;
	admin_port: number;
	public_hostname: string;
	direct_host?: string;
	direct_port?: number;
	secret: string;
	legacy_secret?: string;
	legacy_expires_at?: string;
	backend: string;
	carrier_mode: string;
	upstream_device?: string;
	tls_domain?: string;
}

export interface TgWebProxyStatus {
	installed: boolean;
	installed_telemt?: boolean;
	installed_tproxy?: boolean;
	running: boolean;
	pid: number;
	port: number;
	direct_online?: boolean;
	raw_online?: boolean;
	webproxy_online?: boolean;
	backend_online: boolean;
	backend_addr: string;
	upstream_status?: 'ok' | 'degraded' | 'interface_down';
	carrier_mode: string;
	public_host: string;
	direct_host?: string;
	direct_port?: number;
	tls_domain?: string;
	secret_masked?: string;
	legacy_secret_masked?: string;
	legacy_active?: boolean;
	legacy_expires_at?: string;
	legacy_expired_pending_reconcile?: boolean;
	bridge_url: string;
	tg_url: string;
	tme_url: string;
	mtproxy_secret?: string;
	mtproxy_url?: string;
	mtproxy_tme_url?: string;
}

export interface TgWebProxyRevealData {
	secret: string;
	legacy_secret?: string;
	direct_host: string;
	direct_port: number;
	tls_domain: string;
	tg_url: string;
	tme_url: string;
	bridge_url: string;
	mtproxy_secret: string;
	mtproxy_url: string;
	mtproxy_tme_url: string;
}

// #endregion
