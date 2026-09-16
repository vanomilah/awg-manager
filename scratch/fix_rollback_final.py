import os, re
os.chdir('/mnt/e/AWGM/awg-manager')

with open('internal/mihomo/coordinator.go', 'r', encoding='utf-8') as f:
    c = f.read()

old_func = re.search(r'func \(c \*ApplyCoordinator\) rollbackActiveLocked.*?return rollbackErr\n\}', c, re.DOTALL)
if not old_func:
    old_func = re.search(r'func \(c \*ApplyCoordinator\) rollbackActiveLocked.*?\n\n\treturn nil\n\}', c, re.DOTALL)
if not old_func:
    old_func = re.search(r'func \(c \*ApplyCoordinator\) rollbackActiveLocked.*?\}\n\}\n', c, re.DOTALL)

if old_func:
    new_func = """func (c *ApplyCoordinator) rollbackActiveLocked(ctx context.Context, m *TransactionManifest) error {
	c.log("info", "coordinator.rollback", "Starting full state rollback to pre-transaction snapshot")

	if err := c.transitionManifestLocked(m, StateRollbackInProgress); err != nil {
		c.log("error", "coordinator.rollback", "Failed to transition to rollback_in_progress")
	}

	var rollbackErr error

	if m.PreMutationStoreSnapshotFile != "" {
		if err := c.cfg.StoreTx.RestoreSnapshotFile(m.PreMutationStoreSnapshotFile); err != nil {
			rollbackErr = fmt.Errorf("failed to restore store snapshot: %w", err)
			_ = c.writeRecoveryMarkerLocked("rollback restore store snapshot failed: " + err.Error())
		}
	}

	if rollbackErr == nil {
		if err := c.cfg.Operator.StopAndWait(ctx); err != nil {
			rollbackErr = fmt.Errorf("failed to stop operator during rollback: %w", err)
			_ = c.writeRecoveryMarkerLocked("rollback operator stop failed: " + err.Error())
		}
	}

	if rollbackErr == nil && m.DesiredMode != RuntimeOff {
		var targetBridges []BridgeRef
		if c.appliedRecord != nil {
			targetBridges = c.appliedRecord.AppliedBridges
		}
		
		currentBridgesMap := make(map[string]BridgeRef)
		for _, b := range targetBridges {
			currentBridgesMap[b.KernelInterface] = b
		}
		for _, op := range m.BridgeOperations {
			if op.State == BridgeOpVerified {
				if op.Action == "create" {
					currentBridgesMap[op.BridgeRef.KernelInterface] = op.BridgeRef
				} else if op.Action == "withdraw" {
					delete(currentBridgesMap, op.BridgeRef.KernelInterface)
				}
			}
		}
		var currentBridges []BridgeRef
		for _, b := range currentBridgesMap {
			currentBridges = append(currentBridges, b)
		}

		if err := c.syncBridgesLocked(ctx, m, currentBridges, targetBridges); err != nil {
			rollbackErr = fmt.Errorf("failed to rollback bridges: %w", err)
			_ = c.writeRecoveryMarkerLocked("rollback sync bridges failed: " + err.Error())
		}
	}

	if rollbackErr != nil {
		c.log("error", "coordinator.rollback", "Rollback failed, requiring manual recovery: "+rollbackErr.Error())
		m.State = StateRecoveryRequired
	} else {
		m.State = StateRolledBack
	}
	
	if err := c.transitionManifestLocked(m, m.State); err != nil {
		return err
	}

	return rollbackErr
}"""
    c = c.replace(old_func.group(0), new_func)
    with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
        f.write(c)
    print("Patched rollbackActiveLocked")
else:
    print("rollbackActiveLocked not found")
