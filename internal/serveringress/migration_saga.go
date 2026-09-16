package serveringress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
	"github.com/hoaxisr/awg-manager/internal/xrayserver/xraybin"
)

type MigrationSagaPhase string

const (
	MigrationPhasePrepared                 MigrationSagaPhase = "prepared"
	MigrationPhaseManagedStopPrepared       MigrationSagaPhase = "managed_stop_prepared"
	MigrationPhaseManagedStopped            MigrationSagaPhase = "managed_stopped"
	MigrationPhaseLegacyScriptRestored      MigrationSagaPhase = "legacy_script_restored"
	MigrationPhaseLegacyStartedAndVerified   MigrationSagaPhase = "legacy_started_and_verified"
	MigrationPhaseDecisionCommitted         MigrationSagaPhase = "decision_committed"
	MigrationPhaseFinalized                 MigrationSagaPhase = "finalized"
)

type LegacyProbeResult int

const (
	LegacyProbeStopped LegacyProbeResult = iota
	LegacyProbeRunning
	LegacyProbeConflict
)

type MigrationSagaJournal struct {
	TransactionID             string                     `json:"transaction_id"`
	Phase                     MigrationSagaPhase         `json:"phase"`
	TargetGeneration          string                     `json:"target_generation"`
	ComponentTransactionIDs   map[string]string          `json:"component_transaction_ids"`
	InitScriptActive          string                     `json:"init_script_active"`
	InitScriptDisabled        string                     `json:"init_script_disabled"`
	InitialActiveExists       bool                       `json:"initial_active_exists"`
	InitialDisabledExists     bool                       `json:"initial_disabled_exists"`
	InitScriptWasRenamed      bool                       `json:"init_script_was_renamed"`
	InitScriptChecksum        string                     `json:"init_script_checksum"`
	LegacyListenAddress       string                     `json:"legacy_listen_address"`
	LegacyListenPort          int                        `json:"legacy_listen_port"`
	PreviousDecision          xrayserver.RuntimeDecision `json:"previous_decision"`
	PreviousXrayConfig        xrayserver.Config          `json:"previous_xray_config"`
	PreviousDispatcherConfig  cdndispatcher.Config       `json:"previous_dispatcher_config"`
	PreviousDispatcherRunning bool                       `json:"previous_dispatcher_running"`
	DesiredDispatcherRunning  bool                       `json:"desired_dispatcher_running"`
	PreviousLegacyRunning     bool                       `json:"previous_legacy_running"`
	LegacyStartedBySaga       bool                       `json:"legacy_started_by_saga"`
	Error                     string                     `json:"error,omitempty"`
	CreatedAt                 string                     `json:"created_at"`
	UpdatedAt                 string                     `json:"updated_at"`
}

func (c *Coordinator) sagaJournalPath() string {
	return filepath.Join(c.dataDir, "xray", "migration-saga-transaction.json")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "saga-tx-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tmp = nil

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename to %s: %w", path, err)
	}

	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func writeSagaJournal(path string, j *MigrationSagaJournal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0600)
}

func (c *Coordinator) advanceSagaPhase(current *MigrationSagaJournal, newPhase MigrationSagaPhase) (*MigrationSagaJournal, error) {
	next := *current
	next.Phase = newPhase
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := writeSagaJournal(c.sagaJournalPath(), &next); err != nil {
		return current, fmt.Errorf("advance saga phase %s: %w", newPhase, err)
	}
	return &next, nil
}

