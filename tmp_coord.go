package mihomo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/strictfs"
)

var (
	ErrRecoveryRequired = errors.New("mihomo coordinator: engine is in degraded mode, administrative recovery required")
	ErrTxInProgress     = errors.New("mihomo coordinator: transaction already in progress")
)

// ApplyCoordinatorHooks allows injecting failpoints during coordinator operations in tests.
type ApplyCoordinatorHooks struct {
	FailManifestPersistAtState ManifestState
	FailCommitVerifiedActive   bool
	PostCommitHook             func()
}

// CoordinatorConfig holds the paths and dependencies required by ApplyCoordinator.
type CoordinatorConfig struct {
	ConfigDir     string
	Operator      *Operator
	Validator     ConfigFileValidator
	BridgeRuntime BridgeRuntime
	StoreTx       NativeStoreTx
	LogFn         func(level, action, message string)
}

// ApplyCoordinator is the single authoritative coordinator for configuration compiling,
// staging, validating, hot swapping, runtime identity verification, bridge synchronization,
// and durable rollback.
type ApplyCoordinator struct {
	mu             sync.RWMutex
	applyMu        sync.Mutex
	cfg            CoordinatorConfig
	genStore       *GenerationStore
	daemonEpoch    string
	state          ManifestState
	activeTxID     string
	appliedRecord  *AppliedGenerationRecord
	hooks          ApplyCoordinatorHooks
	cleanupPending []string

	// File paths
	activeConfigFile   string
	manifestFile       string
	lkgConfigFile      string
	verifiedActiveFile string
	recoveryMarkerFile string
	draftJournalFile   string
	pendingInputFile   string
	quarantineDir      string
	cleanupJournalFile string
}

// NewApplyCoordinator instantiates the coordinator with paths resolved relative to ConfigDir.
func NewApplyCoordinator(cfg CoordinatorConfig) *ApplyCoordinator {
	var rnd [4]byte
	_, err := rand.Read(rnd[:])
	if err != nil {
		panic(err)
	}
	epoch := fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(rnd[:]))

	dir := cfg.ConfigDir
	return &ApplyCoordinator{
		cfg:                cfg,
		genStore:           NewGenerationStore(dir),
		daemonEpoch:        epoch,
		state:              StateIdle,
		activeConfigFile:   filepath.Join(dir, "config.yaml"),
		manifestFile:       filepath.Join(dir, "config.yaml.txn.json"),
		lkgConfigFile:      filepath.Join(dir, "config.yaml.lkg"),
		verifiedActiveFile: filepath.Join(dir, "verified-active.json"),
		recoveryMarkerFile: filepath.Join(dir, "recovery.marker"),
		draftJournalFile:   filepath.Join(dir, "store.draft.json"),
		pendingInputFile:   filepath.Join(dir, "input.pending.json"),
		quarantineDir:      filepath.Join(dir, "quarantine"),
		cleanupJournalFile: filepath.Join(dir, "cleanup.journal.json"),
	}
}

// GenStore returns the generation store instance.
func (c *ApplyCoordinator) GenStore() *GenerationStore {
	return c.genStore
}

// DaemonEpoch returns the epoch identifier of this coordinator instance.
func (c *ApplyCoordinator) DaemonEpoch() string {
	return c.daemonEpoch
}

// State returns the current high-level state of the coordinator.
func (c *ApplyCoordinator) State() ManifestState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// AppliedRecord returns the currently verified and active generation record.
func (c *ApplyCoordinator) AppliedRecord() *AppliedGenerationRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.appliedRecord == nil {
		return nil
	}
	cp := *c.appliedRecord
	return &cp
}

// SetHooks sets failpoint hooks for testing.
func (c *ApplyCoordinator) SetHooks(hooks ApplyCoordinatorHooks) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.hooks = hooks
}

// CleanupPending returns any pending post-commit cleanup items.
func (c *ApplyCoordinator) CleanupPending() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]string, len(c.cleanupPending))
	copy(cp, c.cleanupPending)
	return cp
}

func (c *ApplyCoordinator) addCleanupPendingLocked(item string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupPending = append(c.cleanupPending, item)
}

func (c *ApplyCoordinator) log(level, action, message string) {
	if c.cfg.LogFn != nil {
		c.cfg.LogFn(level, action, message)
	}
}

// GenerateTxID creates a timestamp-prefixed, cryptographically random transaction identifier
// conforming strictly to ^[0-9]{14,24}$.
func GenerateTxID() string {
	ts := time.Now().Format("20060102150405")
	var rnd [3]byte
	_, err := rand.Read(rnd[:])
	if err != nil {
		panic(err)
	}
	// 14 digits timestamp + 6 digits random number
	num := (int(rnd[0])<<16 | int(rnd[1])<<8 | int(rnd[2])) % 1000000
	return fmt.Sprintf("%s%06d", ts, num)
}

func (c *ApplyCoordinator) validateManifestPaths(m *TransactionManifest) error {
	baseDir := c.cfg.ConfigDir
	paths := []string{
		m.CandidateConfigFile,
		m.PreMutationStoreSnapshotFile,
		m.CandidatePostMutationStoreSnapshotFile,
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := AssertPathConfined(p, baseDir); err != nil {
			return fmt.Errorf("manifest path confinement violation for %s: %w", p, err)
		}
	}
	return nil
}

