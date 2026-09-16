package mihomo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ManifestState represents the sequential transaction states for configuration apply.
type ManifestState string

const (
	StateIdle               ManifestState = "idle"
	StateSnapshotSecured    ManifestState = "snapshot_secured"
	StateCandidateBuilt     ManifestState = "candidate_built"
	StateCandidatePublished ManifestState = "candidate_published"
	StateSwapIntent         ManifestState = "swap_intent"
	StateSwapApplied        ManifestState = "swap_applied"
	StateSwapVerified       ManifestState = "swap_verified"
	StateRuntimeIntent      ManifestState = "runtime_intent"
	StateRuntimeApplied     ManifestState = "runtime_applied"
	StateRuntimeVerified    ManifestState = "runtime_verified"
	StateBridgesReconciling ManifestState = "bridges_reconciling"
	StateCommitIntent       ManifestState = "commit_intent"
	StateCommitted          ManifestState = "committed"
	StateAbortInProgress    ManifestState = "abort_in_progress"
	StateRollbackInProgress ManifestState = "rollback_in_progress"
	StateRolledBack         ManifestState = "rolled_back"
	StateTerminalCleanup    ManifestState = "terminal_cleanup"
	StateRecoveryRequired   ManifestState = "recovery_required"
)

// DraftState represents the states in DraftJournal.
type DraftState string

const (
	DraftSnapshotSecured DraftState = "snapshot_secured"
	DraftStoreMutated    DraftState = "store_mutated"
	DraftPending         DraftState = "pending"
	DraftConsuming       DraftState = "consuming"
)

// RuntimeMode defines the enforcement level of the Mihomo routing engine.
type RuntimeMode string

const (
	RuntimeEnforced   RuntimeMode = "enforced"
	RuntimePermissive RuntimeMode = "permissive"
	RuntimeOff        RuntimeMode = "off"
)

// ProcessState represents whether the runtime process is expected to run.
type ProcessState string

const (
	ProcessRunning ProcessState = "running"
	ProcessStopped ProcessState = "stopped"
)

// BridgeRef uniquely references an active or candidate proxy bridge interface.
type BridgeRef struct {
	ProxyIndex      int    `json:"proxy_index"`
	ProxyInterface  string `json:"proxy_interface"`
	KernelInterface string `json:"kernel_interface"`
	LegacyOwner     string `json:"legacy_owner,omitempty"`
	OwnerUUID       string `json:"owner_uuid,omitempty"`
	Generation      uint64 `json:"generation,omitempty"`
}

func (r BridgeRef) Digest() string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ObservedBridge reports the actual OS state of a bridge interface.
type ObservedBridge struct {
	BridgeRef
	Exists     bool   `json:"exists"`
	Up         bool   `json:"up"`
	AssignedIP string `json:"assigned_ip,omitempty"`
}

// BridgeOpState defines the execution phase of a bridge creation or withdrawal.
type BridgeOpState string

const (
	BridgeOpIntent   BridgeOpState = "intent"
	BridgeOpApplied  BridgeOpState = "applied"
	BridgeOpVerified BridgeOpState = "verified"
)

// BridgeOperation journals a single bridge lifecycle operation.
type BridgeOperation struct {
	OperationID    string        `json:"operation_id"`
	Action         string        `json:"action"` // "create" | "withdraw" | "update"
	TargetDigest   string        `json:"target_digest"`
	BridgeRef      BridgeRef     `json:"bridge_ref,omitempty"`
	State          BridgeOpState `json:"state"`
	Attempts       int           `json:"attempts"`
	LastError      string        `json:"last_error,omitempty"`
	ObservedResult string        `json:"observed_result,omitempty"`
}

// ListenerSpec defines an inbound listener required for traffic interception.
type ListenerSpec struct {
	Network  string `json:"network"` // "tcp" | "udp"
	Family   string `json:"family"`  // "ipv4" | "ipv6" | "any"
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
	Purpose  string `json:"purpose"`
	Protocol string `json:"protocol,omitempty"` // Backwards compatibility alias for Network
}

// GetNetwork returns the effective network ("tcp" or "udp").
func (l ListenerSpec) GetNetwork() string {
	if l.Network != "" {
		return l.Network
	}
	return l.Protocol
}

