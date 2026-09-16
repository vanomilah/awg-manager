package serveringress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

type XrayComponent interface {
	GetConfig() xrayserver.Config
	GetStatus() xrayserver.Status
	PrepareCandidate(txID string, candidate xrayserver.Config) (string, error)
	CommitPrepared(txID string) error
	FinalizePrepared(txID string) error
	RollbackPrepared(txID string) error
	Restart() error
}

type DispatcherComponent interface {
	GetConfig() cdndispatcher.Config
	IsRunning() bool
	Start() error
	Stop() error
	ApplyConfig(cfg cdndispatcher.Config) error
	Reconfigure(cfg cdndispatcher.Config) error
	PrepareCandidate(txID string, candidate cdndispatcher.Candidate) (string, error)
	CommitPrepared(txID string) error
	RollbackPrepared(txID string) error
	FinalizePrepared(txID string) error
}

type TgWebProxyComponent interface {
	GetConfig() tgwebproxy.PublicConfig
	GetInternalConfig() tgwebproxy.Config
	GetStatus() tgwebproxy.Status
	ApplyManagedIngress(cfg tgwebproxy.ManagedIngressConfig) error
	UpdateConfig(cfg tgwebproxy.Config) error
	PrepareCandidate(txID string, candidate tgwebproxy.Config) (string, error)
	CommitPrepared(txID string) error
	RollbackPrepared(txID string) error
	FinalizePrepared(txID string) error
}

type Coordinator struct {
	mu                   sync.Mutex
	txMu                 sync.Mutex
	dataDir              string
	xraySvc              XrayComponent
	dispatcher           DispatcherComponent
	tgSvc                TgWebProxyComponent
	lock                 IngressLocker
	recoveryNeeded       bool
	recoveryReason       string
	legacyInitActive     string
	legacyInitDisabled   string
	legacyConfigPath     string
	procDir              string
	dialTimeout          func(network, address string, timeout time.Duration) (net.Conn, error)
	legacyOwnershipProbe func(pid int, port int) bool
	legacyStateProbe     func(addr string, port int) (LegacyProbeResult, error)
	legacyExecutor       func(ctx context.Context, script string, action string) ([]byte, error)
	legacyProbeTimeout   time.Duration
	onPhaseChange        func(phase JournalPhase) error
	seamsMu              sync.RWMutex
	writeJournalFn       func(path string, j *TransactionJournal) error
	archiveJournalFn     func(srcPath, reason, txID string) error
}

func New(dataDir string, xray XrayComponent, disp DispatcherComponent, tg TgWebProxyComponent) *Coordinator {
	lockPath := filepath.Join(dataDir, "xray", "server-ingress.lock")
	return &Coordinator{
		dataDir:            dataDir,
		xraySvc:            xray,
		dispatcher:         disp,
		tgSvc:              tg,
		lock:               NewFileLock(lockPath),
		legacyInitActive:   "/opt/etc/init.d/S99xray-cdn",
		legacyInitDisabled: "/opt/etc/init.d/S99xray-cdn.disabled",
		legacyConfigPath:   "/opt/etc/xray-cdn/config.json",
		procDir:            "/proc",
		dialTimeout:        net.DialTimeout,
	}
}

// SetLock configures a custom ingress locker for testing.
func (c *Coordinator) SetLock(l IngressLocker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lock = l
}

// SetOnPhaseChange configures a hook callback before committing phase for testing.
func (c *Coordinator) SetOnPhaseChange(fn func(phase JournalPhase) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPhaseChange = fn
}

func (c *Coordinator) writeJournal(path string, j *TransactionJournal) error {
	c.seamsMu.RLock()
	fn := c.writeJournalFn
	c.seamsMu.RUnlock()
	if fn != nil {
		return fn(path, j)
	}
	return writeJournal(path, j)
}

func (c *Coordinator) archiveJournal(srcPath, reason, txID string) error {
	c.seamsMu.RLock()
	fn := c.archiveJournalFn
	c.seamsMu.RUnlock()
	if fn != nil {
		return fn(srcPath, reason, txID)
	}
	return archiveJournal(srcPath, reason, txID)
}

// SetJournalSeamsForTest configures custom journal write and archive functions for fault injection in tests.
func (c *Coordinator) SetJournalSeamsForTest(writeFn func(path string, j *TransactionJournal) error, archiveFn func(srcPath, reason, txID string) error) {
	c.seamsMu.Lock()
	defer c.seamsMu.Unlock()
	c.writeJournalFn = writeFn
	c.archiveJournalFn = archiveFn
}

// SetLegacyInitPaths configures legacy init script paths for testing.
func (c *Coordinator) SetLegacyInitPaths(active, disabled string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyInitActive = active
	c.legacyInitDisabled = disabled
}

// SetLegacyConfigPath configures the legacy configuration file path for testing.
func (c *Coordinator) SetLegacyConfigPath(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyConfigPath = p
}

// SetProcDir configures a custom procfs root directory for testing.
func (c *Coordinator) SetProcDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.procDir = dir
}

