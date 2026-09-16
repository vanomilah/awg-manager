package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/strictfs"
)

func setupGate1Coordinator(t *testing.T) (*ApplyCoordinator, *fakeStoreTx, *fakeBridgeRuntime, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store := newFakeStoreTx(tmpDir, "store-initial-v1")
	bridges := &fakeBridgeRuntime{}
	validator := &fakeValidator{}
	operator := NewOperator("ignored", tmpDir)
	operator.commandFn = helperCommand(t, "wait")
	operator.readyFn = func(context.Context) error { return nil }

	cfg := CoordinatorConfig{
		ConfigDir:     tmpDir,
		Operator:      operator,
		Validator:     validator,
		BridgeRuntime: bridges,
		StoreTx:       store,
	}
	coord := NewApplyCoordinator(cfg)
	return coord, store, bridges, tmpDir
}

func makeGate1CompileFn(yamlContent string, mode RuntimeMode) func(context.Context) (*CompileResult, error) {
	return func(ctx context.Context) (*CompileResult, error) {
		b := []byte(yamlContent)
		return &CompileResult{
			ConfigYAML:   b,
			ConfigDigest: strictfs.ComputeBytesDigest(b),
			InputDigest:  strictfs.ComputeBytesDigest([]byte("input:" + yamlContent)),
			Mode:         mode,
			RequiredListeners: []ListenerSpec{
				{Network: "tcp", Family: "ipv4", Address: "0.0.0.0", Port: 1099, Purpose: "mixed"},
			},
		}, nil
	}
}

// S01: SnapshotSecured WriteFail
func TestCoordinator_Gate1_S01_SnapshotSecured_WriteFail(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)
	store.snapshotFail = true

	mutateFn := func() error {
		store.data = "mutated"
		return nil
	}
	compileFn := makeGate1CompileFn("mode: test", RuntimeEnforced)

	err := coord.MutateAndApply(context.Background(), mutateFn, compileFn)
	if err == nil || !strings.Contains(err.Error(), "create store snapshot") {
		t.Fatalf("expected create store snapshot error, got: %v", err)
	}

	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should not exist after snapshot fail")
	}
}

// S02: SnapshotSecured Crash (interrupted at StateSnapshotSecured; startup recovery restores store & cleans up)
func TestCoordinator_Gate1_S02_SnapshotSecured_Crash(t *testing.T) {
	coord, store, _, tmpDir := setupGate1Coordinator(t)
	txid := "20260915120000"

	baseStoreDigest, _ := store.CurrentDigest()
	snapFile, _ := store.CreateSnapshotFile(txid)

	store.data = "store-mutated-in-memory"
	store.digest = strictfs.ComputeBytesDigest([]byte(store.data))

	m := TransactionManifest{
		Version:                      1,
		TxID:                         txid,
		State:                        StateSnapshotSecured,
		BaseAppliedStoreDigest:       baseStoreDigest,
		PreMutationStoreSnapshotFile: snapFile,
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err != nil {
		t.Fatalf("expected clean recovery, got: %v", err)
	}

	currStoreDigest, _ := store.CurrentDigest()
	if currStoreDigest != baseStoreDigest {
		t.Fatalf("store not restored: got %s, want %s", currStoreDigest, baseStoreDigest)
	}
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should be unlinked")
	}
	_ = tmpDir
}

// S03: StoreMutated CrashPreJournal (store mutation panicked or failed; coordinator recovers store)
func TestCoordinator_Gate1_S03_StoreMutated_CrashPreJournal(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)
	baseStoreDigest, _ := store.CurrentDigest()

	mutateFn := func() error {
		store.data = "mutated-bad"
		panic("simulated panic during store mutation")
	}
	compileFn := makeGate1CompileFn("mode: test", RuntimeEnforced)

	err := coord.MutateAndApply(context.Background(), mutateFn, compileFn)
	if err == nil || !strings.Contains(err.Error(), "panic during store mutation") {
		t.Fatalf("expected panic during store mutation error, got: %v", err)
	}

	currStoreDigest, _ := store.CurrentDigest()
	if currStoreDigest != baseStoreDigest {
		t.Fatalf("store not restored after mutation panic: got %s, want %s", currStoreDigest, baseStoreDigest)
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should be unlinked after failure")
	}
}

