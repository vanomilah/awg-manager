# Gate 1 Architecture Review Summary

## Overview of Changes
The Gate 1 architecture has been significantly refactored to support a robust Last Known Good (LKG) pointer architecture and an immutable bundle deployment model. All legacy tests have been ported and patched to verify these new architectural invariants.

### 1. CAS (Compare-And-Swap) Re-architecture
- Transitioned CAS operations to a strict `expected current -> immutable next` API design.
- The mutation phase no longer blindly overwrites states. It guarantees atomic transitions by validating pre-mutation digests against expected values before allowing a transaction to advance.
- Pre-mutation store digests and active config digests are rigidly verified during the `StatePreflight` phase.

### 2. Lock & Transaction Confinement
- Interprocess transaction locks are now held throughout the entire `apply` and `recovery` critical operations, eliminating race conditions during multi-process state transitions.
- All suppressed errors (`_ =`) have been removed from the coordinator and store paths to enforce strict error bubbling and deterministic failure handling.

### 3. Non-Consuming LKG Architecture
- The Last Known Good (LKG) bundle is no longer "extracted" or consumed during a rollback.
- `rollbackActiveLocked` now directly restores from the `m.PreMutationStoreSnapshotFile` when recovering a failed transaction.
- The `PublishStagedBundle` method was decoupled from LKG pointer advancement. The LKG pointer is now explicitly and safely advanced via `AdvanceLKGPointer()` *only* at the final `CommitPoint`.
- Pointer write failures (e.g., `lkg.pointer.json`) are now strictly **non-fatal**. If a pointer fails to write, the system logs a warning but proceeds, preserving the bundle and allowing the transaction to succeed.

### 4. Legacy Test Assertions & Fixes
The `gate1_legacy_test.go` suite was refactored to align with the new recovery semantics:
- **S04 (JournalWriteFail)**: Patched to verify that the recovery marker does *not* exist when a transaction fails cleanly before disk modifications, correctly returning to `StateIdle`.
- **S10 (BundleStagingFsync)**: Removed the `pointer_write` and `pointer_fsync` subtests, as LKG pointer staging failures no longer fail the bundle deployment.
- **S11 (PreMutationDigestMismatch)**: Assertions updated to explicitly expect `StateRecoveryRequired` when an external mutation corrupts the store digest, successfully detecting split-brain scenarios.
- **S15 (SwapActive_ManifestWriteFail)**: Updated to expect `StateIdle` (clean rollback) instead of `StateRecoveryRequired`, since the rollback now safely restores state from the persistent, unconsumed LKG bundle.
- **S12, S16, S19, S23, S24**: Successfully updated to handle pointer non-fatality, draft journal confinement, and verified active hooks.

## Instructions for the Reviewing Agent
1. Please review the atomic invariants established in the new `MutateAndApply` state machine (`coordinator.go`).
2. Verify that the decoupling of `PublishStagedBundle` and `AdvanceLKGPointer` (`generation_store.go`) does not introduce edge cases during system crashes.
3. Confirm that the `gate1_legacy_test.go` assertions are cryptographically and logically sound representations of the `StateIdle` vs `StateRecoveryRequired` paradigm.
