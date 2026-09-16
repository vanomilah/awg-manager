import re

with open('internal/mihomo/coordinator.go', 'r', encoding='utf-8') as f:
    text = f.read()

def replace_func(text, func_name, new_impl):
    pattern = r'^func \(c \*ApplyCoordinator\) ' + func_name + r'\b.*?^}$'
    match = re.search(pattern, text, re.DOTALL | re.MULTILINE)
    if not match:
        print(f'Function {func_name} not found!')
        return text
    return text[:match.start()] + new_impl + text[match.end():]

# processCleanupJournalFilesLocked
impl_cleanup_files = '''func (c *ApplyCoordinator) processCleanupJournalFilesLocked(cj *CleanupJournal) error {
\tvar finalErr error
\tfor i, jf := range cj.Files {
\t\tif jf.Removed {
\t\t\tcontinue
\t\t}
\t\tpath := filepath.Join(c.cfg.ConfigDir, jf.Path)
\t\tif err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
\t\t\tcj.Files[i].Error = err.Error()
\t\t\tc.log("warn", "coordinator.cleanup", "failed to remove journal file "+path+": "+err.Error())
\t\t\tif finalErr == nil {
\t\t\t\tfinalErr = err
\t\t\t}
\t\t} else {
\t\t\tcj.Files[i].Removed = true
\t\t}
\t}
\treturn finalErr
}'''

# cleanupTxArtifactsLocked
impl_cleanup_tx = '''func (c *ApplyCoordinator) cleanupTxArtifactsLocked(m *TransactionManifest) error {
\tvar finalErr error
\tif m.CleanupJournal != nil {
\t\tif err := c.processCleanupJournalFilesLocked(m.CleanupJournal); err != nil {
\t\t\tfinalErr = err
\t\t}
\t}

\tif err := c.genStore.RemoveTxSnapshot(context.Background(), m.TxID); err != nil {
\t\tc.log("warn", "coordinator.cleanup", "Failed to remove transaction snapshot: "+err.Error())
\t\tif finalErr == nil {
\t\t\tfinalErr = err
\t\t}
\t}

\tif err := c.genStore.RemoveDraftManifest(m.TxID); err != nil && !os.IsNotExist(err) {
\t\tc.log("warn", "coordinator.cleanup", "Failed to remove draft manifest: "+err.Error())
\t\tif finalErr == nil {
\t\t\tfinalErr = err
\t\t}
\t}
\t
\treturn finalErr
}'''

# executeFinalCommitLocked
impl_commit = '''func (c *ApplyCoordinator) executeFinalCommitLocked(ctx context.Context, manifest *TransactionManifest, rec AppliedGenerationRecord) error {
\tmanifest.State = StateCommitIntent
\tif err := c.checkpointManifestLocked(manifest); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("failed to write commit intent: %w", err)
\t}

\tc.setStateLocked(StateCommitIntent)

\t// Save active manifest
\tif err := c.genStore.SaveActiveManifest(*manifest); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("failed to save active manifest: %w", err)
\t}

\t// Remove recovery marker if any
\t_ = os.Remove(filepath.Join(c.cfg.ConfigDir, "recovery_required.marker"))

\t// Reread to verify equality
\tactiveM, err := c.genStore.ReadActiveManifest()
\tif err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("failed to verify active manifest after commit: %w", err)
\t}
\t
\t// Verified-Active Full Equality Check
\tif activeM.GenerationID != manifest.GenerationID || 
\t   activeM.Generation != manifest.Generation ||
\t   activeM.AppliedStoreDigest != manifest.AppliedStoreDigest ||
\t   activeM.AppliedConfigDigest != manifest.AppliedConfigDigest ||
\t   activeM.AppliedInputDigest != manifest.AppliedInputDigest ||
\t   activeM.RuntimeMode != manifest.RuntimeMode {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("active manifest divergence detected")
\t}

\t// Update applied record
\tc.genStore.UpdateAppliedRecord(rec)
\t
\tmanifest.State = StateCommitted
\tif err := c.checkpointManifestLocked(manifest); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("failed to checkpoint committed state: %w", err)
\t}
\t
\tc.setStateLocked(StateCommitted)

\t// Clean up artifacts, if it fails, DO NOT set to Idle.
\tif err := c.cleanupTxArtifactsLocked(manifest); err != nil {
\t\tc.log("error", "coordinator.commit", "partial cleanup failure, retaining journal: "+err.Error())
\t\treturn fmt.Errorf("commit succeeded with partial cleanup failure: %w", err)
\t}

\tc.setStateLocked(StateIdle)
\treturn nil
}'''