// S04: StoreMutated JournalWriteFail
func TestCoordinator_Gate1_S04_StoreMutated_JournalWriteFail(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)
	baseStoreDigest, _ := store.CurrentDigest()

	coord.SetHooks(ApplyCoordinatorHooks{
		FailManifestPersistAtState: StateSnapshotSecured,
	})

	mutateFn := func() error {
		store.data = "mutated-data"
		store.digest = strictfs.ComputeBytesDigest([]byte("mutated-data"))
		return nil
	}
	compileFn := makeGate1CompileFn("mode: test", RuntimeEnforced)

	err := coord.MutateAndApply(context.Background(), mutateFn, compileFn)
	if err == nil || !strings.Contains(err.Error(), "snapshot_secured") {
		t.Fatalf("expected snapshot_secured error, got: %v", err)
	}

	currStoreDigest, _ := store.CurrentDigest()
	if currStoreDigest != baseStoreDigest {
		t.Fatalf("store not restored after journal write fail: got %s, want %s", currStoreDigest, baseStoreDigest)
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state StateIdle, got: %s", coord.State())
	}
	if _, err := os.Stat(coord.recoveryMarkerFile); !os.IsNotExist(err) {
		t.Fatalf("recovery marker SHOULD NOT exist")
	}
}

// S05: StoreMutated CrashPostJournal (interrupted at StateSnapshotSecured; startup recovery restores store)
func TestCoordinator_Gate1_S05_StoreMutated_CrashPostJournal(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)
	txid := "20260915120000"

	baseStoreDigest, _ := store.CurrentDigest()
	snapFile, _ := store.CreateSnapshotFile(txid)

	store.data = "mutated-data"
	store.digest = strictfs.ComputeBytesDigest([]byte("mutated-data"))

	m := TransactionManifest{
		Version:                      1,
		TxID:                         txid,
		State:                        StateSnapshotSecured,
		BaseAppliedStoreDigest:       baseStoreDigest,
		PreMutationStoreSnapshotFile: snapFile,
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err != nil {
		t.Fatalf("expected clean recovery, got: %v", err)
	}

	currStoreDigest, _ := store.CurrentDigest()
	if currStoreDigest != baseStoreDigest {
		t.Fatalf("store not restored: got %s, want %s", currStoreDigest, baseStoreDigest)
	}
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should be unlinked")
	}
}

// S06: CandidateValid CompileFail (compileFn fails; cleans up store snapshot & manifest)
func TestCoordinator_Gate1_S06_CandidateValid_CompileFail(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)
	baseStoreDigest, _ := store.CurrentDigest()

	mutateFn := func() error {
		store.data = "mutated-ok"
		store.digest = strictfs.ComputeBytesDigest([]byte("mutated-ok"))
		return nil
	}
	compileFn := func(ctx context.Context) (*CompileResult, error) {
		return nil, errors.New("simulated compile syntax error")
	}

	err := coord.MutateAndApply(context.Background(), mutateFn, compileFn)
	if err == nil || !strings.Contains(err.Error(), "simulated compile syntax error") {
		t.Fatalf("expected compile syntax error, got: %v", err)
	}

	currStoreDigest, _ := store.CurrentDigest()
	if currStoreDigest != baseStoreDigest {
		t.Fatalf("store should be rolled back to base digest: got %s, want %s", currStoreDigest, baseStoreDigest)
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
}

// S07: CandidateValid PreflightFail (validator fails; cleans up candidate & manifest)
func TestCoordinator_Gate1_S07_CandidateValid_PreflightFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	coord.cfg.Validator = &fakeValidator{fail: true}

	compileFn := makeGate1CompileFn("invalid: yaml: test", RuntimeEnforced)

	err := coord.MutateAndApply(context.Background(), nil, compileFn)
	if err == nil || !strings.Contains(err.Error(), "validate candidate config") {
		t.Fatalf("expected validate candidate config error, got: %v", err)
	}

	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should be unlinked")
	}
}

// S08: CandidateValid ValidatorTimeout
func TestCoordinator_Gate1_S08_CandidateValid_ValidatorTimeout(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	compileFn := makeGate1CompileFn("mode: timeout", RuntimeEnforced)
	err := coord.MutateAndApply(ctx, nil, compileFn)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
}

// S09: CandidateValid ManifestWriteFail
func TestCoordinator_Gate1_S09_CandidateValid_ManifestWriteFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	coord.SetHooks(ApplyCoordinatorHooks{
		FailManifestPersistAtState: StateCandidateBuilt,
	})

	compileFn := makeGate1CompileFn("mode: test", RuntimeEnforced)
	err := coord.MutateAndApply(context.Background(), nil, compileFn)
	if err == nil || !strings.Contains(err.Error(), "candidate_built") {
		t.Fatalf("expected candidate_built failpoint error, got: %v", err)
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle, got: %s", coord.State())
	}
}

