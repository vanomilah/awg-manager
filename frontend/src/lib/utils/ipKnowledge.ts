// База знаний для быстрого распознавания сервисов, CDN и организаций по IP-адресам
export interface IpKnowledgeInfo {
	title: string;
	org?: string;
	country?: string;
	countryCode?: string;
	category?: string;
}

interface CidrRule {
	net: number;
	mask: number;
	title: string;
	org: string;
	country: string;
	countryCode: string;
	category: string;
}

function parseIp(ip: string): number | null {
	const parts = ip.trim().split('.');
	if (parts.length !== 4) return null;
	let n = 0;
	for (let i = 0; i < 4; i++) {
		const b = parseInt(parts[i], 10);
		if (isNaN(b) || b < 0 || b > 255) return null;
		n = (n << 8) | b;
	}
	return n >>> 0;
}

function parseCidr(cidr: string): { net: number; mask: number } | null {
	const [ipPart, bitsPart] = cidr.split('/');
	const net = parseIp(ipPart);
	if (net === null) return null;
	const bits = parseInt(bitsPart, 10);
	if (isNaN(bits) || bits < 0 || bits > 32) return null;
	const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0;
	return { net: (net & mask) >>> 0, mask };
}

const rawRules: Array<{
	cidr: string;
	title: string;
	org: string;
	country: string;
	cc: string;
	category: string;
}> = [
	// Google / YouTube
	{ cidr: '172.217.0.0/16', title: 'YouTube / Google', org: 'Google LLC', country: 'США', cc: 'US', category: 'media' },
	{ cidr: '142.250.0.0/16', title: 'Google / YouTube', org: 'Google LLC', country: 'США', cc: 'US', category: 'media' },
	{ cidr: '173.194.0.0/16', title: 'YouTube / Google', org: 'Google LLC', country: 'США', cc: 'US', category: 'media' },
	{ cidr: '216.58.192.0/19', title: 'Google Search / API', org: 'Google LLC', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '108.177.0.0/17', title: 'Google Video', org: 'Google LLC', country: 'США', cc: 'US', category: 'media' },
	{ cidr: '74.125.0.0/16', title: 'Google Services', org: 'Google LLC', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '64.233.160.0/19', title: 'Google CDN', org: 'Google LLC', country: 'США', cc: 'US', category: 'cloud' },

	// Meta / WhatsApp / Instagram
	{ cidr: '31.13.64.0/18', title: 'Meta / WhatsApp', org: 'Meta Platforms, Inc.', country: 'США', cc: 'US', category: 'communication' },
	{ cidr: '157.240.0.0/16', title: 'Meta / Instagram', org: 'Meta Platforms, Inc.', country: 'США', cc: 'US', category: 'social' },
	{ cidr: '57.144.0.0/16', title: 'Meta Platforms', org: 'Meta Platforms, Inc.', country: 'США', cc: 'US', category: 'social' },
	{ cidr: '129.134.0.0/16', title: 'Meta Infra', org: 'Meta Platforms, Inc.', country: 'США', cc: 'US', category: 'social' },

	// Telegram
	{ cidr: '149.154.160.0/20', title: 'Telegram', org: 'Telegram Messenger Inc.', country: 'Нидерланды', cc: 'NL', category: 'communication' },
	{ cidr: '91.108.4.0/22', title: 'Telegram', org: 'Telegram Messenger Inc.', country: 'Нидерланды', cc: 'NL', category: 'communication' },
	{ cidr: '91.108.56.0/22', title: 'Telegram', org: 'Telegram Messenger Inc.', country: 'Нидерланды', cc: 'NL', category: 'communication' },
	{ cidr: '91.108.8.0/22', title: 'Telegram Media', org: 'Telegram Messenger Inc.', country: 'Сингапур', cc: 'SG', category: 'communication' },
	{ cidr: '91.108.12.0/22', title: 'Telegram DC4', org: 'Telegram Messenger Inc.', country: 'Нидерланды', cc: 'NL', category: 'communication' },

	// Cloudflare
	{ cidr: '104.16.0.0/12', title: 'Cloudflare CDN', org: 'Cloudflare, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '172.64.0.0/13', title: 'Cloudflare CDN', org: 'Cloudflare, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '162.158.0.0/15', title: 'Cloudflare Proxy', org: 'Cloudflare, Inc.', country: 'США', cc: 'US', category: 'cloud' },

	// VK / Одноклассники / Mail.ru
	{ cidr: '87.240.128.0/18', title: 'ВКонтакте', org: 'VK LLC', country: 'Россия', cc: 'RU', category: 'social' },
	{ cidr: '95.213.0.0/16', title: 'ВКонтакте', org: 'VK LLC', country: 'Россия', cc: 'RU', category: 'social' },
	{ cidr: '5.101.40.0/22', title: 'Одноклассники (VK)', org: 'VK LLC', country: 'Россия', cc: 'RU', category: 'social' },
	{ cidr: '95.163.0.0/16', title: 'Mail.ru / VK', org: 'VK LLC', country: 'Россия', cc: 'RU', category: 'communication' },

	// Яндекс
	{ cidr: '77.88.0.0/18', title: 'Яндекс', org: 'Yandex LLC', country: 'Россия', cc: 'RU', category: 'cloud' },
	{ cidr: '87.250.224.0/19', title: 'Яндекс / Кинопоиск', org: 'Yandex LLC', country: 'Россия', cc: 'RU', category: 'media' },
	{ cidr: '94.139.240.0/20', title: 'Яндекс.Облако', org: 'Yandex.Cloud LLC', country: 'Россия', cc: 'RU', category: 'cloud' },
	{ cidr: '93.158.177.0/24', title: 'Яндекс Инфраструктура', org: 'Yandex LLC', country: 'Россия', cc: 'RU', category: 'cloud' },

	// Apple
	{ cidr: '17.0.0.0/8', title: 'Apple Services', org: 'Apple Inc.', country: 'США', cc: 'US', category: 'cloud' },

	// Microsoft
	{ cidr: '20.0.0.0/8', title: 'Microsoft / Azure', org: 'Microsoft Corporation', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '40.64.0.0/10', title: 'Microsoft / Azure', org: 'Microsoft Corporation', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '52.96.0.0/12', title: 'Microsoft Office 365', org: 'Microsoft Corporation', country: 'США', cc: 'US', category: 'cloud' },

	// GitHub / Fastly / CDN
	{ cidr: '140.82.112.0/20', title: 'GitHub', org: 'GitHub, Inc.', country: 'США', cc: 'US', category: 'dev' },
	{ cidr: '185.199.108.0/22', title: 'GitHub Pages', org: 'GitHub, Inc.', country: 'США', cc: 'US', category: 'dev' },
	{ cidr: '151.101.0.0/16', title: 'Fastly CDN', org: 'Fastly, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '185.76.0.0/16', title: 'Fastly / Discord', org: 'Fastly, Inc.', country: 'США', cc: 'US', category: 'communication' },
	{ cidr: '204.216.188.0/24', title: 'Fastly CDN', org: 'Fastly, Inc.', country: 'США', cc: 'US', category: 'cloud' },

	// Akamai / EdgeCast / CDN
	{ cidr: '23.0.0.0/12', title: 'Akamai CDN', org: 'Akamai Technologies', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '96.211.229.0/24', title: 'Akamai CDN', org: 'Akamai Technologies', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '194.221.250.0/24', title: 'Akamai CDN', org: 'Akamai Technologies', country: 'Европа', cc: 'EU', category: 'cloud' },
	{ cidr: '110.172.149.0/24', title: 'EdgeCast CDN', org: 'Verizon Digital Media', country: 'США', cc: 'US', category: 'cloud' },

	// Hosting & Cloud (OVH, DigitalOcean, Scaleway, AWS, Hetzner, IONOS)
	{ cidr: '3.0.0.0/9', title: 'Amazon AWS', org: 'Amazon.com, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '18.0.0.0/9', title: 'Amazon AWS', org: 'Amazon.com, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '52.0.0.0/10', title: 'Amazon AWS', org: 'Amazon.com, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '54.0.0.0/9', title: 'Amazon AWS', org: 'Amazon.com, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '108.214.136.0/24', title: 'Amazon AWS', org: 'Amazon.com, Inc.', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '198.199.64.0/18', title: 'DigitalOcean', org: 'DigitalOcean, LLC', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '159.203.0.0/16', title: 'DigitalOcean', org: 'DigitalOcean, LLC', country: 'США', cc: 'US', category: 'cloud' },
	{ cidr: '51.89.244.0/24', title: 'OVHcloud', org: 'OVH SAS', country: 'Франция', cc: 'FR', category: 'cloud' },
	{ cidr: '146.239.26.0/24', title: 'OVHcloud', org: 'OVH SAS', country: 'Франция', cc: 'FR', category: 'cloud' },
	{ cidr: '51.15.225.0/24', title: 'Scaleway Cloud', org: 'Scaleway S.A.S.', country: 'Франция', cc: 'FR', category: 'cloud' },
	{ cidr: '217.160.165.0/24', title: 'IONOS Hosting', org: 'IONOS SE', country: 'Германия', cc: 'DE', category: 'cloud' },
	{ cidr: '37.19.210.0/24', title: 'M247 Hosting', org: 'M247 Ltd', country: 'Европа', cc: 'EU', category: 'cloud' },
	{ cidr: '67.144.0.0/16', title: 'Lumen / CenturyLink', org: 'Lumen Technologies', country: 'США', cc: 'US', category: 'isp' },

	// Steam / Valve
	{ cidr: '155.133.224.0/19', title: 'Steam (Valve)', org: 'Valve Corporation', country: 'США', cc: 'US', category: 'gaming' },
	{ cidr: '162.254.192.0/19', title: 'Steam (Valve)', org: 'Valve Corporation', country: 'США', cc: 'US', category: 'gaming' },
];

const compiledRules: CidrRule[] = [];
for (const r of rawRules) {
	const c = parseCidr(r.cidr);
	if (c) {
		compiledRules.push({
			net: c.net,
			mask: c.mask,
			title: r.title,
			org: r.org,
			country: r.country,
			countryCode: r.cc,
			category: r.category,
		});
	}
}

/**
 * Быстрое сопоставление IP-адреса с базой сервисов и стран
 */
export function lookupIpKnowledge(ipStr: string): IpKnowledgeInfo | null {
	const cleanIp = ipStr.split(':')[0].trim();
	const num = parseIp(cleanIp);
	if (num === null) return null;

	for (const rule of compiledRules) {
		if ((num & rule.mask) === rule.net) {
			return {
				title: rule.title,
				org: rule.org,
				country: rule.country,
				countryCode: rule.countryCode,
				category: rule.category,
			};
		}
	}
	return null;
}
