# Mihomo Stage 2 — Gate 1 Remediation Plan: v15 Delta

This document addresses the 10 specific boundaries identified in the v14 Pro Review (`MIHOMO_STAGE2_GATE1_REMEDIATION_PLAN_V14_PRO_REVIEW_2026-09-16.md`) to ensure safe recovery and strict path confinement.

## 6.1 Manifest Checkpoint CAS vs. State-Transition CAS (P0-1)

```go
// Checkpoints are for same-phase payload changes (e.g., adding to a journal).
func (c *Coordinator) checkpointManifestLocked(m *TransactionManifest) error {
    if err := m.ValidateSchemaForPhase(m.State); err != nil {
        return err // Phase-specific validation before any write
    }
    return c.casManifest(m, m.State)
}

// Transitions are strictly for phase progression.
func (c *Coordinator) transitionManifestLocked(m *TransactionManifest, nextState ManifestState) error {
    if m.State == nextState {
        return fmt.Errorf("transition requires state change, use checkpoint for payload updates")
    }
    return c.casManifest(m, nextState)
}

func (c *Coordinator) casManifest(m *TransactionManifest, targetState ManifestState) error {
    durable, err := c.store.ReadDurableManifest()
    if err != nil { return err }

    if durable.TxID != m.TxID || durable.State != m.State || durable.Sequence != m.Sequence {
        return ErrConcurrentModification
    }

    next := m.Clone()
    next.State = targetState
    next.Sequence = m.Sequence + 1
    next.UpdatedAt = time.Now()

    err = c.store.WriteAtomicManifest(next)
    if err != nil {
        if isAmbiguousError(err) {
            actual, readErr := c.store.ReadDurableManifest()
            if readErr != nil { return TransitionOutcome{State: StateRecoveryRequired, Err: err} }
            
            // Exact new accepted
            if actual.Sequence == next.Sequence && actual.State == next.State {
                *m = *next; return nil 
            }
            // Exact old retained, caller must retry
            if actual.Sequence == m.Sequence && actual.State == m.State {
                return ErrRetainExactOld 
            }
            return TransitionOutcome{State: StateRecoveryRequired, Err: err}
        }
        return err
    }
    *m = *next
    return nil
}
```
*Tests added*: Stale checkpoint rejection; ambiguous checkpoint exact-old/new resolution.

## 6.2 Separated RuntimeOn vs. RuntimeOff Swap Protocols (P0-2)

RuntimeOff strictly has no candidate config file and must unlink the active config, not rename it.

```text
// Active Swap / Unlink Resolver
if Candidate.RuntimeMode != "off":
    // 1. Candidate config was securely copied to active staging (Section 6.5)
    // 2. Stop running process
    // 3. Rename staging -> active
    renameOutcome = secDir.RenameAndResolve(activeStaging, activeConfig, ExpectedDigest)
else:
    // 1. Stop running process (must precede unlink)
    // 2. Unlink active config securely
    renameOutcome = secDir.UnlinkAndResolve(activeConfig)
```

**UnlinkAndResolve Outcome Table (RuntimeOff)**:
- `ENOENT` on active file = `OutcomeNewApplied` (Exact new).
- Success on active file = `OutcomeNewApplied` (Exact new).
- Ambiguous network/disk error = read directory. If absent, `OutcomeNewApplied`. If present, `OutcomeOldRetained` (Retryable). If unreadable, `StateRecoveryRequired`.

## 6.3 Commit Intent and Startup Roll-Forward (P0-3)

The split-brain window between writing `verified-active.json` and updating the manifest is solved by a dedicated commit intent and startup roll-forward.

```go
// 1. Commit Intent
c.transitionManifestLocked(m, StateCommitIntent)

// 2. Write verified-active.json
err := writeVerifiedActive(m.CandidateGenerationID)

// 3. Cleanup Journal (Write failure here MUST NOT rollback!)
// 4. Transition to StateCommitted
```

**Startup Recovery Rules around Commit**:
- Compare `verified-active.json` ID with `m.CandidateGenerationID`:
  - **Exact Match**: The commit is durable. Recovery MUST roll forward.
  - **Match with `m.PreviousGenerationID`**: Commit was not applied. Rollback is still allowed.
  - **Any other value**: `StateRecoveryRequired` (corrupt state).

## 6.4 Validated Typed Identifiers for Cleanup (P0-4)

The cleanup journal must never store arbitrary paths.