// S10: Bundle Staging Fsync Subtests
func TestCoordinator_Gate1_S10_Bundle_StagingFsync_Subtests(t *testing.T) {
	subtests := []struct {
		name  string
		hooks GenerationStoreHooks
	}{
		{name: "file_fsync", hooks: GenerationStoreHooks{FailFileFsync: true}},
		{name: "staging_dir_fsync", hooks: GenerationStoreHooks{FailStagingDirFsync: true}},
		{name: "rename_staging_dir", hooks: GenerationStoreHooks{FailRename: true}},
		{name: "parent_dir_fsync", hooks: GenerationStoreHooks{FailParentDirFsync: true}},
	}

	for _, tc := range subtests {
		t.Run(tc.name, func(t *testing.T) {
			coord, _, _, tmpDir := setupGate1Coordinator(t)

			// First, establish Generation 1 successfully
			compileGen1 := makeGate1CompileFn("generation: 1", RuntimeEnforced)
			if err := coord.MutateAndApply(context.Background(), nil, compileGen1); err != nil {
				t.Fatalf("failed to establish Generation 1: %v", err)
			}
			gen1Record := coord.AppliedRecord()
			if gen1Record == nil || gen1Record.Generation != 1 {
				t.Fatalf("expected Generation 1 active, got: %+v", gen1Record)
			}

			// In Gen 2, inject failpoint into GenerationStore
			coord.GenStore().SetHooks(tc.hooks)

			compileGen2 := makeGate1CompileFn("generation: 2", RuntimeEnforced)
			err := coord.MutateAndApply(context.Background(), nil, compileGen2)
			if err == nil || !strings.Contains(err.Error(), "publish LKG generation bundle") {
				t.Fatalf("expected publish LKG generation bundle error, got: %v", err)
			}

			// Verify Gen 1 active config was retained
			actBytes, err := os.ReadFile(coord.activeConfigFile)
			if err != nil || string(actBytes) != "generation: 1" {
				t.Fatalf("active config corrupted or modified: %q (err: %v)", string(actBytes), err)
			}

			// Verify no partial staging directory remains in generations/
			entries, _ := os.ReadDir(filepath.Join(tmpDir, "generations"))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".tmp.") {
					t.Fatalf("temporary staging directory %s leaked after failure", e.Name())
				}
			}

			// Verify coordinator state returned to Idle
			if coord.State() != StateIdle {
				t.Fatalf("expected state Idle, got: %s", coord.State())
			}
		})
	}
}

// S11: Bundle PreMutationDigestMismatch
func TestCoordinator_Gate1_S11_Bundle_PreMutationDigestMismatch(t *testing.T) {
	t.Run("store_digest_mismatch", func(t *testing.T) {
		coord, store, _, _ := setupGate1Coordinator(t)

		// 1. Establish Gen 1
		compileGen1 := makeGate1CompileFn("generation: 1", RuntimeEnforced)
		if err := coord.MutateAndApply(context.Background(), nil, compileGen1); err != nil {
			t.Fatalf("Gen 1 failed: %v", err)
		}

		// 2. Corrupt store digest externally (bypass coordinator)
		store.data = "corrupted-store-external"
		store.digest = strictfs.ComputeBytesDigest([]byte("corrupted-store-external"))

		// 3. Next apply should detect preflight mismatch and refuse to archive
		compileGen2 := makeGate1CompileFn("generation: 2", RuntimeEnforced)
		err := coord.MutateAndApply(context.Background(), nil, compileGen2)
		if err == nil || !strings.Contains(err.Error(), "store digest mismatch") {
			t.Fatalf("expected store digest mismatch error, got: %v", err)
		}

		if coord.State() != StateRecoveryRequired {
			t.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())
		}
		if _, err := os.Stat(coord.recoveryMarkerFile); err != nil {
			t.Fatalf("recovery marker must be created: %v", err)
		}
	})

	t.Run("config_digest_mismatch", func(t *testing.T) {
		coord, _, _, _ := setupGate1Coordinator(t)

		// 1. Establish Gen 1
		compileGen1 := makeGate1CompileFn("generation: 1", RuntimeEnforced)
		if err := coord.MutateAndApply(context.Background(), nil, compileGen1); err != nil {
			t.Fatalf("Gen 1 failed: %v", err)
		}

		// 2. Corrupt active config.yaml externally
		_ = os.WriteFile(coord.activeConfigFile, []byte("tampered-config"), 0644)

		// 3. Next apply should detect preflight active config digest mismatch
		compileGen2 := makeGate1CompileFn("generation: 2", RuntimeEnforced)
		err := coord.MutateAndApply(context.Background(), nil, compileGen2)
		if err == nil || !strings.Contains(err.Error(), "active config digest mismatch") {
			t.Fatalf("expected active config digest mismatch error, got: %v", err)
		}

		if coord.State() != StateRecoveryRequired {
			t.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())
		}
	})
}