// SetDialTimeout configures a custom dialer function for testing.
func (c *Coordinator) SetDialTimeout(d func(network, address string, timeout time.Duration) (net.Conn, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dialTimeout = d
}

func (c *Coordinator) getProcDir() string {
	if c.procDir != "" {
		return c.procDir
	}
	return "/proc"
}

func (c *Coordinator) getDialTimeout() func(network, address string, timeout time.Duration) (net.Conn, error) {
	if c.dialTimeout != nil {
		return c.dialTimeout
	}
	return net.DialTimeout
}

// SetLegacyOwnershipProbe configures a custom ownership probe function for testing.
func (c *Coordinator) SetLegacyOwnershipProbe(p func(pid int, port int) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyOwnershipProbe = p
}

// SetLegacyStateProbe configures a custom tri-state legacy probe function for testing.
func (c *Coordinator) SetLegacyStateProbe(p func(addr string, port int) (LegacyProbeResult, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyStateProbe = p
}

// SetLegacyExecutor configures a custom init script executor for testing.
func (c *Coordinator) SetLegacyExecutor(e func(ctx context.Context, script string, action string) ([]byte, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyExecutor = e
}

// SetLegacyProbeTimeout configures a custom timeout for legacy listener verification for testing.
func (c *Coordinator) SetLegacyProbeTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyProbeTimeout = d
}

func (c *Coordinator) setRecovery(needed bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recoveryNeeded = needed
	c.recoveryReason = reason
}

func (c *Coordinator) isRecoveryNeeded() (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recoveryNeeded, c.recoveryReason
}

func (c *Coordinator) CanAutoStart() (bool, string) {
	needed, reason := c.isRecoveryNeeded()
	if needed {
		return false, reason
	}
	return true, ""
}

func (c *Coordinator) journalPath() string {
	return filepath.Join(c.dataDir, "xray", "server-ingress-transaction.json")
}

func (c *Coordinator) IsRecoveryRequired() (bool, string) {
	return c.isRecoveryNeeded()
}

func (c *Coordinator) computeFingerprints() map[string]string {
	fp := make(map[string]string)

	if c.xraySvc != nil {
		data, _ := json.Marshal(c.xraySvc.GetConfig())
		h := sha256.Sum256(data)
		fp["xray"] = hex.EncodeToString(h[:])
	}
	if c.dispatcher != nil {
		data, _ := json.Marshal(c.dispatcher.GetConfig())
		h := sha256.Sum256(data)
		fp["dispatcher"] = hex.EncodeToString(h[:])
	}
	if c.tgSvc != nil {
		data, _ := json.Marshal(c.tgSvc.GetConfig())
		h := sha256.Sum256(data)
		fp["tgwebproxy"] = hex.EncodeToString(h[:])
	}
	return fp
}

func (c *Coordinator) getCurrentTopology() IngressTopology {
	topo := IngressTopology{}

	if c.xraySvc != nil {
		xc := c.xraySvc.GetConfig()
		topo.XrayEnabled = xc.Enabled
		topo.XrayAddress = xc.ListenAddress
		if topo.XrayAddress == "" {
			topo.XrayAddress = "127.0.0.1"
		}
		topo.XrayPort = xc.ListenPort
		if topo.XrayPort <= 0 {
			topo.XrayPort = 9008
		}
		topo.XrayPublicHostname = xc.PublicDomain
		topo.XrayPublicPort = xc.PublicPort
		if topo.XrayPublicPort <= 0 {
			topo.XrayPublicPort = 443
		}
		topo.XrayPathPrefix = cdndispatcher.NormalizePathPrefix(xc.Path)
		if topo.PublicHostname == "" {
			topo.PublicHostname = xc.PublicDomain
		}
	}

	if c.dispatcher != nil {
		dc := c.dispatcher.GetConfig()
		topo.DispatcherEnabled = c.dispatcher.IsRunning()
		topo.DispatcherAddress = dc.ListenAddr
		if dc.ListenAddr != "" {
			_, portStr, err := net.SplitHostPort(dc.ListenAddr)
			if err == nil {
				if p, err := strconv.Atoi(portStr); err == nil {
					topo.DispatcherPort = p
				}
			} else if strings.HasPrefix(dc.ListenAddr, ":") {
				if p, err := strconv.Atoi(strings.TrimPrefix(dc.ListenAddr, ":")); err == nil {
					topo.DispatcherPort = p
				}
			}
		}
		if topo.DispatcherPort <= 0 {
			topo.DispatcherPort = 9009
		}
		if dc.PublicHostname != "" && topo.PublicHostname == "" {
			topo.PublicHostname = dc.PublicHostname
		}
	}

	if c.tgSvc != nil {
		tc := c.tgSvc.GetInternalConfig()
		topo.TgEnabled = tc.Enabled
		topo.TgScenario = tc.Scenario
		topo.TgDirectAddress = tc.DirectHost
		topo.TgDirectPort = tc.DirectPort
		if tc.Backend != "" {
			host, portStr, err := net.SplitHostPort(tc.Backend)
			if err == nil {
				topo.TgRawAddress = host
				if p, err := strconv.Atoi(portStr); err == nil {
					topo.TgRawPort = p
				}
			}
		}
		if topo.TgRawAddress == "" {
			topo.TgRawAddress = "127.0.0.1"
		}
		if topo.TgRawPort <= 0 {
			topo.TgRawPort = 2398
		}
		topo.TgWebAddress = "127.0.0.1"
		topo.TgWebPort = tc.ListenPort
		if topo.TgWebPort <= 0 {
			topo.TgWebPort = 8085
		}
		topo.TgPublicHostname = tc.PublicHostname
		topo.TgPort = tc.ListenPort
		if topo.PublicHostname == "" {
			topo.PublicHostname = tc.PublicHostname
		}
	}

	return topo
}

func (c *Coordinator) calculateAffected(prev, desired IngressTopology) []string {
	var affected []string

	xrayChanged := prev.XrayEnabled != desired.XrayEnabled ||
		prev.XrayAddress != desired.XrayAddress ||
		prev.XrayPort != desired.XrayPort ||
		prev.XrayPathPrefix != desired.XrayPathPrefix ||
		prev.XrayPublicHostname != desired.XrayPublicHostname ||
		prev.XrayPublicPort != desired.XrayPublicPort ||
		prev.PublicHostname != desired.PublicHostname

	if xrayChanged {
		affected = append(affected, "xray")
	}

	tgChanged := prev.TgEnabled != desired.TgEnabled ||
		prev.TgScenario != desired.TgScenario ||
		prev.TgDirectAddress != desired.TgDirectAddress ||
		prev.TgDirectPort != desired.TgDirectPort ||
		prev.TgRawAddress != desired.TgRawAddress ||
		prev.TgRawPort != desired.TgRawPort ||
		prev.TgWebAddress != desired.TgWebAddress ||
		prev.TgWebPort != desired.TgWebPort ||
		prev.TgPublicHostname != desired.TgPublicHostname ||
		prev.TgPort != desired.TgPort ||
		prev.PublicHostname != desired.PublicHostname

	if tgChanged {
		affected = append(affected, "tgwebproxy")
	}

	dispChanged := prev.DispatcherEnabled != desired.DispatcherEnabled ||
		prev.DispatcherPort != desired.DispatcherPort ||
		prev.DispatcherAddress != desired.DispatcherAddress ||
		prev.PublicHostname != desired.PublicHostname ||
		prev.XrayPublicHostname != desired.XrayPublicHostname ||
		prev.TgPublicHostname != desired.TgPublicHostname ||
		prev.XrayPathPrefix != desired.XrayPathPrefix ||
		prev.XrayAddress != desired.XrayAddress ||
		prev.XrayPort != desired.XrayPort ||
		prev.TgWebPort != desired.TgWebPort ||
		prev.TgPort != desired.TgPort

	if dispChanged {
		affected = append(affected, "dispatcher")
	}

	return affected
}

func (c *Coordinator) withIngressLock(txID string, fn func() error) (retErr error) {
	c.txMu.Lock()
	defer c.txMu.Unlock()

	if err := c.lock.Lock(txID); err != nil {
		return err
	}
	defer func() {
		if err := c.lock.Unlock(); err != nil {
			c.setRecovery(true, fmt.Sprintf("unlock failed for tx %s: %v", txID, err))
			unlockErr := fmt.Errorf("unlock ingress transaction %s: %w", txID, err)
			if retErr != nil {
				retErr = errors.Join(retErr, unlockErr)
			} else {
				retErr = unlockErr
			}
		}
	}()

	retErr = fn()
	return retErr
}

// WithIngressLock acquires the shared server ingress lock and executes fn.
func (c *Coordinator) WithIngressLock(txID string, fn func() error) error {
	return c.withIngressLock(txID, fn)
}

// StartupRecovery checks for and recovers pending transactions after an unexpected restart.
func (c *Coordinator) StartupRecovery(ctx context.Context) error {
	txID := fmt.Sprintf("ing-rec-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		// 1. Recover migration saga first
		if err := c.recoverMigrationSagaLocked(ctx); err != nil {
			return fmt.Errorf("recover migration saga: %w", err)
		}
		// 2. Recover standard ingress transaction second
		return c.recoverIngressJournalLocked(ctx)
	})
}

func (c *Coordinator) recoverIngressJournalLocked(ctx context.Context) error {
	jPath := c.journalPath()
	j, err := readJournal(jPath)
	if err != nil {
		c.setRecovery(true, fmt.Sprintf("corrupt journal: %v", err))
		return ErrCorruptJournal
	}
	if j == nil {
		// No active journal, everything clean
		return nil
	}

	if j.Phase == PhaseCommitted {
		if len(j.Fingerprints) > 0 {
			currentFP := c.computeFingerprints()
			for k, expected := range j.Fingerprints {
				if actual, ok := currentFP[k]; ok && actual != expected {
					c.setRecovery(true, fmt.Sprintf("fingerprint mismatch for component %s: expected %s, got %s", k, expected, actual))
					return ErrRecoveryConflict
				}
			}
		}

		// At PhaseCommitted, verify idempotent finalization and archive journal. NEVER rolls back!
		var finErrs []error
		if c.xraySvc != nil {
			xrayTxID := j.ComponentTransactionIDs["xray"]
			if xrayTxID == "" {
				xrayTxID = j.TransactionID
			}
			if err := c.xraySvc.FinalizePrepared(xrayTxID); err != nil {
				finErrs = append(finErrs, err)
			}
		}
		if c.tgSvc != nil {
			tgTxID := j.ComponentTransactionIDs["tgwebproxy"]
			if tgTxID == "" {
				tgTxID = j.TransactionID
			}
			if err := c.tgSvc.FinalizePrepared(tgTxID); err != nil {
				finErrs = append(finErrs, err)
			}
		}
		if c.dispatcher != nil {
			dispTxID := j.ComponentTransactionIDs["dispatcher"]
			if dispTxID == "" {
				dispTxID = j.TransactionID
			}
			if err := c.dispatcher.FinalizePrepared(dispTxID); err != nil {
				finErrs = append(finErrs, err)
			}
		}
		if len(finErrs) > 0 {
			c.setRecovery(true, fmt.Sprintf("recovery finalize in PhaseCommitted failed: %v", errors.Join(finErrs...)))
			return fmt.Errorf("%w: recovery finalize in PhaseCommitted failed: %v", ErrRecoveryRequired, errors.Join(finErrs...))
		}

		if err := c.archiveJournal(jPath, "committed", j.TransactionID); err != nil {
			c.setRecovery(true, fmt.Sprintf("archive committed journal failed: %v", err))
			return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
		}
		return nil
	}

	if j.Phase == PhaseCommitting {
		// Past point of no return: candidate is ALREADY active on disk. Recovery ONLY calls FinalizePrepared, NEVER CommitPrepared!
		var finErrs []error
		if c.xraySvc != nil {
			xrayTxID := j.ComponentTransactionIDs["xray"]
			if xrayTxID == "" {
				xrayTxID = j.TransactionID
			}
			if err := c.xraySvc.FinalizePrepared(xrayTxID); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize xray (%s): %w", xrayTxID, err))
			}
		}
		if c.tgSvc != nil {
			tgTxID := j.ComponentTransactionIDs["tgwebproxy"]
			if tgTxID == "" {
				tgTxID = j.TransactionID
			}
			if err := c.tgSvc.FinalizePrepared(tgTxID); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize tgwebproxy (%s): %w", tgTxID, err))
			}
		}
		if c.dispatcher != nil {
			dispTxID := j.ComponentTransactionIDs["dispatcher"]
			if dispTxID == "" {
				dispTxID = j.TransactionID
			}
			if err := c.dispatcher.FinalizePrepared(dispTxID); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize dispatcher (%s): %w", dispTxID, err))
			}
		}

		if len(finErrs) > 0 {
			c.setRecovery(true, fmt.Sprintf("recovery finalize in PhaseCommitting failed: %v", errors.Join(finErrs...)))
			return fmt.Errorf("%w: %v", ErrRecoveryRequired, errors.Join(finErrs...))
		}

		j.Phase = PhaseCommitted
		j.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		j.Checksum = computeJournalChecksum(j)
		if err := c.writeJournal(jPath, j); err != nil {
			c.recoveryNeeded = true
			c.recoveryReason = fmt.Sprintf("write committed journal in recovery failed: %v", err)
			return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
		}

		if err := c.archiveJournal(jPath, "committed", j.TransactionID); err != nil {
			c.recoveryNeeded = true
			c.recoveryReason = fmt.Sprintf("archive roll-forward committed journal failed: %v", err)
			return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
		}
		return nil
	}

	// Prior to PhaseCommitting: reverse rollback Dispatcher -> Telegram -> Xray
	if err := c.failAndRollback(j, errors.New("startup recovery rolled back uncommitted transaction")); err != nil {
		if c.recoveryNeeded {
			return err
		}
	}
	return nil
}

