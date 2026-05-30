import { api } from '$lib/api/client';
import { singboxRouter } from '$lib/stores/singboxRouter';
import type { CustomMatcherFields } from './addWizardStore';
import type { TemplateGroup } from './templatesData';
import type { SubmitResult } from './templatesActions';
import { submitWizard } from './addWizardActions';
import { mergeAndSaveSettings } from './settingsActions';

export interface FinishSetupArgs {
  tunnelTag: string;
  selectedTemplates: string[];
  customFields: CustomMatcherFields;
  groups: TemplateGroup[];
}

export async function finishSetup(args: FinishSetupArgs): Promise<SubmitResult> {
  const result = await submitWizard({
    selectedTemplates: args.selectedTemplates,
    customFields: args.customFields,
    outboundCategory: 'tunnel',
    tunnelTag: args.tunnelTag,
    groups: args.groups,
  });
  await api.singboxRouterPutRouteFinal('direct');
  await mergeAndSaveSettings({
    deviceMode: 'all',
    wanAutoDetect: true,
    wanInterface: '',
    snifferEnabled: true,
  });
  await api.singboxRouterEnable();
  await singboxRouter.loadAll();
  return result;
}
