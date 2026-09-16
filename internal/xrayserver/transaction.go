package xrayserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// TxState represents the canonical durable state of a configuration transaction.
type TxState string

const (
	TxStatePrepared         TxState = "prepared"
	TxStateTested           TxState = "tested"
	TxStateBackupReady      TxState = "backup_ready"
	TxStateActiveReplaced   TxState = "active_replaced"
	TxStateRestarting       TxState = "restarting"
	TxStateVerifying        TxState = "verifying"
	TxStateCommitted        TxState = "committed"
	TxStateRollingBack      TxState = "rolling_back"
	TxStateRolledBack       TxState = "rolled_back"
	TxStateRecoveryRequired TxState = "recovery_required"
	TxStateAborted          TxState = "aborted"
)

// ListenerRequirement defines an address and port that must be active for candidate readiness.
type ListenerRequirement struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Tag     string `json:"tag,omitempty"`
}

// SnapshotManifest tracks the durable state and backup artifacts for an active transaction.
type SnapshotManifest struct {
	TxID              string                `json:"tx_id"`
	SchemaVersion     int                   `json:"schema_version"`
	CreatedAt         string                `json:"created_at"`
	UpdatedAt         string                `json:"updated_at,omitempty"`
	State             TxState               `json:"state"`
	Checksums         map[string]string     `json:"checksums"`
	OldConfig         Config                `json:"old_config"`
	CandidateConfig   Config                `json:"candidate_config"`
	WasRunning        bool                  `json:"was_running"`
	CandidateRunning  bool                  `json:"candidate_running,omitempty"`
	RequiredListeners []ListenerRequirement `json:"required_listeners,omitempty"`
	Committed         bool                  `json:"committed,omitempty"`
	Error             string                `json:"error,omitempty"`
}

func isValidTxID(txID string) bool {
	if len(txID) == 0 || len(txID) > 128 {
		return false
	}
	for _, c := range txID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func (s *Service) transactionsDir() string {
	return filepath.Join(s.xrayDir(), "transactions")
}

func (s *Service) txDir(txID string) string {
	return filepath.Join(s.transactionsDir(), txID)
}

func (s *Service) cloneConfigLocked() Config {
	cfg := s.config
	if s.config.Clients != nil {
		cfg.Clients = make([]Client, len(s.config.Clients))
		copy(cfg.Clients, s.config.Clients)
	}
	return cfg
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

func generateShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()%0xffff)
	}
	return hex.EncodeToString(b)
}

func writeManifest(txPath string, manifest *SnapshotManifest) error {
	manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return atomicWriteFile(filepath.Join(txPath, "snapshot-manifest.json"), data, 0600)
}

