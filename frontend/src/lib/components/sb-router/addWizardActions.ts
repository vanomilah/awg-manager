import { api } from '$lib/api/client';
import type { SingboxRouterRule } from '$lib/types';
import { submitTemplates, type SubmitResult } from './templatesActions';
import type { TemplateGroup } from './templatesData';
import type { CustomMatcherFields, OutboundCategory } from './addWizardStore';

export class ValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ValidationError';
  }
}

export function resolveOutbound(
  category: OutboundCategory,
  tunnelTag: string | null,
): string {
  if (category === 'tunnel') {
    if (!tunnelTag) {
      throw new ValidationError('Выберите туннель');
    }
    return tunnelTag;
  }
  if (category === 'direct') return 'direct';
  return 'block';
}

function splitLines(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function parsePorts(text: string): number[] {
  const out: number[] = [];
  for (const part of text.split(',')) {
    const trimmed = part.trim();
    if (!trimmed) continue;
    const n = Number.parseInt(trimmed, 10);
    if (Number.isFinite(n) && n > 0 && n < 65536) out.push(n);
  }
  return out;
}

export function composeCustomRule(
  fields: CustomMatcherFields,
  outboundOrBlock: string,
): Partial<SingboxRouterRule> | null {
  const domains = splitLines(fields.domainSuffix);
  const ips = splitLines(fields.ipCidr);
  const srcIps = splitLines(fields.sourceIpCidr);
  const ports = parsePorts(fields.port);
  const ruleSets = Array.from(fields.ruleSetTags);

  const hasAny = domains.length > 0 || ips.length > 0 || srcIps.length > 0
    || ports.length > 0 || ruleSets.length > 0;
  if (!hasAny) return null;

  const rule: Partial<SingboxRouterRule> = {};
  if (domains.length > 0) rule.domain_suffix = domains;
  if (ips.length > 0) rule.ip_cidr = ips;
  if (srcIps.length > 0) rule.source_ip_cidr = srcIps;
  if (ports.length > 0) rule.port = ports;
  if (ruleSets.length > 0) rule.rule_set = ruleSets;

  if (outboundOrBlock === 'block') {
    rule.action = 'reject';
  } else {
    rule.outbound = outboundOrBlock;
    rule.action = 'route';
  }

  return rule;
}

export interface SubmitWizardArgs {
  selectedTemplates: string[];
  customFields: CustomMatcherFields;
  outboundCategory: OutboundCategory;
  tunnelTag: string | null;
  groups: TemplateGroup[];
}

export async function submitWizard(args: SubmitWizardArgs): Promise<SubmitResult> {
  const outbound = resolveOutbound(args.outboundCategory, args.tunnelTag);
  const customRule = composeCustomRule(args.customFields, outbound);

  if (args.selectedTemplates.length === 0 && customRule === null) {
    throw new ValidationError('Выберите шаблон или опишите правило');
  }

  let combined: SubmitResult = { successes: [], failures: [] };

  if (args.selectedTemplates.length > 0) {
    combined = await submitTemplates(args.selectedTemplates, outbound, args.groups);
  }

  if (customRule !== null) {
    try {
      await api.singboxRouterAddRule(customRule);
      combined.successes.push('custom');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      combined.failures.push({ id: 'custom', error: msg });
    }
  }

  return combined;
}