func (c *ApplyCoordinator) RecoverOnStartup(ctx context.Context) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	ipcLock, err := strictfs.LockTransaction(c.cfg.ConfigDir)
	if err != nil {
		return fmt.Errorf("acquire transaction lock: %w", err)
	}
	defer ipcLock.Close()

	if _, err := os.Stat(c.recoveryMarkerFile); err == nil {
		c.setStateLocked(StateRecoveryRequired)
		c.log("error", "startup-recovery", "recovery.marker present; system enters StateRecoveryRequired")
		return ErrRecoveryRequired
	}

	if b, err := os.ReadFile(c.verifiedActiveFile); err == nil {
		var rec AppliedGenerationRecord
		if err := json.Unmarshal(b, &rec); err == nil && rec.ValidateSchema() == nil {
			c.mu.Lock()
			c.appliedRecord = &rec
			c.mu.Unlock()
		} else {
			_, qErr := strictfs.StrictQuarantine(c.verifiedActiveFile, c.quarantineDir, "corrupt_verified_active")
			if qErr != nil {
				c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
			}
			c.setStateLocked(StateRecoveryRequired)
			err := c.writeRecoveryMarkerLocked("corrupt verified-active.json")
			if err != nil {
				// Failed to write recovery marker, nothing more we can do here
			}
			return ErrRecoveryRequired
		}
	}

	c.recoverCleanupJournalLocked()

	if err := c.recoverDraftJournalLocked(ctx); err != nil {
		c.setStateLocked(StateRecoveryRequired)
		err := c.writeRecoveryMarkerLocked("corrupted or unrecoverable draft journal: " + err.Error())
		if err != nil {
			// Failed to write recovery marker, nothing more we can do here
		}
		return ErrRecoveryRequired
	}

	if err := c.recoverManifestLocked(ctx); err != nil {
		c.setStateLocked(StateRecoveryRequired)
		err := c.writeRecoveryMarkerLocked("transaction recovery failed: " + err.Error())
		if err != nil {
			// Failed to write recovery marker, nothing more we can do here
		}
		return ErrRecoveryRequired
	}

	c.reconcilePendingInputLocked()
	c.setStateLocked(StateIdle)
	return nil
}

func (c *ApplyCoordinator) reconcilePendingInputLocked() {
	b, err := os.ReadFile(c.pendingInputFile)
	if err != nil {
		return
	}
	var rec PendingInputRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.pendingInputFile, c.quarantineDir, "corrupt_pending_input")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return
	}
	if c.appliedRecord != nil && c.appliedRecord.AppliedInputDigest == rec.TargetInputDigest {
		if uErr := strictfs.StrictUnlink(c.pendingInputFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
	}
}

func (c *ApplyCoordinator) recoverDraftJournalLocked(ctx context.Context) error {
	data, err := os.ReadFile(c.draftJournalFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read draft journal: %w", err)
	}

	var dj DraftJournal
	if err := json.Unmarshal(data, &dj); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.draftJournalFile, c.quarantineDir, "corrupt_draft_journal")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("corrupt draft journal: %w", err)
	}
	if err := dj.ValidateSchema(); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.draftJournalFile, c.quarantineDir, "invalid_draft_journal_schema")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("invalid draft journal schema: %w", err)
	}

	switch dj.State {
	case DraftSnapshotSecured:
		if dj.DraftSnapshotFile != "" {
			if restErr := c.cfg.StoreTx.RestoreSnapshotFile(dj.DraftSnapshotFile); restErr != nil {
				c.writeRecoveryMarkerLocked("restore snapshot failed: " + restErr.Error())
			}
			if uErr := strictfs.StrictUnlink(dj.DraftSnapshotFile); uErr != nil && !os.IsNotExist(uErr) {
				c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
			}
		}
		if uErr := strictfs.StrictUnlink(c.draftJournalFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
	case DraftStoreMutated:
		if dj.DraftSnapshotFile != "" {
			if restErr := c.cfg.StoreTx.RestoreSnapshotFile(dj.DraftSnapshotFile); restErr != nil {
				c.writeRecoveryMarkerLocked("restore snapshot failed: " + restErr.Error())
			}
			if uErr := strictfs.StrictUnlink(dj.DraftSnapshotFile); uErr != nil && !os.IsNotExist(uErr) {
				c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
			}
		}
		if uErr := strictfs.StrictUnlink(c.draftJournalFile); uErr != nil && !os.IsNotExist(uErr) {
			c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
		}
	case DraftPending:
		c.log("info", "startup-recovery", "retained valid pending draft journal")
	case DraftConsuming:
		manifestData, mErr := os.ReadFile(c.manifestFile)
		if mErr == nil {
			var m TransactionManifest
			if json.Unmarshal(manifestData, &m) == nil && m.State == StateCommitted {
				if dj.DraftSnapshotFile != "" {
					if uErr := strictfs.StrictUnlink(dj.DraftSnapshotFile); uErr != nil && !os.IsNotExist(uErr) {
						c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
					}
				}
				if uErr := strictfs.StrictUnlink(c.draftJournalFile); uErr != nil && !os.IsNotExist(uErr) {
					c.writeRecoveryMarkerLocked("unlink failed: " + uErr.Error())
				}
				return nil
			}
		}
		dj.State = DraftPending
		dj.AssociatedApplyTxID = ""
		dj.UpdatedAt = time.Now()
		if b, err := json.MarshalIndent(dj, "", "  "); err == nil {
			if wErr := strictfs.StrictWriteAtomic(c.draftJournalFile, b, 0600); wErr != nil {
				c.writeRecoveryMarkerLocked("atomic write failed: " + wErr.Error())
			}
		}
	default:
		_, qErr := strictfs.StrictQuarantine(c.draftJournalFile, c.quarantineDir, "unknown_draft_state")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("unknown draft journal state: %s", dj.State)
	}
	return nil
}