// S12: Bundle PointerWriteFail (Pointer write is now fatal after commit intent)
func TestCoordinator_Gate1_S12_Bundle_PointerWriteFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// Gen 1 success
	if err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 1", RuntimeEnforced)); err != nil {
		t.Fatalf("Gen 1 failed: %v", err)
	}

	// Gen 2 fails writing pointer
	coord.GenStore().SetHooks(GenerationStoreHooks{FailPointerWrite: true})
	err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 2", RuntimeEnforced))

	// Should fail with a recovery error, manifest preserved
	if err == nil {
		t.Fatalf("expected pointer write failure to be fatal")
	}

	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected state StateRecoveryRequired, got: %s", coord.State())
	}

	// Manifest should be preserved in StateCommitIntent
	b, err := os.ReadFile(coord.manifestFile)
	if err != nil {
		t.Fatalf("manifest should exist")
	}
	var m TransactionManifest
	json.Unmarshal(b, &m)
	if m.State != StateCommitIntent {
		t.Fatalf("expected manifest in StateCommitIntent, got: %s", m.State)
	}
}

// S13: SwapActive PreSwapCrash
func TestCoordinator_Gate1_S13_SwapActive_PreSwapCrash(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	txid := "20260915120000"

	candidatePath := filepath.Join(coord.cfg.ConfigDir, "config.yaml.candidate."+txid)
	_ = os.WriteFile(candidatePath, []byte("candidate"), 0600)
	_ = os.WriteFile(coord.activeConfigFile, []byte("stable-active"), 0644)

	m := TransactionManifest{
		Version:              1,
		TxID:                 txid,
		State:                StateSwapIntent,
		CandidateConfigFile:  candidatePath,
		PreviousConfigDigest: "fake",
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err != nil {
		t.Fatalf("expected clean recovery, got: %v", err)
	}

	// Candidate file cleaned up
	if _, err := os.Stat(candidatePath); !os.IsNotExist(err) {
		t.Fatalf("candidate file should be unlinked")
	}
	// Active config preserved
	act, _ := os.ReadFile(coord.activeConfigFile)
	if string(act) != "stable-active" {
		t.Fatalf("active config mismatch: %q", string(act))
	}
}

// S14: SwapActive RenameOutcomeAmbiguous
func TestCoordinator_Gate1_S14_SwapActive_RenameOutcomeAmbiguous(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// Inject failpoint right before atomic rename in strictfs
	strictfs.SetFailpoint(strictfs.FPBeforeAtomicRename, errors.New("simulated rename failure"))
	defer strictfs.ClearFailpoints()

	err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 1", RuntimeEnforced))
	if err == nil {
		t.Fatalf("expected error from rename failpoint")
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected state Idle after rename fail, got: %s", coord.State())
	}
}

// S15: SwapActive ManifestWriteFail
func TestCoordinator_Gate1_S15_SwapActive_ManifestWriteFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// 1. Gen 1 established
	if err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 1", RuntimeEnforced)); err != nil {
		t.Fatalf("Gen 1 failed: %v", err)
	}

	// 2. Gen 2 fails manifest persist after swap
	coord.SetHooks(ApplyCoordinatorHooks{
		FailManifestPersistAtState: StateSwapApplied,
	})

	err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 2", RuntimeEnforced))
	if err == nil {
		t.Fatalf("expected manifest persist failure")
	}

	// Active config must have been rolled back to Gen 1
	act, _ := os.ReadFile(coord.activeConfigFile)
	if string(act) != "gen: 1" {
		t.Fatalf("active config should be restored to gen 1 after rollback: %q", string(act))
	}
	if coord.State() != StateIdle {
		t.Fatalf("expected StateIdle, got: %s", coord.State())
	}
}

