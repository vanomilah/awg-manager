import re

with open('internal/mihomo/coordinator.go', 'r', encoding='utf-8') as f:
    text = f.read()

# 1. rollbackActiveLocked
def patch_rollback(text):
    old_func_sig = 'func (c *Coordinator) rollbackActiveLocked(ctx context.Context, manifest ApplyManifest) error {'
    start_idx = text.find(old_func_sig)
    end_idx = text.find('}\n\n// restoreBridgesLocked', start_idx)
    if start_idx == -1 or end_idx == -1:
        print('rollbackActiveLocked not found')
        return text
    
    new_func = '''func (c *Coordinator) rollbackActiveLocked(ctx context.Context, manifest ApplyManifest) error {
\tc.cfg.LogFn("info", "coordinator.rollback", "Starting full state rollback to generation: "+fmt.Sprint(manifest.Generation-1))

\tvar rollbackErr error

\t// 1. Rollback files
\tif err := c.genStore.RestoreSnapshotFile(ctx, manifest.TxID); err != nil {
\t\trollbackErr = fmt.Errorf("failed to restore file snapshot: %w", err)
\t}
\t
\t// 2. Rollback active configs
\tif rollbackErr == nil {
\t\tif err := c.cfg.Operator.Restore(ctx, manifest.TxID); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to restore operator config: %w", err)
\t\t}
\t}

\t// 3. Rollback Bridges
\tif rollbackErr == nil && manifest.RuntimeMode == RuntimeOn {
\t\t// In rollback, desired state is exactly what was active before this transaction started.
\t\t// So target state is manifest.ActiveBridges.
\t\tif err := c.syncBridgesLocked(ctx, manifest, manifest.ActiveBridges, "rollback"); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to rollback bridges: %w", err)
\t\t}
\t}

\tif rollbackErr != nil {
\t\tc.cfg.LogFn("error", "coordinator.rollback", "Rollback failed, requiring manual recovery: "+rollbackErr.Error())
\t\tc.state = recovery_required
\t\t_ = c.transitionManifestLocked(manifest, c.state)
\t\treturn fmt.Errorf("rollback critical failure: %w", rollbackErr)
\t}

\t// 4. Cleanup on success
\t_ = c.genStore.RemoveTxSnapshot(ctx, manifest.TxID)
\tc.state = StateRolledBack
\tif err := c.transitionManifestLocked(manifest, c.state); err != nil {
\t\treturn err // Failed to record rolled back state
\t}

\treturn nil
'''
    return text[:start_idx] + new_func + text[end_idx:]

text = patch_rollback(text)

with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
    f.write(text)
