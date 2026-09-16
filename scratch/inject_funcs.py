import os

file_path = "E:/AWGM/awg-manager/internal/mihomo/coordinator.go"

with open(file_path, "r", encoding="utf-8") as f:
    content = f.read()

funcs = """

func (c *ApplyCoordinator) setStateLocked(state ManifestState) {
	c.state = state
}

func (c *ApplyCoordinator) writeRecoveryMarkerLocked(msg string) error {
	return strictfs.StrictWriteAtomic(c.recoveryMarkerFile, []byte(msg), 0600)
}
"""

if "func (c *ApplyCoordinator) setStateLocked" not in content:
    with open(file_path, "a", encoding="utf-8") as f:
        f.write(funcs)
    print("Injected functions.")
else:
    print("Functions already present.")
