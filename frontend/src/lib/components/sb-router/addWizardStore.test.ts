import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

function resetEnv(url: string) {
  window.history.replaceState({}, '', url);
  vi.resetModules();
}

describe('addWizardStore', () => {
  beforeEach(() => {
    resetEnv('/');
  });

  it('default state: closed, all empty', async () => {
    const m = await import('./addWizardStore');
    expect(get(m.addWizardOpen)).toBe(false);
    expect(get(m.wizardOutboundCategory)).toBe(null);
    expect(get(m.wizardTunnelTag)).toBe(null);
    const c = get(m.wizardCustom);
    expect(c.domainSuffix).toBe('');
    expect(c.ipCidr).toBe('');
    expect(c.sourceIpCidr).toBe('');
    expect(c.port).toBe('');
    expect(c.ruleSetTags.size).toBe(0);
  });

  it('openAddWizard sets URL ?add=1 + open=true', async () => {
    const m = await import('./addWizardStore');
    m.openAddWizard();
    expect(get(m.addWizardOpen)).toBe(true);
    expect(window.location.search).toContain('add=1');
  });

  it('closeAddWizard removes URL + clears all state', async () => {
    const m = await import('./addWizardStore');
    m.openAddWizard();
    m.setOutboundCategory('tunnel');
    m.setTunnelTag('warp');
    m.updateCustomField('domainSuffix', 'a.com');
    m.toggleCustomRuleSet('telegram');
    m.closeAddWizard();
    expect(get(m.addWizardOpen)).toBe(false);
    expect(get(m.wizardOutboundCategory)).toBe(null);
    expect(get(m.wizardTunnelTag)).toBe(null);
    expect(get(m.wizardCustom).domainSuffix).toBe('');
    expect(get(m.wizardCustom).ruleSetTags.size).toBe(0);
    expect(window.location.search).not.toContain('add=1');
  });

  it('setOutboundCategory updates', async () => {
    const m = await import('./addWizardStore');
    m.setOutboundCategory('tunnel');
    expect(get(m.wizardOutboundCategory)).toBe('tunnel');
    m.setOutboundCategory('block');
    expect(get(m.wizardOutboundCategory)).toBe('block');
    m.setOutboundCategory(null);
    expect(get(m.wizardOutboundCategory)).toBe(null);
  });

  it('setTunnelTag updates', async () => {
    const m = await import('./addWizardStore');
    m.setTunnelTag('warp');
    expect(get(m.wizardTunnelTag)).toBe('warp');
    m.setTunnelTag(null);
    expect(get(m.wizardTunnelTag)).toBe(null);
  });

  it('updateCustomField updates field value', async () => {
    const m = await import('./addWizardStore');
    m.updateCustomField('domainSuffix', 'a.com\nb.com');
    expect(get(m.wizardCustom).domainSuffix).toBe('a.com\nb.com');
    m.updateCustomField('port', '443');
    expect(get(m.wizardCustom).port).toBe('443');
  });

  it('toggleCustomRuleSet adds and removes', async () => {
    const m = await import('./addWizardStore');
    m.toggleCustomRuleSet('telegram');
    expect(get(m.wizardCustom).ruleSetTags.has('telegram')).toBe(true);
    m.toggleCustomRuleSet('telegram');
    expect(get(m.wizardCustom).ruleSetTags.has('telegram')).toBe(false);
  });

  it('resetWizardState keeps open, clears selection/category/tunnel/custom', async () => {
    const m = await import('./addWizardStore');
    m.openAddWizard();
    m.setOutboundCategory('tunnel');
    m.setTunnelTag('warp');
    m.updateCustomField('domainSuffix', 'a.com');
    m.resetWizardState();
    expect(get(m.addWizardOpen)).toBe(true);
    expect(get(m.wizardOutboundCategory)).toBe(null);
    expect(get(m.wizardTunnelTag)).toBe(null);
    expect(get(m.wizardCustom).domainSuffix).toBe('');
  });

  it('module init с URL ?add=1 → open=true', async () => {
    resetEnv('/?add=1');
    const m = await import('./addWizardStore');
    expect(get(m.addWizardOpen)).toBe(true);
  });

  it('module init с URL ?add=1&trace=1 → wizard wins (trace closed)', async () => {
    resetEnv('/?add=1&trace=1');
    const m = await import('./addWizardStore');
    expect(get(m.addWizardOpen)).toBe(true);
    // trace param removed by addWizard init logic
    expect(window.location.search).not.toContain('trace=1');
  });
});