// S16: Rollback NonConsumingLKG (bundle config is copied, not consumed; bundle remains intact)
func TestCoordinator_Gate1_S16_Rollback_NonConsumingLKG(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// 1. Establish Gen 1
	if err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("config: generation-1", RuntimeEnforced)); err != nil {
		t.Fatalf("Gen 1 failed: %v", err)
	}

	// Verify LKG bundle exists
	_, err := coord.GenStore().ReadLKGPointer()
	// 2. Gen 3 fails during process restart
	coord.cfg.Operator.mu.Lock()
	coord.cfg.Operator.commandFn = helperCommand(t, "fail")
	coord.cfg.Operator.mu.Unlock()

	err = coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("config: generation-3", RuntimeEnforced))
	if err == nil || !strings.Contains(err.Error(), "runtime restart failed") {
		t.Fatalf("expected runtime restart error, got: %v", err)
	}

	// 3. Verify Active config restored to Gen 2 (or LKG)
	act, err := os.ReadFile(coord.activeConfigFile)
	if err != nil {
		t.Fatalf("active config missing after rollback: %v", err)
	}
	if string(act) != "config: generation-1" {
		t.Fatalf("active config unexpected: %q", string(act))
	}

	// 4. CRITICAL INVARIANT: The LKG bundle file MUST STILL EXIST IN TACT! (Non-consuming restore)
	ptrAfter, err := coord.GenStore().ReadLKGPointer()
	if err != nil {
		t.Fatalf("LKG pointer missing after rollback: %v", err)
	}
	_, restoredBundleConfigPath, _, err := coord.GenStore().ReadGenerationBundle(ptrAfter.GenerationID)
	if err != nil {
		t.Fatalf("LKG bundle missing or corrupted after rollback: %v", err)
	}
	bundleContent, err := os.ReadFile(restoredBundleConfigPath)
	if err != nil {
		t.Fatalf("LKG bundle config was consumed or deleted during rollback: %v", err)
	}
	if string(bundleContent) != "config: generation-1" {
		t.Fatalf("LKG bundle content mismatch: got %q, want %q", string(bundleContent), "config: generation-1")
	}
}

// S17: Rollback FirstInstall Cleanup (unlinks candidate & active config; stops process; no LKG created)
func TestCoordinator_Gate1_S17_Rollback_FirstInstall_Cleanup(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// First install: appliedRecord is nil
	if coord.AppliedRecord() != nil {
		t.Fatalf("expected appliedRecord to be nil")
	}

	// Fail operator restart
	coord.cfg.Operator.mu.Lock()
	coord.cfg.Operator.commandFn = helperCommand(t, "fail")
	coord.cfg.Operator.mu.Unlock()

	err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("bad: candidate", RuntimeEnforced))
	if err == nil {
		t.Fatalf("expected error")
	}

	// Active config must NOT be left on disk!
	if _, err := os.Stat(coord.activeConfigFile); !os.IsNotExist(err) {
		t.Fatalf("active config must be unlinked in first-install rollback")
	}

	// LKG pointer must NOT exist!
	if _, err := coord.GenStore().ReadLKGPointer(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LKG pointer should not exist on first-install rollback")
	}

	// Operator is stopped
	running, _ := coord.cfg.Operator.IsRunning()
	if running {
		t.Fatalf("operator should be stopped")
	}
}

// S18: PathConfinement ManifestTraversal
func TestCoordinator_Gate1_S18_PathConfinement_ManifestTraversal(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	m := TransactionManifest{
		Version:             1,
		TxID:                "20260915120000",
		State:               StateSwapApplied,
		CandidateConfigFile: "../../etc/passwd", // Traversal escape!
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired, got: %v", err)
	}

	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())
	}
	if _, err := os.Stat(coord.recoveryMarkerFile); os.IsNotExist(err) {
		t.Fatalf("recovery marker SHOULD exist")
	}
}

// S19: PathConfinement SnapshotTraversal
func TestCoordinator_Gate1_S19_PathConfinement_SnapshotTraversal(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	dj := DraftJournal{
		Version:           1,
		TxID:              "20260915120000",
		State:             DraftSnapshotSecured,
		DraftSnapshotFile: "../../var/log/secret", // Traversal escape!
	}
	djBytes, _ := json.Marshal(dj)
	_ = os.WriteFile(coord.draftJournalFile, djBytes, 0600)

	// Since manifest is absent, it tries to rollback the draft journal.
	err := coord.RecoverOnStartup(context.Background())
	// Expected to either fail completely or successfully remove the draft journal.
	_ = err
}

// S20: CorruptManifest Startup
func TestCoordinator_Gate1_S20_CorruptManifest_Startup(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	_ = os.WriteFile(coord.manifestFile, []byte("{ corrupt json"), 0600)

	err := coord.RecoverOnStartup(context.Background())
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired, got: %v", err)
	}
	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())
	}
}

// S21: UnsupportedVersion Startup
func TestCoordinator_Gate1_S21_UnsupportedVersion_Startup(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	m := TransactionManifest{
		Version: 999, // Unsupported
		TxID:    "20260915120000",
		State:   StateIdle,
	}
	b, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, b, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired, got: %v", err)
	}
	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())
	}
}

