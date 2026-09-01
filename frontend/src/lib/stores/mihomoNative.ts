import { api } from '$lib/api/client';
import type {
	MihomoNativeProxy,
	MihomoNativeSubscription,
	MihomoRuntimeProvider,
	MihomoRuntimeProxy,
} from '$lib/types';
import { createPollingStore } from './polling';

export interface MihomoNativeSnapshot {
	proxies: MihomoNativeProxy[];
	subscriptions: MihomoNativeSubscription[];
	runtimeProxies: Record<string, MihomoRuntimeProxy>;
	runtimeProviders: Record<string, MihomoRuntimeProvider>;
}

async function fetchSnapshot(): Promise<MihomoNativeSnapshot> {
	const [proxies, subscriptions, runtime, providers] = await Promise.all([
		api.mihomoNativeProxies(),
		api.mihomoNativeSubscriptions(),
		api.mihomoRuntimeProxies().catch(() => ({ proxies: {} })),
		api.mihomoRuntimeProviders().catch(() => ({ providers: {} })),
	]);
	return {
		proxies,
		subscriptions,
		runtimeProxies: runtime.proxies ?? {},
		runtimeProviders: providers.providers ?? {},
	};
}

export const mihomoNativeResources = createPollingStore<MihomoNativeSnapshot>(fetchSnapshot, {
	staleTime: 5_000,
	pollInterval: 10_000,
});