func (c *ApplyCoordinator) recoverManifestLocked(ctx context.Context) error {
	data, err := os.ReadFile(c.manifestFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var m TransactionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.manifestFile, c.quarantineDir, "corrupt_manifest")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("corrupt manifest: %w", err)
	}
	if err := m.ValidateSchema(); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.manifestFile, c.quarantineDir, "invalid_manifest_schema")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("invalid manifest schema: %w", err)
	}
	if err := c.validateManifestPaths(&m); err != nil {
		_, qErr := strictfs.StrictQuarantine(c.manifestFile, c.quarantineDir, "manifest_path_traversal")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("manifest path traversal: %w", err)
	}

	c.log("warn", "startup-recovery", fmt.Sprintf("recovering interrupted transaction %s in state %s", m.TxID, m.State))

	switch m.State {
	case StateIdle, StateCommitted, StateRolledBack, StateTerminalCleanup:
		c.cleanupTxArtifactsLocked(&m)
		return nil
	case StateSnapshotSecured, StateCandidateBuilt, StateCandidatePublished, StateSwapIntent, StateSwapApplied, StateSwapVerified, StateRuntimeIntent, StateRuntimeApplied, StateRuntimeVerified, StateBridgesReconciling, StateAbortInProgress:
		c.log("warn", "startup-recovery", "rolling back interrupted transaction")
		if err := c.rollbackActiveLocked(ctx, &m); err != nil {
			return fmt.Errorf("rollback failed during recovery: %w", err)
		}
		c.cleanupTxArtifactsLocked(&m)
		return nil
	case StateCommitIntent:
		c.log("info", "startup-recovery", "rolling forward commit intent")
		var rec AppliedGenerationRecord
		if m.CandidateGenerationID != "" {
			genMan, _, _, err := c.genStore.ReadGenerationBundle(m.CandidateGenerationID)
			if err != nil {
				c.writeRecoveryMarkerLocked("read bundle failed: " + err.Error())
				return ErrRecoveryRequired
			}
			rec = AppliedGenerationRecord{
				Version:             1,
				Generation:          genMan.GenerationNumber,
				GenerationID:        genMan.GenerationID,
				AppliedAt:           time.Now(),
				AppliedStoreDigest:  genMan.AppliedStoreDigest,
				AppliedConfigDigest: genMan.AppliedConfigDigest,
				AppliedInputDigest:  genMan.AppliedInputDigest,
				AppliedListeners:    genMan.AppliedListeners,
				AppliedBridges:      genMan.AppliedBridges,
				RuntimeMode:         genMan.RuntimeMode,
			}
		} else {
			rec = AppliedGenerationRecord{
				Version:             1,
				Generation:          m.VerifiedActiveGeneration + 1,
				GenerationID:        m.CandidateGenerationID,
				AppliedAt:           time.Now(),
				AppliedStoreDigest:  m.TargetDesiredStoreDigest,
				AppliedConfigDigest: "",
				AppliedInputDigest:  m.TargetInputDigest,
				AppliedBridges:      nil,
				RuntimeMode:         RuntimeOff,
			}
		}

		// delegate to final commit helper
		if err := c.executeFinalCommitLocked(ctx, &m, rec); err != nil {
			return err // already sets marker and state
		}
		c.cleanupTxArtifactsLocked(&m)
		return nil
	case StateRecoveryRequired:
		return ErrRecoveryRequired
	default:
		_, qErr := strictfs.StrictQuarantine(c.manifestFile, c.quarantineDir, "unknown_manifest_state")
		if qErr != nil {
			c.writeRecoveryMarkerLocked("quarantine failed: " + qErr.Error())
		}
		return fmt.Errorf("unknown manifest state: %s", m.State)
	}
}