// Apply atomically coordinates topology changes across Xray, Dispatcher, and Telegram Web Proxy.
func (c *Coordinator) Apply(ctx context.Context, desired IngressTopology) error {
	txID := fmt.Sprintf("ing-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		if c.recoveryNeeded {
			return ErrRecoveryRequired
		}
		return c.applyLocked(ctx, txID, desired, nil, nil)
	})
}

func (c *Coordinator) applyLocked(ctx context.Context, txID string, desired IngressTopology, candidateXray *xrayserver.Config, candidateTg *tgwebproxy.Config) error {
	prev := c.getCurrentTopology()
	affected := c.calculateAffected(prev, desired)
	if len(affected) == 0 {
		return nil
	}

	jPath := c.journalPath()
	journal := &TransactionJournal{
		Version:                 1,
		TransactionID:           txID,
		Phase:                   PhaseStaged,
		CreatedAt:               time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:               time.Now().UTC().Format(time.RFC3339),
		Desired:                 desired,
		Previous:                prev,
		Affected:                affected,
		ComponentTransactionIDs: make(map[string]string),
	}
	journal.Checksum = computeJournalChecksum(journal)

	if err := c.writeJournal(jPath, journal); err != nil {
		return fmt.Errorf("write staged journal: %w", err)
	}

	// Step 1: Prepare Phase
	if contains(affected, "xray") && c.xraySvc != nil {
		var candidate xrayserver.Config
		if candidateXray != nil {
			candidate = *candidateXray
		} else {
			candidate = c.xraySvc.GetConfig()
			candidate.Enabled = desired.XrayEnabled
			if desired.XrayAddress != "" {
				candidate.ListenAddress = desired.XrayAddress
			}
			if desired.XrayPort > 0 {
				candidate.ListenPort = desired.XrayPort
			}
			if desired.XrayPathPrefix != "" {
				candidate.Path = desired.XrayPathPrefix
			}
			if desired.XrayPublicHostname != "" {
				candidate.PublicDomain = desired.XrayPublicHostname
			} else if desired.PublicHostname != "" {
				candidate.PublicDomain = desired.PublicHostname
			}
			if desired.XrayPublicPort > 0 {
				candidate.PublicPort = desired.XrayPublicPort
			}
		}

		xrayTxID, err := c.xraySvc.PrepareCandidate(txID, candidate)
		if err != nil {
			return c.failAndRollback(journal, fmt.Errorf("prepare xray candidate: %w", err))
		}
		journal.ComponentTransactionIDs["xray"] = xrayTxID
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("write staged journal with xray tx id: %w", err))
		}
	}

	if contains(affected, "tgwebproxy") && c.tgSvc != nil {
		var candidate tgwebproxy.Config
		if candidateTg != nil {
			candidate = *candidateTg
		} else {
			candidate = c.tgSvc.GetInternalConfig()
			candidate.Enabled = desired.TgEnabled
			if desired.TgScenario != "" {
				candidate.Scenario = desired.TgScenario
			}
			if desired.TgDirectAddress != "" {
				candidate.DirectHost = desired.TgDirectAddress
			}
			if desired.TgDirectPort > 0 {
				candidate.DirectPort = desired.TgDirectPort
			}
			rawHost := desired.TgRawAddress
			if rawHost == "" {
				rawHost = "127.0.0.1"
			}
			rawPort := desired.TgRawPort
			if rawPort <= 0 {
				rawPort = 2398
			}
			candidate.Backend = fmt.Sprintf("%s:%d", rawHost, rawPort)

			if desired.TgWebPort > 0 {
				candidate.ListenPort = desired.TgWebPort
			} else if desired.TgPort > 0 {
				candidate.ListenPort = desired.TgPort
			}
			candidate.PublicHostname = desired.TgPublicHostname
			if candidate.PublicHostname == "" && desired.PublicHostname != "" {
				candidate.PublicHostname = desired.PublicHostname
			}
		}

		tgTxID, err := c.tgSvc.PrepareCandidate(txID, candidate)
		if err != nil {
			return c.failAndRollback(journal, fmt.Errorf("prepare tg candidate: %w", err))
		}
		journal.ComponentTransactionIDs["tgwebproxy"] = tgTxID
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("write staged journal with tg tx id: %w", err))
		}
	}

	if contains(affected, "dispatcher") && c.dispatcher != nil {
		dispPort := desired.DispatcherPort
		if dispPort <= 0 {
			dispPort = 9009
		}
		xrayAddr := desired.XrayAddress
		if xrayAddr == "" {
			xrayAddr = "127.0.0.1"
		}
		xrayPort := desired.XrayPort
		if xrayPort <= 0 {
			xrayPort = 9008
		}
		tgAddr := desired.TgWebAddress
		if tgAddr == "" {
			tgAddr = "127.0.0.1"
		}
		tgPort := desired.TgWebPort
		if tgPort <= 0 {
			tgPort = desired.TgPort
		}
		if tgPort <= 0 {
			tgPort = 8085
		}

		dispCandidate := cdndispatcher.Candidate{
			Enabled: desired.DispatcherEnabled,
			Config: cdndispatcher.Config{
				ListenAddr:     fmt.Sprintf(":%d", dispPort),
				XrayTarget:     fmt.Sprintf("http://%s:%d", xrayAddr, xrayPort),
				TgTarget:       fmt.Sprintf("http://%s:%d", tgAddr, tgPort),
				PublicHostname: desired.PublicHostname,
				XrayPublicHost: desired.XrayPublicHostname,
				TgPublicHost:   desired.TgPublicHostname,
				XrayPathPrefix: desired.XrayPathPrefix,
			},
		}

		dispTxID, err := c.dispatcher.PrepareCandidate(txID, dispCandidate)
		if err != nil {
			return c.failAndRollback(journal, fmt.Errorf("prepare dispatcher candidate: %w", err))
		}
		journal.ComponentTransactionIDs["dispatcher"] = dispTxID
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("write staged journal with dispatcher tx id: %w", err))
		}
	}

	// Step 2: Candidate Activation (CommitPrepared)
	if contains(affected, "xray") && c.xraySvc != nil {
		xrayTxID := journal.ComponentTransactionIDs["xray"]
		if xrayTxID == "" {
			xrayTxID = txID
		}
		if err := c.xraySvc.CommitPrepared(xrayTxID); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("activate xray candidate: %w", err))
		}
	}

	if contains(affected, "tgwebproxy") && c.tgSvc != nil {
		tgTxID := journal.ComponentTransactionIDs["tgwebproxy"]
		if tgTxID == "" {
			tgTxID = txID
		}
		if err := c.tgSvc.CommitPrepared(tgTxID); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("activate tg candidate: %w", err))
		}
	}

	if contains(affected, "dispatcher") && c.dispatcher != nil {
		dispTxID := journal.ComponentTransactionIDs["dispatcher"]
		if dispTxID == "" {
			dispTxID = txID
		}
		if err := c.dispatcher.CommitPrepared(dispTxID); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("activate dispatcher candidate: %w", err))
		}
	}

	journal.Phase = PhaseCandidateActive
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	journal.Checksum = computeJournalChecksum(journal)
	if err := c.writeJournal(jPath, journal); err != nil {
		return c.failAndRollback(journal, fmt.Errorf("write candidate_active journal: %w", err))
	}

	// Step 3: Committing Phase (Point of No Return)
	if c.onPhaseChange != nil {
		if err := c.onPhaseChange(PhaseCommitting); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("onPhaseChange hook failed before committing: %w", err))
		}
	}

	journal.Phase = PhaseCommitting
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	journal.Checksum = computeJournalChecksum(journal)
	if err := c.writeJournal(jPath, journal); err != nil {
		return c.failAndRollback(journal, fmt.Errorf("write committing journal: %w", err))
	}

	// Step 4: Finalize Components
	var finErrs []error
	if contains(affected, "xray") && c.xraySvc != nil {
		if err := c.xraySvc.FinalizePrepared(journal.ComponentTransactionIDs["xray"]); err != nil {
			finErrs = append(finErrs, fmt.Errorf("finalize xray: %w", err))
		}
	}
	if contains(affected, "tgwebproxy") && c.tgSvc != nil {
		if err := c.tgSvc.FinalizePrepared(journal.ComponentTransactionIDs["tgwebproxy"]); err != nil {
			finErrs = append(finErrs, fmt.Errorf("finalize tgwebproxy: %w", err))
		}
	}
	if contains(affected, "dispatcher") && c.dispatcher != nil {
		if err := c.dispatcher.FinalizePrepared(journal.ComponentTransactionIDs["dispatcher"]); err != nil {
			finErrs = append(finErrs, fmt.Errorf("finalize dispatcher: %w", err))
		}
	}
	if len(finErrs) > 0 {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("finalize components failed: %v", errors.Join(finErrs...))
		return fmt.Errorf("%w: finalize components failed: %v", ErrRecoveryRequired, errors.Join(finErrs...))
	}

	// Step 5: Committed Phase (ONLY after all FinalizePrepared succeed!)
	journal.Phase = PhaseCommitted
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	journal.Fingerprints = c.computeFingerprints()
	journal.Checksum = computeJournalChecksum(journal)
	if err := c.writeJournal(jPath, journal); err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("write committed journal failed: %v", err)
		return fmt.Errorf("%w: write committed journal: %v", ErrRecoveryRequired, err)
	}

	// Step 6: Archive committed journal
	if err := c.archiveJournal(jPath, "committed", txID); err != nil {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("archive committed journal failed: %v", err)
		return fmt.Errorf("%w: archive journal: %v", ErrRecoveryRequired, err)
	}

	return nil
}

