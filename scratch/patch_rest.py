import re

with open('scratch/coordinator_patched2.go', 'r', encoding='utf-8') as f:
    text = f.read()

def replace_func(text, func_name, new_impl):
    pattern = r'^func \(c \*ApplyCoordinator\) ' + func_name + r'\b.*?^}'
    match = re.search(pattern, text, re.DOTALL | re.MULTILINE)
    if not match:
        print(f'Function {func_name} not found!')
        return text
    return text[:match.start()] + new_impl + text[match.end():]

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
\t\t\t\tTargetDigest: ref.Digest(),
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
\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("bridge %s side-effect succeeded but checkpoint failed: %v", ref.KernelInterface, cpErr))
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
\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("bridge %s verified but checkpoint failed: %v", ref.KernelInterface, cpErr))
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
\t\t_ = c.writeRecoveryMarkerLocked("rollback restore snapshot failed: " + err.Error())
\t}

\tif rollbackErr == nil {
\t\tif err := c.cfg.Operator.Restore(ctx, m.TxID); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to restore operator config: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked("rollback operator restore failed: " + err.Error())
\t\t}
\t}

\tif rollbackErr == nil && m.RuntimeMode == RuntimeOn {
\t\tif err := c.syncBridgesLocked(ctx, m, m.TargetBridges, m.ActiveBridges); err != nil {
\t\t\trollbackErr = fmt.Errorf("failed to rollback bridges: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked("rollback sync bridges failed: " + err.Error())
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

\t_ = c.cleanupTxArtifactsLocked(m)

\tc.setStateLocked(StateRolledBack)
\treturn c.transitionManifestLocked(m, StateRolledBack)
}'''


text = replace_func(text, 'applyRuntimeOffLocked', impl_runtime_off)
text = replace_func(text, 'syncBridgesLocked', impl_sync_bridges)
text = replace_func(text, 'rollbackActiveLocked', impl_rollback)

with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
    f.write(text)
