export type WizardKind = 'tgwebproxy' | 'xray';

export type CheckStatus = 'ok' | 'warning' | 'blocked';

export interface PreflightCheck {
	id: string;
	title: string;
	status: CheckStatus;
	message: string;
	details?: string;
	remediation?: string;
}

export interface PreflightResponse {
	can_proceed: boolean;
	has_warnings: boolean;
	checks: PreflightCheck[];
	fingerprint: string;
}

export interface CDNProfile {
	id: string;
	name: string;
	description: string;
	capabilities: string[];
	instructions: string;
	recommended: boolean;
}

export interface EgressOption {
	id: string;
	name: string;
	kind: 'direct' | 'interface' | 'socks' | 'router_outbound';
	owner: string;
	available: boolean;
	degraded_msg?: string;
	supports_tcp: boolean;
	supports_udp: boolean;
	generation: number;
}

export interface CapabilitiesResponse {
	kind: WizardKind;
	scenarios?: string[];
	modes?: string[];
	profiles: CDNProfile[];
	egress_options: EgressOption[];
	configured: boolean;
	running: boolean;
	recovery_required: boolean;
}

export interface PlanItem {
	action: string;
	target: string;
	description: string;
	old_value?: string;
	new_value?: string;
}

export interface ChangePlan {
	summary: string;
	items: PlanItem[];
	restart_required: boolean;
	state_fingerprint: string;
}

export interface WizardPlanRequest {
	kind: WizardKind;
	scenario?: string;
	device_type?: string;
	mode?: string;
	public_domain?: string;
	public_port?: number;
	path?: string;
	listen_port?: number;
	direct_host?: string;
	direct_port?: number;
	tls_domain?: string;
	upstream_device?: string;
	client_remark?: string;
	cdn_profile_id?: string;
}

export interface ServerPlanRecord {
	plan_id: string;
	kind: WizardKind;
	session_id?: string;
	request: WizardPlanRequest;
	plan: ChangePlan;
	state_fingerprint: string;
	expires_at: string;
	used: boolean;
	created_at?: string;
}

export type JobPhase =
	| 'pending'
	| 'preparing'
	| 'applying'
	| 'verifying'
	| 'committing'
	| 'succeeded'
	| 'cancelling'
	| 'rolling_back'
	| 'cancelled'
	| 'failed'
	| 'recovery_required';

export interface JobStatusResponse {
	id: string;
	kind: WizardKind;
	phase: JobPhase;
	progress: number;
	current_step: string;
	error?: string;
	error_code?: string;
	result_available: boolean;
	created_at: string;
	updated_at: string;
}

export interface RevealCredentials {
	kind: WizardKind;
	vless_url?: string;
	happ_json?: string;
	singbox_json?: string;
	mihomo_yaml?: string;
	uuid?: string;
	remark?: string;
	direct_link?: string;
	web_link?: string;
	tg_secret?: string;
	direct_host?: string;
	direct_port?: number;
	public_host?: string;
}