func (c *Coordinator) failAndRollback(j *TransactionJournal, origErr error) error {
	jPath := c.journalPath()
	j.Phase = PhaseRollingBack
	j.Error = origErr.Error()
	j.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	j.Checksum = computeJournalChecksum(j)

	var persistErrs []error
	if err := c.writeJournal(jPath, j); err != nil {
		persistErrs = append(persistErrs, fmt.Errorf("write rolling_back journal: %w", err))
	}

	var rollbackErrs []error

	// Reverse order: Dispatcher -> Telegram -> Xray
	if dispTxID := j.ComponentTransactionIDs["dispatcher"]; dispTxID != "" && c.dispatcher != nil {
		if err := c.dispatcher.RollbackPrepared(dispTxID); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback dispatcher (%s): %w", dispTxID, err))
		}
	} else if contains(j.Affected, "dispatcher") && c.dispatcher != nil {
		prevDisp := cdndispatcher.Config{
			ListenAddr:     fmt.Sprintf(":%d", j.Previous.DispatcherPort),
			XrayTarget:     fmt.Sprintf("http://%s:%d", j.Previous.XrayAddress, j.Previous.XrayPort),
			TgTarget:       fmt.Sprintf("http://127.0.0.1:%d", j.Previous.TgPort),
			PublicHostname: j.Previous.PublicHostname,
			XrayPathPrefix: j.Previous.XrayPathPrefix,
		}
		if err := c.dispatcher.ApplyConfig(prevDisp); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback dispatcher apply config: %w", err))
		}
		if j.Previous.DispatcherEnabled && !c.dispatcher.IsRunning() {
			_ = c.dispatcher.Start()
		} else if !j.Previous.DispatcherEnabled && c.dispatcher.IsRunning() {
			_ = c.dispatcher.Stop()
		}
	}

	if tgTxID := j.ComponentTransactionIDs["tgwebproxy"]; tgTxID != "" && c.tgSvc != nil {
		if err := c.tgSvc.RollbackPrepared(tgTxID); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback tgwebproxy (%s): %w", tgTxID, err))
		}
	} else if contains(j.Affected, "tgwebproxy") && c.tgSvc != nil {
		prevTG := tgwebproxy.ManagedIngressConfig{
			Enabled:        j.Previous.TgEnabled,
			ListenPort:     j.Previous.TgPort,
			PublicHostname: j.Previous.PublicHostname,
		}
		if err := c.tgSvc.ApplyManagedIngress(prevTG); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback tgwebproxy: %w", err))
		}
	}

	if xrayTxID := j.ComponentTransactionIDs["xray"]; xrayTxID != "" && c.xraySvc != nil {
		if err := c.xraySvc.RollbackPrepared(xrayTxID); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback xray (%s): %w", xrayTxID, err))
		}
	}

	if len(rollbackErrs) > 0 {
		c.setRecovery(true, fmt.Sprintf("rollback failed for components: %v", errors.Join(rollbackErrs...)))
		j.Error = fmt.Sprintf("%v; rollback errors: %v", origErr, errors.Join(rollbackErrs...))
		j.Checksum = computeJournalChecksum(j)
		if err := c.writeJournal(jPath, j); err != nil {
			persistErrs = append(persistErrs, fmt.Errorf("write rolling_back error journal: %w", err))
		}
		allErrs := append([]error{ErrRecoveryRequired, origErr}, rollbackErrs...)
		allErrs = append(allErrs, persistErrs...)
		return errors.Join(allErrs...)
	}

	if err := c.archiveJournal(jPath, "failed", j.TransactionID); err != nil {
		persistErrs = append(persistErrs, fmt.Errorf("archive failed journal: %w", err))
	}

	if len(persistErrs) > 0 {
		c.recoveryNeeded = true
		c.recoveryReason = fmt.Sprintf("rollback persistence failed: %v", errors.Join(persistErrs...))
		return errors.Join(append([]error{ErrRecoveryRequired, origErr}, persistErrs...)...)
	}

	return origErr
}

