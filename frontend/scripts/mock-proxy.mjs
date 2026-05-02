// Stateful mock proxy: sits between Vite and Prism.
// - Holds usageLevel in memory; persists across GET/POST.
// - Forwards all other requests transparently.
// - Optional: simulate /singbox/install failure via env MOCK_SINGBOX_INSTALL_FAIL=1
//   or runtime POST /__mock/singbox-install-fail body {"enabled": true|false}.
// - Streams /events normally (Prism handles SSE shape).
// - Injects 8 fake singbox log entries into GET /logs (covers all 6 subgroups
//   and 4 levels). Honors group/subgroup/level filter query params.
// - Sing-box composite proxies (Feature 1): stateful stubs for
//   /singbox/router/proxies/{list,select,test} so the redesigned routing UI
//   can be smoke-tested without a real router. Selections persist in-memory.
// Default upstream: http://127.0.0.1:8080 (Prism). Listen: 8081.

import http from 'node:http';

const UPSTREAM = process.env.UPSTREAM ?? 'http://127.0.0.1:8080';
const PORT = Number(process.env.PORT ?? 8081);
const VALID = new Set(['basic', 'advanced', 'expert']);

// In-memory state. Default 'expert' so all advanced surfaces (singbox
// router, rule sets, device proxy, etc.) are visible by default — the
// realistic case for development against the redesigned routing page.
let usageLevel = 'expert';
let singboxInstallShouldFail = process.env.MOCK_SINGBOX_INSTALL_FAIL === '1';
const FAKE_INSTALL_STDERR = `Collected errors:
 * verify_pkg_installable: Only have 12 KB available on filesystem /opt, pkg sing-box needs 18432
 * opkg_install_cmd: Cannot install package sing-box.
opkg_install_cmd: failed.
exit code 255`;

async function fetchJSON(path, init) {
	const r = await fetch(`${UPSTREAM}${path}`, init);
	const text = await r.text();
	try {
		return { status: r.status, body: JSON.parse(text) };
	} catch {
		return { status: r.status, body: text };
	}
}

function send(res, status, body, contentType = 'application/json') {
	res.writeHead(status, { 'Content-Type': contentType });
	res.end(typeof body === 'string' ? body : JSON.stringify(body));
}

const FAKE_SINGBOX_LOGS = [
	{ group: 'singbox', subgroup: 'process',  action: 'stdout', level: 'info',  target: '', message: 'sing-box version 1.9.3 starting' },
	{ group: 'singbox', subgroup: 'process',  action: 'stderr', level: 'error', target: '', message: 'FATAL: failed to bind tproxy: address already in use' },
	{ group: 'singbox', subgroup: 'process',  action: 'stderr', level: 'warn',  target: '', message: 'WARN: deprecated config field "auto_detect_interface"' },
	{ group: 'singbox', subgroup: 'runtime',  action: 'clash',  level: 'info',  target: '', message: '[Connection] tcp 192.168.1.50:54321 -> example.com:443' },
	{ group: 'singbox', subgroup: 'inbound',  action: 'tproxy', level: 'info',  target: '', message: '[TPROXY] mark=0x1 fwmark applied to flow' },
	{ group: 'singbox', subgroup: 'outbound', action: 'route',  level: 'info',  target: '', message: '[Outbound] selected: vless-server-1' },
	{ group: 'singbox', subgroup: 'dns',      action: 'lookup', level: 'debug', target: '', message: '[DNS] resolve example.com via 1.1.1.1' },
	{ group: 'singbox', subgroup: 'router',   action: 'match',  level: 'full',  target: '', message: '[Router] match rule "geo:RU" -> outbound: direct' },
];

function buildFakeSingboxEntries() {
	const nowMs = Date.now();
	return FAKE_SINGBOX_LOGS.map((e, i) => ({
		...e,
		// Backend serializes time.Time as RFC3339; match that so the frontend
		// formatTime helper renders correctly. Stagger by 1s per entry.
		timestamp: new Date(nowMs - (FAKE_SINGBOX_LOGS.length - i) * 1000).toISOString(),
	}));
}

function applyFilters(entries, qs) {
	let out = entries;
	const sub = qs.get('subgroup');
	if (sub) out = out.filter((e) => e.subgroup === sub);
	const lvl = qs.get('level');
	if (lvl) {
		const levelOrder = ['error', 'warn', 'info', 'full', 'debug'];
		const idx = levelOrder.indexOf(lvl);
		if (idx >= 0) {
			const allowed = new Set(levelOrder.slice(0, idx + 1));
			// ERROR and WARN always visible regardless of configured level.
			allowed.add('error');
			allowed.add('warn');
			out = out.filter((e) => allowed.has(e.level));
		}
	}
	return out;
}