// RuntimeProcessIdentity contains facts about the OS process verified from /proc.
type RuntimeProcessIdentity struct {
	PID            int    `json:"pid"`
	ProcStartTicks uint64 `json:"proc_start_ticks"`
	Generation     uint64 `json:"generation"`
	ExecutablePath string `json:"executable_path"`
	ConfigDir      string `json:"config_dir"`
}

// ProcessReceipt supplements RuntimeProcessIdentity with coordinator metadata.
type ProcessReceipt struct {
	RuntimeProcessIdentity
	DaemonEpoch       string    `json:"daemon_epoch"`
	AppliedGeneration uint64    `json:"applied_generation"`
	VerifiedAt        time.Time `json:"verified_at"`
}

// CompileResult represents the output of pure configuration compilation.
type CompileResult struct {
	ConfigYAML        []byte
	ConfigDigest      string
	InputDigest       string
	RequiredListeners []ListenerSpec
	TargetBridges     []BridgeRef
	Mode              RuntimeMode
}

// BridgeTransition partitions bridge adjustments into additions, removals, and unchanges.
type BridgeTransition struct {
	ToCreate   []BridgeRef
	ToWithdraw []BridgeRef
	ToPreserve []BridgeRef
}

// DraftJournal tracks the lifecycle of pending drafts in the native store before apply.
type DraftJournal struct {
	Version                  int        `json:"version"` // 1
	TxID                     string     `json:"txid"`
	State                    DraftState `json:"state"`
	AssociatedApplyTxID      string     `json:"associated_apply_txid,omitempty"`
	DraftSnapshotFile        string     `json:"draft_snapshot_file"`
	BaseDesiredStoreDigest   string     `json:"base_desired_store_digest"`
	TargetDesiredStoreDigest string     `json:"target_desired_store_digest"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

// PendingInputRecord tracks durable convergence requests across restarts.
type PendingInputRecord struct {
	Version                int       `json:"version"` // 1
	TxID                   string    `json:"txid"`
	OperationID            string    `json:"operation_id,omitempty"`
	BaseAppliedInputDigest string    `json:"base_applied_input_digest"`
	TargetInputDigest      string    `json:"target_input_digest"`
	Source                 string    `json:"source"` // "settings" | "slots" | "tunnels" | "native"
	Reason                 string    `json:"reason"`
	State                  string    `json:"state"` // "intent" | "pending" | "converging"
	CreatedAt              time.Time `json:"created_at"`
}

// AppliedGenerationRecord tracks the currently verified and active configuration running in the OS.
type AppliedGenerationRecord struct {
	Version             int            `json:"version"` // 1
	Generation          uint64         `json:"generation"`
	GenerationID        string         `json:"generation_id,omitempty"`
	AppliedAt           time.Time      `json:"applied_at"`
	AppliedStoreDigest  string         `json:"applied_store_digest"`
	AppliedConfigDigest string         `json:"applied_config_digest"`
	AppliedInputDigest  string         `json:"applied_input_digest"`
	AppliedListeners    []ListenerSpec `json:"applied_listeners"`
	AppliedBridges      []BridgeRef    `json:"applied_bridges"`
	RuntimeMode         RuntimeMode    `json:"runtime_mode"`
}

// GenerationManifest is the metadata preserved in each immutable generation bundle.
type GenerationManifest struct {
	Version             int            `json:"version"` // 1
	GenerationID        string         `json:"generation_id"`
	GenerationNumber    uint64         `json:"generation_number"`
	ArchivedAt          time.Time      `json:"archived_at"`
	AppliedStoreDigest  string         `json:"applied_store_digest"`
	AppliedConfigDigest string         `json:"applied_config_digest"`
	AppliedInputDigest  string         `json:"applied_input_digest"`
	AppliedListeners    []ListenerSpec `json:"applied_listeners"`
	AppliedBridges      []BridgeRef    `json:"applied_bridges"`
	RuntimeMode         RuntimeMode    `json:"runtime_mode"`
}

// LKGPointer points to the current verified last-known-good generation bundle.
type LKGPointer struct {
	Version             int       `json:"version"` // 1
	GenerationID        string    `json:"generation_id"`
	GenerationNumber    uint64    `json:"generation_number"`
	AppliedConfigDigest string    `json:"applied_config_digest"`
	AppliedStoreDigest  string    `json:"applied_store_digest"`
	UpdatedEpoch        string    `json:"updated_epoch"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// TransactionManifest records the active compile/apply transaction state on disk.
type TransactionManifest struct {
	Version   int           `json:"version"` // 1
	Sequence  uint64        `json:"sequence"`
	TxID      string        `json:"txid"`
	State     ManifestState `json:"state"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`

	// Cross-generation pointers
	PreviousGenerationID       string `json:"previous_generation_id,omitempty"`
	RollbackTargetGenerationID string `json:"rollback_target_generation_id,omitempty"`
	CandidateGenerationID      string `json:"candidate_generation_id,omitempty"`
	LKGGenerationID            string `json:"lkg_generation_id,omitempty"`
	VerifiedActiveGeneration   uint64 `json:"verified_active_generation"`

	// Digests and Files
	BaseAppliedStoreDigest   string `json:"base_applied_store_digest,omitempty"`
	BaseDesiredStoreDigest   string `json:"base_desired_store_digest,omitempty"`
	TargetDesiredStoreDigest string `json:"target_desired_store_digest,omitempty"`
	BaseAppliedInputDigest   string `json:"base_applied_input_digest,omitempty"`
	TargetInputDigest        string `json:"target_input_digest,omitempty"`

	PreMutationStoreSnapshotFile           string `json:"pre_mutation_store_snapshot_file,omitempty"`
	PreMutationStoreDigest                 string `json:"pre_mutation_store_digest,omitempty"`
	CandidatePostMutationStoreSnapshotFile string `json:"candidate_post_mutation_store_snapshot_file,omitempty"`
	CandidatePostMutationStoreDigest       string `json:"candidate_post_mutation_store_digest,omitempty"`

	PreviousConfigDigest  string `json:"previous_config_digest,omitempty"`
	CandidateConfigDigest string `json:"candidate_config_digest,omitempty"`
	CandidateConfigFile   string `json:"candidate_config_file,omitempty"`
	ConfigPresent         bool   `json:"config_present"`

	// Bridges
	BridgeOperations []BridgeOperation `json:"bridge_operations,omitempty"`

	// Runtime Mode
	DesiredMode RuntimeMode `json:"desired_mode"`

	// Errors and Outcomes
	FailureCode      string `json:"failure_code,omitempty"`
	FailedStage      string `json:"failed_stage,omitempty"`
	SanitizedSummary string `json:"sanitized_summary,omitempty"`
	FailureReason    string `json:"failure_reason,omitempty"`
}

// MigrationJournal records one-time legacy resource migration state.
type MigrationJournal struct {
	Version     int       `json:"version"` // 1
	State       string    `json:"state"`   // "in_progress" | "completed"
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Imported    []string  `json:"imported"`
}

// CleanupJournal records items that must be securely removed during rollback or cleanup.
type CleanupJournal struct {
	Version  int      `json:"version"` // 1
	Sequence uint64   `json:"sequence"`
	TxID     string   `json:"txid"`
	Files    []string `json:"files,omitempty"` // basenames of files in ConfigDir
}

// ValidateSchema verifies CleanupJournal integrity.
func (cj *CleanupJournal) ValidateSchema() error {
	if cj.Version != 1 {
		return fmt.Errorf("unsupported cleanup journal version: %d (want 1)", cj.Version)
	}
	if err := ValidateTxID(cj.TxID); err != nil {
		return fmt.Errorf("cleanup journal txid: %w", err)
	}
	for _, file := range cj.Files {
		if err := ValidateBasename(file); err != nil {
			return fmt.Errorf("cleanup journal file basename invalid: %w", err)
		}
	}
	return nil
}

// EvidenceExport contains allowlisted structural facts for diagnostic analysis without secrets.
type EvidenceExport struct {
	ExportedAt        time.Time      `json:"exported_at"`
	DaemonEpoch       string         `json:"daemon_epoch"`
	CurrentState      ManifestState  `json:"current_state"`
	ActiveGeneration  uint64         `json:"active_generation"`
	FailureCode       string         `json:"failure_code,omitempty"`
	FailedStage       string         `json:"failed_stage,omitempty"`
	SanitizedSummary  string         `json:"sanitized_summary,omitempty"`
	ConfigDigest      string         `json:"config_digest,omitempty"`
	ConfigSizeBytes   int64          `json:"config_size_bytes,omitempty"`
	RuleCount         int            `json:"rule_count"`
	ProxyGroupCount   int            `json:"proxy_group_count"`
	TargetInputDigest string         `json:"target_input_digest,omitempty"`
	ObservedListeners []ListenerSpec `json:"observed_listeners,omitempty"`
	ActiveBridges     []string       `json:"active_bridges,omitempty"`
	QuarantineList    []string       `json:"quarantine_basenames,omitempty"`
}

// NativeStoreTx provides the transactional capabilities required by ApplyCoordinator.
type NativeStoreTx interface {
	CreateSnapshotFile(txid string) (string, error)
	RestoreSnapshotFile(snapshotPath string) error
	RemoveSnapshotFile(snapshotPath string) error
	CurrentDigest() (string, error)
	ListBridges() []BridgeRef
}

// BridgeRuntime abstracts bridge interface management in the OS (NDMS / policy routing).
type BridgeRuntime interface {
	ApplyBridges(ctx context.Context, bridges []BridgeRef) error
	WithdrawBridges(ctx context.Context, bridges []BridgeRef) error
	VerifyBridges(ctx context.Context, bridges []BridgeRef) error
	ListActiveBridges(ctx context.Context) ([]BridgeRef, error)
}

// ExactBridgeRuntime provides exact, atomic, verifiable bridge operations.
type ExactBridgeRuntime interface {
	PublishBridge(ctx context.Context, ref BridgeRef) error
	WithdrawBridge(ctx context.Context, ref BridgeRef) error
	InspectBridge(ctx context.Context, ref BridgeRef) (ObservedBridge, error)
	ListActiveBridges(ctx context.Context) ([]ObservedBridge, error)
}

// ConfigFileValidator validates candidate configuration files.
type ConfigFileValidator interface {
	ValidateConfigFile(ctx context.Context, configPath string) error
}

var (
	txidRegex = regexp.MustCompile(`^[0-9]{14,24}(?:-[a-zA-Z0-9_-]+)?$`)
	baseRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
)

// ValidateTxID checks if a transaction identifier is well-formed.
func ValidateTxID(id string) error {
	if !txidRegex.MatchString(id) {
		return fmt.Errorf("invalid transaction ID %q: must match %s", id, txidRegex.String())
	}
	return nil
}

// ValidateBasename ensures a filename contains no directory traversal elements.
func ValidateBasename(name string) error {
	if name == "" || name == "." || name == ".." {
		return errors.New("basename cannot be empty, '.', or '..'")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("basename %q contains path separators", name)
	}
	if !baseRegex.MatchString(name) {
		return fmt.Errorf("basename %q contains invalid characters", name)
	}
	return nil
}

// AssertPathConfined verifies that targetPath is strictly inside allowedDir.
func AssertPathConfined(targetPath, allowedDir string) error {
	cleanTarget := filepath.Clean(targetPath)
	cleanAllowed := filepath.Clean(allowedDir)

	rel, err := filepath.Rel(cleanAllowed, cleanTarget)
	if err != nil {
		return fmt.Errorf("determine relative path of %s from %s: %w", cleanTarget, cleanAllowed, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %s escapes allowed directory %s (rel: %s)", cleanTarget, cleanAllowed, rel)
	}
	return nil
}

// ValidateSchema verifies TransactionManifest integrity.
func (m *TransactionManifest) ValidateSchema() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version: %d (want 1)", m.Version)
	}
	if err := ValidateTxID(m.TxID); err != nil {
		return fmt.Errorf("manifest txid: %w", err)
	}
	if m.State == "" {
		return errors.New("manifest state cannot be empty")
	}
	return nil
}