func (c *Coordinator) executeKeepLegacySaga(ctx context.Context, txID string) error {
	initActive := c.legacyInitActive
	if initActive == "" {
		initActive = "/opt/etc/init.d/S99xray-cdn"
	}
	initDisabled := c.legacyInitDisabled
	if initDisabled == "" {
		initDisabled = "/opt/etc/init.d/S99xray-cdn.disabled"
	}

	activeExists := fileExists(initActive)
	disabledExists := fileExists(initDisabled)
	if !activeExists && !disabledExists {
		return fmt.Errorf("legacy init script not found at %s or %s", initActive, initDisabled)
	}
	if activeExists && disabledExists {
		return fmt.Errorf("legacy init script conflict: both active (%s) and disabled (%s) exist; failing closed", initActive, initDisabled)
	}

	inspectPath := initActive
	if !activeExists {
		inspectPath = initDisabled
	}

	scriptData, err := os.ReadFile(inspectPath)
	if err != nil {
		return fmt.Errorf("read legacy init script: %w", err)
	}
	sum := sha256.Sum256(scriptData)
	scriptChecksum := hex.EncodeToString(sum[:])

	prevDecision, err := xrayserver.GetRuntimeDecision(c.dataDir)
	if err != nil {
		prevDecision = xrayserver.RuntimeDecision{
			ActiveGeneration: "new",
			MigrationStatus:  "migrated",
		}
	}

	var prevXray xrayserver.Config
	if c.xraySvc != nil {
		prevXray = c.xraySvc.GetConfig()
	}

	var prevDisp cdndispatcher.Config
	var prevDispRunning bool
	if c.dispatcher != nil {
		prevDisp = c.dispatcher.GetConfig()
		prevDispRunning = c.dispatcher.IsRunning()
	}

	tgActive := c.tgSvc != nil && c.tgSvc.GetConfig().Enabled
	desiredDispRunning := tgActive

	legacyCfgPath := c.legacyConfigPath
	if legacyCfgPath == "" {
		legacyCfgPath = "/opt/etc/xray-cdn/config.json"
	}

	disc, discErr := xrayserver.DiscoverLegacy(legacyCfgPath, inspectPath)
	if discErr != nil {
		return fmt.Errorf("discover legacy failed on %s: %w", legacyCfgPath, discErr)
	}

	legacyAddr := "127.0.0.1"
	legacyPort := 443
	if disc != nil && disc.Found {
		if disc.Topology == xrayserver.TopologyC {
			return fmt.Errorf("discover legacy config conflict on %s: %s", legacyCfgPath, disc.ConflictReason)
		}
		if disc.ListenPort <= 0 {
			return fmt.Errorf("discover legacy config %s missing valid listen port (%d)", legacyCfgPath, disc.ListenPort)
		}
		legacyPort = disc.ListenPort
		if disc.ListenAddress != "" && disc.ListenAddress != "0.0.0.0" {
			legacyAddr = disc.ListenAddress
		} else {
			legacyAddr = "127.0.0.1"
		}
	} else if disc != nil && !disc.Found {
		if c.legacyStateProbe == nil && c.legacyOwnershipProbe == nil {
			return fmt.Errorf("legacy configuration not found on %s", legacyCfgPath)
		}
	}

	probeRes, probeErr := c.probeLegacyState(legacyAddr, legacyPort, legacyCfgPath, 200*time.Millisecond)
	if probeRes == LegacyProbeConflict {
		return fmt.Errorf("legacy listener pre-flight conflict on %s:%d: %w", legacyAddr, legacyPort, probeErr)
	}
	prevLegacyRunning := (probeRes == LegacyProbeRunning)

	journal := &MigrationSagaJournal{
		TransactionID:             txID,
		Phase:                     MigrationPhasePrepared,
		TargetGeneration:          "legacy",
		ComponentTransactionIDs:   make(map[string]string),
		InitScriptActive:          initActive,
		InitScriptDisabled:        initDisabled,
		InitialActiveExists:       activeExists,
		InitialDisabledExists:     disabledExists,
		InitScriptChecksum:        scriptChecksum,
		LegacyListenAddress:       legacyAddr,
		LegacyListenPort:          legacyPort,
		PreviousDecision:          prevDecision,
		PreviousXrayConfig:        prevXray,
		PreviousDispatcherConfig:  prevDisp,
		PreviousDispatcherRunning: prevDispRunning,
		DesiredDispatcherRunning:  desiredDispRunning,
		PreviousLegacyRunning:     prevLegacyRunning,
		LegacyStartedBySaga:       false,
		CreatedAt:                 time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:                 time.Now().UTC().Format(time.RFC3339),
	}

	if err := writeSagaJournal(c.sagaJournalPath(), journal); err != nil {
		return fmt.Errorf("write prepared saga journal: %w", err)
	}

	if c.xraySvc != nil && prevXray.Enabled {
		stopCandidate := prevXray
		stopCandidate.Enabled = false
		xrayTxID := txID + "-xray"

		preparedTxID, pErr := c.xraySvc.PrepareCandidate(xrayTxID, stopCandidate)
		if pErr != nil {
			return c.rollbackSaga(journal, fmt.Errorf("prepare stop xray candidate: %w", pErr))
		}
		journal.ComponentTransactionIDs["xray"] = preparedTxID

		journal, err = c.advanceSagaPhase(journal, MigrationPhaseManagedStopPrepared)
		if err != nil {
			return c.rollbackSaga(journal, err)
		}

		if cErr := c.xraySvc.CommitPrepared(preparedTxID); cErr != nil {
			return c.rollbackSaga(journal, fmt.Errorf("commit stop xray: %w", cErr))
		}
	}

	journal, err = c.advanceSagaPhase(journal, MigrationPhaseManagedStopped)
	if err != nil {
		return c.rollbackSaga(journal, err)
	}

	if c.dispatcher != nil {
		if !desiredDispRunning && c.dispatcher.IsRunning() {
			if sErr := c.dispatcher.Stop(); sErr != nil {
				return c.rollbackSaga(journal, fmt.Errorf("stop dispatcher for legacy: %w", sErr))
			}
		}
	}

	if fileExists(initDisabled) {
		journal.InitScriptWasRenamed = true
		if err := writeSagaJournal(c.sagaJournalPath(), journal); err != nil {
			return c.rollbackSaga(journal, fmt.Errorf("write rename intent saga journal: %w", err))
		}
		if rErr := os.Rename(initDisabled, initActive); rErr != nil {
			return c.rollbackSaga(journal, fmt.Errorf("rename legacy init script: %w", rErr))
		}
	}
	if cErr := os.Chmod(initActive, 0755); cErr != nil {
		return c.rollbackSaga(journal, fmt.Errorf("chmod legacy init script: %w", cErr))
	}

	journal, err = c.advanceSagaPhase(journal, MigrationPhaseLegacyScriptRestored)
	if err != nil {
		return c.rollbackSaga(journal, err)
	}

	journal.LegacyStartedBySaga = true
	if err := writeSagaJournal(c.sagaJournalPath(), journal); err != nil {
		return c.rollbackSaga(journal, fmt.Errorf("write start intent saga journal: %w", err))
	}

	if c.legacyExecutor != nil {
		if out, sErr := c.legacyExecutor(ctx, initActive, "start"); sErr != nil {
			return c.rollbackSaga(journal, fmt.Errorf("start legacy init script (%s): %v, output: %s", initActive, sErr, strings.TrimSpace(string(out))))
		}
	} else {
		startCmd := exec.CommandContext(ctx, initActive, "start")
		if out, sErr := startCmd.CombinedOutput(); sErr != nil {
			return c.rollbackSaga(journal, fmt.Errorf("start legacy init script (%s): %v, output: %s", initActive, sErr, strings.TrimSpace(string(out))))
		}
	}

	probeTimeout := 5 * time.Second
	if c.legacyProbeTimeout > 0 {
		probeTimeout = c.legacyProbeTimeout
	}
	deadline := time.Now().Add(probeTimeout)
	var verifyErr error
	verified := false
	for time.Now().Before(deadline) {
		res, err := c.probeLegacyState(legacyAddr, legacyPort, legacyCfgPath, 100*time.Millisecond)
		if res == LegacyProbeRunning {
			verified = true
			break
		}
		if res == LegacyProbeConflict {
			verifyErr = err
			break
		}
		verifyErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if !verified {
		return c.rollbackSaga(journal, fmt.Errorf("verify legacy listener ownership on %s:%d: %w", legacyAddr, legacyPort, verifyErr))
	}

	journal, err = c.advanceSagaPhase(journal, MigrationPhaseLegacyStartedAndVerified)
	if err != nil {
		return c.rollbackSaga(journal, err)
	}

	newDecision := xrayserver.RuntimeDecision{
		ActiveGeneration:  "legacy",
		MigrationStatus:   "deferred",
		GenerationCounter: prevDecision.GenerationCounter + 1,
		Reason:            "user requested keep_legacy",
		DecidedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if dErr := xrayserver.SaveRuntimeDecision(c.dataDir, newDecision); dErr != nil {
		return c.rollbackSaga(journal, fmt.Errorf("save legacy runtime decision: %w", dErr))
	}

	journal, err = c.advanceSagaPhase(journal, MigrationPhaseDecisionCommitted)
	if err != nil {
		return c.rollbackSaga(journal, err)
	}

	for comp, compTxID := range journal.ComponentTransactionIDs {
		if comp == "xray" && c.xraySvc != nil {
			if fErr := c.xraySvc.FinalizePrepared(compTxID); fErr != nil {
				c.recoveryNeeded = true
				c.recoveryReason = fmt.Sprintf("finalize xray component (%s) failed: %v", compTxID, fErr)
				journal.Error = fErr.Error()
				_ = writeSagaJournal(c.sagaJournalPath(), journal)
				return fmt.Errorf("finalize xray component: %w", fErr)
			}
		}
	}

	journal, err = c.advanceSagaPhase(journal, MigrationPhaseFinalized)
	if err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("write finalized saga journal: %v", err)
		return err
	}

	if aErr := archiveJournal(c.sagaJournalPath(), "committed", txID); aErr != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("archive committed saga journal failed: %v", aErr)
		return fmt.Errorf("archive saga journal: %w", aErr)
	}

	return nil
}