func (c *ApplyCoordinator) processCleanupJournalFilesLocked(cj *CleanupJournal) error {
	if cj == nil || len(cj.Files) == 0 {
		var finalErr error
		if err := strictfs.StrictUnlink(c.cleanupJournalFile); err != nil && !os.IsNotExist(err) {
			finalErr = fmt.Errorf("cleanup journal unlink failed: %w", err)
			_ = c.writeRecoveryMarkerLocked(finalErr.Error())
		}
		if err := strictfs.StrictUnlink(c.manifestFile); err != nil && !os.IsNotExist(err) {
			if finalErr == nil { finalErr = fmt.Errorf("manifest unlink failed: %w", err) }
			_ = c.writeRecoveryMarkerLocked(err.Error())
		}
		return finalErr
	}

	var remaining []string
	changed := false
	var unlinkErrs []error
	for _, f := range cj.Files {
		targetPath := filepath.Join(c.cfg.ConfigDir, f)
		err := strictfs.StrictUnlink(targetPath)
		if err != nil && !os.IsNotExist(err) {
			remaining = append(remaining, f)
			unlinkErrs = append(unlinkErrs, fmt.Errorf("unlink %s: %w", f, err))
			_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup unlink failed %s: %v", f, err))
		} else {
			if _, statErr := os.Stat(targetPath); statErr == nil || !os.IsNotExist(statErr) {
				remaining = append(remaining, f)
				unlinkErrs = append(unlinkErrs, fmt.Errorf("verify unlink %s: %v", f, statErr))
				_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup unlink verify failed %s: stat %v", f, statErr))
			} else {
				changed = true
			}
		}
	}

	var finalErr error
	if len(unlinkErrs) > 0 {
		finalErr = unlinkErrs[0]
	}

	if len(remaining) == 0 {
		if err := strictfs.StrictUnlink(c.cleanupJournalFile); err != nil && !os.IsNotExist(err) {
			finalErr = fmt.Errorf("cleanup journal unlink failed: %w", err)
			_ = c.writeRecoveryMarkerLocked(finalErr.Error())
		}
		if err := strictfs.StrictUnlink(c.manifestFile); err != nil && !os.IsNotExist(err) {
			if finalErr == nil { finalErr = fmt.Errorf("manifest unlink failed: %w", err) }
			_ = c.writeRecoveryMarkerLocked(err.Error())
		}
	} else if changed {
		cj.Sequence++
		cj.Files = remaining
		if b, err := json.MarshalIndent(cj, "", "  "); err == nil {
			if err := strictfs.StrictWriteAtomic(c.cleanupJournalFile, b, 0600); err != nil {
				_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup journal rewrite failed: %v", err))
			}
		}
	}
	return finalErr
}


func (c *ApplyCoordinator) cleanupTxArtifactsLocked(m *TransactionManifest) error {
	var files []string
	if m.PreMutationStoreSnapshotFile != "" {
		files = append(files, filepath.Base(m.PreMutationStoreSnapshotFile))
	}
	if m.CandidateConfigFile != "" {
		files = append(files, filepath.Base(m.CandidateConfigFile))
	}
	if m.CandidatePostMutationStoreSnapshotFile != "" {
		files = append(files, filepath.Base(m.CandidatePostMutationStoreSnapshotFile))
	}

	var cj *CleanupJournal
	if len(files) > 0 {
		cj = &CleanupJournal{
			Version:  1,
			Sequence: 1,
			TxID:     m.TxID,
			Files:    files,
		}
		if b, err := json.MarshalIndent(cj, "", "  "); err == nil {
			if wErr := strictfs.StrictWriteAtomic(c.cleanupJournalFile, b, 0600); wErr != nil {
				_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup journal write failed: %v", wErr))
				return fmt.Errorf("cleanup journal write failed: %w", wErr)
			}
		}
	}

	if err := c.transitionManifestLocked(m, StateTerminalCleanup); err != nil {
		_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("terminal cleanup transition failed: %v", err))
		return fmt.Errorf("terminal cleanup transition failed: %w", err)
	}

	return c.processCleanupJournalFilesLocked(cj)
}

func (c *ApplyCoordinator) recoverCleanupJournalLocked() {
	data, err := os.ReadFile(c.cleanupJournalFile)
	if err == nil {
		var cj CleanupJournal
		if json.Unmarshal(data, &cj) == nil && cj.ValidateSchema() == nil {
			c.processCleanupJournalFilesLocked(&cj)
			return
		}
	}
	// No journal or invalid journal, just try to clean up the files directly
	if err := strictfs.StrictUnlink(c.cleanupJournalFile); err != nil && !os.IsNotExist(err) {
		err := c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup journal unlink failed: %v", err))
		if err != nil {
			// Failed to write recovery marker, nothing more we can do here
		}
	}
}

func (c *ApplyCoordinator) GarbageCollectGenerations(keepRecent int) error {
	ipcLock, err := strictfs.LockTransaction(c.cfg.ConfigDir)
	if err != nil {
		return fmt.Errorf("acquire transaction lock: %w", err)
	}
	defer ipcLock.Close()

	c.mu.RLock()
	var activeGenID string
	if c.appliedRecord != nil {
		activeGenID = c.appliedRecord.GenerationID
	}
	c.mu.RUnlock()

	lkgPtr, _ := c.genStore.ReadLKGPointer()
	var lkgGenID string
	if lkgPtr != nil {
		lkgGenID = lkgPtr.GenerationID
	}

	var protectedGenIDs []string

	// Protect generations referenced in the active transaction manifest
	data, err := os.ReadFile(c.manifestFile)
	if err == nil {
		var m TransactionManifest
		if json.Unmarshal(data, &m) == nil {
			if m.CandidateGenerationID != "" {
				protectedGenIDs = append(protectedGenIDs, m.CandidateGenerationID)
			}
			if m.PreviousGenerationID != "" {
				protectedGenIDs = append(protectedGenIDs, m.PreviousGenerationID)
			}
			if m.RollbackTargetGenerationID != "" {
				protectedGenIDs = append(protectedGenIDs, m.RollbackTargetGenerationID)
			}
			if m.LKGGenerationID != "" {
				protectedGenIDs = append(protectedGenIDs, m.LKGGenerationID)
			}
		}
	}

	// Protect generations that are actively being removed by the cleanup journal
	cjData, cjErr := os.ReadFile(c.cleanupJournalFile)
	if cjErr == nil {
		var cj CleanupJournal
		if json.Unmarshal(cjData, &cj) == nil {
			for _, file := range cj.Files {
				if strings.HasPrefix(file, "gen-") {
					protectedGenIDs = append(protectedGenIDs, file)
				}
			}
		}
	}

	// DraftJournal and PendingInputRecord do not currently hold GenerationID references.
	// If they are updated to reference generations in the future, parse them here.

	return c.genStore.RunRetentionGC(activeGenID, lkgGenID, protectedGenIDs, keepRecent)
}