func (c *Coordinator) rollback(j *TransactionJournal, xrayTxID string, origErr error) error {
	return c.failAndRollback(j, origErr)
}

func (c *Coordinator) topologyForXrayConfig(cfg xrayserver.Config) IngressTopology {
	prev := c.getCurrentTopology()
	desired := prev

	desired.XrayEnabled = cfg.Enabled
	if cfg.ListenAddress != "" {
		desired.XrayAddress = cfg.ListenAddress
	} else if desired.XrayAddress == "" {
		desired.XrayAddress = "127.0.0.1"
	}
	if cfg.ListenPort > 0 {
		desired.XrayPort = cfg.ListenPort
	} else if desired.XrayPort <= 0 {
		desired.XrayPort = 9008
	}
	desired.XrayPublicHostname = cfg.PublicDomain
	if cfg.PublicDomain != "" {
		desired.PublicHostname = cfg.PublicDomain
	} else if prev.TgPublicHostname == "" || prev.PublicHostname == prev.XrayPublicHostname {
		desired.PublicHostname = ""
		desired.TgPublicHostname = ""
	}
	if cfg.PublicPort > 0 {
		desired.XrayPublicPort = cfg.PublicPort
	} else if desired.XrayPublicPort <= 0 {
		desired.XrayPublicPort = 443
	}
	if cfg.Path != "" {
		desired.XrayPathPrefix = cdndispatcher.NormalizePathPrefix(cfg.Path)
	} else if desired.XrayPathPrefix == "" {
		desired.XrayPathPrefix = cdndispatcher.NormalizePathPrefix("")
	}
	if cfg.DispatcherPort > 0 {
		desired.DispatcherPort = cfg.DispatcherPort
	} else if desired.DispatcherPort <= 0 {
		desired.DispatcherPort = 9009
	}

	// Dispatcher needed check
	desired.DispatcherEnabled = (desired.XrayEnabled && desired.XrayPublicHostname != "") ||
		(desired.TgEnabled && (desired.TgScenario == "cdn_http" || desired.TgScenario == "dual"))
	return desired
}