func (c *Coordinator) rollbackSaga(j *MigrationSagaJournal, origErr error) error {
	var rbErrs []error
	initActive := j.InitScriptActive
	if initActive == "" {
		initActive = c.legacyInitActive
	}
	initDisabled := j.InitScriptDisabled
	if initDisabled == "" {
		initDisabled = c.legacyInitDisabled
	}

	if j.LegacyStartedBySaga && !j.PreviousLegacyRunning && initActive != "" && fileExists(initActive) {
		if c.legacyExecutor != nil {
			if _, err := c.legacyExecutor(context.Background(), initActive, "stop"); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("stop legacy daemon: %w", err))
			}
		} else {
			stopCmd := exec.Command(initActive, "stop")
			if err := stopCmd.Run(); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("stop legacy daemon: %w", err))
			}
		}
	}

	shouldRestoreDisabled := j.InitScriptWasRenamed || (j.InitialDisabledExists && !j.InitialActiveExists && fileExists(initActive))
	if shouldRestoreDisabled && initActive != "" && initDisabled != "" && fileExists(initActive) && !fileExists(initDisabled) {
		if err := os.Rename(initActive, initDisabled); err != nil {
			rbErrs = append(rbErrs, fmt.Errorf("rename init script back to disabled: %w", err))
		}
	}

	if xrayTxID, ok := j.ComponentTransactionIDs["xray"]; ok && c.xraySvc != nil {
		if err := c.xraySvc.RollbackPrepared(xrayTxID); err != nil {
			rbErrs = append(rbErrs, fmt.Errorf("rollback xray snapshot (%s): %w", xrayTxID, err))
		}
	}

	if c.dispatcher != nil {
		if j.PreviousDispatcherRunning && !c.dispatcher.IsRunning() {
			if err := c.dispatcher.Start(); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("start dispatcher during saga rollback: %w", err))
			}
		} else if !j.PreviousDispatcherRunning && c.dispatcher.IsRunning() {
			if err := c.dispatcher.Stop(); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("stop dispatcher during saga rollback: %w", err))
			}
		}
	}

	if err := xrayserver.SaveRuntimeDecision(c.dataDir, j.PreviousDecision); err != nil {
		rbErrs = append(rbErrs, fmt.Errorf("restore runtime decision: %w", err))
	}

	if origErr != nil {
		j.Error = origErr.Error()
	} else {
		j.Error = "incomplete migration saga interrupted by restart"
	}

	if len(rbErrs) > 0 {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("saga rollback failed: %v", errors.Join(rbErrs...))
		if origErr != nil {
			j.Error = fmt.Sprintf("%v; rollback errors: %v", origErr, errors.Join(rbErrs...))
		} else {
			j.Error = fmt.Sprintf("rollback errors: %v", errors.Join(rbErrs...))
		}
		if wErr := writeSagaJournal(c.sagaJournalPath(), j); wErr != nil {
			return errors.Join(origErr, errors.Join(rbErrs...), fmt.Errorf("write saga rollback journal: %w", wErr))
		}
		return errors.Join(origErr, errors.Join(rbErrs...))
	}

	if wErr := writeSagaJournal(c.sagaJournalPath(), j); wErr != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("write rollback saga journal failed: %v", wErr)
		return errors.Join(origErr, wErr)
	}

	if err := archiveJournal(c.sagaJournalPath(), "failed", j.TransactionID); err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("archive failed saga journal: %v", err)
		return errors.Join(origErr, err)
	}

	return origErr
}