func (c *ApplyCoordinator) MutateAndApply(
	ctx context.Context,
	mutateFn func() error,
	compileFn func(ctx context.Context) (*CompileResult, error),
) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	ipcLock, err := strictfs.LockTransaction(c.cfg.ConfigDir)
	if err != nil {
		return fmt.Errorf("acquire transaction lock: %w", err)
	}
	defer ipcLock.Close()

	if c.State() == StateRecoveryRequired {
		return ErrRecoveryRequired
	}

	txid := GenerateTxID()
	c.activeTxID = txid
	defer func() { c.activeTxID = "" }()

	var baseAppliedStoreDigest, baseAppliedInputDigest, previousGenID string
	var verifiedGeneration uint64

	if c.appliedRecord != nil {
		baseAppliedStoreDigest = c.appliedRecord.AppliedStoreDigest
		baseAppliedInputDigest = c.appliedRecord.AppliedInputDigest
		verifiedGeneration = c.appliedRecord.Generation
		previousGenID = c.appliedRecord.GenerationID
	}

	baseDesiredStoreDigest, dErr := c.cfg.StoreTx.CurrentDigest()
	if dErr != nil {
		return fmt.Errorf("failed to get current store digest: %w", dErr)
	}

	if c.appliedRecord != nil {
		if c.appliedRecord.AppliedStoreDigest != baseDesiredStoreDigest {
			c.setStateLocked(StateRecoveryRequired)
			_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("store digest mismatch: applied=%s actual=%s", c.appliedRecord.AppliedStoreDigest, baseDesiredStoreDigest))
			return fmt.Errorf("preflight store digest mismatch: applied=%s actual=%s", c.appliedRecord.AppliedStoreDigest, baseDesiredStoreDigest)
		}

		actualConfigBytes, err := os.ReadFile(c.activeConfigFile)
		if err == nil {
			actualConfigDigest := strictfs.ComputeBytesDigest(actualConfigBytes)
			if c.appliedRecord.AppliedConfigDigest != actualConfigDigest {
				c.setStateLocked(StateRecoveryRequired)
				_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("active config digest mismatch: applied=%s actual=%s", c.appliedRecord.AppliedConfigDigest, actualConfigDigest))
				return fmt.Errorf("preflight active config digest mismatch: applied=%s actual=%s", c.appliedRecord.AppliedConfigDigest, actualConfigDigest)
			}
		}
	}

	lkgPtr, _ := c.genStore.ReadLKGPointer()
	var lkgGenID string
	if lkgPtr != nil {
		lkgGenID = lkgPtr.GenerationID
	}

	manifest := TransactionManifest{
		Version:                  1,
		TxID:                     txid,
		State:                    StateIdle,
		CreatedAt:                time.Now(),
		UpdatedAt:                time.Now(),
		PreviousGenerationID:     previousGenID,
		LKGGenerationID:          lkgGenID,
		BaseAppliedStoreDigest:   baseAppliedStoreDigest,
		BaseDesiredStoreDigest:   baseDesiredStoreDigest,
		BaseAppliedInputDigest:   baseAppliedInputDigest,
		VerifiedActiveGeneration: verifiedGeneration,
	}

	var snapPath string
	if mutateFn != nil {
		snapPath, err = c.cfg.StoreTx.CreateSnapshotFile(txid)
		if err != nil {
			return fmt.Errorf("create store snapshot: %w", err)
		}
		manifest.PreMutationStoreSnapshotFile = snapPath
		snapDigest, _ := strictfs.ComputeFileDigest(snapPath)
		manifest.PreMutationStoreDigest = snapDigest

		if err := c.initManifestLocked(&manifest, StateSnapshotSecured); err != nil {
			if remErr := c.cfg.StoreTx.RemoveSnapshotFile(snapPath); remErr != nil && !os.IsNotExist(remErr) {
				c.writeRecoveryMarkerLocked("remove snapshot failed: " + remErr.Error())
			}
			return err
		}

		mutErr := func() (retErr error) {
			defer func() {
				if r := recover(); r != nil {
					retErr = fmt.Errorf("panic during store mutation: %v", r)
				}
			}()
			return mutateFn()
		}()

		if mutErr != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return mutErr
		}

		targetStoreDigest, _ := c.cfg.StoreTx.CurrentDigest()
		manifest.TargetDesiredStoreDigest = targetStoreDigest

		postSnapPath, err := c.cfg.StoreTx.CreateSnapshotFile(txid + "-post")
		if err != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return fmt.Errorf("create post-mutation store snapshot: %w", err)
		}
		manifest.CandidatePostMutationStoreSnapshotFile = postSnapPath
	} else {
		manifest.TargetDesiredStoreDigest = baseDesiredStoreDigest
	}

	compileResult, err := compileFn(ctx)
	if err != nil {
		if manifest.PreMutationStoreSnapshotFile != "" {
			if restErr := c.cfg.StoreTx.RestoreSnapshotFile(manifest.PreMutationStoreSnapshotFile); restErr != nil {
				c.setStateLocked(StateRecoveryRequired)
				err := c.writeRecoveryMarkerLocked(fmt.Sprintf("compile failed, and restore failed: %v", restErr))
				if err != nil {
					// Failed to write recovery marker, nothing more we can do here
				}
				return fmt.Errorf("compile failed, and restore failed: %w", restErr)
			}
		}
		c.cleanupTxArtifactsLocked(&manifest)
		c.setStateLocked(StateIdle)
		return fmt.Errorf("compile mihomo config: %w", err)
	}

	manifest.TargetInputDigest = compileResult.InputDigest
	manifest.CandidateConfigDigest = compileResult.ConfigDigest
	manifest.DesiredMode = compileResult.Mode

	if compileResult.Mode != RuntimeOff {
		manifest.ConfigPresent = true
		candidatePath := filepath.Join(c.cfg.ConfigDir, fmt.Sprintf("config.yaml.candidate.%s", txid))
		manifest.CandidateConfigFile = candidatePath
		if err := strictfs.StrictWriteAtomic(candidatePath, compileResult.ConfigYAML, 0600); err != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return fmt.Errorf("write candidate config: %w", err)
		}
		if err := c.cfg.Validator.ValidateConfigFile(ctx, candidatePath); err != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return fmt.Errorf("validate candidate config: %w", err)
		}
	} else {
		manifest.ConfigPresent = false
	}

	if manifest.Sequence == 0 {
		if err := c.initManifestLocked(&manifest, StateCandidateBuilt); err != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return err
		}
	} else {
		if err := c.transitionManifestLocked(&manifest, StateCandidateBuilt); err != nil {
			c.cleanupTxArtifactsLocked(&manifest)
			c.setStateLocked(StateIdle)
			return err
		}
	}

	newGen := manifest.VerifiedActiveGeneration + 1
	newGenID := fmt.Sprintf("gen-%06d-%s", newGen, txid)
	manifest.CandidateGenerationID = newGenID

	newRec := AppliedGenerationRecord{
		Version:             1,
		GenerationID:        newGenID,
		Generation:          newGen,
		AppliedStoreDigest:  manifest.TargetDesiredStoreDigest,
		AppliedConfigDigest: manifest.CandidateConfigDigest,
		AppliedInputDigest:  manifest.TargetInputDigest,
		AppliedListeners:    compileResult.RequiredListeners,
		AppliedBridges:      compileResult.TargetBridges,
		RuntimeMode:         compileResult.Mode,
		AppliedAt:           time.Now(),
	}

	if err := c.genStore.PublishStagedBundle(
		newGenID,
		newGen,
		compileResult.ConfigYAML,
		manifest.CandidatePostMutationStoreSnapshotFile,
		newRec,
		c.DaemonEpoch(),
	); err != nil {
		c.cleanupTxArtifactsLocked(&manifest)
		c.setStateLocked(StateIdle)
		return fmt.Errorf("publish LKG generation bundle: %w", err)
	}

	if err := c.transitionManifestLocked(&manifest, StateCandidatePublished); err != nil {
		c.cleanupTxArtifactsLocked(&manifest)
		c.setStateLocked(StateIdle)
		return err
	}

	if compileResult.Mode == RuntimeOff {
		return c.applyRuntimeOffLocked(ctx, &manifest, compileResult)
	}

	if err := c.transitionManifestLocked(&manifest, StateSwapIntent); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}
	outcome, swapErr := strictfs.RenameAndResolve(manifest.CandidateConfigFile, c.activeConfigFile, "", manifest.CandidateConfigDigest)
	if swapErr != nil && outcome != strictfs.OutcomeNewApplied {
		return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("swap candidate to active config: %w", swapErr))
	}

	if err := c.transitionManifestLocked(&manifest, StateSwapApplied); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}
	if err := c.transitionManifestLocked(&manifest, StateSwapVerified); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}

	if err := c.transitionManifestLocked(&manifest, StateRuntimeIntent); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}
	restartErr := c.restartControlledLocked(ctx, manifest.CandidateConfigDigest, compileResult.RequiredListeners)
	if restartErr != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("runtime restart failed: %w", restartErr))
	}
	if err := c.transitionManifestLocked(&manifest, StateRuntimeApplied); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}
	if err := c.transitionManifestLocked(&manifest, StateRuntimeVerified); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}

	if err := c.transitionManifestLocked(&manifest, StateBridgesReconciling); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}
	var beforeBridges []BridgeRef
	if c.appliedRecord != nil {
		beforeBridges = c.appliedRecord.AppliedBridges
	}
	bridgeErr := c.syncBridgesLocked(ctx, &manifest, beforeBridges, compileResult.TargetBridges)
	if bridgeErr != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, fmt.Errorf("bridge sync failed: %w", bridgeErr))
	}

	if err := c.transitionManifestLocked(&manifest, StateCommitIntent); err != nil {
		return c.handleApplyFailureLocked(ctx, &manifest, err)
	}

	rec := AppliedGenerationRecord{
		Version:             1,
		Generation:          newGen,
		GenerationID:        newGenID,
		AppliedAt:           time.Now(),
		AppliedStoreDigest:  manifest.TargetDesiredStoreDigest,
		AppliedConfigDigest: manifest.CandidateConfigDigest,
		AppliedInputDigest:  manifest.TargetInputDigest,
		AppliedListeners:    compileResult.RequiredListeners,
		AppliedBridges:      compileResult.TargetBridges,
		RuntimeMode:         compileResult.Mode,
	}

	return c.executeFinalCommitLocked(ctx, &manifest, rec)
}

