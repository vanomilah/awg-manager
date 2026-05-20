export interface InlineRuleParseResult {
	rules: Record<string, unknown>[];
	warnings: string[];
	errors: string[];
}

export interface InlineLossyAnalysis {
	lossy: boolean;
	issues: string[];
}

function asStringArray(v: unknown): string[] {
	if (!Array.isArray(v)) return [];
	return v.filter((x): x is string => typeof x === 'string');
}

function asNumberArray(v: unknown): number[] {
	if (!Array.isArray(v)) return [];
	return v.filter((x): x is number => typeof x === 'number');
}

export function stringifyInlineRuleList(rules: Record<string, unknown>[] | undefined): string {
	if (!Array.isArray(rules) || rules.length === 0) return '';
	const lines: string[] = [];

	for (const r of rules) {
		const domains = asStringArray(r['domain']);
		for (const d of domains) lines.push(`domain:${d}`);

		const suffixes = asStringArray(r['domain_suffix']);
		for (const s of suffixes) lines.push(`domain_suffix:${s}`);

		const keywords = asStringArray(r['domain_keyword']);
		for (const k of keywords) lines.push(`keyword:${k}`);

		const regexes = asStringArray(r['domain_regex']);
		for (const rx of regexes) lines.push(`regex:${rx}`);

		const ipCidrs = asStringArray(r['ip_cidr']);
		for (const ip of ipCidrs) {
			lines.push(ip.includes('/') ? `cidr:${ip}` : `ip:${ip}`);
		}

		const srcIpCidrs = asStringArray(r['source_ip_cidr']);
		for (const ip of srcIpCidrs) lines.push(`src_ip:${ip}`);

		const ports = asNumberArray(r['port']);
		if (ports.length > 0) lines.push(`port:${ports.join(',')}`);

		const processNames = asStringArray(r['process_name']);
		for (const p of processNames) lines.push(`process:${p}`);

		const processPaths = asStringArray(r['process_path']);
		for (const p of processPaths) lines.push(`process_path:${p}`);

		const packageNames = asStringArray(r['package_name']);
		for (const p of packageNames) lines.push(`package:${p}`);

		const networks = asStringArray(r['network']);
		for (const n of networks) lines.push(`network:${n}`);
	}

	return lines.join('\n');
}

const SUPPORTED_INLINE_KEYS = new Set([
	'domain',
	'domain_suffix',
	'domain_keyword',
	'domain_regex',
	'ip_cidr',
	'source_ip_cidr',
	'port',
	'process_name',
	'process_path',
	'package_name',
	'network',
]);

const KEY_BUCKET: Record<string, string> = {
	domain: 'domain',
	domain_suffix: 'domain',
	domain_keyword: 'domain',
	domain_regex: 'domain',
	ip_cidr: 'ip_cidr',
	source_ip_cidr: 'source_ip_cidr',
	port: 'port',
	process_name: 'process_name',
	process_path: 'process_path',
	package_name: 'package_name',
	network: 'network',
};

export function analyzeInlineRuleListLossy(rules: Record<string, unknown>[] | undefined): InlineLossyAnalysis {
	if (!Array.isArray(rules) || rules.length === 0) return { lossy: false, issues: [] };

	const issues = new Set<string>();

	for (const rule of rules) {
		const supportedKeysInRule: string[] = [];

		for (const [key, value] of Object.entries(rule)) {
			if (!SUPPORTED_INLINE_KEYS.has(key)) {
				issues.add(`unsupported key: ${key}`);
				continue;
			}
			supportedKeysInRule.push(key);

			if (key === 'port') {
				if (!Array.isArray(value) || value.some((v) => typeof v !== 'number')) {
					issues.add(`invalid type for key: ${key}`);
				}
				continue;
			}

			if (!Array.isArray(value) || value.some((v) => typeof v !== 'string')) {
				issues.add(`invalid type for key: ${key}`);
			}
		}

		const buckets = new Set<string>();
		for (const key of supportedKeysInRule) {
			const bucket = KEY_BUCKET[key];
			if (bucket) buckets.add(bucket);
		}
		if (buckets.size > 1) {
			issues.add(
				`mixed supported keys in one rule are not round-trip safe: ${supportedKeysInRule.join(', ')}`,
			);
		}

		// Current list parser expands `domain:` to domain + domain_suffix.
		// For safe round-trip each exact domain must already have matching
		// ".domain" suffix in the same rule object.
		if (Array.isArray(rule.domain) && rule.domain.length > 0) {
			const suffixes = new Set(
				Array.isArray(rule.domain_suffix)
					? rule.domain_suffix.filter((v): v is string => typeof v === 'string')
					: [],
			);
			for (const d of rule.domain) {
				if (typeof d !== 'string') continue;
				if (!suffixes.has(`.${d}`)) {
					issues.add(`domain exact-only rule may widen in list mode: ${d}`);
				}
			}
		}
	}

	return { lossy: issues.size > 0, issues: [...issues] };
}

