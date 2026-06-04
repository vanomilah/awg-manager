#!/usr/bin/env node
/**
 * Merge short domain/CIDR lists from internal/presets/decompiled/*.json
 * into defaults.json DNS engines (cap: 500 domains per preset).
 *
 * Usage: node scripts/rulesets/apply-decompiled-dns.js [--dry-run]
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.join(__dirname, '../..');
const DECOMPILED = path.join(ROOT, 'internal/presets/decompiled');
const DEFAULTS_PATH = path.join(ROOT, 'internal/presets/defaults.json');
const MAX_DOMAINS = 500;
const DRY = process.argv.includes('--dry-run');

/** Presets that keep subscriptionUrl only (lists too long inline). */
const SKIP_IDS = new Set(['unavailable-in-russia', 'rkn', 'all-blocked', 'russian-services']);

/** Composite DNS from several decompiled service rule-sets. */
const COMPOSITE = {
	'category-games': {
		files: [
			'sagernet/geosite-steam.json',
			'sagernet/geosite-epicgames.json',
			'sagernet/geosite-playstation.json',
			'sagernet/geosite-xbox.json',
			'sagernet/geosite-blizzard.json',
			'sagernet/geosite-ea.json',
			'sagernet/geosite-oculus.json',
			'roblox.json',
			'nintendo.json',
		],
	},
	'category-media': {
		files: [
			'netflix.json',
			'sagernet/geosite-spotify.json',
			'sagernet/geosite-deezer.json',
			'sagernet/geosite-disney.json',
			'sagernet/geosite-hbo.json',
			'sagernet/geosite-hulu.json',
			'sagernet/geosite-primevideo.json',
			'sagernet/geosite-tidal.json',
			'sagernet/geosite-twitch.json',
			'sagernet/geosite-soundcloud.json',
			'sagernet/geosite-vimeo.json',
			'sagernet/geosite-bbc.json',
		],
	},
};

function extractFromRules(rules) {
	const domains = new Set();
	const subnets = new Set();
	for (const r of rules) {
		const add = (d) => {
			if (!d) return;
			let x = String(d).trim().toLowerCase();
			if (x.startsWith('.')) x = x.slice(1);
			if (x && !x.includes('*') && !x.startsWith('^')) domains.add(x);
		};
		const addList = (v) => {
			if (!v) return;
			if (Array.isArray(v)) v.forEach(add);
			else add(v);
		};
		addList(r.domain);
		addList(r.domain_suffix);
		const cidrs = r.ip_cidr;
		if (Array.isArray(cidrs)) cidrs.forEach((c) => subnets.add(c));
		else if (cidrs) subnets.add(cidrs);
	}
	return {
		domains: [...domains].sort(),
		subnets: [...subnets].sort(),
	};
}

function loadExtract(relPath) {
	const p = path.join(DECOMPILED, relPath);
	if (!fs.existsSync(p)) return null;
	const data = JSON.parse(fs.readFileSync(p, 'utf8'));
	return extractFromRules(data.rules || []);
}

function mergeExtracts(files) {
	const domains = new Set();
	const subnets = new Set();
	for (const f of files) {
		const e = loadExtract(f);
		if (!e) continue;
		e.domains.forEach((d) => domains.add(d));
		e.subnets.forEach((s) => subnets.add(s));
	}
	return {
		domains: [...domains].sort(),
		subnets: [...subnets].sort(),
	};
}

function findDecompiledForPreset(preset) {
	const tries = new Set();
	const composite = COMPOSITE[preset.id];
	if (composite) return mergeExtracts(composite.files);

	const sb = preset.engines?.singbox;
	if (!sb?.ruleSets) return null;

	for (const rs of sb.ruleSets) {
		if (rs.tag) tries.add(path.join('sagernet', `${rs.tag}.json`));
		if (rs.url?.includes('vernette')) {
			const base = path.basename(rs.url, '.srs');
			tries.add(path.join('vernette', `${base}.json`));
			tries.add(`${base}.json`);
		}
	}
	tries.add(path.join('vernette', `${preset.id}.json`));
	tries.add(`${preset.id}.json`);

	for (const rel of tries) {
		const e = loadExtract(rel);
		if (e && (e.domains.length || e.subnets.length)) return e;
	}
	return null;
}

function buildDns(existing, extracted) {
	if (!extracted) return existing;
	const domains = new Set(existing?.domains || []);
	const subnets = new Set(existing?.subnets || []);
	for (const d of extracted.domains) domains.add(d);
	for (const s of extracted.subnets) subnets.add(s);

	const out = {
		domains: [...domains].sort(),
		subnets: [...subnets].sort(),
	};
	if (existing?.subscriptionUrl) out.subscriptionUrl = existing.subscriptionUrl;
	return out;
}

const presets = JSON.parse(fs.readFileSync(DEFAULTS_PATH, 'utf8'));
const changes = [];

for (const preset of presets) {
	if (!preset.engines?.singbox) continue;
	if (SKIP_IDS.has(preset.id)) continue;

	const existing = preset.engines.dns;
	if (existing?.subscriptionUrl && !COMPOSITE[preset.id]) continue;

	const extracted = findDecompiledForPreset(preset);
	if (!extracted || (!extracted.domains.length && !extracted.subnets.length)) continue;

	const next = buildDns(existing, extracted);
	if (next.domains.length > MAX_DOMAINS) {
		changes.push({ id: preset.id, skip: `domains ${next.domains.length} > ${MAX_DOMAINS}` });
		continue;
	}

	const beforeD = existing?.domains?.length ?? 0;
	const beforeS = existing?.subnets?.length ?? 0;
	if (
		next.domains.length === beforeD &&
		next.subnets.length === beforeS &&
		(existing || next.domains.length === 0)
	) {
		continue;
	}

	preset.engines.dns = next.domains.length || next.subnets.length || next.subscriptionUrl ? next : undefined;
	if (!preset.engines.dns) delete preset.engines.dns;

	changes.push({
		id: preset.id,
		domains: `${beforeD} -> ${next.domains.length}`,
		subnets: `${beforeS} -> ${next.subnets.length}`,
	});
}

console.log(JSON.stringify(changes, null, 2));

if (!DRY && changes.some((c) => !c.skip)) {
	fs.writeFileSync(DEFAULTS_PATH, `${JSON.stringify(presets, null, 2)}\n`);
	console.error(`updated ${DEFAULTS_PATH}`);
}