func (c *ApplyCoordinator) handleApplyFailureLocked(ctx context.Context, m *TransactionManifest, origErr error) error {
	if rErr := c.rollbackActiveLocked(ctx, m); rErr != nil {
		c.writeRecoveryMarkerLocked("rollback failed: " + rErr.Error())
		c.setStateLocked(StateRecoveryRequired)
	} else {
		c.cleanupTxArtifactsLocked(m)
		c.setStateLocked(StateIdle)
	}
	return origErr
}

func (c *ApplyCoordinator) applyRuntimeOffLocked(ctx context.Context, manifest *TransactionManifest, compileResult *CompileResult) error {
	c.log("info", "coordinator.apply", "Applying configuration in RuntimeOff mode (files only)")

	var beforeBridges []BridgeRef
	if c.appliedRecord != nil {
		beforeBridges = c.appliedRecord.AppliedBridges
	}
	if err := c.syncBridgesLocked(ctx, manifest, beforeBridges, nil); err != nil {
		return fmt.Errorf("failed to withdraw bridges during RuntimeOff apply: %w", err)
	}

	manifest.State = StateApplied
	if err := c.transitionManifestLocked(manifest, manifest.State); err != nil {
		return err
	}

	manifest.State = StateVerified
	if err := c.transitionManifestLocked(manifest, manifest.State); err != nil {
		return err
	}

	return nil
}

