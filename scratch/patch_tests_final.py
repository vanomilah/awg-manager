import os

file_path = "E:/AWGM/awg-manager/internal/mihomo/gate1_legacy_test.go"

tests = """

// 10. Rollback fail-closed Bridge compensation
func TestCoordinator_Gate1_S10_Rollback_FailClosed(t *testing.T) {
	coord, _, bridges, _ := setupGate1Coordinator(t)

	m := TransactionManifest{
		Version: 1,
		TxID:    "20260915120002",
		State:   StateAbortInProgress,
		PreMutationStoreSnapshotFile: "snapshot.db",
		Rules: &RulesManifest{
			PreMutationBridges: []BridgeRef{{KernelInterface: "test-br"}},
			TargetBridges:      []BridgeRef{},
		},
	}
	mBytes, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)
	
	// Create mock snapshot file so it doesn't fail
	_ = os.WriteFile(filepath.Join(coord.cfg.ConfigDir, "snapshot.db"), []byte("data"), 0600)

	err := coord.rollbackActiveLocked(context.Background(), &m)
	// It may return an error if it requires manual recovery or if there are other issues,
	// but we just want to ensure bridges were compensated.

	if len(bridges.applied) != 1 || bridges.applied[0].KernelInterface != "test-br" {
		t.Fatalf("rollback did not compensate bridges correctly. applied: %v, err: %v", bridges.applied, err)
	}
}

// 11. Cleanup partial failure preserves journal
func TestCoordinator_Gate1_S11_CleanupPartialFailure(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	cj := CleanupJournal{
		Sequence: 1,
		Files:    []string{"file1.txt", "file2.txt"},
	}
	cjBytes, _ := json.Marshal(cj)
	_ = os.WriteFile(coord.draftJournalFile, cjBytes, 0600)
	
	_ = os.WriteFile(filepath.Join(coord.cfg.ConfigDir, "file1.txt"), []byte("data"), 0600)
	_ = os.WriteFile(filepath.Join(coord.cfg.ConfigDir, "file2.txt"), []byte("data"), 0600)

	coord.SetHooks(ApplyCoordinatorHooks{
		FailCleanupUnlink: true,
	})

	err := coord.processCleanupJournalFilesLocked(&cj)
	if err == nil {
		t.Fatalf("expected error from processCleanupJournalFilesLocked")
	}

	checkCjBytes, err := os.ReadFile(coord.draftJournalFile)
	if err != nil {
		t.Fatalf("journal should be preserved, but read failed: %v", err)
	}
	var checkCj CleanupJournal
	_ = json.Unmarshal(checkCjBytes, &checkCj)
	if len(checkCj.Files) != 2 {
		t.Fatalf("journal should have retained 2 files, got: %v", checkCj.Files)
	}

	if _, statErr := os.Stat(coord.recoveryMarkerFile); statErr != nil {
		t.Fatalf("recovery marker should have been written: %v", statErr)
	}
}

// 12. Bridge checkpoint error triggers recovery
func TestCoordinator_Gate1_S12_BridgeCheckpointError(t *testing.T) {
	coord, _, bridges, _ := setupGate1Coordinator(t)

	m := TransactionManifest{
		Version: 1,
		TxID:    "20260915120003",
		State:   StateBridgesReconciling,
		BridgeOperations: []BridgeOperation{
			{
				OperationID: "20260915120003-create-op1",
				Action:      "create",
				State:       BridgeOpIntent,
			},
		},
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	coord.SetHooks(ApplyCoordinatorHooks{
		FailManifestPersistBridgeOpState: BridgeOpApplied,
	})

	refs := []BridgeRef{{KernelInterface: "op1"}}

	err := coord.syncBridgesLocked(context.Background(), &m, nil, refs)
	if err == nil {
		t.Fatalf("expected syncBridgesLocked to fail")
	}
	if !strings.Contains(err.Error(), "checkpoint failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bridges.applied) != 1 || bridges.applied[0].KernelInterface != "op1" {
		t.Fatalf("expected op1 to be applied before checkpoint failure, got: %v", bridges.applied)
	}

	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected state StateRecoveryRequired, got %s", coord.State())
	}
}
"""

with open(file_path, "a", encoding="utf-8") as f:
    f.write(tests)

print("Tests injected.")