func (c *Coordinator) topologyForTelegramConfig(cfg tgwebproxy.Config) IngressTopology {
	prev := c.getCurrentTopology()
	desired := prev

	desired.TgEnabled = cfg.Enabled
	if cfg.Scenario != "" {
		desired.TgScenario = cfg.Scenario
	}
	if cfg.DirectHost != "" {
		desired.TgDirectAddress = cfg.DirectHost
	}
	if cfg.DirectPort > 0 {
		desired.TgDirectPort = cfg.DirectPort
	}
	if cfg.Backend != "" {
		host, portStr, err := net.SplitHostPort(cfg.Backend)
		if err == nil {
			desired.TgRawAddress = host
			if p, err := strconv.Atoi(portStr); err == nil {
				desired.TgRawPort = p
			}
		}
	}
	if desired.TgRawAddress == "" {
		desired.TgRawAddress = "127.0.0.1"
	}
	if desired.TgRawPort <= 0 {
		desired.TgRawPort = 2398
	}
	desired.TgWebAddress = "127.0.0.1"
	if cfg.ListenPort > 0 {
		desired.TgWebPort = cfg.ListenPort
		desired.TgPort = cfg.ListenPort
	}
	if desired.TgWebPort <= 0 {
		desired.TgWebPort = 8085
		desired.TgPort = 8085
	}
	if cfg.PublicHostname != "" {
		desired.TgPublicHostname = cfg.PublicHostname
	}
	if desired.DispatcherPort <= 0 {
		desired.DispatcherPort = 9009
	}

	desired.DispatcherEnabled = (desired.XrayEnabled && desired.XrayPublicHostname != "") ||
		(desired.TgEnabled && (desired.TgScenario == "cdn_http" || desired.TgScenario == "dual"))
	return desired
}

// ApplyXrayConfig translates an Xray configuration into a desired topology and atomically applies it.
func (c *Coordinator) ApplyXrayConfig(ctx context.Context, cfg xrayserver.Config) error {
	txID := fmt.Sprintf("ing-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		if c.recoveryNeeded {
			return ErrRecoveryRequired
		}
		desired := c.topologyForXrayConfig(cfg)
		return c.applyLocked(ctx, txID, desired, &cfg, nil)
	})
}

// ApplyTelegramConfig updates Telegram Web Proxy configuration and coordinates Dispatcher state.
func (c *Coordinator) ApplyTelegramConfig(ctx context.Context, cfg tgwebproxy.Config) error {
	txID := fmt.Sprintf("ing-tg-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		if c.recoveryNeeded {
			return ErrRecoveryRequired
		}
		if c.tgSvc == nil {
			return errors.New("tgwebproxy service unavailable")
		}
		desired := c.topologyForTelegramConfig(cfg)
		return c.applyLocked(ctx, txID, desired, nil, &cfg)
	})
}

// SetXrayEnabled toggles Xray enabled status and coordinates Dispatcher state.
func (c *Coordinator) SetXrayEnabled(ctx context.Context, enabled bool) error {
	if c.xraySvc == nil {
		return errors.New("xray service unavailable")
	}
	c.mu.Lock()
	if c.recoveryNeeded {
		c.mu.Unlock()
		return ErrRecoveryRequired
	}
	cfg := c.xraySvc.GetConfig()
	cfg.Enabled = enabled
	c.mu.Unlock()

	return c.ApplyXrayConfig(ctx, cfg)
}

// RestartXray restarts the Xray process under coordinator lock.
func (c *Coordinator) RestartXray(ctx context.Context) error {
	txID := fmt.Sprintf("ing-restart-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		if c.recoveryNeeded {
			return ErrRecoveryRequired
		}
		if c.xraySvc == nil {
			return errors.New("xray service unavailable")
		}
		return c.xraySvc.Restart()
	})
}

// ResolveMigration executes migration strategy under coordinator lock.
func (c *Coordinator) ResolveMigration(ctx context.Context, strategy string, legacyCfg *xrayserver.Config) error {
	txID := fmt.Sprintf("ing-mig-%d-%d", time.Now().UnixNano(), os.Getpid())
	return c.withIngressLock(txID, func() error {
		if c.recoveryNeeded {
			return ErrRecoveryRequired
		}

		switch strategy {
		case "keep_new":
			return xrayserver.SaveRuntimeDecision(c.dataDir, xrayserver.RuntimeDecision{
				ActiveGeneration: "new",
				MigrationStatus:  "migrated",
				DecidedAt:        time.Now().UTC().Format(time.RFC3339),
			})
		case "keep_legacy":
			return c.executeKeepLegacySaga(ctx, txID)
		case "import_legacy_draft":
			if legacyCfg == nil {
				return errors.New("legacy config is nil")
			}
			desired := c.topologyForXrayConfig(*legacyCfg)
			if err := c.applyLocked(ctx, txID, desired, legacyCfg, nil); err != nil {
				return err
			}
			return xrayserver.SaveRuntimeDecision(c.dataDir, xrayserver.RuntimeDecision{
				ActiveGeneration: "new",
				MigrationStatus:  "migrated",
				DecidedAt:        time.Now().UTC().Format(time.RFC3339),
			})
		default:
			return fmt.Errorf("unknown strategy: %s", strategy)
		}
	})
}