func (c *Coordinator) recoverMigrationSagaLocked(ctx context.Context) error {
	sagaPath := c.sagaJournalPath()
	if !fileExists(sagaPath) {
		return nil
	}

	data, err := os.ReadFile(sagaPath)
	if err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("read migration saga journal: %v", err)
		return err
	}

	var j MigrationSagaJournal
	if err := json.Unmarshal(data, &j); err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("corrupt migration saga journal: %v", err)
		return err
	}

	switch j.Phase {
	case MigrationPhaseDecisionCommitted, MigrationPhaseFinalized:
		var finalizeErrs []error
		for comp, compTxID := range j.ComponentTransactionIDs {
			if comp == "xray" && c.xraySvc != nil {
				if err := c.xraySvc.FinalizePrepared(compTxID); err != nil {
					finalizeErrs = append(finalizeErrs, err)
				}
			}
		}
		if len(finalizeErrs) > 0 {
			c.recoveryNeeded = true
			c.recoveryReason = fmt.Sprintf("recovery finalize failed: %v", errors.Join(finalizeErrs...))
			return errors.Join(finalizeErrs...)
		}
		if err := archiveJournal(sagaPath, "committed", j.TransactionID); err != nil {
			c.recoveryNeeded = true
			c.recoveryReason = fmt.Sprintf("archive committed saga journal failed: %v", err)
			return fmt.Errorf("archive saga journal: %w", err)
		}
		return nil

	default:
		if rbErr := c.rollbackSaga(&j, nil); rbErr != nil {
			c.recoveryNeeded = true
			c.recoveryReason = fmt.Sprintf("saga recovery rollback failed: %v", rbErr)
			return rbErr
		}
		return nil
	}
}

