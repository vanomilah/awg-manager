import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('$lib/api/client', () => ({
  api: {
    singboxRouterEnable: vi.fn(),
    singboxRouterPutRouteFinal: vi.fn(),
  },
}));

vi.mock('./addWizardActions', () => ({
  submitWizard: vi.fn(async () => ({ successes: ['svc:netflix'], failures: [] })),
}));

vi.mock('./settingsActions', () => ({
  mergeAndSaveSettings: vi.fn(async () => {}),
}));

vi.mock('$lib/stores/singboxRouter', () => {
  const settings = { subscribe: vi.fn(() => () => {}) };
  return {
    singboxRouter: {
      settings,
      loadAll: vi.fn(async () => {}),
    },
  };
});

import { api } from '$lib/api/client';
import { singboxRouter } from '$lib/stores/singboxRouter';
import { submitWizard } from './addWizardActions';
import { mergeAndSaveSettings } from './settingsActions';
import { finishSetup } from './emptyStateActions';

describe('emptyStateActions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('finishSetup: правила(tunnel) → final=direct → bake defaults(all) → enable → loadAll', async () => {
    const empty = {
      domainSuffix: '', ipCidr: '', sourceIpCidr: '', port: '', ruleSetTags: new Set<string>(),
    };
    const res = await finishSetup({
      tunnelTag: 'wg-nl',
      selectedTemplates: ['svc:netflix'],
      customFields: empty,
      groups: [],
    });
    expect(submitWizard).toHaveBeenCalledWith(
      expect.objectContaining({ outboundCategory: 'tunnel', tunnelTag: 'wg-nl', selectedTemplates: ['svc:netflix'] }),
    );
    expect(api.singboxRouterPutRouteFinal).toHaveBeenCalledWith('direct');
    expect(mergeAndSaveSettings).toHaveBeenCalledWith(
      expect.objectContaining({ deviceMode: 'all', wanAutoDetect: true, wanInterface: '', snifferEnabled: true }),
    );
    expect(api.singboxRouterEnable).toHaveBeenCalled();
    expect(singboxRouter.loadAll).toHaveBeenCalled();
    expect(res.successes).toContain('svc:netflix');
  });
});
