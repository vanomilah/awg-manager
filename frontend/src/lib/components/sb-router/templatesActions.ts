import { api } from '$lib/api/client';
import type { TemplateGroup, TemplateItem } from './templatesData';

export interface SubmitResult {
  successes: string[];
  failures: Array<{ id: string; error: string }>;
}

function findItem(groups: TemplateGroup[], id: string): TemplateItem | undefined {
  for (const g of groups) {
    const found = g.items.find((it) => it.id === id);
    if (found) return found;
  }
  return undefined;
}

async function applyOne(item: TemplateItem, outboundOrBlock: string): Promise<void> {
  if (item.category === 'services') {
    const outbound = outboundOrBlock === 'block' ? '' : outboundOrBlock;
    await api.singboxRouterApplyPreset(item.presetId, outbound);
    return;
  }
  // rulesets
  if (outboundOrBlock === 'block') {
    await api.singboxRouterAddRule({
      rule_set: [item.tag],
      action: 'reject',
    });
  } else {
    await api.singboxRouterAddRule({
      rule_set: [item.tag],
      outbound: outboundOrBlock,
      action: 'route',
    });
  }
}

export async function submitTemplates(
  selection: string[],
  outboundOrBlock: string,
  groups: TemplateGroup[],
): Promise<SubmitResult> {
  const tasks = selection.map(async (id) => {
    const item = findItem(groups, id);
    if (!item) {
      return { id, ok: false as const, error: 'template not found' };
    }
    try {
      await applyOne(item, outboundOrBlock);
      return { id, ok: true as const };
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      return { id, ok: false as const, error: msg };
    }
  });

  const results = await Promise.all(tasks);
  return {
    successes: results.filter((r) => r.ok).map((r) => r.id),
    failures: results.filter((r) => !r.ok).map((r) => ({ id: r.id, error: (r as { error: string }).error })),
  };
}
