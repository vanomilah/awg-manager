export interface VKCallsGenerateRequest {
	token?: string;
	groupId?: number;
	count?: number;
	saveToken?: boolean;
}

export interface VKCallsGenerateResponse {
	success: boolean;
	links: string[];
	hashes: string[];
	callIds?: string[];
	error?: string;
}

export interface VKCallsCheckItem {
	link: string;
	hash: string;
	alive: boolean;
	error?: string;
}

export interface VKCallsCheckResponse {
	results: VKCallsCheckItem[];
}

export interface VKCallsConfigResponse {
	hasToken: boolean;
	maskedToken?: string;
	groupId?: number;
}