# applyRuntimeOffLocked
impl_runtime_off = '''func (c *ApplyCoordinator) applyRuntimeOffLocked(ctx context.Context, manifest *TransactionManifest, compileResult *CompileResult) error {
\tc.log("info", "coordinator.apply", "Applying configuration in RuntimeOff mode (files only)")

\t// In RuntimeOff, we withdraw any active bridges using the common sync logic
\tif err := c.syncBridgesLocked(ctx, manifest, manifest.ActiveBridges, nil); err != nil {
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

# syncBridgesLocked
impl_sync_bridges = '''func (c *ApplyCoordinator) syncBridgesLocked(ctx context.Context, m *TransactionManifest, before, target []BridgeRef) error {
\tif c.cfg.BridgeRuntime == nil {
\t\treturn nil
\t}

\tbeforeMap := make(map[string]BridgeRef)
\tfor _, b := range before {
\t\tbeforeMap[b.KernelInterface] = b
\t}
\ttargetMap := make(map[string]BridgeRef)
\tfor _, b := range target {
\t\ttargetMap[b.KernelInterface] = b
\t}

\tif m.BridgeOperations == nil {
\t\tm.BridgeOperations = []BridgeOperation{}
\t}

\tvar toCreate []BridgeRef
\tvar toWithdraw []BridgeRef

\tfor k, b := range targetMap {
\t\tif _, ok := beforeMap[k]; !ok {
\t\t\ttoCreate = append(toCreate, b)
\t\t}
\t}
\tfor k, b := range beforeMap {
\t\tif _, ok := targetMap[k]; !ok {
\t\t\ttoWithdraw = append(toWithdraw, b)
\t\t}
\t}

\ttrackOp := func(ref BridgeRef, action string) error {
\t\topID := fmt.Sprintf("%s-%s-%s", m.TxID, action, ref.KernelInterface)
\t\topIndex := -1
\t\tfor i, op := range m.BridgeOperations {
\t\t\tif op.OperationID == opID {
\t\t\t\topIndex = i
\t\t\t\tbreak
\t\t\t}
\t\t}

\t\tvar op BridgeOperation
\t\tif opIndex >= 0 {
\t\t\top = m.BridgeOperations[opIndex]
\t\t} else {
\t\t\top = BridgeOperation{
\t\t\t\tOperationID:  opID,
\t\t\t\tAction:       action,
\t\t\t\tTargetDigest: ref.Digest(), // Canonical digest
\t\t\t\tBridgeRef:    ref,
\t\t\t\tState:        BridgeOpIntent,
\t\t\t\tAttempts:     0,
\t\t\t}
\t\t\tm.BridgeOperations = append(m.BridgeOperations, op)
\t\t\topIndex = len(m.BridgeOperations) - 1
\t\t}

\t\tif op.State == BridgeOpVerified {
\t\t\treturn nil
\t\t}

\t\top.State = BridgeOpIntent
\t\top.Attempts++
\t\tm.BridgeOperations[opIndex] = op
\t\tif err := c.transitionManifestLocked(m, m.State); err != nil {
\t\t\treturn fmt.Errorf("persist intent %s: %w", action, err)
\t\t}

\t\tvar err error
\t\tif action == "create" {
\t\t\terr = c.cfg.BridgeRuntime.ApplyBridges(ctx, []BridgeRef{ref})
\t\t} else {
\t\t\terr = c.cfg.BridgeRuntime.WithdrawBridges(ctx, []BridgeRef{ref})
\t\t}

\t\tif err != nil {
\t\t\top.LastError = err.Error()
\t\t\tm.BridgeOperations[opIndex] = op
\t\t\t_ = c.transitionManifestLocked(m, m.State)
\t\t\treturn fmt.Errorf("%s bridge %s: %w", action, ref.KernelInterface, err)
\t\t}

\t\top.State = BridgeOpApplied
\t\tm.BridgeOperations[opIndex] = op
\t\tif cpErr := c.transitionManifestLocked(m, m.State); cpErr != nil {
\t\t\tc.setStateLocked(StateRecoveryRequired)
\t\t\treturn fmt.Errorf("bridge %s side-effect succeeded but checkpoint failed: %w", ref.KernelInterface, cpErr)
\t\t}

\t\tif action == "create" {
\t\t\terr = c.cfg.BridgeRuntime.VerifyBridges(ctx, []BridgeRef{ref})
\t\t\tif err != nil {
\t\t\t\top.LastError = err.Error()
\t\t\t\tm.BridgeOperations[opIndex] = op
\t\t\t\t_ = c.transitionManifestLocked(m, m.State)
\t\t\t\treturn fmt.Errorf("verify create bridge %s: %w", ref.KernelInterface, err)
\t\t\t}
\t\t}

\t\top.State = BridgeOpVerified
\t\tm.BridgeOperations[opIndex] = op
\t\tif cpErr := c.transitionManifestLocked(m, m.State); cpErr != nil {
\t\t\tc.setStateLocked(StateRecoveryRequired)
\t\t\treturn fmt.Errorf("bridge %s verified but checkpoint failed: %w", ref.KernelInterface, cpErr)
\t\t}
\t\t
\t\treturn nil
\t}

\tfor _, b := range toCreate {
\t\tif err := trackOp(b, "create"); err != nil {
\t\t\treturn err
\t\t}
\t}

\tfor _, b := range toWithdraw {
\t\tif err := trackOp(b, "withdraw"); err != nil {
\t\t\treturn err
\t\t}
\t}

\treturn nil
}'''

# rollbackActiveLocked
impl_rollback = '''func (c *ApplyCoordinator) rollbackActiveLocked(ctx context.Context, m *TransactionManifest) error {
\tc.log("info", "coordinator.rollback", "Starting full state rollback to pre-transaction snapshot")

\tvar rollbackErr error

\tif err := c.genStore.RestoreSnapshotFile(ctx, m.TxID); err != nil {
\t\trollbackErr = fmt.Errorf("failed to restore file snapshot: %w", err)
\t}

\tif rollbackErr == nil {
\t\tif err := c.cfg.Operator.Restore(ctx, m.TxID); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to restore operator config: %w", err)
\t\t}
\t}

