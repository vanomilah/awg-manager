import { api } from './client';
import type {
	XrayStatus,
	XrayProfileSummaryDTO,
	XrayProfileDetailDTO,
	XrayManagedConfig,
	XrayConfigDiff,
	XrayCapabilities,
	XrayProfileRole
} from '$lib/types/xray';

export async function xrayGetStatus(): Promise<XrayStatus> {
	return api.xrayStatus();
}

export async function xrayInstall(): Promise<XrayStatus> {
	return api.xrayInstall();
}

export async function xrayUninstall(): Promise<void> {
	return api.xrayUninstall();
}

export async function xrayListProfiles(): Promise<XrayProfileSummaryDTO[]> {
	return api.xrayListProfiles();
}

export async function xrayCreateProfile(req: {
	id?: string;
	name: string;
	role?: XrayProfileRole;
	enabled?: boolean;
	config: XrayManagedConfig;
	raw_overlay?: string;
}): Promise<XrayProfileDetailDTO> {
	return api.xrayCreateProfile(req);
}

export async function xrayGetProfile(id: string): Promise<XrayProfileDetailDTO> {
	return api.xrayGetProfile(id);
}

export async function xrayUpdateProfile(
	id: string,
	req: {
		name: string;
		role?: XrayProfileRole;
		enabled: boolean;
		config: XrayManagedConfig;
		raw_overlay?: string;
	}
): Promise<XrayProfileDetailDTO> {
	return api.xrayUpdateProfile(id, req);
}

export async function xrayDeleteProfile(id: string): Promise<void> {
	return api.xrayDeleteProfile(id);
}

export async function xrayImportProfile(req: {
	name?: string;
	role?: XrayProfileRole;
	content: string;
}): Promise<XrayProfileDetailDTO> {
	return api.xrayImportProfile(req);
}

export async function xrayExportRedactedProfile(id: string): Promise<Record<string, unknown>> {
	return api.xrayExportRedactedProfile(id);
}

/**
 * Mandatory Security Constraint 2:
 * Export private configuration strictly via POST with no-store headers.
 * Secrets never leak into GET URLs, browser history, or server access logs.
 */
export async function xrayExportPrivateProfile(id: string): Promise<Record<string, unknown>> {
	return api.xrayExportPrivateProfile(id);
}

export async function xrayPreviewProfileDiff(id: string): Promise<XrayConfigDiff> {
	return api.xrayPreviewProfileDiff(id);
}

export async function xrayApplyProfile(
	id: string
): Promise<{ success: boolean; profile_id: string; generation_id: string }> {
	return api.xrayApplyProfile(id);
}

export async function xrayListGenerations(id: string): Promise<any[]> {
	return api.xrayListGenerations(id);
}

export async function xrayRollbackProfile(
	id: string,
	generationId: string
): Promise<{ success: boolean; generation_id: string }> {
	return api.xrayRollbackProfile(id, generationId);
}

export async function xrayGetCapabilities(): Promise<XrayCapabilities> {
	return api.xrayGetCapabilities();
}

export async function xrayResolveRecovery(strategy = 'clear'): Promise<{ success: boolean }> {
	return api.xrayResolveRecovery(strategy);
}


