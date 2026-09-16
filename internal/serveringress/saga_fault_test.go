package serveringress

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

func TestMigrationSaga_CrashBetweenRenameAndPhaseAdvance(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{
		cfg: xrayserver.Config{ListenPort: 9008},
	}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	coord.SetLegacyInitPaths(initActive, initDisabled)

	// Simulate crash: disabled script was renamed to active, but phase was still ManagedStopPrepared
	if err := os.WriteFile(initActive, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	txID := "saga-crash-rename-tx"
	j := MigrationSagaJournal{
		TransactionID:           txID,
		Phase:                   MigrationPhaseManagedStopPrepared,
		TargetGeneration:        "legacy",
		InitScriptActive:        initActive,
		InitScriptDisabled:      initDisabled,
		InitScriptWasRenamed:    true,
		InitialDisabledExists:   true,
		InitialActiveExists:     false,
		ComponentTransactionIDs: map[string]string{"xray": "xray-prep-tx"},
	}

	if err := writeSagaJournal(coord.sagaJournalPath(), &j); err != nil {
		t.Fatal(err)
	}

	// Run recovery
	ctx := context.Background()
	if err := coord.StartupRecovery(ctx); err != nil {
		t.Fatalf("StartupRecovery failed: %v", err)
	}

	// In crash recovery of an incomplete saga, rollback restores initial script state
	if !fileExists(initDisabled) {
		t.Errorf("expected %s to be restored during recovery", initDisabled)
	}
	if fileExists(initActive) {
		t.Errorf("expected %s to have been rolled back / renamed back", initActive)
	}

	// Managed candidate should have been rolled back
	if len(mockX.rollbackCalls) != 1 || mockX.rollbackCalls[0] != "xray-prep-tx" {
		t.Errorf("expected xray rollback call for xray-prep-tx, got %v", mockX.rollbackCalls)
	}

	// Active journal must be archived as failed
	if fileExists(coord.sagaJournalPath()) {
		t.Errorf("active saga journal should be archived after rollback")
	}
}

func TestMigrationSaga_PreExistingLegacyPreservedDuringRollback(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{
		cfg: xrayserver.Config{ListenPort: 9008},
	}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	_ = os.WriteFile(initActive, []byte("#!/bin/sh\nexit 0\n"), 0755)
	coord.SetLegacyInitPaths(initActive, initDisabled)

	var stopCalls []string
	coord.SetLegacyExecutor(func(ctx context.Context, script string, action string) ([]byte, error) {
		if action == "stop" {
			stopCalls = append(stopCalls, script)
		}
		return []byte("ok"), nil
	})

	// Pre-existing legacy daemon was running, saga did NOT start it
	j := MigrationSagaJournal{
		TransactionID:         "saga-preexist-tx",
		Phase:                 MigrationPhaseManagedStopPrepared,
		TargetGeneration:      "legacy",
		InitScriptActive:      initActive,
		InitScriptDisabled:    initDisabled,
		PreviousLegacyRunning: true,
		LegacyStartedBySaga:   false,
		InitialActiveExists:   true,
		InitialDisabledExists: false,
		InitScriptWasRenamed:  false,
	}

	if err := writeSagaJournal(coord.sagaJournalPath(), &j); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := coord.StartupRecovery(ctx); err != nil {
		t.Fatalf("StartupRecovery failed: %v", err)
	}

	// Verify stop was NOT called on pre-existing legacy daemon
	if len(stopCalls) > 0 {
		t.Errorf("expected zero stop calls on pre-existing legacy daemon, got %d", len(stopCalls))
	}
}

func TestMigrationSaga_SagaStartedLegacyDaemonStoppedDuringRollback(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{
		cfg: xrayserver.Config{ListenPort: 9008},
	}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	_ = os.WriteFile(initActive, []byte("#!/bin/sh\nexit 0\n"), 0755)
	coord.SetLegacyInitPaths(initActive, initDisabled)

	var stopCalls []string
	coord.SetLegacyExecutor(func(ctx context.Context, script string, action string) ([]byte, error) {
		if action == "stop" {
			stopCalls = append(stopCalls, script)
		}
		return []byte("ok"), nil
	})

	// Legacy was NOT running initially, saga started it
	j := MigrationSagaJournal{
		TransactionID:         "saga-started-tx",
		Phase:                 MigrationPhaseLegacyScriptRestored,
		TargetGeneration:      "legacy",
		InitScriptActive:      initActive,
		InitScriptDisabled:    initDisabled,
		PreviousLegacyRunning: false,
		LegacyStartedBySaga:   true,
		InitialActiveExists:   false,
		InitialDisabledExists: true,
		InitScriptWasRenamed:  true,
	}

	if err := writeSagaJournal(coord.sagaJournalPath(), &j); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := coord.StartupRecovery(ctx); err != nil {
		t.Fatalf("StartupRecovery failed: %v", err)
	}

	// Verify stop WAS called on saga-started legacy daemon
	if len(stopCalls) != 1 {
		t.Errorf("expected exactly 1 stop call on saga-started legacy daemon, got %d", len(stopCalls))
	}
}

func TestMigrationSaga_CrashRecoveryInDecisionCommitted(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{
		cfg: xrayserver.Config{ListenPort: 9008},
	}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	txID := "saga-committed-crash-tx"
	j := MigrationSagaJournal{
		TransactionID:           txID,
		Phase:                   MigrationPhaseDecisionCommitted,
		TargetGeneration:        "legacy",
		ComponentTransactionIDs: map[string]string{"xray": "xray-tx-999"},
	}

	if err := writeSagaJournal(coord.sagaJournalPath(), &j); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := coord.StartupRecovery(ctx); err != nil {
		t.Fatalf("StartupRecovery failed: %v", err)
	}

	// Idempotent finalization should have finalized the xray transaction
	if len(mockX.finalizeCalls) != 1 || mockX.finalizeCalls[0] != "xray-tx-999" {
		t.Errorf("expected finalize call for xray-tx-999, got %v", mockX.finalizeCalls)
	}

	// Active journal must be archived as committed
	if fileExists(coord.sagaJournalPath()) {
		t.Errorf("active saga journal should be archived after committed recovery")
	}

	req, _ := coord.IsRecoveryRequired()
	if req {
		t.Errorf("recovery should not be required after successful committed recovery")
	}
}

func TestMigrationSaga_ArchiveFailureDuringRecoverySetsRecoveryNeeded(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{
		cfg: xrayserver.Config{ListenPort: 9008},
	}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	txID := "saga-archive-fail-tx"
	j := MigrationSagaJournal{
		TransactionID:           txID,
		Phase:                   MigrationPhaseDecisionCommitted,
		TargetGeneration:        "legacy",
		ComponentTransactionIDs: map[string]string{"xray": "xray-tx-888"},
	}

	sagaPath := coord.sagaJournalPath()
	if err := writeSagaJournal(sagaPath, &j); err != nil {
		t.Fatal(err)
	}

	// Block archiving by creating a non-empty directory at the archive path
	archiveDest := sagaPath + ".committed." + txID
	if err := os.MkdirAll(archiveDest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDest, "blocker"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err := coord.StartupRecovery(ctx)
	if err == nil {
		t.Fatalf("expected StartupRecovery to return error on archive failure")
	}

	req, reason := coord.IsRecoveryRequired()
	if !req {
		t.Errorf("expected recoveryNeeded to be true after archive failure")
	}
	if reason == "" {
		t.Errorf("expected non-empty recovery reason")
	}
}

func TestMigrationSaga_FailClosedOwnershipProbe(t *testing.T) {
	dataDir := t.TempDir()
	coord := New(dataDir, nil, nil, nil)

	// Case 1: Custom mock probe returning false (simulating alien process or timeout)
	coord.SetLegacyOwnershipProbe(func(pid int, port int) bool {
		return false
	})

	err := coord.probeLegacyOwnership("127.0.0.1", 9999, 100*time.Millisecond)
	if err == nil {
		t.Errorf("expected probeLegacyOwnership to fail when probe returns false")
	}

	// Case 2: Custom probe validating matching PID
	var receivedPID int
	coord.SetLegacyOwnershipProbe(func(pid int, port int) bool {
		receivedPID = pid
		return pid == 12345
	})

	// probe with false
	_ = coord.probeLegacyOwnership("127.0.0.1", 9999, 50*time.Millisecond)
	if receivedPID == 12345 {
		t.Errorf("expected probe to not match fictitious pid 12345 by default")
	}
}

func TestMigrationSaga_InitScriptConflictFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{cfg: xrayserver.Config{ListenPort: 9008}}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	coord.SetLegacyInitPaths(initActive, initDisabled)

	activeContent := []byte("#!/bin/sh\necho active\n")
	disabledContent := []byte("#!/bin/sh\necho disabled\n")
	if err := os.WriteFile(initActive, activeContent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initDisabled, disabledContent, 0755); err != nil {
		t.Fatal(err)
	}

	var startStopCalls []string
	coord.SetLegacyExecutor(func(ctx context.Context, script string, action string) ([]byte, error) {
		startStopCalls = append(startStopCalls, action)
		return []byte("ok"), nil
	})

	err := coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil {
		t.Fatalf("expected ResolveMigration to fail when both active and disabled init scripts exist")
	}
	if !strings.Contains(err.Error(), "legacy init script conflict") {
		t.Errorf("expected conflict error, got: %v", err)
	}

	// Verify both files still exist with unchanged contents
	actData, err := os.ReadFile(initActive)
	if err != nil || string(actData) != string(activeContent) {
		t.Errorf("active script was modified or missing: %v", err)
	}
	disData, err := os.ReadFile(initDisabled)
	if err != nil || string(disData) != string(disabledContent) {
		t.Errorf("disabled script was modified or missing: %v", err)
	}

	// Verify no processes were started or stopped
	if len(startStopCalls) > 0 {
		t.Errorf("expected zero start/stop calls during conflict rejection, got %v", startStopCalls)
	}
}

func TestMigrationSaga_PreFlightConflictAlienProcessFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{cfg: xrayserver.Config{ListenPort: 9008}}
	mockDisp := &mockDispatcher{}
	mockTG := &mockTgWebProxy{}
	coord := New(dataDir, mockX, mockDisp, mockTG)

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	coord.SetLegacyInitPaths(initActive, initDisabled)
	if err := os.WriteFile(initDisabled, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Simulate alien process or indeterminate conflict on port
	coord.SetLegacyStateProbe(func(addr string, port int) (LegacyProbeResult, error) {
		return LegacyProbeConflict, errors.New("port occupied by alien nginx process")
	})

	var startStopCalls []string
	coord.SetLegacyExecutor(func(ctx context.Context, script string, action string) ([]byte, error) {
		startStopCalls = append(startStopCalls, action)
		return []byte("ok"), nil
	})

	err := coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil {
		t.Fatalf("expected ResolveMigration to fail on pre-flight listener conflict")
	}
	if !strings.Contains(err.Error(), "pre-flight conflict") {
		t.Errorf("expected pre-flight conflict error, got: %v", err)
	}

	// Disabled script must NOT have been renamed
	if !fileExists(initDisabled) || fileExists(initActive) {
		t.Errorf("init script was mutated despite pre-flight conflict")
	}

	// Zero start/stop calls
	if len(startStopCalls) > 0 {
		t.Errorf("expected zero start/stop calls on pre-flight conflict, got %v", startStopCalls)
	}
}

type mockConn struct {
	net.Conn
}

func (m *mockConn) Close() error { return nil }

type mockTimeoutError struct{}

func (e *mockTimeoutError) Error() string   { return "i/o timeout" }
func (e *mockTimeoutError) Timeout() bool   { return true }
func (e *mockTimeoutError) Temporary() bool { return true }

func TestMigrationSaga_ProbeDialTimeoutIsConflict(t *testing.T) {
	// 1. Connection refused is classified as refused
	if !isConnectionRefused(syscall.ECONNREFUSED) {
		t.Errorf("expected syscall.ECONNREFUSED to be classified as connection refused")
	}
	if !isConnectionRefused(errors.New("dial tcp 127.0.0.1:443: connect: connection refused")) {
		t.Errorf("expected connection refused error string to be classified as connection refused")
	}

	// 2. Timeout, generic errors, and extraneous errors containing the word "refused" are NOT connection refused
	if isConnectionRefused(context.DeadlineExceeded) {
		t.Errorf("DeadlineExceeded must not be classified as connection refused")
	}
	if isConnectionRefused(errors.New("i/o timeout")) {
		t.Errorf("i/o timeout must not be classified as connection refused")
	}
	if isConnectionRefused(errors.New("network is unreachable")) {
		t.Errorf("network is unreachable must not be classified as connection refused")
	}
	if isConnectionRefused(errors.New("request refused by administrator")) {
		t.Errorf("extraneous refused error must not be classified as connection refused")
	}
	if isConnectionRefused(errors.New("operation refused by security policy")) {
		t.Errorf("extraneous refused error must not be classified as connection refused")
	}

	// 3. Real production dial branch with dial timeout
	coord := New(t.TempDir(), nil, nil, nil)
	coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, &mockTimeoutError{}
	})
	res, err := coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict on production dial timeout path, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "legacy probe dial timeout") {
		t.Errorf("expected dial timeout error message, got: %v", err)
	}

	// 4. ECONNREFUSED with procfs unavailable -> LegacyProbeConflict
	emptyProc := t.TempDir()
	coord.SetProcDir(emptyProc)
	coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	})
	res, err = coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when procfs is unavailable, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "procfs is unavailable") {
		t.Errorf("expected procfs unavailable error message, got: %v", err)
	}

	// 5. ECONNREFUSED with procfs available, no listener -> LegacyProbeStopped
	procWithNet := t.TempDir()
	if err := os.MkdirAll(filepath.Join(procWithNet, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procWithNet, "net", "tcp"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	coord.SetProcDir(procWithNet)
	res, err = coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
	if res != LegacyProbeStopped {
		t.Errorf("expected LegacyProbeStopped when dial is refused and socket is absent, got %v", res)
	}
	if err != nil {
		t.Errorf("expected no error on LegacyProbeStopped, got: %v", err)
	}

	// 6. ECONNREFUSED with procfs available, but socket registered -> LegacyProbeConflict
	tcpContent := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 55555 1 0000000000000000 100 0 0 10 0\n"
	if err := os.WriteFile(filepath.Join(procWithNet, "net", "tcp"), []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procWithNet, "888", "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[55555]", filepath.Join(fdDir, "3")); err == nil {
		res, err = coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
		if res != LegacyProbeConflict {
			t.Errorf("expected LegacyProbeConflict when socket is registered, got %v", res)
		}
		if err == nil || !strings.Contains(err.Error(), "socket is registered with listening PID 888") {
			t.Errorf("expected socket registered error, got: %v", err)
		}
	}
}

func TestMigrationSaga_DiscoverLegacyErrorFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	mockX := &mockXray{cfg: xrayserver.Config{ListenPort: 9008}}
	coord := New(dataDir, mockX, &mockDispatcher{}, &mockTgWebProxy{})

	initActive := filepath.Join(dataDir, "S99xray-cdn")
	initDisabled := filepath.Join(dataDir, "S99xray-cdn.disabled")
	coord.SetLegacyInitPaths(initActive, initDisabled)
	if err := os.WriteFile(initDisabled, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Case 1: Legacy config file points to non-existent custom path
	badCfg := filepath.Join(dataDir, "corrupted.json")
	coord.SetLegacyConfigPath(badCfg)

	// Since file does not exist, DiscoverLegacy returns !Found, which fails closed
	err := coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil || !strings.Contains(err.Error(), "legacy configuration not found") {
		t.Fatalf("expected legacy configuration not found error, got: %v", err)
	}

	// Case 2: Legacy config has malformed JSON
	if err := os.WriteFile(badCfg, []byte("{ malformed json"), 0644); err != nil {
		t.Fatal(err)
	}
	err = coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil || !strings.Contains(err.Error(), "discover legacy config conflict") {
		t.Fatalf("expected discover legacy conflict on malformed JSON, got: %v", err)
	}

	// Case 3: Legacy config has TopologyC (e.g. zero VLESS inbounds)
	emptyInbounds := `{"inbounds":[{"protocol":"dokodemo-door","port":1234}]}`
	if err := os.WriteFile(badCfg, []byte(emptyInbounds), 0644); err != nil {
		t.Fatal(err)
	}
	err = coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil || !strings.Contains(err.Error(), "discover legacy config conflict") {
		t.Fatalf("expected discover legacy conflict on missing vless inbound, got: %v", err)
	}

	// Case 4: Legacy config has invalid port 0
	invalidPort := `{"inbounds":[{"protocol":"vless","port":0,"streamSettings":{"xhttpSettings":{"host":"domain.com"}}}]}`
	if err := os.WriteFile(badCfg, []byte(invalidPort), 0644); err != nil {
		t.Fatal(err)
	}
	err = coord.ResolveMigration(context.Background(), "keep_legacy", nil)
	if err == nil || !strings.Contains(err.Error(), "missing valid listen port") {
		t.Fatalf("expected invalid port error, got: %v", err)
	}
}

func TestMigrationSaga_ExactConfigVerification(t *testing.T) {
	// Test ipToProcHex
	if hex := procnet.IPToProcHex("127.0.0.1"); hex != "0100007F" {
		t.Errorf("expected 0100007F, got %s", hex)
	}
	if hex := procnet.IPToProcHex("0.0.0.0"); hex != "00000000" {
		t.Errorf("expected 00000000, got %s", hex)
	}
	if hex := procnet.IPToProcHex("invalid-ip"); hex != "" {
		t.Errorf("expected empty string on invalid ip, got %s", hex)
	}

	// Test matchLaunchConfig exact arguments
	targetCfg := "/opt/etc/xray-cdn/config.json"

	// Valid matching cmdlines
	valid1 := []byte("xray\x00run\x00-c\x00/opt/etc/xray-cdn/config.json\x00")
	if !matchLaunchConfig(valid1, targetCfg) {
		t.Errorf("expected valid1 (-c) to match")
	}

	valid2 := []byte("xray\x00run\x00-config\x00/opt/etc/xray-cdn/config.json\x00")
	if !matchLaunchConfig(valid2, targetCfg) {
		t.Errorf("expected valid2 (-config) to match")
	}

	valid3 := []byte("xray\x00run\x00-c=/opt/etc/xray-cdn/config.json\x00")
	if !matchLaunchConfig(valid3, targetCfg) {
		t.Errorf("expected valid3 (-c=) to match")
	}

	valid4 := []byte("xray\x00run\x00--config=/opt/etc/xray-cdn/config.json\x00")
	if !matchLaunchConfig(valid4, targetCfg) {
		t.Errorf("expected valid4 (--config=) to match")
	}

	valid5 := []byte("xray\x00run\x00/opt/etc/xray-cdn/config.json\x00")
	if !matchLaunchConfig(valid5, targetCfg) {
		t.Errorf("expected valid5 (direct path argument) to match")
	}

	// Non-matching cmdlines: must NOT match by config.json basename or directory
	alienSameBasename := []byte("xray\x00run\x00-c\x00/tmp/other/config.json\x00")
	if matchLaunchConfig(alienSameBasename, targetCfg) {
		t.Errorf("alien process with same basename config.json must NOT match target config")
	}

	alienSameDir := []byte("xray\x00run\x00-c\x00/opt/etc/xray-cdn/other.json\x00")
	if matchLaunchConfig(alienSameDir, targetCfg) {
		t.Errorf("alien process in same directory must NOT match target config")
	}

	alienNoConfig := []byte("xray\x00run\x00--test\x00")
	if matchLaunchConfig(alienNoConfig, targetCfg) {
		t.Errorf("cmdline with no config arguments must NOT match target config")
	}
}

func TestMigrationSaga_ExecutableVerificationFailsClosed(t *testing.T) {
	procDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	tcpContent := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 77777 1 0000000000000000 100 0 0 10 0\n"
	if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	pidDir := filepath.Join(procDir, "303")
	fdDir := filepath.Join(pidDir, "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[77777]", filepath.Join(fdDir, "4")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	cmdline := []byte("xray\x00run\x00-c\x00/opt/etc/xray-cdn/config.json\x00")
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), cmdline, 0644); err != nil {
		t.Fatal(err)
	}

	coord := New(t.TempDir(), nil, nil, nil)
	coord.SetProcDir(procDir)
	coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return &mockConn{}, nil
	})

	// Scenario 1: exe link missing -> fails closed
	res, err := coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when exe link is missing, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "failed to verify executable link") {
		t.Errorf("expected failed to verify executable link error, got: %v", err)
	}

	// Scenario 2: exe link points to alien process (nginx) -> fails closed
	exeLink := filepath.Join(pidDir, "exe")
	if err := os.Symlink("/usr/sbin/nginx", exeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, err = coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when exe is alien, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "does not match xray") {
		t.Errorf("expected executable does not match xray error, got: %v", err)
	}

	// Scenario 2a: exe link points to /opt/bin/notxray (substring "xray" trap) -> fails closed
	_ = os.Remove(exeLink)
	if err := os.Symlink("/opt/bin/notxray", exeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, err = coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when exe is /opt/bin/notxray, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "does not match xray") {
		t.Errorf("expected does not match xray error for notxray, got: %v", err)
	}

	// Scenario 2b: exe link points to /opt/bin/xray-wrapper (substring prefix trap) -> fails closed
	_ = os.Remove(exeLink)
	if err := os.Symlink("/opt/bin/xray-wrapper", exeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, err = coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when exe is /opt/bin/xray-wrapper, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "does not match xray") {
		t.Errorf("expected does not match xray error for xray-wrapper, got: %v", err)
	}

	// Scenario 2c: alien cmdline containing config path "/opt/etc/xray-cdn/config.json" with exe /opt/bin/other -> fails closed
	_ = os.Remove(exeLink)
	if err := os.Symlink("/opt/bin/other", exeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	alienCmdline := []byte("/usr/bin/python\x00/opt/etc/xray-cdn/config.json\x00")
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), alienCmdline, 0644); err != nil {
		t.Fatal(err)
	}
	res, err = coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when alien process specifies config in cmdline, got %v", res)
	}
	if err == nil || (!strings.Contains(err.Error(), "alien non-xray process") && !strings.Contains(err.Error(), "does not match xray")) {
		t.Errorf("expected alien process error for /opt/bin/other, got: %v", err)
	}

	// Restore valid cmdline with exact xray binary name
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), cmdline, 0644); err != nil {
		t.Fatal(err)
	}

	// Scenario 3: exe link points to /opt/bin/xray -> running verified
	_ = os.Remove(exeLink)
	if err := os.Symlink("/opt/bin/xray", exeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, err = coord.probeLegacyState("127.0.0.1", 443, "/opt/etc/xray-cdn/config.json", 100*time.Millisecond)
	if res != LegacyProbeRunning {
		t.Errorf("expected LegacyProbeRunning when exe matches xray, got %v (err: %v)", res, err)
	}
	if err != nil {
		t.Errorf("expected no error on LegacyProbeRunning, got: %v", err)
	}
}

