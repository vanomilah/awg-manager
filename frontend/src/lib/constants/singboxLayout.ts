/** Sing-box surfaces: main tunnels tab, subscriptions tab, subscription detail members. */
export type SingboxLayoutMode = 'grid' | 'list';

export const SINGBOX_LAYOUT_STORAGE_KEY = 'singbox_layout_mode';

export function parseSingboxLayoutMode(value: string | null): SingboxLayoutMode | null {
	if (value === 'grid' || value === 'list') return value;
	return null;
}