// Clone creates a deep copy of the transaction manifest.
func (m *TransactionManifest) Clone() *TransactionManifest {
	if m == nil {
		return nil
	}
	cpy := *m
	if m.BridgeOperations != nil {
		cpy.BridgeOperations = make([]BridgeOperation, len(m.BridgeOperations))
		copy(cpy.BridgeOperations, m.BridgeOperations)
	}
	return &cpy
}

// ValidateSchemaForPhase verifies the presence of required fields based on the manifest's current state.
func (m *TransactionManifest) ValidateSchemaForPhase() error {
	if err := m.ValidateSchema(); err != nil {
		return err
	}

	switch m.State {
	case StateSnapshotSecured:
		if m.PreMutationStoreSnapshotFile == "" {
			return errors.New("pre_mutation_store_snapshot_file is required in snapshot_secured state")
		}
	case StateCandidateBuilt:
		if m.ConfigPresent && m.CandidateConfigDigest == "" {
			return errors.New("candidate_config_digest is required when config is present")
		}
	case StateCommitIntent:
		if m.CandidateGenerationID == "" {
			return errors.New("candidate_generation_id is required in commit_intent state")
		}
	}
	return nil
}

// ValidateSchema verifies DraftJournal integrity.
func (dj *DraftJournal) ValidateSchema() error {
	if dj.Version != 1 {
		return fmt.Errorf("unsupported draft journal version: %d (want 1)", dj.Version)
	}
	if err := ValidateTxID(dj.TxID); err != nil {
		return fmt.Errorf("draft journal txid: %w", err)
	}
	if dj.State == "" {
		return errors.New("draft journal state cannot be empty")
	}
	return nil
}