func TestMigrationSaga_ProcfsListeningSocketUnmappedPIDFailsClosed(t *testing.T) {
	procDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	// net/tcp contains LISTEN line for 127.0.0.1:443 with inode 77777
	tcpContent := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 77777 1 0000000000000000 100 0 0 10 0\n"
	if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	// PID directory exists, but NO matching fd symlink for inode 77777 exists
	pidDir := filepath.Join(procDir, "404")
	fdDir := filepath.Join(pidDir, "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}

	coord := New(t.TempDir(), nil, nil, nil)
	coord.SetProcDir(procDir)
	coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	})

	// Probe when dial returns ECONNREFUSED but listening socket exists without mapped PID
	res, err := coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
	if res != LegacyProbeConflict {
		t.Errorf("expected LegacyProbeConflict when socket is in net/tcp but PID is unmapped, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "PID unmapped") {
		t.Errorf("expected PID unmapped error, got: %v", err)
	}
}

func TestMigrationSaga_SocketTableErrorsAndAddressFamilies(t *testing.T) {
	// Subtest 1: net/tcp is a directory -> LegacyProbeConflict
	t.Run("SocketTableIsDirectory", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net", "tcp"), 0755); err != nil {
			t.Fatal(err)
		}
		coord := New(t.TempDir(), nil, nil, nil)
		coord.SetProcDir(procDir)
		coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
			return nil, syscall.ECONNREFUSED
		})
		res, err := coord.probeLegacyState("127.0.0.1", 443, "", 100*time.Millisecond)
		if res != LegacyProbeConflict {
			t.Errorf("expected LegacyProbeConflict when net/tcp is a directory, got %v", res)
		}
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Errorf("expected directory error, got: %v", err)
		}
	})

	// Subtest 2: IPv6 target with missing net/tcp6 -> LegacyProbeConflict
	t.Run("IPv6TargetMissingTCP6Table", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
			t.Fatal(err)
		}
		// Provide only net/tcp (IPv4)
		if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		coord := New(t.TempDir(), nil, nil, nil)
		coord.SetProcDir(procDir)
		coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
			return nil, syscall.ECONNREFUSED
		})
		res, err := coord.probeLegacyState("::1", 443, "", 100*time.Millisecond)
		if res != LegacyProbeConflict {
			t.Errorf("expected LegacyProbeConflict when probing IPv6 without net/tcp6, got %v", res)
		}
		if err == nil || !strings.Contains(err.Error(), "ipv6 socket table") {
			t.Errorf("expected ipv6 socket table error, got: %v", err)
		}
	})

	// Subtest 3: IPv6 target with net/tcp6 as a directory -> LegacyProbeConflict
	t.Run("IPv6TargetTCP6IsDirectory", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net", "tcp6"), 0755); err != nil {
			t.Fatal(err)
		}
		coord := New(t.TempDir(), nil, nil, nil)
		coord.SetProcDir(procDir)
		coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
			return nil, syscall.ECONNREFUSED
		})
		res, err := coord.probeLegacyState("::1", 443, "", 100*time.Millisecond)
		if res != LegacyProbeConflict {
			t.Errorf("expected LegacyProbeConflict when net/tcp6 is a directory, got %v", res)
		}
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Errorf("expected directory error, got: %v", err)
		}
	})

	// Subtest 4: IPv6 target with clean net/tcp6 and no listener -> LegacyProbeStopped
	t.Run("IPv6TargetNoListenerStopped", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(procDir, "net", "tcp6"), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		coord := New(t.TempDir(), nil, nil, nil)
		coord.SetProcDir(procDir)
		coord.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
			return nil, syscall.ECONNREFUSED
		})
		res, err := coord.probeLegacyState("::1", 443, "", 100*time.Millisecond)
		if res != LegacyProbeStopped {
			t.Errorf("expected LegacyProbeStopped when IPv6 listener is absent, got %v (err: %v)", res, err)
		}
		if err != nil {
			t.Errorf("expected no error on LegacyProbeStopped, got: %v", err)
		}
	})
}