```go
type CleanupJournal struct {
    Sequence       uint64
    TxID           string
    StagingNames   []string // e.g., "staging-gen-xxx"
    SnapshotNames  []string // e.g., "snapshot-xxx.json"
    GenerationIDs  []string // e.g., "gen-xxx"
}
```
- **Validation**: Regex `^[a-zA-Z0-9_-]+$`. Reject `.`, `..`, separators, absolute paths, empty values, alternate volumes.
- **Root Selection**: `StagingNames` and `GenerationIDs` are removed relative to `generationsDirFD`. `SnapshotNames` relative to `snapshotsDirFD`.
- **Failure**: A malformed identifier or unparsable journal causes cleanup to fail closed (preserve all artifacts).

## 6.5 Active Staging is a Secure Copy (P1-1)

Activation DOES NOT rename `generations/<id>/config.yaml`.
- The bundle is fully immutable.
- During activation, a secure copy (or `reflink`) is created at a staging path `generations/active.staging.yaml`.
- Its digest is verified *after* copying to ensure no TOCTOU during copy.
- This staging file is then renamed to the active location `mihomo.yaml`.

## 6.6 Durable Bridge Ownership and Withdrawal Journal (P1-2, P1-3)

**Ownership Contract**: Bridge names must be deterministically tagged with the TxID or Generation ID, OR the kernel interface must store a verifiable alias/comment that matches `m.TxID`. Recovery inspects the actual system state, not just the manifest.

**Withdrawal Protocol**:
```go
m.BridgeWithdrawals = append(m.BridgeWithdrawals, BridgeOperation{Ref: b, State: BridgeIntent})
c.checkpointManifestLocked(m) // Use checkpoint!

err := c.bridgeRuntime.DeleteBridge(b)
actualState, inspectErr := c.bridgeRuntime.InspectBridge(b)
if inspectErr != nil { return StateRecoveryRequired }

if !actualState.Exists {
    m.BridgeWithdrawals[len(m.BridgeWithdrawals)-1].State = BridgeVerified
    c.checkpointManifestLocked(m)
} else if actualState.Owner != m.TxID {
     // Exists but we don't own it anymore (or never did). Withdrawal is moot/successful.
    m.BridgeWithdrawals[len(m.BridgeWithdrawals)-1].State = BridgeVerified
    c.checkpointManifestLocked(m)
} else {
    // Retryable
    return ErrRetainExactOld
}
```

## 6.7 OutcomeAbsent Phase Table (P1-4)

When `InspectDirectoryPublication` returns `OutcomeAbsent` (target and staging both absent), the caller resolves it via the current phase:
- **Phase <= `StateStaging`**: Target was never published. Treat as normal pre-publication absence (abort is safe).
- **Phase >= `StateCandidatePublished`**: A record claims publication occurred. Absence means rollback evidence is lost. Transition to `StateRecoveryRequired`.

## 6.8 Bundle Contents & Secure Deletion Invariants (P1-5, P1-6)

**Bundle Integrity Requirements (`VerifyBundleIntegrity`)**:
- Strictly read via `openat` with `O_NOFOLLOW | O_CLOEXEC`.
- Required: `manifest.json`.
- Optional: `config.yaml` (MUST be absent if `RuntimeOff`), `store.snapshot.json`.
- Reject any unknown files.
- All entries must be regular files (`S_IFREG`) and link count == 1.
- Specific UID/GID ownership and restricted mode requirements must pass.

**Secure Deletion Invariants (`SecureRemoveAll`)**:
- MUST compare `st_dev` of the parent descriptor with the trusted root `st_dev` to prevent mount point crossings.
- MUST reject special files (fifos, device nodes).
- Only unlinks validated regular files and directories.

## 6.9 Idle & Manifest Removal Lifecycle (P1-7)

- `StateIdle` is a **durable tombstone** indicating the transaction is definitively complete.
- After successfully transitioning to `StateIdle`, the coordinator attempts to securely unlink `tx-manifest.json`.
- A failed or ambiguous unlink leaves the `StateIdle` manifest on disk.
- Startup handles `StateIdle` idempotently: it observes the completed transaction, performs no recovery actions, and safely deletes the manifest.

## 6.10 Verification Commands (P1-8)

Restored Gate 1 validation matrix. No IPKs or deployments.
```bash
go test -race -v -count=1 ./internal/strictfs
go test -race -v -count=1 ./internal/mihomo
go vet ./internal/mihomo/...
go test -race -count=1 ./internal/strictfs ./internal/mihomonative ./internal/singbox/router ./internal/mihomo
go test -count=1 ./internal/...
go test -count=1 ./...
git diff --check
```
