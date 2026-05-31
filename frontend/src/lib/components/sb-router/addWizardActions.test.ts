import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('$lib/api/client', () => ({
  api: {
    singboxRouterApplyPreset: vi.fn(),
    singboxRouterAddRule: vi.fn(),
  },
}));

import { api } from '$lib/api/client';
import {
  composeCustomRule, resolveOutbound, submitWizard, ValidationError,
} from './addWizardActions';
import type { CustomMatcherFields } from './addWizardStore';
import type { TemplateGroup } from './templatesData';

const emptyCustom: CustomMatcherFields = {
  domainSuffix: '', ipCidr: '', sourceIpCidr: '', port: '',
  ruleSetTags: new Set(),
};
const groups: TemplateGroup[] = [
  {
    category: 'services', title: 'Сервисы',
    items: [{ id: 'svc:netflix', category: 'services', presetId: 'netflix', name: 'Netflix' }],
  },
];

describe('resolveOutbound', () => {
  it('tunnel returns tag', () => {
    expect(resolveOutbound('tunnel', 'warp')).toBe('warp');
  });

  it('tunnel without tag throws', () => {
    expect(() => resolveOutbound('tunnel', null)).toThrow(ValidationError);
  });

  it('direct returns "direct"', () => {
    expect(resolveOutbound('direct', null)).toBe('direct');
  });

  it('block returns "block"', () => {
    expect(resolveOutbound('block', null)).toBe('block');
  });
});

describe('composeCustomRule', () => {
  it('empty fields → null', () => {
    expect(composeCustomRule(emptyCustom, 'warp')).toBe(null);
  });

  it('domainSuffix only → domain_suffix array + outbound', () => {
    const r = composeCustomRule({ ...emptyCustom, domainSuffix: 'a.com\nb.com\n' }, 'warp');
    expect(r).toEqual({ domain_suffix: ['a.com', 'b.com'], outbound: 'warp', action: 'route' });
  });

  it('ipCidr only', () => {
    const r = composeCustomRule({ ...emptyCustom, ipCidr: '1.1.1.1/32' }, 'warp');
    expect(r).toEqual({ ip_cidr: ['1.1.1.1/32'], outbound: 'warp', action: 'route' });
  });

  it('block outbound → action=reject, no outbound key', () => {
    const r = composeCustomRule({ ...emptyCustom, domainSuffix: 'a.com' }, 'block');
    expect(r).toEqual({ domain_suffix: ['a.com'], action: 'reject' });
  });

  it('rule_set tags from Set', () => {
    const r = composeCustomRule(
      { ...emptyCustom, ruleSetTags: new Set(['telegram', 'netflix-asn']) },
      'warp',
    );
    expect(r?.rule_set?.sort()).toEqual(['netflix-asn', 'telegram']);
    expect(r?.outbound).toBe('warp');
  });

  it('port string parsed', () => {
    const r = composeCustomRule({ ...emptyCustom, ipCidr: '1.1.1.1/32', port: '443' }, 'warp');
    expect(r?.port).toEqual([443]);
  });
});

describe('submitWizard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('no templates AND no custom → throws ValidationError', async () => {
    await expect(submitWizard({
      selectedTemplates: [], customFields: emptyCustom,
      outboundCategory: 'tunnel', tunnelTag: 'warp', groups,
    })).rejects.toThrow(ValidationError);
  });

  it('templates only → submitTemplates called', async () => {
    (api.singboxRouterApplyPreset as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    const r = await submitWizard({
      selectedTemplates: ['svc:netflix'], customFields: emptyCustom,
      outboundCategory: 'tunnel', tunnelTag: 'warp', groups,
    });
    expect(api.singboxRouterApplyPreset).toHaveBeenCalledWith('netflix', 'warp');
    expect(r.successes).toEqual(['svc:netflix']);
  });

  it('custom only → singboxRouterAddRule called with custom payload', async () => {
    (api.singboxRouterAddRule as ReturnType<typeof vi.fn>).mockResolvedValue(0);
    const r = await submitWizard({
      selectedTemplates: [],
      customFields: { ...emptyCustom, domainSuffix: 'a.com' },
      outboundCategory: 'tunnel', tunnelTag: 'warp', groups,
    });
    expect(api.singboxRouterAddRule).toHaveBeenCalledWith({
      domain_suffix: ['a.com'], outbound: 'warp', action: 'route',
    });
    expect(r.successes).toEqual(['custom']);
  });

  it('templates + custom → both submitted, aggregated', async () => {
    (api.singboxRouterApplyPreset as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    (api.singboxRouterAddRule as ReturnType<typeof vi.fn>).mockResolvedValue(0);
    const r = await submitWizard({
      selectedTemplates: ['svc:netflix'],
      customFields: { ...emptyCustom, domainSuffix: 'b.com' },
      outboundCategory: 'tunnel', tunnelTag: 'warp', groups,
    });
    expect(r.successes.sort()).toEqual(['custom', 'svc:netflix']);
    expect(r.failures).toEqual([]);
  });

  it('partial failure: custom API fails', async () => {
    (api.singboxRouterApplyPreset as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    (api.singboxRouterAddRule as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('bad rule'));
    const r = await submitWizard({
      selectedTemplates: ['svc:netflix'],
      customFields: { ...emptyCustom, domainSuffix: 'b.com' },
      outboundCategory: 'tunnel', tunnelTag: 'warp', groups,
    });
    expect(r.successes).toEqual(['svc:netflix']);
    expect(r.failures).toEqual([{ id: 'custom', error: 'bad rule' }]);
  });

  it('tunnel category без tunnelTag → throws', async () => {
    await expect(submitWizard({
      selectedTemplates: ['svc:netflix'], customFields: emptyCustom,
      outboundCategory: 'tunnel', tunnelTag: null, groups,
    })).rejects.toThrow(ValidationError);
  });
});
