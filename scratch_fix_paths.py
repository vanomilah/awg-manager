import re

with open(r'e:\AWGM\awg-manager\internal\mihomo\coordinator.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Add handleApplyFailureLocked right after MutateAndApply
helper = '''
func (c *ApplyCoordinator) handleApplyFailureLocked(ctx context.Context, m *TransactionManifest, origErr error) error {
	if rErr := c.rollbackActiveLocked(ctx, m); rErr != nil {
		c.writeRecoveryMarkerLocked("rollback failed: " + rErr.Error())
		c.setStateLocked(StateRecoveryRequired)
	} else {
		c.cleanupTxArtifactsLocked(m)
		c.setStateLocked(StateIdle)
	}
	return origErr
}
'''
if 'func (c *ApplyCoordinator) handleApplyFailureLocked' not in content:
    content = content.replace('func (c *ApplyCoordinator) applyRuntimeOffLocked', helper.strip() + '\n\nfunc (c *ApplyCoordinator) applyRuntimeOffLocked')

# Replace the boilerplate in MutateAndApply
content = re.sub(r'if rErr := c\.rollbackActiveLocked\(ctx, &manifest\); rErr != nil \{\s*c\.writeRecoveryMarkerLocked\("rollback failed: " \+ rErr\.Error\(\)\)\s*\}\s*return fmt\.Errorf\("swap candidate to active config: %w", swapErr\)',
                 r'return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("swap candidate to active config: %w", swapErr))', content)

content = re.sub(r'if rErr := c\.rollbackActiveLocked\(ctx, &manifest\); rErr != nil \{\s*c\.writeRecoveryMarkerLocked\("rollback failed: " \+ rErr\.Error\(\)\)\s*\}\s*return fmt\.Errorf\("runtime restart failed: %w", restartErr\)',
                 r'return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("runtime restart failed: %w", restartErr))', content)

content = re.sub(r'if rErr := c\.rollbackActiveLocked\(ctx, &manifest\); rErr != nil \{\s*c\.writeRecoveryMarkerLocked\("rollback failed: " \+ rErr\.Error\(\)\)\s*\}\s*return fmt\.Errorf\("bridge sync failed: %w", bridgeErr\)',
                 r'return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("bridge sync failed: %w", bridgeErr))', content)

content = re.sub(r'if err := c\.transitionManifestLocked\(&manifest, State([A-Za-z]+)\); err != nil \{\s*if rErr := c\.rollbackActiveLocked\(ctx, &manifest\); rErr != nil \{\s*c\.writeRecoveryMarkerLocked\("rollback failed: " \+ rErr\.Error\(\)\)\s*\}\s*return err\s*\}',
                 r'if err := c.transitionManifestLocked(&manifest, State\1); err != nil {\n\t\treturn c.handleApplyFailureLocked(ctx, &manifest, err)\n\t}', content)

# And for StateSwapIntent which had no rollbackActiveLocked but should have:
content = re.sub(r'if err := c\.transitionManifestLocked\(&manifest, StateSwapIntent\); err != nil \{\s*return err\s*\}',
                 r'if err := c.transitionManifestLocked(&manifest, StateSwapIntent); err != nil {\n\t\treturn c.handleApplyFailureLocked(ctx, &manifest, err)\n\t}', content)

content = re.sub(r'if err := c\.transitionManifestLocked\(&manifest, StateCommitIntent\); err != nil \{\s*return err\s*\}',
                 r'if err := c.transitionManifestLocked(&manifest, StateCommitIntent); err != nil {\n\t\treturn c.handleApplyFailureLocked(ctx, &manifest, err)\n\t}', content)

with open(r'e:\AWGM\awg-manager\internal\mihomo\coordinator.go', 'w', encoding='utf-8') as f:
    f.write(content)
print("Updated MutateAndApply.")