// PrepareCandidate stages a candidate config into an isolated transaction snapshot directory.
func (s *Service) PrepareCandidate(txID string, candidate Config) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryRequired {
		return "", ErrRecoveryRequired
	}

	if txID == "" {
		txID = fmt.Sprintf("tx-%d-%s", time.Now().UnixNano(), generateShortID())
	} else if !isValidTxID(txID) {
		return "", fmt.Errorf("invalid transaction id: %s", txID)
	}

	txPath := s.txDir(txID)
	if err := os.MkdirAll(txPath, 0700); err != nil {
		return "", fmt.Errorf("create tx dir: %w", err)
	}

	listenAddr := candidate.ListenAddress
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}
	listenPort := candidate.ListenPort
	if listenPort <= 0 {
		listenPort = 9008
	}

	manifest := SnapshotManifest{
		TxID:             txID,
		SchemaVersion:    1,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		State:            TxStatePrepared,
		Checksums:        make(map[string]string),
		OldConfig:        s.cloneConfigLocked(),
		CandidateConfig:  candidate,
		WasRunning:       s.proc != nil && s.proc.IsRunning(),
		CandidateRunning: candidate.Enabled,
		RequiredListeners: []ListenerRequirement{
			{Address: listenAddr, Port: listenPort, Tag: "vless-in"},
		},
	}

	abortTx := func(reason error) (string, error) {
		manifest.State = TxStateAborted
		manifest.Error = reason.Error()
		_ = writeManifest(txPath, &manifest)
		return "", reason
	}

	// 1. Snapshot settings file
	cfgFile := s.cfgPath()
	if fileExists(cfgFile) {
		sum, err := sha256File(cfgFile)
		if err != nil {
			return abortTx(fmt.Errorf("hash settings file: %w", err))
		}
		manifest.Checksums["xray-server-settings.json.bak"] = sum
		data, _ := os.ReadFile(cfgFile)
		if err := atomicWriteFile(filepath.Join(txPath, "xray-server-settings.json.bak"), data, 0600); err != nil {
			return abortTx(fmt.Errorf("backup settings: %w", err))
		}
	}

	// 2. Snapshot runtime config file
	rtFile := s.xrayRuntimeConfigPath()
	if fileExists(rtFile) {
		sum, err := sha256File(rtFile)
		if err != nil {
			return abortTx(fmt.Errorf("hash runtime config: %w", err))
		}
		manifest.Checksums["config.json.bak"] = sum
		data, _ := os.ReadFile(rtFile)
		if err := atomicWriteFile(filepath.Join(txPath, "config.json.bak"), data, 0600); err != nil {
			return abortTx(fmt.Errorf("backup runtime config: %w", err))
		}
	}

	// 3. Render candidate runtime config
	candidateRuntime, err := RenderRuntimeConfig(candidate)
	if err != nil {
		return abortTx(fmt.Errorf("render candidate runtime: %w", err))
	}

	candidateRtPath := filepath.Join(txPath, "config.json.candidate")
	if err := atomicWriteFile(candidateRtPath, candidateRuntime, 0600); err != nil {
		return abortTx(fmt.Errorf("write candidate runtime: %w", err))
	}

	// 4. Test candidate syntax with real binary test mode
	if fileExists(s.binPath) {
		if err := s.TestConfig(candidate); err != nil {
			return abortTx(fmt.Errorf("candidate test failed: %w", err))
		}
	}
	manifest.State = TxStateTested

	// 5. Verify backups are intact -> backup_ready
	manifest.State = TxStateBackupReady
	if err := writeManifest(txPath, &manifest); err != nil {
		return abortTx(fmt.Errorf("write manifest: %w", err))
	}

	return txID, nil
}

// CommitPrepared applies the prepared transaction candidate atomically following the 11-state machine.
func (s *Service) CommitPrepared(txID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txPath := s.txDir(txID)
	manifestPath := filepath.Join(txPath, "snapshot-manifest.json")
	if !fileExists(manifestPath) {
		return fmt.Errorf("transaction %s not found", txID)
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest SnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("unmarshal manifest: %w", err)
	}

	if manifest.State == TxStateCommitted {
		return nil
	}

	rollbackAndJoin := func(origErr error) error {
		manifest.State = TxStateRollingBack
		manifest.Error = origErr.Error()
		_ = writeManifest(txPath, &manifest)

		_ = s.stopLocked()
		rbErr := s.rollbackLocked(txID, &manifest)
		if rbErr != nil {
			manifest.State = TxStateRecoveryRequired
			_ = writeManifest(txPath, &manifest)
			s.recoveryRequired = true
			s.recoveryReason = fmt.Sprintf("commit failed (%v) and rollback failed (%v)", origErr, rbErr)
			return errors.Join(origErr, fmt.Errorf("rollback failed: %w", rbErr))
		}

		manifest.State = TxStateRolledBack
		_ = writeManifest(txPath, &manifest)
		return origErr
	}

	candidateRtPath := filepath.Join(txPath, "config.json.candidate")
	candidateRtBytes, err := os.ReadFile(candidateRtPath)
	if err != nil {
		return rollbackAndJoin(fmt.Errorf("read candidate runtime: %w", err))
	}

	// 1. State: active_replaced
	if err := atomicWriteFile(s.xrayRuntimeConfigPath(), candidateRtBytes, 0600); err != nil {
		return rollbackAndJoin(fmt.Errorf("write active runtime config: %w", err))
	}
	manifest.State = TxStateActiveReplaced
	_ = writeManifest(txPath, &manifest)

	candidate := manifest.CandidateConfig

	// 2. State: restarting & verifying
	if candidate.Enabled && fileExists(s.binPath) {
		manifest.State = TxStateRestarting
		_ = writeManifest(txPath, &manifest)

		_ = s.stopLocked()

		cmd := exec.Command(s.binPath, "run", "-c", s.xrayRuntimeConfigPath())
		proc := newManagedProc(cmd)
		if err := proc.Start(); err != nil {
			return rollbackAndJoin(fmt.Errorf("start xray with candidate: %w", err))
		}
		s.proc = proc

		pid := proc.PID()
		if pid <= 0 {
			return rollbackAndJoin(fmt.Errorf("xray process started with invalid PID %d", pid))
		}
		if err := s.writePIDRecord(pid, s.xrayRuntimeConfigPath()); err != nil {
			return rollbackAndJoin(fmt.Errorf("write PID record after commit start: %w", err))
		}

		manifest.State = TxStateVerifying
		_ = writeManifest(txPath, &manifest)

		readyTimeout := 3 * time.Second
		probeInterval := 50 * time.Millisecond
		deadline := time.Now().Add(readyTimeout)

		// Multi-listener readiness verification
		for _, req := range manifest.RequiredListeners {
			isReady := false
			addr := req.Address
			if addr == "" {
				addr = "127.0.0.1"
			}
			port := req.Port

			for time.Now().Before(deadline) {
				if !proc.IsRunning() {
					exitErr := proc.ExitError()
					return rollbackAndJoin(fmt.Errorf("xray exited prematurely with candidate config: %v", exitErr))
				}

				if s.probe != nil {
					owned, err := s.probe.IsAddressPortOwnedByPID(pid, addr, port)
					if err == nil && owned {
						isReady = true
						break
					}
				} else {
					isReady = true
					break
				}
				time.Sleep(probeInterval)
			}

			if !isReady {
				return rollbackAndJoin(fmt.Errorf("candidate listener failed to bind %s:%d within %v", addr, port, readyTimeout))
			}
		}
	} else if !candidate.Enabled {
		_ = s.stopLocked()
	}

	// 3. Atomically write settings file
	settingsBytes, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return rollbackAndJoin(fmt.Errorf("marshal candidate settings: %w", err))
	}
	if err := atomicWriteFile(s.cfgPath(), settingsBytes, 0600); err != nil {
		return rollbackAndJoin(fmt.Errorf("write candidate settings: %w", err))
	}

	s.config = candidate

	// 4. Commit secret stager if present
	if s.secrets != nil {
		_ = s.secrets.CommitTx(context.Background(), txID)
	}

	// 5. State: committed
	manifest.Committed = true
	manifest.State = TxStateCommitted
	manifest.CandidateRunning = (s.proc != nil && s.proc.IsRunning())
	return writeManifest(txPath, &manifest)
}