func (c *ApplyCoordinator) restartControlledLocked(ctx context.Context, expectedDigest string, listeners []ListenerSpec) error {
	actDigest, err := strictfs.ComputeFileDigest(c.activeConfigFile)
	if err != nil || actDigest != expectedDigest {
		return fmt.Errorf("active config digest mismatch before spawn: got %s, want %s", actDigest, expectedDigest)
	}
	if running, _ := c.cfg.Operator.IsRunning(); running {
		if err := c.cfg.Operator.StopAndWait(ctx); err != nil {
			return fmt.Errorf("stop running mihomo: %w", err)
		}
	}
	if err := c.cfg.Operator.Start(); err != nil {
		return fmt.Errorf("start fresh mihomo: %w", err)
	}
	postDigest, err := strictfs.ComputeFileDigest(c.activeConfigFile)
	if err != nil || postDigest != expectedDigest {
		return fmt.Errorf("active config modified during spawn: got %s, want %s", postDigest, expectedDigest)
	}
	return nil
}

func (c *ApplyCoordinator) syncBridgesLocked(ctx context.Context, m *TransactionManifest, before, target []BridgeRef) error {
	if c.cfg.BridgeRuntime == nil {
		return nil
	}

	beforeMap := make(map[string]BridgeRef)
	for _, b := range before {
		beforeMap[b.KernelInterface] = b
	}
	targetMap := make(map[string]BridgeRef)
	for _, b := range target {
		targetMap[b.KernelInterface] = b
	}

	if m.BridgeOperations == nil {
		m.BridgeOperations = []BridgeOperation{}
	}

	var toCreate []BridgeRef
	var toWithdraw []BridgeRef

	for k, b := range targetMap {
		if _, ok := beforeMap[k]; !ok {
			toCreate = append(toCreate, b)
		}
	}
	for k, b := range beforeMap {
		if _, ok := targetMap[k]; !ok {
			toWithdraw = append(toWithdraw, b)
		}
	}

	trackOp := func(ref BridgeRef, action string) error {
		opID := fmt.Sprintf("%s-%s-%s", m.TxID, action, ref.KernelInterface)
		opIndex := -1
		for i, op := range m.BridgeOperations {
			if op.OperationID == opID {
				opIndex = i
				break
			}
		}

		var op BridgeOperation
		if opIndex >= 0 {
			op = m.BridgeOperations[opIndex]
		} else {
			op = BridgeOperation{
				OperationID:  opID,
				Action:       action,
				TargetDigest: ref.Digest(),
				BridgeRef:    ref,
				State:        BridgeOpIntent,
				Attempts:     0,
			}
			m.BridgeOperations = append(m.BridgeOperations, op)
			opIndex = len(m.BridgeOperations) - 1
		}

		if op.State == BridgeOpVerified {
			return nil
		}

		op.State = BridgeOpIntent
		op.Attempts++
		m.BridgeOperations[opIndex] = op
		if err := c.transitionManifestLocked(m, m.State); err != nil {
			return fmt.Errorf("persist intent %s: %w", action, err)
		}

		var err error
		if action == "create" {
			err = c.cfg.BridgeRuntime.ApplyBridges(ctx, []BridgeRef{ref})
		} else {
			err = c.cfg.BridgeRuntime.WithdrawBridges(ctx, []BridgeRef{ref})
		}

		if err != nil {
			op.LastError = err.Error()
			m.BridgeOperations[opIndex] = op
			_ = c.transitionManifestLocked(m, m.State)
			return fmt.Errorf("%s bridge %s: %w", action, ref.KernelInterface, err)
		}

		op.State = BridgeOpApplied
		m.BridgeOperations[opIndex] = op
		if cpErr := c.transitionManifestLocked(m, m.State); cpErr != nil {
			c.setStateLocked(StateRecoveryRequired)
			_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("bridge %s side-effect succeeded but checkpoint failed: %v", ref.KernelInterface, cpErr))
			return fmt.Errorf("bridge %s side-effect succeeded but checkpoint failed: %w", ref.KernelInterface, cpErr)
		}

		if action == "create" {
			err = c.cfg.BridgeRuntime.VerifyBridges(ctx, []BridgeRef{ref})
			if err != nil {
				op.LastError = err.Error()
				m.BridgeOperations[opIndex] = op
				_ = c.transitionManifestLocked(m, m.State)
				return fmt.Errorf("verify create bridge %s: %w", ref.KernelInterface, err)
			}
		}

		op.State = BridgeOpVerified
		m.BridgeOperations[opIndex] = op
		if cpErr := c.transitionManifestLocked(m, m.State); cpErr != nil {
			c.setStateLocked(StateRecoveryRequired)
			_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("bridge %s verified but checkpoint failed: %v", ref.KernelInterface, cpErr))
			return fmt.Errorf("bridge %s verified but checkpoint failed: %w", ref.KernelInterface, cpErr)
		}
		
		return nil
	}

	for _, b := range toCreate {
		if err := trackOp(b, "create"); err != nil {
			return err
		}
	}

	for _, b := range toWithdraw {
		if err := trackOp(b, "withdraw"); err != nil {
			return err
		}
	}

	return nil
}