type LogEntry = { line: number; msg: string };

function mkLogger(lineNum: number, w: LogEntry[], e: LogEntry[]) {
	return {
		warn(msg: string) { w.push({ line: lineNum, msg }); },
		err(msg: string) { e.push({ line: lineNum, msg }); },
	};
}

function real(val: string | undefined): boolean {
	return !!val && val !== '';
}

export function parseInlineRuleList(input: string): InlineRuleParseResult {
	const warnings: LogEntry[] = [];
	const errors: LogEntry[] = [];

	const domainGroup: Set<string> = new Set();
	const domainSuffixGroup: Set<string> = new Set();
	const domainKeywordGroup: Set<string> = new Set();
	const domainRegexGroup: Set<string> = new Set();
	const ipCidrGroup: Set<string> = new Set();
	const sourceIpCidrGroup: Set<string> = new Set();
	const portGroup: Set<number> = new Set();
	const processNameGroup: Set<string> = new Set();
	const processPathGroup: Set<string> = new Set();
	const packageNameGroup: Set<string> = new Set();
	const networkGroup: Set<string> = new Set();

	const lines = input.split(/\r?\n/);

	/** Strip trailing inline comments: space-semicolon or space-hash */
	function stripInline(s: string): string {
		return s
			.replace(/\s+;\s.*$/, '')
			.replace(/\s+#\s.*$/, '')
			.trim();
	}

	for (let i = 0; i < lines.length; i++) {
		let line = stripInline(lines[i]);
		const lineNum = i + 1;

		// skip empty and full-line comments
		if (line === '' || line.startsWith(';') || line.startsWith('#') || line.startsWith('//')) continue;

		const lg = mkLogger(lineNum, warnings, errors);

		// ── adblock exceptions ───────────────────────────────────
		if (line.startsWith('@@')) {
			lg.warn('исключения @@ пока не поддерживаются в inline rule set');
			continue;
		}

		// ── strip adblock prefix noise ───────────────────────────
		if (line.startsWith('||') && line.endsWith('^')) {
			line = line.slice(2, line.length - 1).trim();
		} else if (line.startsWith('||')) {
			line = line.slice(2).trim();
		} else if (line.endsWith('^')) {
			line = line.slice(0, line.length - 1).trim();
		}

		// ── parse key:value prefixes ─────────────────────────────
		const colonIdx = line.indexOf(':');
		if (colonIdx > 0) {
			const rawKey = line.slice(0, colonIdx);
			const val = line.slice(colonIdx + 1).trim();

			switch (rawKey.toLowerCase()) {
				case 'port':
					if (!real(val)) { lg.err('port требует значение после двоеточия'); continue; }
					let portAdded = false;
					for (const p of val.split(',').map((s) => s.trim()).filter(real)) {
						if (!/^\d+$/.test(p)) { lg.err(`Некорректный порт: ${p}`); continue; }
						const n = parseInt(p, 10);
						if (n < 1 || n > 65535) { lg.err(`Некорректный порт: ${p}`); continue; }
						portGroup.add(n);
						portAdded = true;
					}
					if (portAdded) { lg.warn('port создаёт отдельное правило и может матчить широкий трафик'); }
					continue;

				case 'port_range':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					lg.warn('port_range пока не поддерживается в smart list');
					continue;

				case 'process':
				case 'process_name':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					processNameGroup.add(val);
					lg.warn('process относится к локальным процессам роутера/хоста, а не к LAN-клиентам');
					continue;

				case 'process_path':
				case 'path':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					processPathGroup.add(val);
					lg.warn('process_path относится к локальным процессам роутера/хоста, а не к LAN-клиентам');
					continue;

				case 'package':
				case 'package_name':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					packageNameGroup.add(val);
					continue;

				case 'network':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					const network = val.toLowerCase();
					if (network !== 'tcp' && network !== 'udp') {
						lg.err(`Некорректный network: ${val} — допустимы только tcp/udp`);
						continue;
					}
					networkGroup.add(network);
					continue;

				case 'ip':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					for (const p of val.split(',').map((s) => s.trim()).filter(real)) {
						if (isValidIpCidr(p) || isValidSimpleIp(p) || isValidSimpleIpv6(p)) {
							ipCidrGroup.add(p);
						} else {
							lg.err(`Некорректный IP/CIDR для ip: ${p}`);
						}
					}
					continue;

				case 'cidr':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					for (const p of val.split(',').map((s) => s.trim()).filter(real)) {
						if (isValidIpCidr(p)) {
							ipCidrGroup.add(p);
						} else {
							lg.err(`Некорректный CIDR для cidr: ${p}`);
						}
					}
					continue;

				case 'src_ip':
				case 'source_ip':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					for (const p of val.split(',').map((s) => s.trim()).filter(real)) {
						if (isValidIpCidr(p) || isValidSimpleIp(p) || isValidSimpleIpv6(p)) {
							sourceIpCidrGroup.add(p);
						} else {
							lg.err(`Некорректный IP/CIDR для src_ip: ${p}`);
						}
					}
					continue;

				case 'domain_keyword':
				case 'keyword':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					domainKeywordGroup.add(val);
					continue;

				case 'domain_regex':
				case 'regex':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					try { new RegExp(val); } catch { lg.err(`Некорректный regex: ${val}`); continue; }
					domainRegexGroup.add(val);
					continue;

				case 'domain':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					if (isValidDomain(val)) {
						domainGroup.add(val);
						domainSuffixGroup.add('.' + val);
					} else {
						lg.err(`Некорректный домен для domain: ${val}`);
					}
					continue;

				case 'domain_suffix':
				case 'suffix':
					if (!real(val)) { lg.err(`${rawKey} требует значение после двоеточия`); continue; }
					const normalizedSuffix = val.replace(/^\*\./, '').replace(/^\./, '');
					if (isValidDomain(normalizedSuffix)) {
						domainSuffixGroup.add('.' + normalizedSuffix);
					} else {
						lg.err(`Некорректный домен для ${rawKey}: ${val}`);
					}
					continue;

				default:
					break;
			}
		}

		// ── domain / hostname / bare IP ──────────────────────────
		if (line === '') continue;

		let domain = line;

		// URL → hostname
		if (/^https?:\/\//i.test(domain)) {
			try { domain = new URL(domain).hostname; } catch (_) { /* keep as-is */ }
		}

		// ".example.com" → domain_suffix ".example.com"
		if (domain.startsWith('.')) {
			const suf = domain.slice(1);
			if (isValidDomain(suf)) {
				domainSuffixGroup.add('.' + suf);
			} else {
				lg.warn(`Невалидный домен (точка-префикс): ${domain}`);
			}
			continue;
		}

		// "*.example.com" → domain_suffix ".example.com"
		if (domain.startsWith('*.')) {
			const suf = domain.slice(2);
			if (isValidDomain(suf)) {
				domainSuffixGroup.add('.' + suf);
			} else {
				lg.warn(`Невалидный домен (wildcard): ${domain}`);
			}
			continue;
		}

		// CIDR notation "a.b.c.d/m"
		if (isValidIpCidr(domain)) {
			ipCidrGroup.add(domain);
			continue;
		}

		// bare IPv4
		if (isValidSimpleIp(domain)) {
			ipCidrGroup.add(domain);
			continue;
		}

		// bare IPv6 (no slash — single address)
		if (isValidSimpleIpv6(domain)) {
			ipCidrGroup.add(domain);
			continue;
		}

		// pure-IP strings (e.g. "999.999.999.999") that didn't match the canonical
		// isValidSimpleIp but match DOMAIN_RE by coincidence — drain here
		if (/^\d+\.\d+\.\d+\.\d+$/.test(domain)) {
			lg.warn(`Не распознано как правило: ${line}`);
			continue;
		}

		// plain valid domain
		if (isValidDomain(domain)) {
			domainGroup.add(domain);
			domainSuffixGroup.add('.' + domain);
			continue;
		}

		lg.warn(`Не распознано как правило: ${line}`);
	}

	// ── build rules array ────────────────────────────────────────
	const rules: Record<string, unknown>[] = [];

	if (domainGroup.size > 0 || domainSuffixGroup.size > 0 || domainKeywordGroup.size > 0 || domainRegexGroup.size > 0) {
		const g: Record<string, unknown> = {};
		if (domainKeywordGroup.size > 0) g['domain_keyword'] = [...domainKeywordGroup];
		if (domainSuffixGroup.size > 0) g['domain_suffix'] = [...domainSuffixGroup];
		if (domainGroup.size > 0) g['domain'] = [...domainGroup];
		if (domainRegexGroup.size > 0) g['domain_regex'] = [...domainRegexGroup];
		rules.push(g);
	}
	if (ipCidrGroup.size > 0) rules.push({ ip_cidr: [...ipCidrGroup] });
	if (sourceIpCidrGroup.size > 0) rules.push({ source_ip_cidr: [...sourceIpCidrGroup] });
	if (portGroup.size > 0) rules.push({ port: [...portGroup].sort((a, b) => a - b) });
	if (processNameGroup.size > 0) rules.push({ process_name: [...processNameGroup] });
	if (processPathGroup.size > 0) rules.push({ process_path: [...processPathGroup] });
	if (packageNameGroup.size > 0) rules.push({ package_name: [...packageNameGroup] });
	if (networkGroup.size > 0) rules.push({ network: [...networkGroup] });

	// ── format for return ───────────────────────────────────────
	const warnMsgs = warnings.map((w) => `Строка ${w.line}: ${w.msg}`);
	const errMsgs = errors.map((e) => `Строка ${e.line}: ${e.msg}`);

	if (rules.length === 0 && errMsgs.length === 0 && warnMsgs.length === 0) {
		errMsgs.push('Нет валидных строк для inline rule set');
	}

	return { rules, warnings: warnMsgs, errors: errMsgs };
}

// ─────────────────────────────────────────────────────────────
// Validation helpers
// ─────────────────────────────────────────────────────────────

const DOMAIN_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/i;

function isValidDomain(s: string): boolean {
	return DOMAIN_RE.test(s);
}

function isValidIpCidr(s: string): boolean {
	const parts = s.split('/');
	if (parts.length !== 2) return false;

	const [ip, mask] = parts;
	if (!ip || !mask) return false;
	if (!/^\d+$/.test(mask)) return false;

	const maskNum = Number(mask);

	if (isValidSimpleIp(ip)) {
		return maskNum >= 0 && maskNum <= 32;
	}

	if (isValidSimpleIpv6(ip)) {
		return maskNum >= 0 && maskNum <= 128;
	}

	return false;
}

function isValidSimpleIp(s: string): boolean {
	const parts = s.split('.');
	if (parts.length !== 4) return false;
	return parts.every((p) => /^\d{1,3}$/.test(p) && parseInt(p, 10) <= 255);
}

function isValidSimpleIpv6(addr: string): boolean {
	if (!addr.includes(':')) return false;
	// Allow hex chars, ':', '.' (IPv4-mapped suffix)
	if (/[^0-9a-f:.]/i.test(addr)) return false;
	const hextets = addr.split(':');
	if (hextets.length < 3 || hextets.length > 8) return false;
	return hextets.every((h) => h === '' || /^[0-9a-f]{0,4}$/i.test(h));
}