// ValidateSchema verifies PendingInputRecord integrity.
func (rec *PendingInputRecord) ValidateSchema() error {
	if rec.Version != 1 {
		return fmt.Errorf("unsupported pending input version: %d (want 1)", rec.Version)
	}
	if rec.TxID != "" {
		if err := ValidateTxID(rec.TxID); err != nil {
			return fmt.Errorf("pending input txid: %w", err)
		}
	}
	if rec.State != "intent" && rec.State != "pending" && rec.State != "converging" {
		return fmt.Errorf("invalid pending input state: %q", rec.State)
	}
	return nil
}

// ValidateSchema verifies LKGPointer integrity.
func (ptr *LKGPointer) ValidateSchema() error {
	if ptr.Version != 1 {
		return fmt.Errorf("unsupported lkg pointer version: %d (want 1)", ptr.Version)
	}
	if err := ValidateBasename(ptr.GenerationID); err != nil {
		return fmt.Errorf("lkg pointer generation_id: %w", err)
	}
	return nil
}

// ValidateSchema verifies GenerationManifest integrity.
func (gm *GenerationManifest) ValidateSchema() error {
	if gm.Version != 1 {
		return fmt.Errorf("unsupported generation manifest version: %d (want 1)", gm.Version)
	}
	if err := ValidateBasename(gm.GenerationID); err != nil {
		return fmt.Errorf("generation manifest generation_id: %w", err)
	}
	return nil
}