func (c *Coordinator) probeLegacyState(addr string, port int, cfgPath string, timeout time.Duration) (LegacyProbeResult, error) {
	if c.legacyStateProbe != nil {
		return c.legacyStateProbe(addr, port)
	}

	procDir := c.getProcDir()
	dialFn := c.getDialTimeout()

	if c.legacyOwnershipProbe != nil {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			lookup, lErr := findListeningProcess(procDir, addr, port)
			if lErr != nil {
				return LegacyProbeConflict, lErr
			}
			if c.legacyOwnershipProbe(lookup.PID, port) {
				return LegacyProbeRunning, nil
			}
			if lookup.SocketFound {
				return LegacyProbeConflict, fmt.Errorf("ownership probe rejected PID %d (inode %s) on %s:%d", lookup.PID, lookup.SocketInode, addr, port)
			}
			time.Sleep(50 * time.Millisecond)
		}
		return LegacyProbeStopped, nil
	}

	target := net.JoinHostPort(addr, strconv.Itoa(port))
	conn, err := dialFn("tcp", target, timeout)
	if err != nil {
		if isConnectionRefused(err) {
			lookup, lErr := findListeningProcess(procDir, addr, port)
			if lErr != nil {
				return LegacyProbeConflict, fmt.Errorf("dial was refused on %s but procfs is unavailable to verify listening socket: %w", target, lErr)
			}
			if lookup.SocketFound {
				if lookup.PID > 0 {
					return LegacyProbeConflict, fmt.Errorf("dial was refused on %s but socket is registered with listening PID %d", target, lookup.PID)
				}
				return LegacyProbeConflict, fmt.Errorf("dial was refused on %s but listening socket is registered in procfs (inode %s, PID unmapped)", target, lookup.SocketInode)
			}
			return LegacyProbeStopped, nil
		}

		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return LegacyProbeConflict, fmt.Errorf("legacy probe dial timeout on %s: %w", target, err)
		}
		return LegacyProbeConflict, fmt.Errorf("legacy probe dial failed indeterminately on %s: %w", target, err)
	}
	_ = conn.Close()

	// Port is OPEN: ownership MUST be proven fail-closed
	lookup, lErr := findListeningProcess(procDir, addr, port)
	if lErr != nil {
		return LegacyProbeConflict, fmt.Errorf("port %s is open but procfs is unavailable to verify legacy process ownership: %w", target, lErr)
	}
	if !lookup.SocketFound {
		return LegacyProbeConflict, fmt.Errorf("port %s is open but no listening socket found in procfs", target)
	}
	if lookup.PID <= 0 {
		return LegacyProbeConflict, fmt.Errorf("port %s is open (inode %s) but listening PID could not be identified", target, lookup.SocketInode)
	}

	pid := lookup.PID

	cmdlinePath := filepath.Join(procDir, fmt.Sprintf("%d", pid), "cmdline")
	cmdlineData, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return LegacyProbeConflict, fmt.Errorf("failed to read %s for port %s: %w", cmdlinePath, target, err)
	}

	rawArgs := bytes.Split(cmdlineData, []byte{0})
	if len(rawArgs) == 0 || len(rawArgs[0]) == 0 {
		return LegacyProbeConflict, fmt.Errorf("port %s pid %d cmdline is empty", target, pid)
	}
	argv0 := string(rawArgs[0])
	argv0Base := strings.ToLower(filepath.Base(argv0))
	if !xraybin.IsXrayExecutableName(argv0Base) {
		return LegacyProbeConflict, fmt.Errorf("port %s occupied by alien non-xray process (pid %d argv[0]: %q)", target, pid, argv0)
	}

	if cfgPath != "" {
		if !matchLaunchConfig(cmdlineData, cfgPath) {
			cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
			return LegacyProbeConflict, fmt.Errorf("port %s occupied by xray process (pid %d) but launch arguments do not match config path %s (cmdline: %q)", target, pid, cfgPath, cmdline)
		}
	}

	exePath := filepath.Join(procDir, fmt.Sprintf("%d", pid), "exe")
	exe, err := os.Readlink(exePath)
	if err != nil {
		return LegacyProbeConflict, fmt.Errorf("port %s pid %d failed to verify executable link (%s): %w", target, pid, exePath, err)
	}
	exeBase := strings.ToLower(filepath.Base(exe))
	if !xraybin.IsXrayExecutableName(exeBase) {
		return LegacyProbeConflict, fmt.Errorf("port %s pid %d executable %q does not match xray", target, pid, exe)
	}

	return LegacyProbeRunning, nil
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			if errors.Is(sysErr.Err, syscall.ECONNREFUSED) {
				return true
			}
		}
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection refused")
}