// ── Sing-box composite proxies (Feature 1) ─────────────────────
// Stateful across calls: selecting a member persists in this map
// so the UI's optimistic update is reflected on the next /list poll.
const mockProxies = {
	'veesp-fast': {
		type: 'selector',
		now: 'vless-1',
		all: ['vless-1', 'vless-2', 'vless-3'],
	},
	'auto': {
		type: 'urltest',
		now: 'vless-2',
		all: ['vless-1', 'vless-2', 'vless-3', 'vless-4'],
	},
};
const mockProxyDelays = {
	'vless-1': 45,
	'vless-2': 78,
	'vless-3': 180,
	'vless-4': 320,
};
function randomizeDelays() {
	for (const k of Object.keys(mockProxyDelays)) {
		const base = mockProxyDelays[k];
		if (Math.random() < 0.05) {
			mockProxyDelays[k] = 0; // 5% timeout
		} else {
			mockProxyDelays[k] = Math.max(10, base + Math.round((Math.random() - 0.5) * 40));
		}
	}
}

const server = http.createServer((req, res) => {
	const url = new URL(req.url, `http://${req.headers.host}`);
	const path = url.pathname;

	if (req.method === 'GET' && path === '/settings/get') {
		fetchJSON('/settings/get').then(({ status, body }) => {
			if (body && typeof body === 'object' && body.data) {
				body.data.usageLevel = usageLevel;
			}
			send(res, status, body);
		});
		return;
	}

	if (req.method === 'POST' && path === '/settings/update') {
		let raw = '';
		req.on('data', (c) => (raw += c));
		req.on('end', async () => {
			try {
				const payload = JSON.parse(raw);
				if (typeof payload.usageLevel === 'string') {
					if (!VALID.has(payload.usageLevel)) {
						send(res, 400, {
							success: false,
							error: 'invalid usageLevel',
							code: 'INVALID_USAGE_LEVEL',
						});
						return;
					}
					usageLevel = payload.usageLevel;
				}
				const { status, body } = await fetchJSON('/settings/get');
				if (body && typeof body === 'object' && body.data) {
					body.data.usageLevel = usageLevel;
				}
				send(res, status, body);
				console.log(`[mock-proxy] usageLevel → ${usageLevel}`);
			} catch (e) {
				send(res, 500, { success: false, error: String(e) });
			}
		});
		return;
	}

	if (req.method === 'GET' && path === '/logs') {
		const group = url.searchParams.get('group');
		if (group === 'singbox') {
			// Pure singbox view — bypass Prism entirely.
			const fake = applyFilters(buildFakeSingboxEntries(), url.searchParams);
			send(res, 200, { data: { enabled: true, logs: fake, total: fake.length }, success: true });
			return;
		}
		// Mixed view — pass through to Prism, then merge in singbox entries
		// so the singbox chip lights up with content even from the all-groups view.
		fetchJSON(req.url).then(({ status, body }) => {
			if (body && typeof body === 'object' && body.data && Array.isArray(body.data.logs)) {
				const fake = applyFilters(buildFakeSingboxEntries(), url.searchParams);
				body.data.logs = body.data.logs.concat(fake);
				body.data.total = (body.data.total ?? body.data.logs.length);
			}
			send(res, status, body);
		});
		return;
	}

	if (req.method === 'GET' && path === '/singbox/router/proxies/list') {
		randomizeDelays();
		const groups = Object.entries(mockProxies).map(([tag, g]) => ({
			tag,
			type: g.type,
			now: g.now,
			all: g.all.map((memberTag) => ({
				tag: memberTag,
				type: 'vless',
				lastDelay: mockProxyDelays[memberTag] ?? 0,
			})),
		}));
		send(res, 200, { success: true, data: { groups } });
		return;
	}

	if (req.method === 'POST' && path === '/singbox/router/proxies/select') {
		let raw = '';
		req.on('data', (c) => (raw += c));
		req.on('end', () => {
			try {
				const payload = JSON.parse(raw || '{}');
				const group = typeof payload.group === 'string' ? payload.group : '';
				const member = typeof payload.member === 'string' ? payload.member : '';
				const g = mockProxies[group];
				if (!g) {
					send(res, 404, {
						success: false,
						error: { code: 'PROXY_GROUP_NOT_FOUND', message: `group ${group} not found` },
					});
					return;
				}
				if (g.type !== 'selector') {
					send(res, 400, {
						success: false,
						error: { code: 'PROXY_GROUP_NOT_SELECTABLE', message: `group ${group} is ${g.type}, not selector` },
					});
					return;
				}
				if (!g.all.includes(member)) {
					send(res, 400, {
						success: false,
						error: { code: 'PROXY_MEMBER_NOT_FOUND', message: `member ${member} not in group ${group}` },
					});
					return;
				}
				g.now = member;
				send(res, 200, { success: true, data: {} });
				console.log(`[mock-proxy] proxies.select ${group} → ${member}`);
			} catch (e) {
				send(res, 400, {
					success: false,
					error: { code: 'INVALID_REQUEST', message: String(e) },
				});
			}
		});
		return;
	}

	if (req.method === 'POST' && path === '/singbox/router/proxies/test') {
		let raw = '';
		req.on('data', (c) => (raw += c));
		req.on('end', () => {
			try {
				const payload = JSON.parse(raw || '{}');
				const group = typeof payload.group === 'string' ? payload.group : '';
				const g = mockProxies[group];
				if (!g) {
					send(res, 404, {
						success: false,
						error: { code: 'PROXY_GROUP_NOT_FOUND', message: `group ${group} not found` },
					});
					return;
				}
				randomizeDelays();
				const delays = {};
				for (const memberTag of g.all) {
					delays[memberTag] = mockProxyDelays[memberTag] ?? 0;
				}
				send(res, 200, { success: true, data: { delays } });
				console.log(`[mock-proxy] proxies.test ${group} → ${JSON.stringify(delays)}`);
			} catch (e) {
				send(res, 400, {
					success: false,
					error: { code: 'INVALID_REQUEST', message: String(e) },
				});
			}
		});
		return;
	}

	if (req.method === 'GET' && path === '/singbox/config-preview') {
		const merged = {
			log: { level: 'trace', timestamp: true },
			dns: {
				servers: [
					{ tag: 'cf', address: '1.1.1.1', detour: 'direct' },
					{ tag: 'local', address: '192.168.1.1', detour: 'direct' },
				],
				rules: [{ outbound: 'any', server: 'local' }],
				final: 'cf',
			},
			inbounds: [
				{ tag: 'tproxy-in', type: 'tproxy', listen: '::', listen_port: 51272, sniff: true },
			],
			outbounds: [
				{ tag: 'direct', type: 'direct' },
				{ tag: 'block', type: 'block' },
				{ tag: 'awg-tunnel-1', type: 'wireguard', server: '203.0.113.7', server_port: 51820 },
			],
			route: {
				rules: [
					{ action: 'sniff' },
					{ protocol: 'dns', action: 'hijack-dns' },
					{ rule_set: ['geoip-ru'], outbound: 'direct' },
					{ rule_set: ['geosite-tracker'], outbound: 'block' },
				],
				rule_set: [
					{ tag: 'geoip-ru', type: 'remote', format: 'binary', url: 'https://example/ru.srs' },
					{ tag: 'geosite-tracker', type: 'remote', format: 'binary', url: 'https://example/tracker.srs' },
				],
				final: 'awg-tunnel-1',
				auto_detect_interface: true,
			},
			experimental: {
				clash_api: { external_controller: '127.0.0.1:9090', external_ui: 'ui' },
				cache_file: { enabled: true, path: '/opt/etc/sing-box/cache.db' },
			},
		};
		send(res, 200, {
			success: true,
			data: { json: JSON.stringify(merged, null, 2) },
		});
		return;
	}

	if (req.method === 'POST' && path === '/singbox/install') {
		if (singboxInstallShouldFail) {
			send(res, 500, {
				success: false,
				error: FAKE_INSTALL_STDERR,
				code: 'SINGBOX_INSTALL_ERROR',
			});
			console.log('[mock-proxy] simulated /singbox/install failure');
			return;
		}
		// Falls through to the generic pass-through below.
	}

	// When the install-fail flag is on, also report sing-box as not-installed
	// so the Settings UI shows the "Установить" button (and clicking it hits
	// the /singbox/install override above with a 500 + fake stderr).
	if (req.method === 'GET' && path === '/singbox/status' && singboxInstallShouldFail) {
		fetchJSON(req.url).then(({ status, body }) => {
			if (body && typeof body === 'object' && body.data) {
				body.data.installed = false;
				body.data.running = false;
				body.data.pid = 0;
			}
			send(res, status, body);
		});
		return;
	}

	if (req.method === 'POST' && path === '/__mock/singbox-install-fail') {
		let raw = '';
		req.on('data', (c) => (raw += c));
		req.on('end', () => {
			try {
				const body = JSON.parse(raw);
				singboxInstallShouldFail = !!body.enabled;
				send(res, 200, { ok: true, singboxInstallShouldFail });
				console.log(`[mock-proxy] singboxInstallShouldFail → ${singboxInstallShouldFail}`);
			} catch (e) {
				send(res, 400, { error: String(e) });
			}
		});
		return;
	}

	// Pass-through for everything else (including /events SSE).
	const upstream = new URL(UPSTREAM);
	const proxyReq = http.request(
		{
			hostname: upstream.hostname,
			port: upstream.port,
			path: req.url,
			method: req.method,
			headers: { ...req.headers, host: upstream.host },
		},
		(proxyRes) => {
			res.writeHead(proxyRes.statusCode ?? 502, proxyRes.headers);
			proxyRes.pipe(res);
		},
	);
	proxyReq.on('error', (e) => {
		if (!res.headersSent) {
			send(res, 502, { error: String(e) });
		} else {
			res.end();
		}
	});
	req.pipe(proxyReq);
});

server.listen(PORT, '127.0.0.1', () => {
	console.log(`mock-proxy on http://127.0.0.1:${PORT} → ${UPSTREAM} (usageLevel=${usageLevel})`);
});