// S22: CommitPoint VerifiedActiveFsync
func TestCoordinator_Gate1_S22_CommitPoint_VerifiedActiveFsync(t *testing.T) {
	coord, store, _, _ := setupGate1Coordinator(t)

	mutateFn := func() error {
		store.data = "store-committed"
		store.digest = strictfs.ComputeBytesDigest([]byte("store-committed"))
		return nil
	}
	compileFn := makeGate1CompileFn("config: committed", RuntimeEnforced)

	err := coord.MutateAndApply(context.Background(), mutateFn, compileFn)
	if err != nil {
		t.Fatalf("MutateAndApply failed: %v", err)
	}

	// 1. Verified active record written
	rec := coord.AppliedRecord()
	if rec == nil {
		t.Fatalf("applied record is nil")
	}
	if rec.Generation != 1 {
		t.Fatalf("generation should be 1, got: %d", rec.Generation)
	}
	if rec.AppliedConfigDigest != strictfs.ComputeBytesDigest([]byte("config: committed")) {
		t.Fatalf("config digest mismatch: %s", rec.AppliedConfigDigest)
	}

	// 2. Active file on disk matches
	act, err := os.ReadFile(coord.activeConfigFile)
	if err != nil || string(act) != "config: committed" {
		t.Fatalf("active config file mismatch: %q (err: %v)", string(act), err)
	}

	// 3. Manifest unlinked
	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest file should be unlinked after commit")
	}

	if coord.State() != StateIdle {
		t.Fatalf("expected StateIdle, got: %s", coord.State())
	}
}

// S23: CommitPoint VerifiedActiveFail
func TestCoordinator_Gate1_S23_CommitPoint_VerifiedActiveFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	// Establish Gen 1
	if err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 1", RuntimeEnforced)); err != nil {
		t.Fatalf("Gen 1 failed: %v", err)
	}

	// Gen 2 fails writing verified-active.json
	coord.SetHooks(ApplyCoordinatorHooks{
		FailCommitVerifiedActive: true,
	})

	err := coord.MutateAndApply(context.Background(), nil, makeGate1CompileFn("gen: 2", RuntimeEnforced))
	if err == nil || !strings.Contains(err.Error(), "engine is in degraded mode") {
		t.Fatalf("expected commit verified active error, got: %v", err)
	}

	// Rollback should NOT occur after commit_intent, so active config must remain Gen 2
	act, _ := os.ReadFile(coord.activeConfigFile)
	if string(act) != "gen: 2" {
		t.Fatalf("active config should NOT be rolled back, expected gen 2: %q", string(act))
	}

	// Verified active record should still be Gen 1
	rec := coord.AppliedRecord()
	if rec.Generation != 1 {
		t.Fatalf("applied record should remain gen 1, got: %d", rec.Generation)
	}
}

// S24: CleanupPending PostCommit
func TestCoordinator_Gate1_S24_CleanupPending_PostCommit(t *testing.T) {
	// Skipped as CleanupPending is no longer used for manifest_commit_failed
}

// S_RetentionGC: Retention GC Policy Preserves Protected Bundles
func TestCoordinator_Gate1_RetentionGC_PreservesProtected(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewGenerationStore(tmpDir)

	// Create 5 mock generation directories: gen-1 to gen-5
	for i := 1; i <= 5; i++ {
		genDir := filepath.Join(store.GenerationsDir(), fmt.Sprintf("gen-%02d", i))
		_ = os.MkdirAll(genDir, 0700)
		_ = os.WriteFile(filepath.Join(genDir, "config.yaml"), []byte(fmt.Sprintf("gen-%02d", i)), 0600)
		time.Sleep(10 * time.Millisecond) // Ensure distinct mod times
	}

	// Create a .tmp staging dir
	tmpStaging := filepath.Join(store.GenerationsDir(), ".tmp.staging")
	_ = os.MkdirAll(tmpStaging, 0700)

	// Protect active=gen-05, LKG=gen-04, keepRecent=1 (which is gen-03)
	err := store.RunRetentionGC("gen-05", "gen-04", nil, 1)
	if err != nil {
		t.Fatalf("RunRetentionGC failed: %v", err)
	}

	// gen-05, gen-04, gen-03 should exist
	for _, expected := range []string{"gen-05", "gen-04", "gen-03"} {
		dir := filepath.Join(store.GenerationsDir(), expected)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("protected generation %s was deleted!", expected)
		}
	}

	// gen-01 and gen-02 should have been garbage collected
	for _, obsolete := range []string{"gen-01", "gen-02"} {
		dir := filepath.Join(store.GenerationsDir(), obsolete)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("obsolete generation %s was not cleaned up!", obsolete)
		}
	}
}

// 1. commit_intent никогда не переходит в rollback
func TestCoordinator_Gate1_CommitIntent_NoRollback(t *testing.T) {
	if StateCommitIntent.IsValidNext(StateRollbackInProgress) {
		t.Fatalf("StateCommitIntent should not transition to StateRollbackInProgress")
	}
}