func (c *ApplyCoordinator) rollbackActiveLocked(ctx context.Context, m *TransactionManifest) error {
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
}
func (c *ApplyCoordinator) initManifestLocked(m *TransactionManifest, firstState ManifestState) error {
	m.Sequence = 1
	m.State = firstState
	m.UpdatedAt = time.Now()

	if err := c.casManifest(nil, m); err != nil {
		return err
	}
	c.setStateLocked(firstState)
	return nil
}

func (c *ApplyCoordinator) executeFinalCommitLocked(ctx context.Context, manifest *TransactionManifest, rec AppliedGenerationRecord) error {
	// 1. require durable StateCommitIntent
	if manifest.State != StateCommitIntent {
		return fmt.Errorf("invalid state for final commit: %s", manifest.State)
	}

	// 2. write verified-active
	if c.hooks.FailCommitVerifiedActive {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("failpoint: commit verified active failed")
		return ErrRecoveryRequired
	}

	recBytes, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("marshal verified active: " + err.Error())
		return ErrRecoveryRequired
	}

	if err := strictfs.StrictWriteAtomic(c.verifiedActiveFile, recBytes, 0600); err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("write verified active: " + err.Error())
		return ErrRecoveryRequired
	}

	// 3. write current/LKG generation pointer
	if err := c.genStore.AdvanceLKGPointer(rec.GenerationID, rec.Generation, rec, c.DaemonEpoch()); err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("update LKG pointer: " + err.Error())
		return ErrRecoveryRequired
	}

	// 4. read both, 5. verify
	vaData, err := os.ReadFile(c.verifiedActiveFile)
	if err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("verify verified-active read: " + err.Error())
		return ErrRecoveryRequired
	}
	var verifyRec AppliedGenerationRecord
	if err := json.Unmarshal(vaData, &verifyRec); err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("verify verified-active parse: " + err.Error())
		return ErrRecoveryRequired
	}
	
	// Verified-Active Full Equality Check
	if verifyRec.GenerationID != rec.GenerationID || 
	   verifyRec.Generation != rec.Generation ||
	   verifyRec.AppliedStoreDigest != rec.AppliedStoreDigest ||
	   verifyRec.AppliedConfigDigest != rec.AppliedConfigDigest ||
	   verifyRec.AppliedInputDigest != rec.AppliedInputDigest ||
	   verifyRec.RuntimeMode != rec.RuntimeMode {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("verified-active full equality mismatch during commit verification")
		return ErrRecoveryRequired
	}

	ptr, err := c.genStore.ReadLKGPointer()
	if err != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("verify LKG pointer read: " + err.Error())
		return ErrRecoveryRequired
	}
	if ptr.GenerationID != rec.GenerationID || ptr.GenerationNumber != rec.Generation || ptr.AppliedConfigDigest != rec.AppliedConfigDigest {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("LKG pointer generation mismatch during commit verification")
		return ErrRecoveryRequired
	}

	c.mu.Lock()
	c.appliedRecord = &rec
	c.mu.Unlock()

	// 6. write StateCommitted
	if tErr := c.transitionManifestLocked(manifest, StateCommitted); tErr != nil {
		c.setStateLocked(StateRecoveryRequired)
		_ = c.writeRecoveryMarkerLocked("transition to committed failed: " + tErr.Error())
		return fmt.Errorf("committed transition: %w", tErr)
	}

	// 7. terminal cleanup
	if err := c.cleanupTxArtifactsLocked(manifest); err != nil {
		c.log("error", "coordinator.commit", "commit succeeded with partial cleanup failure: " + err.Error())
		return fmt.Errorf("commit succeeded with partial cleanup failure: %w", err)
	}

	c.setStateLocked(StateIdle)
	return nil
}
