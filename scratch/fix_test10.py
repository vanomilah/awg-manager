import os

file_path = "E:/AWGM/awg-manager/internal/mihomo/gate1_legacy_test.go"

with open(file_path, "r", encoding="utf-8") as f:
    content = f.read()

bad_test = """	m := TransactionManifest{
		Version: 1,
		TxID:    "20260915120002",
		State:   StateAbortInProgress,
		PreMutationStoreSnapshotFile: "snapshot.db",
		Rules: &RulesManifest{
			PreMutationBridges: []BridgeRef{{KernelInterface: "test-br"}},
			TargetBridges:      []BridgeRef{},
		},
	}"""

good_test = """	coord.appliedRecord = &AppliedGenerationRecord{
		AppliedBridges: []BridgeRef{{KernelInterface: "test-br"}},
	}
	m := TransactionManifest{
		Version: 1,
		TxID:    "20260915120002",
		State:   StateAbortInProgress,
		DesiredMode: RuntimeMihomo,
		PreMutationStoreSnapshotFile: "snapshot.db",
	}"""

content = content.replace(bad_test, good_test)

with open(file_path, "w", encoding="utf-8") as f:
    f.write(content)

print("Test 10 fixed.")