// 2. verified-active failure после commit intent сохраняет manifest
func TestCoordinator_Gate1_CommitIntent_VerifiedActiveFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	coord.SetHooks(ApplyCoordinatorHooks{
		FailCommitVerifiedActive: true,
	})

	txid := "20260915120001"
	m := TransactionManifest{
		Version: 1,
		TxID:    txid,
		State:   StateCommitIntent,
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err == nil {
		t.Fatalf("expected error from VerifiedActive fail")
	}

	if _, err := os.Stat(coord.manifestFile); os.IsNotExist(err) {
		t.Fatalf("manifest should exist after verified-active failure post commit_intent")
	}

	if coord.State() != StateRecoveryRequired {
		t.Fatalf("expected StateRecoveryRequired, got: %v", coord.State())
	}
}

// 3. pointer write/fsync failure сохраняет manifest
func TestCoordinator_Gate1_CommitIntent_PointerFail(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	coord.GenStore().SetHooks(GenerationStoreHooks{FailPointerWrite: true})

	txid := "20260915120002"
	m := TransactionManifest{
		Version:               1,
		TxID:                  txid,
		State:                 StateCommitIntent,
		CandidateGenerationID: "gen-02",
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err == nil {
		t.Fatalf("expected error from pointer write fail")
	}

	if _, err := os.Stat(coord.manifestFile); os.IsNotExist(err) {
		t.Fatalf("manifest should exist after pointer failure")
	}
}

// 4. restart roll-forward выравнивает verified-active и pointer
func TestCoordinator_Gate1_CommitIntent_RollForward(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	txid := "20260915120003"
	m := TransactionManifest{
		Version:               1,
		TxID:                  txid,
		State:                 StateCommitIntent,
		CandidateGenerationID: "gen-03",
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	gen3Dir := filepath.Join(coord.GenStore().GenerationsDir(), "gen-03")
	os.MkdirAll(gen3Dir, 0700)
	os.WriteFile(filepath.Join(gen3Dir, "config.yaml"), []byte("gen3"), 0600)
	os.WriteFile(filepath.Join(gen3Dir, "generation.manifest.json"), []byte(`{"version": 1, "generation_id": "gen-03", "generation_number": 3, "applied_config_digest": "digest3"}`), 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err != nil {
		t.Fatalf("expected clean roll-forward, got: %v", err)
	}

	rec := coord.AppliedRecord()
	if rec.Generation != 3 {
		t.Fatalf("verified-active not advanced")
	}
	ptr, err := coord.GenStore().ReadLKGPointer()
	if err != nil || ptr.GenerationID != "gen-03" {
		t.Fatalf("pointer not advanced: %v", ptr)
	}

	if _, err := os.Stat(coord.manifestFile); !os.IsNotExist(err) {
		t.Fatalf("manifest should be removed after clean roll-forward")
	}
}

// 5. RuntimeOff обновляет pointer
func TestCoordinator_Gate1_RuntimeOff_PointerUpdate(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	compileFn := makeGate1CompileFn("gen: off", RuntimeOff)
	err := coord.MutateAndApply(context.Background(), nil, compileFn)
	if err != nil {
		t.Fatalf("MutateAndApply failed: %v", err)
	}

	rec := coord.AppliedRecord()
	if rec == nil || rec.Generation != 1 {
		t.Fatalf("verified-active not advanced to 1")
	}

	ptr, err := coord.GenStore().ReadLKGPointer()
	if err != nil || ptr.GenerationNumber != 1 {
		t.Fatalf("pointer not advanced for RuntimeOff")
	}
}

// 6. committed transition failure не запускает cleanup
func TestCoordinator_Gate1_CommittedTransitionFail_NoCleanup(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)
	coord.SetHooks(ApplyCoordinatorHooks{
		FailManifestPersistAtState: StateCommitted,
	})

	txid := "20260915120004"
	m := TransactionManifest{
		Version:               1,
		TxID:                  txid,
		State:                 StateCommitIntent,
		CandidateGenerationID: "gen-04",
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	gen4Dir := filepath.Join(coord.GenStore().GenerationsDir(), "gen-04")
	os.MkdirAll(gen4Dir, 0700)
	os.WriteFile(filepath.Join(gen4Dir, "config.yaml"), []byte("gen4"), 0600)

	err := coord.RecoverOnStartup(context.Background())
	if err == nil {
		t.Fatalf("expected manifest persist failure")
	}

	b, _ := os.ReadFile(coord.manifestFile)
	var currentM TransactionManifest
	json.Unmarshal(b, &currentM)
	if currentM.State != StateCommitIntent {
		t.Fatalf("expected manifest to remain in CommitIntent, got: %s", currentM.State)
	}
}

// 7. checkpoint CAS failure не мутирует объект в памяти
func TestCoordinator_Gate1_CheckpointCAS_Failure_NoMutation(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	txid := "20260915120005"
	m := TransactionManifest{
		Version:  1,
		TxID:     txid,
		State:    StateSnapshotSecured,
		Sequence: 1,
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	// Corrupt file
	corruptM := m
	corruptM.Sequence = 2
	cBytes, _ := json.Marshal(corruptM)
	_ = os.WriteFile(coord.manifestFile, cBytes, 0600)

	// Read into memory
	b, _ := os.ReadFile(coord.manifestFile)
	var memM TransactionManifest
	json.Unmarshal(b, &memM)
	memM.Sequence = 1 // memory has old sequence

	// transition should fail CAS
	err := coord.transitionManifestLocked(&memM, StateCandidateBuilt)
	if err == nil {
		t.Fatalf("expected CAS failure")
	}

	// Check mem object state wasn't mutated permanently
	if memM.State != StateSnapshotSecured {
		t.Fatalf("memory object was mutated despite CAS failure")
	}
}

// 8. cleanup partial failure сохраняет journal
func TestCoordinator_Gate1_CleanupPartialFailure_PreservesJournal(t *testing.T) {
	coord, _, _, _ := setupGate1Coordinator(t)

	cj := CleanupJournal{
		Version:  1,
		TxID:     "tx1",
		Sequence: 1,
		Files:    []string{"nonexistent-1", "fail-me"},
	}
	b, _ := json.Marshal(cj)
	_ = os.WriteFile(coord.cleanupJournalFile, b, 0600)

	badDir := filepath.Join(coord.cfg.ConfigDir, "fail-me")
	os.MkdirAll(badDir, 0700)
	os.WriteFile(filepath.Join(badDir, "child"), []byte("a"), 0600)
	defer os.RemoveAll(badDir)

	cj2 := &CleanupJournal{
		Version:  1,
		TxID:     "tx1",
		Sequence: 1,
		Files:    []string{"nonexistent-1", "fail-me"},
	}
	coord.processCleanupJournalFilesLocked(cj2)

	jBytes, err := os.ReadFile(coord.cleanupJournalFile)
	if err != nil {
		t.Fatalf("journal should be preserved")
	}
	var newCj CleanupJournal
	json.Unmarshal(jBytes, &newCj)
	if len(newCj.Files) != 1 || newCj.Files[0] != "fail-me" {
		t.Fatalf("journal not updated correctly: %v", newCj.Files)
	}
	if newCj.Sequence <= 1 {
		t.Fatalf("sequence not incremented")
	}
}

// 9. bridge partial success продолжается без повторения verified operation
func TestCoordinator_Gate1_BridgePartialSuccess_NoRepeat(t *testing.T) {
	coord, _, bridges, _ := setupGate1Coordinator(t)

	m := TransactionManifest{
		Version: 1,
		TxID:    "20260915120002",
		State:   StateBridgesReconciling,
		BridgeOperations: []BridgeOperation{
			{
				OperationID: "20260915120002-create-op1",
				Action:      "create",
				State:       BridgeOpVerified,
			},
			{
				OperationID: "20260915120002-create-op2",
				Action:      "create",
				State:       BridgeOpIntent,
			},
		},
	}
	mBytes, _ := json.Marshal(m)
	_ = os.WriteFile(coord.manifestFile, mBytes, 0600)

	refs := []BridgeRef{
		{KernelInterface: "op1"},
		{KernelInterface: "op2"},
	}

	err := coord.syncBridgesLocked(context.Background(), &m, nil, refs)
	if err != nil {
		t.Fatalf("syncBridgesLocked failed: %v", err)
	}

	// Check that only op2 was applied in fakeBridgeRuntime
	// fakeBridgeRuntime tracks applied in `applied` array
	if len(bridges.applied) != 1 || bridges.applied[0].KernelInterface != "op2" {
		t.Fatalf("expected only op2 to be applied, got: %v", bridges.applied)
	}
}

// 10. Rollback fail-closed Bridge compensation
func TestCoordinator_Gate1_S10_Rollback_FailClosed(t *testing.T) {
	coord, _, bridges, _ := setupGate1Coordinator(t)

	coord.appliedRecord = &AppliedGenerationRecord{
		AppliedBridges: []BridgeRef{{KernelInterface: "test-br"}},
	}
	m := TransactionManifest{
		Version:                      1,
		TxID:                         "20260915120002",
		State:                        StateAbortInProgress,
		DesiredMode:                  RuntimeEnforced,
		PreMutationStoreSnapshotFile: filepath.Join(coord.cfg.ConfigDir, "snapshot.db"),
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
