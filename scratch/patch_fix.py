import re

with open('internal/mihomo/coordinator.go', 'r', encoding='utf-8') as f:
    text = f.read()

def replace_func(text, func_name, new_impl):
    pattern = r'^func \(c \*ApplyCoordinator\) ' + func_name + r'\b.*?^}'
    match = re.search(pattern, text, re.DOTALL | re.MULTILINE)
    if not match:
        print(f'Function {func_name} not found!')
        return text
    return text[:match.start()] + new_impl + text[match.end():]

impl_runtime_off = '''func (c *ApplyCoordinator) applyRuntimeOffLocked(ctx context.Context, manifest *TransactionManifest, compileResult *CompileResult) error {
\tc.log("info", "coordinator.apply", "Applying configuration in RuntimeOff mode (files only)")

\tvar beforeBridges []BridgeRef
\tif c.appliedRecord != nil {
\t\tbeforeBridges = c.appliedRecord.AppliedBridges
\t}
\tif err := c.syncBridgesLocked(ctx, manifest, beforeBridges, nil); err != nil {
\t\treturn fmt.Errorf("failed to withdraw bridges during RuntimeOff apply: %w", err)
\t}

\tmanifest.State = StateApplied
\tif err := c.transitionManifestLocked(manifest, manifest.State); err != nil {
\t\treturn err
\t}

\tmanifest.State = StateVerified
\tif err := c.transitionManifestLocked(manifest, manifest.State); err != nil {
\t\treturn err
\t}

\treturn nil
}'''

impl_rollback = '''func (c *ApplyCoordinator) rollbackActiveLocked(ctx context.Context, m *TransactionManifest) error {
\tc.log("info", "coordinator.rollback", "Starting full state rollback to pre-transaction snapshot")

\tvar rollbackErr error

\tif err := c.genStore.RestoreSnapshotFile(ctx, m.TxID); err != nil {
\t\trollbackErr = fmt.Errorf("failed to restore file snapshot: %w", err)
\t\t_ = c.writeRecoveryMarkerLocked("rollback restore snapshot failed: " + err.Error())
\t}

\tif rollbackErr == nil {
\t\tif err := c.cfg.Operator.Restore(ctx, m.TxID); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to restore operator config: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked("rollback operator restore failed: " + err.Error())
\t\t}
\t}

\tif rollbackErr == nil && m.DesiredMode != RuntimeOff {
\t\tvar targetBridges []BridgeRef
\t\tif c.appliedRecord != nil {
\t\t\ttargetBridges = c.appliedRecord.AppliedBridges
\t\t}
\t\t
\t\tcurrentBridgesMap := make(map[string]BridgeRef)
\t\tfor _, b := range targetBridges {
\t\t\tcurrentBridgesMap[b.KernelInterface] = b
\t\t}
\t\tfor _, op := range m.BridgeOperations {
\t\t\tif op.State == BridgeOpVerified {
\t\t\t\tif op.Action == "create" {
\t\t\t\t\tcurrentBridgesMap[op.BridgeRef.KernelInterface] = op.BridgeRef
\t\t\t\t} else if op.Action == "withdraw" {
\t\t\t\t\tdelete(currentBridgesMap, op.BridgeRef.KernelInterface)
\t\t\t\t}
\t\t\t}
\t\t}
\t\tvar currentBridges []BridgeRef
\t\tfor _, b := range currentBridgesMap {
\t\t\tcurrentBridges = append(currentBridges, b)
\t\t}

\t\t// Note that trackOp modifies m.BridgeOperations so it tracks rollback ops too
\t\tif err := c.syncBridgesLocked(ctx, m, currentBridges, targetBridges); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to rollback bridges: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked("rollback sync bridges failed: " + err.Error())
\t\t}
\t}

\tif rollbackErr != nil {
\t\tc.log("error", "coordinator.rollback", "Rollback failed, requiring manual recovery: "+rollbackErr.Error())
\t\tm.State = StateRecoveryRequired
\t\tm.FailureReason = rollbackErr.Error()
\t\tm.UpdatedAt = time.Now()
\t\t_ = c.checkpointManifestLocked(m)
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("rollback critical failure: %w", rollbackErr)
\t}

\t_ = c.cleanupTxArtifactsLocked(m)

\tc.setStateLocked(StateRolledBack)
\treturn c.transitionManifestLocked(m, StateRolledBack)
}'''

text = replace_func(text, 'applyRuntimeOffLocked', impl_runtime_off)
text = replace_func(text, 'rollbackActiveLocked', impl_rollback)

with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
    f.write(text)
