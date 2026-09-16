import re

with open('internal/mihomo/gate1_legacy_test.go', 'r', encoding='utf-8') as f:
    text = f.read()

tests_to_add = '''
// S10: Rollback Fail-Closed
func TestCoordinator_Gate1_S10_Rollback_FailClosed(t *testing.T) {
\tcoord, store, _, tmpDir := setupGate1Coordinator(t)
\tstore.restoreFail = true // Force rollback to fail

\tcompileFn := makeGate1CompileFn("mode: bad", RuntimeEnforced)
\tcoord.cfg.Operator.readyFn = func(context.Context) error { return errors.New("operator start failed") }

\terr := coord.MutateAndApply(context.Background(), func() error { return nil }, compileFn)
\tif err == nil || !strings.Contains(err.Error(), "rollback critical failure") {
\t\tt.Fatalf("expected rollback critical failure, got: %v", err)
\t}

\t// Must NOT be idle or rolled_back
\tif coord.State() != StateRecoveryRequired {
\t\tt.Fatalf("expected RecoveryRequired, got: %s", coord.State())
\t}

\t// Recovery marker must exist
\tif _, err := os.Stat(filepath.Join(tmpDir, "recovery_required.marker")); err != nil {
\t\tt.Fatalf("recovery marker not found: %v", err)
\t}
}

// S11: Cleanup Partial Failure
func TestCoordinator_Gate1_S11_CleanupPartialFailure(t *testing.T) {
\tcoord, _, _, tmpDir := setupGate1Coordinator(t)
\t
\t// Force cleanup to fail by making a dummy journal file path point to an un-removable thing,
\t// or actually we can just mock it in GenerationStore if we used it, but processCleanupJournalFilesLocked uses os.RemoveAll directly.
\t// On Windows/WSL, we can create a directory and remove its write permissions.
\t// But actually we can just create a file, and change processCleanupJournalFilesLocked to fail if it's read-only? No, os.RemoveAll deletes read-only files.
\t// Let's just mock genStore.RemoveTxSnapshot instead to fail!
\t// Oh wait, setupGate1Coordinator creates fakeStoreTx, but genStore is initialized using cfg.StoreTx ? No, genStore uses strictfs.
\t// Actually, the easiest way to test CleanupPartialFailure is to lock the file or dir, but in Go tests it's tricky.
\t// Wait, does fakeStoreTx implement RemoveTxSnapshot? No, it's a NativeStoreTx mock, it only has CreateSnapshotFile, RemoveStoreSnapshot, etc.
\t// Actually, GenerationStore takes a NativeStoreTx. RemoveTxSnapshot calls NativeStoreTx.RemoveStoreSnapshot.
\t// Let's check NativeStoreTx in types.go or generation_store.go.
}
'''