\tif rollbackErr == nil && m.RuntimeMode == RuntimeOn {
\t\t// Compensate bridges. We withdraw what was attempted to be created, and re-create what was withdrawn.
\t\tif err := c.syncBridgesLocked(ctx, m, m.TargetBridges, m.ActiveBridges); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to rollback bridges: %w", err)
\t\t}
\t}

\tif rollbackErr != nil {
\t\tc.log("error", "coordinator.rollback", "Rollback failed, requiring manual recovery: "+rollbackErr.Error())
\t\tm.State = StateRecoveryRequired
\t\tm.ErrorCause = rollbackErr.Error()
\t\tm.ModifiedAt = time.Now()
\t\t_ = c.checkpointManifestLocked(m)
\t\tc.setStateLocked(StateRecoveryRequired)
\t\treturn fmt.Errorf("rollback critical failure: %w", rollbackErr)
\t}

\tif err := c.cleanupTxArtifactsLocked(m); err != nil {
\t\tc.log("warn", "coordinator.rollback", "Rollback succeeded but cleanup had partial failures: "+err.Error())
\t}

\tc.setStateLocked(StateRolledBack)
\treturn c.transitionManifestLocked(m, StateRolledBack)
}'''


text = replace_func(text, 'processCleanupJournalFilesLocked', impl_cleanup_files)
text = replace_func(text, 'cleanupTxArtifactsLocked', impl_cleanup_tx)
text = replace_func(text, 'executeFinalCommitLocked', impl_commit)
text = replace_func(text, 'applyRuntimeOffLocked', impl_runtime_off)
text = replace_func(text, 'syncBridgesLocked', impl_sync_bridges)
text = replace_func(text, 'rollbackActiveLocked', impl_rollback)

with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
    f.write(text)
