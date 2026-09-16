import os

file_path = "E:/AWGM/awg-manager/internal/mihomo/coordinator.go"

with open(file_path, "r", encoding="utf-8") as f:
    content = f.read()

# Patch syncBridgesLocked
hook_sync = """		op.State = BridgeOpApplied
		m.BridgeOperations[opIndex] = op
		if c.hooks.FailManifestPersistBridgeOpState == BridgeOpApplied {
			c.setStateLocked(StateRecoveryRequired)
			_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("bridge %s side-effect succeeded but checkpoint failed: failpoint", ref.KernelInterface))
			return fmt.Errorf("bridge %s side-effect succeeded but checkpoint failed: failpoint", ref.KernelInterface)
		}
"""
content = content.replace("		op.State = BridgeOpApplied\n		m.BridgeOperations[opIndex] = op\n", hook_sync)

# Patch processCleanupJournalFilesLocked
hook_cleanup = """		err := strictfs.StrictUnlink(targetPath)
		if c.hooks.FailCleanupUnlink {
			err = fmt.Errorf("failpoint: unlink failed")
		}
"""
content = content.replace("		err := strictfs.StrictUnlink(targetPath)\n", hook_cleanup)

with open(file_path, "w", encoding="utf-8") as f:
    f.write(content)

print("Injected hooks logic.")