// ValidateSchema verifies AppliedGenerationRecord integrity.
func (rec *AppliedGenerationRecord) ValidateSchema() error {
	if rec.Version != 1 {
		return fmt.Errorf("unsupported applied record version: %d (want 1)", rec.Version)
	}
	if rec.AppliedConfigDigest == "" && rec.RuntimeMode != RuntimeOff {
		return errors.New("applied record applied_config_digest cannot be empty for active mode")
	}
	return nil
}

// IsValidNext checks if transitioning from the current state to the next state is permitted by the state machine.
func (s ManifestState) IsValidNext(next ManifestState) bool {
	switch s {
	case StateIdle:
		return next == StateSnapshotSecured
	case StateSnapshotSecured:
		return next == StateCandidateBuilt || next == StateAbortInProgress
	case StateCandidateBuilt:
		return next == StateCandidatePublished || next == StateAbortInProgress
	case StateCandidatePublished:
		return next == StateSwapIntent || next == StateAbortInProgress
	case StateSwapIntent:
		return next == StateSwapApplied || next == StateRollbackInProgress
	case StateSwapApplied:
		return next == StateSwapVerified || next == StateRollbackInProgress
	case StateSwapVerified:
		return next == StateRuntimeIntent || next == StateRollbackInProgress
	case StateRuntimeIntent:
		return next == StateRuntimeApplied || next == StateRollbackInProgress
	case StateRuntimeApplied:
		return next == StateRuntimeVerified || next == StateRollbackInProgress
	case StateRuntimeVerified:
		return next == StateBridgesReconciling || next == StateRollbackInProgress
	case StateBridgesReconciling:
		return next == StateCommitIntent || next == StateRollbackInProgress
	case StateCommitIntent:
		return next == StateCommitted || next == StateRecoveryRequired
	case StateCommitted:
		return next == StateTerminalCleanup
	case StateAbortInProgress:
		return next == StateTerminalCleanup || next == StateRecoveryRequired
	case StateRollbackInProgress:
		return next == StateRolledBack || next == StateRecoveryRequired
	case StateRolledBack:
		return next == StateTerminalCleanup
	case StateTerminalCleanup:
		return next == StateIdle || next == StateRecoveryRequired
	case StateRecoveryRequired:
		return next == StateAbortInProgress || next == StateRollbackInProgress
	default:
		return false
	}
}