// AbortPrepared cancels an uncommitted transaction in early stages (prepared, tested, backup_ready).
func (s *Service) AbortPrepared(txID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txPath := s.txDir(txID)
	manifestPath := filepath.Join(txPath, "snapshot-manifest.json")
	if !fileExists(manifestPath) {
		return nil
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest for abort: %w", err)
	}
	var manifest SnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("unmarshal manifest for abort: %w", err)
	}

	if manifest.State == TxStateCommitted || manifest.State == TxStateActiveReplaced || manifest.State == TxStateRestarting || manifest.State == TxStateVerifying {
		return fmt.Errorf("cannot abort transaction in state %s: requires rollback", manifest.State)
	}

	manifest.State = TxStateAborted
	_ = writeManifest(txPath, &manifest)

	if s.secrets != nil {
		_ = s.secrets.RollbackTx(context.Background(), txID)
	}

	return os.RemoveAll(txPath)
}

// FinalizePrepared cleans up transaction snapshot directory after all multi-component commits succeed.
func (s *Service) FinalizePrepared(txID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txPath := s.txDir(txID)
	if !dirExists(txPath) {
		return nil
	}

	manifestBytes, err := os.ReadFile(filepath.Join(txPath, "snapshot-manifest.json"))
	if err != nil {
		return fmt.Errorf("read manifest for finalize: %w", err)
	}
	var manifest SnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("unmarshal manifest for finalize: %w", err)
	}
	if manifest.State != TxStateCommitted {
		return fmt.Errorf("cannot finalize transaction %s in state %s: must be committed", txID, manifest.State)
	}

	return os.RemoveAll(txPath)
}

// RollbackPrepared rolls back a prepared or committed transaction snapshot.
func (s *Service) RollbackPrepared(txID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txPath := s.txDir(txID)
	if !dirExists(txPath) {
		return nil
	}

	manifestBytes, err := os.ReadFile(filepath.Join(txPath, "snapshot-manifest.json"))
	if err != nil {
		return fmt.Errorf("read snapshot manifest: %w", err)
	}

	var manifest SnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("unmarshal snapshot manifest: %w", err)
	}

	return s.rollbackLocked(txID, &manifest)
}

