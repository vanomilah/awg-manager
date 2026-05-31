/**
 * Heuristic: singbox rule → preset.id (slug for PresetIcon).
 *
 * Использует SERVICE_PRESETS из $lib/data/presets — единый источник
 * domain matchers, поддерживается командой и обновляется. Дублировать
 * паттерны тут — антипаттерн (см. F2 review).
 */

import { SERVICE_PRESETS } from '$lib/data/presets';
import type { SingboxRouterRule } from '$lib/types';

function suffixMatches(domain: string, suffix: string): boolean {
  const d = domain.toLowerCase();
  const s = suffix.toLowerCase();
  return d === s || d.endsWith('.' + s);
}

/** Strip "geosite-" / "geoip-" prefix and normalize for matching to preset.id. */
function normalizeRuleSetName(name: string): string {
  return name.toLowerCase().replace(/^geo(site|ip)[-_]/, '');
}

/**
 * Returns preset.id (e.g. 'netflix', 'telegram', 'youtube') for known services,
 * or 'custom' otherwise. The id is fed to PresetIcon for brand-correct rendering.
 */
export function detectServiceKey(rule: SingboxRouterRule): string {
  const domains = rule.domain_suffix ?? [];
  if (domains.length > 0) {
    for (const preset of SERVICE_PRESETS) {
      const presetDomains = preset.domains ?? [];
      // Skip presets with no domain matchers or only-CIDR matchers
      const onlyCIDR = presetDomains.every((d) => /^\d|^[\da-f]+:/i.test(d));
      if (presetDomains.length === 0 || onlyCIDR) continue;
      for (const ruleDomain of domains) {
        for (const presetDomain of presetDomains) {
          // Skip CIDR-looking entries inside presets (e.g. telegram has IP ranges mixed with domains)
          if (/^\d|^[\da-f]+:/i.test(presetDomain)) continue;
          if (suffixMatches(ruleDomain, presetDomain)) {
            return preset.id;
          }
        }
      }
    }
  }

  const sets = rule.rule_set ?? [];
  if (sets.length > 0) {
    for (const rs of sets) {
      const normalized = normalizeRuleSetName(rs);
      // Direct id match: rule_set 'telegram' or 'geosite-telegram' → 'telegram'
      const preset = SERVICE_PRESETS.find((p) => p.id.toLowerCase() === normalized);
      if (preset) return preset.id;
      // Special case: 'ru' / 'russia' → russian-services
      if (normalized === 'ru' || normalized === 'russia') {
        const ru = SERVICE_PRESETS.find((p) => p.id === 'russian-services');
        if (ru) return ru.id;
      }
    }
  }

  return 'custom';
}