// ExecuteIngressTransaction executes a full coordinated transaction for Server Wizards with candidate isolation and readiness probes.
func (c *Coordinator) ExecuteIngressTransaction(ctx context.Context, params IngressTransactionParams) error {
	txID := params.TxID
	if txID == "" {
		txID = fmt.Sprintf("ing-tx-%d-%d", time.Now().UnixNano(), os.Getpid())
	}

	return c.withIngressLock(txID, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if needed, _ := c.isRecoveryNeeded(); needed {
			return ErrRecoveryRequired
		}

		// Fingerprint check under lock
		if params.FingerprintFunc != nil {
			currentFp, err := params.FingerprintFunc(ctx)
			if err != nil {
				return fmt.Errorf("state probe failed: %w", err)
			}
			if params.ExpectedFingerprint != "" && currentFp != params.ExpectedFingerprint {
				return fmt.Errorf("%w: expected %s, got %s", ErrPlanStale, params.ExpectedFingerprint, currentFp)
			}
		}

		prevTopo := c.getCurrentTopology()
		desiredTopo := prevTopo

		var candXray *xrayserver.Config
		if params.Xray != nil && c.xraySvc != nil {
			cfg := c.xraySvc.GetConfig()
			cfg.Enabled = params.Xray.Enabled
			if params.Xray.ListenAddress != "" {
				cfg.ListenAddress = params.Xray.ListenAddress
			} else if cfg.ListenAddress == "" {
				cfg.ListenAddress = "127.0.0.1"
			}
			if params.Xray.ListenPort > 0 {
				cfg.ListenPort = params.Xray.ListenPort
			} else if cfg.ListenPort == 0 {
				cfg.ListenPort = 9008
			}
			if params.Xray.PublicDomain != "" {
				cfg.PublicDomain = params.Xray.PublicDomain
			}
			if params.Xray.PublicPort > 0 {
				cfg.PublicPort = params.Xray.PublicPort
			}
			if params.Xray.Path != "" {
				cfg.Path = params.Xray.Path
			}
			if params.Xray.Transport != "" {
				cfg.Transport = params.Xray.Transport
			}
			if params.Xray.Mode != "" {
				cfg.Mode = params.Xray.Mode
			}
			if params.Xray.UplinkMethod != "" {
				cfg.UplinkMethod = params.Xray.UplinkMethod
			}
			if params.Xray.OutboundMode != "" {
				cfg.OutboundMode = params.Xray.OutboundMode
			}
			if params.Xray.OutboundInterface != "" {
				cfg.OutboundInterface = params.Xray.OutboundInterface
			}
			if params.Xray.OutboundSocksPort > 0 {
				cfg.OutboundSocksPort = params.Xray.OutboundSocksPort
			}
			if params.Xray.ClientUUID != "" {
				remark := params.Xray.ClientRemark
				if remark == "" {
					remark = "Default Client"
				}
				found := false
				for i, cl := range cfg.Clients {
					if cl.ID == params.Xray.ClientUUID || cl.Remark == remark {
						cfg.Clients[i].ID = params.Xray.ClientUUID
						cfg.Clients[i].Remark = remark
						cfg.Clients[i].Enabled = true
						found = true
						break
					}
				}
				if !found {
					cfg.Clients = append(cfg.Clients, xrayserver.Client{
						ID:        params.Xray.ClientUUID,
						Remark:    remark,
						Enabled:   true,
						CreatedAt: time.Now().UTC().Format(time.RFC3339),
					})
				}
			}
			candXray = &cfg

			desiredTopo.XrayEnabled = candXray.Enabled
			desiredTopo.XrayAddress = candXray.ListenAddress
			desiredTopo.XrayPort = candXray.ListenPort
			desiredTopo.XrayPathPrefix = cdndispatcher.NormalizePathPrefix(candXray.Path)
			desiredTopo.XrayPublicHostname = candXray.PublicDomain
			desiredTopo.XrayPublicPort = candXray.PublicPort
			if candXray.PublicDomain != "" && desiredTopo.PublicHostname == "" {
				desiredTopo.PublicHostname = candXray.PublicDomain
			}
		}

		var candTg *tgwebproxy.Config
		if params.Telegram != nil && c.tgSvc != nil {
			cfg := c.tgSvc.GetInternalConfig()
			cfg.Enabled = params.Telegram.Enabled
			if params.Telegram.Scenario != "" {
				cfg.Scenario = params.Telegram.Scenario
			}
			if params.Telegram.DirectHost != "" {
				cfg.DirectHost = params.Telegram.DirectHost
			}
			if params.Telegram.DirectPort > 0 {
				cfg.DirectPort = params.Telegram.DirectPort
			}
			if params.Telegram.TlsDomain != "" {
				cfg.TlsDomain = params.Telegram.TlsDomain
			}
			if params.Telegram.ListenPort > 0 {
				cfg.ListenPort = params.Telegram.ListenPort
			}
			if params.Telegram.BackendPort > 0 {
				cfg.Backend = fmt.Sprintf("127.0.0.1:%d", params.Telegram.BackendPort)
			}
			if params.Telegram.AdminPort > 0 {
				cfg.AdminPort = params.Telegram.AdminPort
			}
			if params.Telegram.PublicHostname != "" {
				cfg.PublicHostname = params.Telegram.PublicHostname
			}
			if params.Telegram.CarrierMode != "" {
				cfg.CarrierMode = params.Telegram.CarrierMode
			}
			if params.Telegram.UpstreamDevice != "" {
				cfg.UpstreamDevice = params.Telegram.UpstreamDevice
			}
			if params.Telegram.Secret != "" {
				cfg.Secret = params.Telegram.Secret
			}
			candTg = &cfg

			desiredTopo.TgEnabled = candTg.Enabled
			desiredTopo.TgScenario = candTg.Scenario
			desiredTopo.TgDirectAddress = candTg.DirectHost
			desiredTopo.TgDirectPort = candTg.DirectPort
			desiredTopo.TgWebAddress = "127.0.0.1"
			desiredTopo.TgWebPort = candTg.ListenPort
			desiredTopo.TgPort = candTg.ListenPort
			desiredTopo.TgPublicHostname = candTg.PublicHostname
			if candTg.PublicHostname != "" && desiredTopo.PublicHostname == "" {
				desiredTopo.PublicHostname = candTg.PublicHostname
			}
		}

		// Dispatcher required if Xray or TG uses CDN ingress
		needsDispatcher := false
		if desiredTopo.XrayEnabled && (desiredTopo.XrayPublicHostname != "" || desiredTopo.PublicHostname != "" || desiredTopo.XrayPathPrefix != "") {
			needsDispatcher = true
		}
		if desiredTopo.TgEnabled {
			scenario := desiredTopo.TgScenario
			if scenario == "cdn_http" || scenario == "dual" || scenario == "cdn_domain" || scenario == "both" || scenario == "" {
				if desiredTopo.TgPublicHostname != "" || desiredTopo.PublicHostname != "" {
					needsDispatcher = true
				}
			}
		}
		desiredTopo.DispatcherEnabled = needsDispatcher
		if desiredTopo.DispatcherPort == 0 {
			if prevTopo.DispatcherPort > 0 {
				desiredTopo.DispatcherPort = prevTopo.DispatcherPort
			} else {
				desiredTopo.DispatcherPort = 9009
			}
		}

		affected := c.calculateAffected(prevTopo, desiredTopo)
		if params.Xray != nil && !contains(affected, "xray") {
			affected = append(affected, "xray")
		}
		if params.Telegram != nil && !contains(affected, "tgwebproxy") {
			affected = append(affected, "tgwebproxy")
		}

		jPath := c.journalPath()
		journal := &TransactionJournal{
			Version:                 2,
			TransactionID:           txID,
			Phase:                   PhaseStaged,
			CreatedAt:               time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:               time.Now().UTC().Format(time.RFC3339),
			Desired:                 desiredTopo,
			Previous:                prevTopo,
			Affected:                affected,
			ComponentTransactionIDs: make(map[string]string),
		}
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			return fmt.Errorf("write staged journal: %w", err)
		}

		var xrayTxID string
		if contains(affected, "xray") && c.xraySvc != nil && candXray != nil {
			var err error
			xrayTxID, err = c.xraySvc.PrepareCandidate(txID, *candXray)
			if err != nil {
				return c.failAndRollback(journal, fmt.Errorf("prepare xray candidate: %w", err))
			}
			journal.ComponentTransactionIDs["xray"] = xrayTxID
			journal.Checksum = computeJournalChecksum(journal)
			if err := c.writeJournal(jPath, journal); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("write staged journal with xray tx id: %w", err))
			}
		}

		if contains(affected, "tgwebproxy") && c.tgSvc != nil && candTg != nil {
			tgTxID, err := c.tgSvc.PrepareCandidate(txID, *candTg)
			if err != nil {
				return c.failAndRollback(journal, fmt.Errorf("prepare tg candidate: %w", err))
			}
			journal.ComponentTransactionIDs["tgwebproxy"] = tgTxID
			journal.Checksum = computeJournalChecksum(journal)
			if err := c.writeJournal(jPath, journal); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("write staged journal with tg tx id: %w", err))
			}
		}

		if contains(affected, "dispatcher") && c.dispatcher != nil {
			dispPort := desiredTopo.DispatcherPort
			if dispPort <= 0 {
				dispPort = 9009
			}
			xrayAddr := desiredTopo.XrayAddress
			if xrayAddr == "" {
				xrayAddr = "127.0.0.1"
			}
			xrayPort := desiredTopo.XrayPort
			if xrayPort <= 0 {
				xrayPort = 9008
			}
			tgAddr := desiredTopo.TgWebAddress
			if tgAddr == "" {
				tgAddr = "127.0.0.1"
			}
			tgPort := desiredTopo.TgWebPort
			if tgPort <= 0 {
				tgPort = desiredTopo.TgPort
			}
			if tgPort <= 0 {
				tgPort = 8085
			}

			dispCandidate := cdndispatcher.Candidate{
				Enabled: desiredTopo.DispatcherEnabled,
				Config: cdndispatcher.Config{
					ListenAddr:     fmt.Sprintf(":%d", dispPort),
					XrayTarget:     fmt.Sprintf("http://%s:%d", xrayAddr, xrayPort),
					TgTarget:       fmt.Sprintf("http://%s:%d", tgAddr, tgPort),
					PublicHostname: desiredTopo.PublicHostname,
					XrayPublicHost: desiredTopo.XrayPublicHostname,
					TgPublicHost:   desiredTopo.TgPublicHostname,
					XrayPathPrefix: cdndispatcher.NormalizePathPrefix(desiredTopo.XrayPathPrefix),
				},
			}
			dispTxID, err := c.dispatcher.PrepareCandidate(txID, dispCandidate)
			if err != nil {
				return c.failAndRollback(journal, fmt.Errorf("prepare dispatcher candidate: %w", err))
			}
			journal.ComponentTransactionIDs["dispatcher"] = dispTxID
			journal.Checksum = computeJournalChecksum(journal)
			if err := c.writeJournal(jPath, journal); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("write staged journal with dispatcher tx id: %w", err))
			}
		}

		// Activate candidates (CommitPrepared) BEFORE PhaseCandidateActive and BEFORE ReadinessProbe!
		if contains(affected, "xray") && c.xraySvc != nil {
			xrayTxID := journal.ComponentTransactionIDs["xray"]
			if xrayTxID == "" {
				xrayTxID = txID
			}
			if err := c.xraySvc.CommitPrepared(xrayTxID); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("activate xray candidate: %w", err))
			}
		}

		if contains(affected, "tgwebproxy") && c.tgSvc != nil {
			tgTxID := journal.ComponentTransactionIDs["tgwebproxy"]
			if tgTxID == "" {
				tgTxID = txID
			}
			if err := c.tgSvc.CommitPrepared(tgTxID); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("activate tg candidate: %w", err))
			}
		}

		if contains(affected, "dispatcher") && c.dispatcher != nil {
			dispTxID := journal.ComponentTransactionIDs["dispatcher"]
			if dispTxID == "" {
				dispTxID = txID
			}
			if err := c.dispatcher.CommitPrepared(dispTxID); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("activate dispatcher candidate: %w", err))
			}
		}

		// Advance to PhaseCandidateActive
		journal.Phase = PhaseCandidateActive
		journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			return c.failAndRollback(journal, fmt.Errorf("write candidate_active journal: %w", err))
		}

		// Check context before probe
		if err := ctx.Err(); err != nil {
			return c.failAndRollback(journal, err)
		}

		// Run readiness probe with combined topology!
		if params.ReadinessProbe != nil {
			if err := params.ReadinessProbe(ctx, desiredTopo); err != nil {
				rbErr := c.failAndRollback(journal, err)
				if rbErr != nil && !errors.Is(rbErr, err) {
					return errors.Join(fmt.Errorf("readiness probe failed: %w", err), fmt.Errorf("rollback failed: %w", rbErr))
				}
				return fmt.Errorf("readiness probe failed: %w", err)
			}
		}

		// Check context before point of no return
		if err := ctx.Err(); err != nil {
			rbErr := c.failAndRollback(journal, err)
			if rbErr != nil && !errors.Is(rbErr, err) {
				return errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
			}
			return err
		}

		// Pre-committing hook (can still rollback if hook fails)
		if params.OnPhaseChange != nil {
			if err := params.OnPhaseChange(PhaseCommitting); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("OnPhaseChange hook failed before committing: %w", err))
			}
		} else if c.onPhaseChange != nil {
			if err := c.onPhaseChange(PhaseCommitting); err != nil {
				return c.failAndRollback(journal, fmt.Errorf("onPhaseChange hook failed before committing: %w", err))
			}
		}

		// Point of no return: PhaseCommitting
		journal.Phase = PhaseCommitting
		journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			// Journal write failed: point of no return was NOT reached! Rollback active candidates.
			return c.failAndRollback(journal, fmt.Errorf("write committing journal: %w", err))
		}

		// Post-boundary callback: durable point of no return reached!
		if params.OnPointOfNoReturn != nil {
			if err := params.OnPointOfNoReturn(txID); err != nil {
				c.setRecovery(true, fmt.Sprintf("OnPointOfNoReturn callback failed: %v", err))
				return errors.Join(ErrRecoveryRequired, fmt.Errorf("OnPointOfNoReturn callback failed: %w", err))
			}
		}

		// Finalize Components (FinalizePrepared) - candidates were already activated!
		var finErrs []error
		if contains(affected, "xray") && c.xraySvc != nil {
			if err := c.xraySvc.FinalizePrepared(journal.ComponentTransactionIDs["xray"]); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize xray: %w", err))
			}
		}
		if contains(affected, "tgwebproxy") && c.tgSvc != nil {
			if err := c.tgSvc.FinalizePrepared(journal.ComponentTransactionIDs["tgwebproxy"]); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize tgwebproxy: %w", err))
			}
		}
		if contains(affected, "dispatcher") && c.dispatcher != nil {
			if err := c.dispatcher.FinalizePrepared(journal.ComponentTransactionIDs["dispatcher"]); err != nil {
				finErrs = append(finErrs, fmt.Errorf("finalize dispatcher: %w", err))
			}
		}
		if len(finErrs) > 0 {
			c.setRecovery(true, fmt.Sprintf("finalize components failed: %v", errors.Join(finErrs...)))
			return fmt.Errorf("%w: finalize components failed: %v", ErrRecoveryRequired, errors.Join(finErrs...))
		}

		// PhaseCommitted (ONLY after all FinalizePrepared succeed!)
		journal.Phase = PhaseCommitted
		journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		journal.Fingerprints = c.computeFingerprints()
		journal.Checksum = computeJournalChecksum(journal)
		if err := c.writeJournal(jPath, journal); err != nil {
			c.setRecovery(true, fmt.Sprintf("write committed journal failed: %v", err))
			return fmt.Errorf("%w: write committed journal: %v", ErrRecoveryRequired, err)
		}

		// Archive journal as committed
		if err := c.archiveJournal(jPath, "committed", txID); err != nil {
			c.setRecovery(true, fmt.Sprintf("archive committed journal failed: %v", err))
			return fmt.Errorf("%w: archive journal: %v", ErrRecoveryRequired, err)
		}

		return nil
	})
}

func (c *Coordinator) rollbackIngressTx(j *TransactionJournal, xrayTxID string, prevTg tgwebproxy.Config, prevDisp cdndispatcher.Config, origErr error) error {
	return c.failAndRollback(j, origErr)
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