func (s *Service) rollbackLocked(txID string, manifest *SnapshotManifest) error {
	txPath := s.txDir(txID)

	// 1. Verify sha256 checksums of backups before restoring
	for filename, expectedHash := range manifest.Checksums {
		src := filepath.Join(txPath, filename)
		actualHash, err := sha256File(src)
		if err != nil || actualHash != expectedHash {
			return fmt.Errorf("snapshot corruption in %s: hash mismatch (want %s, got %s)", filename, expectedHash, actualHash)
		}
	}

	var rollbackErrs []error

	// 2. Restore settings file
	if _, ok := manifest.Checksums["xray-server-settings.json.bak"]; ok {
		src := filepath.Join(txPath, "xray-server-settings.json.bak")
		data, err := os.ReadFile(src)
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("read backup settings: %w", err))
		} else if err := atomicWriteFile(s.cfgPath(), data, 0600); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore settings: %w", err))
		}
	}

	// 3. Restore runtime config file
	if _, ok := manifest.Checksums["config.json.bak"]; ok {
		src := filepath.Join(txPath, "config.json.bak")
		data, err := os.ReadFile(src)
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("read backup runtime config: %w", err))
		} else if err := atomicWriteFile(s.xrayRuntimeConfigPath(), data, 0600); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore runtime config: %w", err))
		}
	}

	// If file restoration had any error, DO NOT attempt to restart the process!
	_ = s.stopLocked()
	if len(rollbackErrs) > 0 {
		return errors.Join(rollbackErrs...)
	}

	s.config = manifest.OldConfig

	// Rollback staged secrets
	if s.secrets != nil {
		_ = s.secrets.RollbackTx(context.Background(), txID)
	}

	// 4. Restore process state ONLY after files were successfully restored
	if manifest.WasRunning && fileExists(s.binPath) && s.config.Enabled {
		cmd := exec.Command(s.binPath, "run", "-c", s.xrayRuntimeConfigPath())
		proc := newManagedProc(cmd)
		if err := proc.Start(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restart xray during rollback: %w", err))
		} else {
			s.proc = proc
			pid := proc.PID()
			if pid <= 0 {
				_ = s.stopLocked()
				rollbackErrs = append(rollbackErrs, fmt.Errorf("invalid pid %d during rollback", pid))
			} else if err := s.writePIDRecord(pid, s.xrayRuntimeConfigPath()); err != nil {
				_ = s.stopLocked()
				rollbackErrs = append(rollbackErrs, fmt.Errorf("write PID record during rollback: %w", err))
			}
		}
	}

	if len(rollbackErrs) > 0 {
		return errors.Join(rollbackErrs...)
	}

	// Mark state rolled_back
	manifest.State = TxStateRolledBack
	_ = writeManifest(txPath, manifest)

	// Remove tx dir
	_ = os.RemoveAll(txPath)
	return nil
}

// RecoverPendingTransactions scans transactions dir on startup to recover incomplete states.
func (s *Service) RecoverPendingTransactions() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txBase := s.transactionsDir()
	if !dirExists(txBase) {
		return nil
	}

	entries, err := os.ReadDir(txBase)
	if err != nil {
		return fmt.Errorf("read transactions dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		txID := entry.Name()
		manifestPath := filepath.Join(txBase, txID, "snapshot-manifest.json")
		if !fileExists(manifestPath) {
			_ = os.RemoveAll(filepath.Join(txBase, txID))
			continue
		}

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest SnapshotManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		switch manifest.State {
		case TxStatePrepared, TxStateTested, TxStateBackupReady, TxStateAborted:
			// Early state or aborted: clean up safely
			manifest.State = TxStateAborted
			_ = writeManifest(filepath.Join(txBase, txID), &manifest)
			if s.secrets != nil {
				_ = s.secrets.RollbackTx(context.Background(), txID)
			}
			_ = os.RemoveAll(filepath.Join(txBase, txID))

		case TxStateActiveReplaced, TxStateRestarting, TxStateVerifying, TxStateRollingBack:
			// Late interrupted state: trigger recovery rollback
			if err := s.rollbackLocked(txID, &manifest); err != nil {
				s.recoveryRequired = true
				s.recoveryReason = fmt.Sprintf("crash recovery rollback failed for tx %s: %v", txID, err)
			}

		case TxStateCommitted:
			// Fully committed prior to crash
			_ = os.RemoveAll(filepath.Join(txBase, txID))

		case TxStateRecoveryRequired:
			s.recoveryRequired = true
			s.recoveryReason = fmt.Sprintf("pending transaction %s in recovery_required state", txID)
		}
	}

	return nil
}