func matchLaunchConfig(cmdlineData []byte, expectedCfgPath string) bool {
	if expectedCfgPath == "" {
		return true
	}
	expectedClean := filepath.Clean(expectedCfgPath)
	expectedReal, _ := filepath.EvalSymlinks(expectedClean)

	rawArgs := bytes.Split(cmdlineData, []byte{0})
	var args []string
	for _, a := range rawArgs {
		if len(a) > 0 {
			args = append(args, string(a))
		}
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		var candidate string

		if (arg == "-c" || arg == "-config" || arg == "--config") && i+1 < len(args) {
			candidate = args[i+1]
		} else if strings.HasPrefix(arg, "-c=") {
			candidate = strings.TrimPrefix(arg, "-c=")
		} else if strings.HasPrefix(arg, "-config=") {
			candidate = strings.TrimPrefix(arg, "-config=")
		} else if strings.HasPrefix(arg, "--config=") {
			candidate = strings.TrimPrefix(arg, "--config=")
		} else if !strings.HasPrefix(arg, "-") && strings.HasSuffix(strings.ToLower(arg), ".json") {
			candidate = arg
		}

		if candidate != "" {
			candClean := filepath.Clean(candidate)
			if candClean == expectedClean {
				return true
			}
			if expectedReal != "" {
				if candReal, err := filepath.EvalSymlinks(candClean); err == nil && candReal == expectedReal {
					return true
				}
			}
		}
	}
	return false
}

func (c *Coordinator) probeLegacyOwnership(addr string, port int, timeout time.Duration) error {
	res, err := c.probeLegacyState(addr, port, "", timeout)
	if res == LegacyProbeRunning {
		return nil
	}
	if res == LegacyProbeConflict {
		return err
	}
	return fmt.Errorf("legacy listener did not become available on %s:%d within %v", addr, port, timeout)
}

type listenerLookup = procnet.ListenerLookup

func findListeningProcess(procDir string, addr string, port int) (listenerLookup, error) {
	return procnet.FindListeningProcess(procDir, addr, port)
}

func findListeningPIDForAddressPort(procDir string, addr string, port int) int {
	return procnet.FindListeningPIDForAddressPort(procDir, addr, port)
}

func findListeningPIDForPort(port int) int {
	return procnet.FindListeningPIDForPort(port)
}
