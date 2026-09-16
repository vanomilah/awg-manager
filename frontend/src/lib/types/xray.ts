import type { XrayClient } from './servers';

export interface XrayStatus {
	installed: boolean;
	running: boolean;
	pid?: number;
	port?: number;
	version?: string;
	domain?: string;
	path?: string;
	uuid?: string;
	uplinkHTTPMethod?: string;
	cdnHost?: string;
	serverIP?: string;
	source?: 'opkg' | 'managed' | 'external';
	canUninstall?: boolean;
	blockers?: string[];
	error?: string;
	clients_count?: number;
	clients?: XrayClient[];
	recovery_required?: boolean;
	recovery_reason?: string;
	recovery_fingerprint?: string;
	migration_status?: 'migrated' | 'deferred' | 'conflict';
	active_generation?: 'new' | 'legacy';
}

export interface XrayMigrationStatus {
	discovery?: {
		found: boolean;
		topology?: 'A' | 'B' | 'C';
		listen_address?: string;
		listen_port?: number;
		public_domain?: string;
		path?: string;
		clients?: XrayClient[];
		conflict_reason?: string;
	};
	decision?: {
		active_generation: 'new' | 'legacy';
		migration_status: 'migrated' | 'deferred' | 'conflict';
		decided_at?: string;
		generation_counter?: number;
		reason?: string;
	};
}

export interface XrayConfigRequest {
	cdnHost: string;
	uplinkHTTPMethod: string;
	port: number;
}

export type XrayTransactionState =
	| 'idle'
	| 'prepared'
	| 'tested'
	| 'backup_ready'
	| 'active_replaced'
	| 'restarting'
	| 'verifying'
	| 'committed'
	| 'rolling_back'
	| 'rolled_back'
	| 'recovery_required'
	| 'aborted';

export interface XrayProfileMetadata {
	id: string;
	name: string;
	description?: string;
	created_at: string;
	updated_at: string;
	active: boolean;
	version: number;
}

export interface XrayProfileSummary {
	id: string;
	name: string;
	active: boolean;
	updated_at: string;
}

export interface XrayInbound {
	id?: string;
	tag: string;
	protocol: string;
	listen?: string;
	port?: number;
	security?: string;
	settings?: Record<string, unknown>;
	stream_settings?: Record<string, unknown>;
	sniffing?: Record<string, unknown>;
	clients?: unknown[];
}

export interface XrayOutbound {
	id?: string;
	tag: string;
	protocol: string;
	method?: string;
	server?: string;
	port?: number;
	uuid?: string;
	password?: string;
	security?: string;
	network?: string;
	settings?: Record<string, unknown>;
	stream_settings?: Record<string, unknown>;
}

export interface XrayBalancer {
	tag: string;
	selector: string[];
	strategy?: string;
}

export interface XrayManagedConfig {
	schema_version: number;
	log?: Record<string, unknown>;
	dns?: Record<string, unknown>;
	routing?: Record<string, unknown>;
	inbounds?: XrayInbound[];
	outbounds?: XrayOutbound[];
	balancers?: XrayBalancer[];
}

export interface XrayGenerationRecord {
	id: string;
	created_at: string;
	metadata: XrayProfileMetadata;
	managed_hash?: string;
	raw_hash?: string;
}

export interface XrayProfile {
	metadata: XrayProfileMetadata;
	active_generation?: string;
	generations?: Record<string, XrayGenerationRecord>;
	config?: XrayManagedConfig;
	raw_config?: Record<string, unknown>;
}

export interface XrayProfileDetail {
	profile: XrayProfile;
	validation_errors?: string[];
}

export interface XrayDiffPreview {
	has_changes: boolean;
	diff_summary: string;
	managed_diff: string;
	raw_diff: string;
	structural_changes: string[];
}

export interface XrayApplyResponse {
	profile_id: string;
	generation_id: string;
	state: XrayTransactionState;
	message?: string;
}

export type XrayProfileRole = 'client' | 'server' | 'mixed' | 'unspecified';

export interface XrayProfileSummaryDTO {
	id: string;
	name: string;
	role: XrayProfileRole;
	enabled: boolean;
	schema_version: number;
	created_at: string;
	generation_id: string;
}

export interface XrayProfileDetailDTO {
	id: string;
	name: string;
	role: XrayProfileRole;
	enabled: boolean;
	schema_version: number;
	created_at: string;
	generation_id: string;
	config: XrayManagedConfig;
	raw_overlay?: string;
}

export interface XrayDiffItem {
	action: 'add' | 'remove' | 'modify';
	category: string;
	target: string;
	description: string;
}

export interface XrayConfigDiff {
	summary: string;
	items: XrayDiffItem[];
	restart_required: boolean;
}

export interface XrayCapabilities {
	version?: string;
	raw_version?: string;
	semantic_version?: string;
	supports_xhttp: boolean;
	supports_reality: boolean;
	supports_shadowsocks_2022?: boolean;
	supports_mux_cool?: boolean;
	protocols: string[];
	security?: string[];
	transports?: string[];
}

