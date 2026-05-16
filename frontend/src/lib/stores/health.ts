import { writable, type Readable } from 'svelte/store';

export type HealthState = {
	online: boolean;
	lastCheckAt: number;
	consecutiveFailures: number;
};

export interface HealthMonitor extends Readable<HealthState> {
	start: () => void;
	stop: () => void;
}

const POLL_INTERVAL_MS = 5_000;
const OFFLINE_THRESHOLD = 2; // consecutive failures before flipping online=false

function createHealthMonitor(): HealthMonitor {
	const state = writable<HealthState>({
		online: true,
		lastCheckAt: 0,
		consecutiveFailures: 0,
	});

	let timer: ReturnType<typeof setInterval> | null = null;
	let running = false;
	let visibilityHandler: (() => void) | null = null;

	async function tick() {
		try {
			const res = await fetch('/api/health', { method: 'GET' });
			if (!res.ok) throw new Error(`health ${res.status}`);
			state.set({ online: true, lastCheckAt: Date.now(), consecutiveFailures: 0 });
		} catch {
			state.update(s => {
				const fails = s.consecutiveFailures + 1;
				return { ...s, lastCheckAt: Date.now(), consecutiveFailures: fails, online: fails < OFFLINE_THRESHOLD };
			});
		}
	}

	function startTimer() {
		if (timer !== null) return;
		timer = setInterval(tick, POLL_INTERVAL_MS);
	}

	function stopTimer() {
		if (timer !== null) {
			clearInterval(timer);
			timer = null;
		}
	}

	return {
		subscribe: state.subscribe,
		start() {
			if (running) return;
			running = true;
			void tick();
			startTimer();
			if (typeof document !== 'undefined' && visibilityHandler === null) {
				visibilityHandler = () => {
					if (document.visibilityState === 'hidden') {
						stopTimer();
					} else {
						void tick();
						startTimer();
					}
				};
				document.addEventListener('visibilitychange', visibilityHandler);
			}
		},
		stop() {
			running = false;
			stopTimer();
			if (typeof document !== 'undefined' && visibilityHandler !== null) {
				document.removeEventListener('visibilitychange', visibilityHandler);
				visibilityHandler = null;
			}
		},
	};
}

export const healthMonitor = createHealthMonitor();
