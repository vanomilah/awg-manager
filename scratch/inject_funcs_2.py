import os

file_path = "E:/AWGM/awg-manager/internal/mihomo/coordinator.go"

funcs = """
func (c *ApplyCoordinator) casManifest(expected, next *TransactionManifest) error {
	if expected != nil && expected.Sequence > 0 {
		checkData, err := os.ReadFile(c.manifestFile)
		if err != nil {
			return fmt.Errorf("read manifest for cas: %w", err)
		}
		var checkM TransactionManifest
		if err := json.Unmarshal(checkData, &checkM); err != nil {
			return fmt.Errorf("parse manifest for cas: %w", err)
		}
		if checkM.TxID != expected.TxID || checkM.Sequence != expected.Sequence {
			return fmt.Errorf("concurrent modification detected (expected seq %d, found %d)", expected.Sequence, checkM.Sequence)
		}
	}

	newData, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := strictfs.StrictWriteAtomic(c.manifestFile, newData, 0600); err != nil {
		checkData, checkErr := os.ReadFile(c.manifestFile)
		if checkErr == nil {
			var checkM TransactionManifest
			if json.Unmarshal(checkData, &checkM) == nil {
				if checkM.TxID == next.TxID && checkM.Sequence == next.Sequence && checkM.State == next.State {
					return nil
				}
			}
		}
		return fmt.Errorf("write manifest (ambiguous outcome): %w", err)
	}
	return nil
}

func (c *ApplyCoordinator) checkpointManifestLocked(m *TransactionManifest) error {
	expected := m.Clone()
	next := m.Clone()
	next.Sequence++
	next.UpdatedAt = time.Now()
	if err := c.casManifest(expected, next); err != nil {
		return err
	}
	*m = *next
	return nil
}

func (c *ApplyCoordinator) transitionManifestLocked(m *TransactionManifest, nextState ManifestState) error {
	expected := m.Clone()
	next := m.Clone()
	next.Sequence++
	next.State = nextState
	next.UpdatedAt = time.Now()

	if err := c.casManifest(expected, next); err != nil {
		return err
	}

	*m = *next
	c.setStateLocked(nextState)
	return nil
}

// Reconcile handles administrative recovery commands.
func (c *ApplyCoordinator) Reconcile(ctx context.Context, action string, force bool) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	ipcLock, err := strictfs.LockTransaction(c.cfg.ConfigDir)
	if err != nil {
		return fmt.Errorf("acquire transaction lock: %w", err)
	}
	defer ipcLock.Close()

	switch action {
	case "rollback_to_lkg":
		if _, err := os.Stat(c.lkgConfigFile); err != nil {
			return fmt.Errorf("LKG config file not found: %w", err)
		}
		if renErr := strictfs.StrictRename(c.lkgConfigFile, c.activeConfigFile); renErr != nil {
			c.writeRecoveryMarkerLocked("rename failed: " + renErr.Error())
		}
		if uErr := strictfs.StrictUnlink(c.recoveryMarkerFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
		if uErr := strictfs.StrictUnlink(c.manifestFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
		c.setStateLocked(StateIdle)
		return nil

	case "clear_marker":
		if !force {
			return fmt.Errorf("clear_marker requires explicit confirmation")
		}
		if uErr := strictfs.StrictUnlink(c.recoveryMarkerFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
		if uErr := strictfs.StrictUnlink(c.manifestFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
		c.setStateLocked(StateIdle)
		return nil

	default:
		return fmt.Errorf("unsupported recovery action: %s", action)
	}
}
"""

with open(file_path, "a", encoding="utf-8") as f:
    f.write(funcs)
print("Injected casManifest, checkpointManifestLocked, transitionManifestLocked, Reconcile")
