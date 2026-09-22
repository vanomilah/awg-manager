# Mihomo Remediation Gate B Rev11 — Resolution Report

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Task Specification:** `reports/mihomo/MIHOMO_GATE_B_REV10_ACCEPTANCE_AND_FIX_NOW_2026-09-22.md`  
**Associated Patch:** `reports/mihomo/GATE_B_REV11_DIFF_2026-09-22.patch`  

---

## 1. Executive Summary

This report documents the resolution of the defects identified in `reports/mihomo/MIHOMO_GATE_B_REV10_ACCEPTANCE_AND_FIX_NOW_2026-09-22.md` (P0-1, P0-2, P0-3, P0-4, P1-1, P1-2, P1-3). All changes have been implemented directly in the production codebase and verified by automated unit, crash, and race test suites.

- **Filesystem Path Containment (P0-1):** Path traversal checks and symlink rejection added for all transaction recovery files. Candidate generation IDs are validated via `ValidateBasename` and confirmed to resolve strictly within `generationsDir`. Backups must be direct children of `ConfigDir`. Malicious or malformed manifests fail closed into `recovery_required` without modifying authoritative files or deleting external paths.
- **Pre-Commit Crash Recovery & Convergence (P0-2):** Pre-commit recovery restores the clean legacy pair, unlinks candidate generations, and under the same transaction lock immediately executes a fresh migration to converge to v1 with complete refs. Production validation semantics (`ValidateComplete()` and slot uniqueness) are enforced in `DurableBridgeRegistry` and tested across all pre-commit crash points. Subsequent restarts are idempotent.
- **Fail-Closed Roll-Forward Recovery (P0-3):** Post-commit roll-forward validates candidate bundle data, verifies serialized candidate record digest against `CandidateRecordDigest`, writes both files atomically, and re-reads and asserts 5-point agreement before committing and unlinking backups. Recovery write and cleanup failures are handled explicitly and fail closed.
- **Recovery Marker Precedence (P0-4):** Structurally valid in-flight migration transactions are reconciled before generic recovery markers block startup. Transaction-owned markers are cleared upon successful convergence; foreign/unrelated recovery markers remain blocked.
- **Fail-Closed Backup Creation (P1-1):** Legacy migration requires a valid LKG pointer agreeing with the legacy record, verifies backup file digests, and records backup paths and digests in the manifest before persisting migration intent.
- **Explicit Rollback Error Propagation (P1-2):** `rollbackMigrationBeforeCommitLocked` returns an explicit `error`, validates restored file digests against the manifest, keeps manifest/backups when restoration is incomplete, and propagates `SetActiveTransactionRoles` errors.
- **Role Reconstruction State Allowlist & Provenance (P1-3):** `isAuthorizedPortReplacementLocked` enforces `ValidateSchemaForPhase()`, manifest basename provenance, an explicit allowlist of states (`StateBridgesReconciling`, `StateSwapApplied`, `StateSwapVerified`, `StateRuntimeIntent`, `StateRuntimeApplied`, `StateRollbackBridgesVerified`), rejects `OperationMigration`, and verifies matching bridge digest proof.

---

## 2. Changed Files

| File | Status | Description |
|---|---|---|
| `internal/mihomo/types.go` | Modified | Added basename validation to `TransactionManifest.ValidateSchema()`; enforced structural path containment checks for backup files and candidate generation IDs in `ValidateSchemaForPhase()`; added `StateMigrationActiveWritten` and `StateMigrationPointerWriteIntent` to valid transitions to `StateMigrationCommitted`. |
| `internal/mihomo/generation_store.go` | Modified | Updated `PublishStagedBundle` to preserve `AppliedAt` when non-zero to guarantee byte-identical candidate record serialization and digest match during roll-forward recovery. |
| `internal/mihomo/coordinator.go` | Modified | Implemented strict path containment helpers `validateMigrationBackupPathLocked` and `validateMigrationManifestLocked`; updated `rollbackMigrationBeforeCommitLocked` to return `error` and verify restored digests; updated `rollForwardMigrationLocked` to fail closed on read/write/verify/unlink errors and assert 5-point file agreement; updated `RecoverOnStartup` to reconcile valid in-flight migrations before generic markers, clear matching transaction markers, and execute immediate fresh migration on pre-commit rollback; added recovery failpoints. |
| `cmd/awg-manager/mihomo_bridge_runtime.go` | Modified | Added missing `path/filepath` import; updated `isAuthorizedPortReplacementLocked` with explicit allowlist of states, schema and phase validation, rejection of migration operations, and manifest path provenance. |
| `cmd/awg-manager/mihomo_bridge_runtime_test.go` | Modified | Updated `TestAuthorizedPortReplacement_P1_3_NegativeCases` to use `newGateBTestCluster`, `ProxyBridge`, and valid `ManifestState` constants (`StateCandidatePublished`, `StateCommitted`). |
| `internal/mihomo/gate_b_rev8_crash_test.go` | Modified | Enforced `ValidateComplete()` and slot uniqueness in `persistentNDMSRuntime.ReplaceDurableBridges`; updated `verifyPostCrashMigration` to assert v1 convergence on first restart across all 8 crash points and idempotent second restart. |
| `internal/mihomo/gate_b_rev8_migration_test.go` | Modified | Enforced `ValidateComplete()` and slot uniqueness in `mockRev8Registry.ReplaceDurableBridges`; updated `TestGateB_Rev8_StartupMigration_RestartAfterInjectedFailures` to verify recovery to `StateIdle`; added P0-1 path containment tests, P0-4 foreign marker tests, and P0-3 recovery write failure tests. |
| `reports/mihomo/GATE_B_REV11_DIFF_2026-09-22.patch` | **New File** | Clean UTF-8 patch against committed baseline covering all Rev 11 remediation changes. |
| `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV11_RESOLUTION_REPORT_2026-09-22.md` | **New File** | This resolution report written with UTF-8 BOM. |

---

## 3. Explicit Mapping of Audit Findings to Implementation

### 3.1 P0-1: Path Containment & Manifest Validation in Migration Recovery

- **Defect:** `recoverMigrationTransactionIfPresentLocked` consumed `PreviousRecordFile`, `PreviousLKGPointerFile`, and `CandidateGenerationID` without schema/phase validation, path resolution, or containment checks, posing traversal and symlink escape risks.
- **Resolution:**
  - `internal/mihomo/types.go`:
    - `TransactionManifest.ValidateSchema()` validates `CandidateGenerationID`, `PreviousGenerationID`, `LKGGenerationID`, etc. with `strictfs.ValidateBasename`.
    - `TransactionManifest.ValidateSchemaForPhase()` for migration states enforces clean, non-empty paths for `PreviousRecordFile` and `PreviousLKGPointerFile`.
  - `internal/mihomo/coordinator.go`:
    - Added `validateMigrationBackupPathLocked`: verifies `filepath.Dir(clean) == cleanConfigDir`, `filepath.Base(clean) == expectedBasename` (`migration_backup_active_<txid>.json` / `migration_backup_pointer_<txid>.json`), checks `os.Lstat` to reject symlinks / reparse points (`ModeSymlink != 0`).
    - Added `validateMigrationManifestLocked`: invokes `m.ValidateSchema()`, `m.ValidateSchemaForPhase()`, `strictfs.ValidateBasename(m.CandidateGenerationID)`, and validates both backup file paths.
    - Candidate directory is resolved strictly via `filepath.Join(c.genStore.GenerationsDir(), m.CandidateGenerationID)` and removed via `c.genStore.RemoveCandidateGeneration(m.CandidateGenerationID, ...)` which enforces `strictfs.NewSecureDir`, containment, and directory fsync.
    - Any validation failure logs error, preserves manifest/evidence, enters `StateRecoveryRequired`, and leaves authoritative files untouched.
- **Tests Implemented:**
  - `TestGateB_Rev11_P0_1_MaliciousManifest_CandidatePathTraversal`: tests traversal attempts (`..`, `../outside_victim`). Asserts `outside_victim` directory remains intact and state enters `recovery_required`.
  - `TestGateB_Rev11_P0_1_MaliciousManifest_OutsidePreviousRecord`: asserts external file outside `ConfigDir` is not read or deleted.
  - `TestGateB_Rev11_P0_1_MaliciousManifest_SymlinkBackup`: asserts symlinked backup file fails closed and target is not modified.
  - `TestGateB_Rev11_P0_1_MaliciousManifest_InvalidSequenceStateDigests`: asserts invalid state/sequence manifests fail closed.

### 3.2 P0-2: Pre-Commit Crash Recovery & Clean Convergence to v1

- **Defect:** Pre-commit rollback wrote `migration.rolled_back` marker which permanently suppressed migration, leaving legacy refs passed to `ReplaceDurableBridges`.
- **Resolution:**
  - Removed `migration.rolled_back` marker file.
  - In `RecoverOnStartup`, when a pre-commit crash transaction is rolled back, the legacy record and LKG pointer are restored to their clean pre-migration state, uncommitted candidate generations are removed, and `RecoverOnStartup` immediately invokes `upgradeLegacyAppliedBridgeIdentityLocked` under the same transaction lock.
  - The migration completes cleanly during startup, promoting the system to `BridgeIdentityVersion = 1` with complete refs and transitioning to `StateIdle`.
  - A second restart is idempotent: `rec.IsLegacyBridgeIdentity()` evaluates to false, startup migration is skipped, and the v1 record is loaded directly.
  - In `gate_b_rev8_crash_test.go` and `gate_b_rev8_migration_test.go`, the test registries (`persistentNDMSRuntime` and `mockRev8Registry`) were updated to enforce production validation: every bridge ref must satisfy `b.ValidateComplete() == nil` and slot keys must be unique.
- **Tests Implemented:**
  - `TestGateB_Rev8_Crash_Migration_01_BeforeBundlePublish` through `03_BeforeActiveWrite`: all 3 pre-commit crash points verified to converge to `StateIdle` on first restart and remain idempotent on second restart with 0 legacy refs passed to `ReplaceDurableBridges`.

### 3.3 P0-3: Fail-Closed Post-Commit Roll-Forward Recovery

- **Defect:** Post-commit roll-forward recovery ignored errors from bundle reading, JSON marshaling, atomic writes, pointer advancement, agreement verification, and cleanup unlinks.
- **Resolution:**
  - `internal/mihomo/coordinator.go` (`rollForwardMigrationLocked`):
    - Reads candidate bundle via `c.genStore.ReadGenerationBundle(m.CandidateGenerationID)`. Returns error if missing or corrupt.
    - Reconstructs candidate record from bundle metadata and verifies `candidateRec.Digest() == m.CandidateRecordDigest`. Returns error on mismatch.
    - Writes `verified-active.json` atomically via `strictfs.StrictWriteAtomic`. Returns error on write failure.
    - Advances LKG pointer via `c.genStore.AdvanceLKGPointer(...)`. Returns error on failure.
    - Re-reads and decodes both `verified-active.json` and `lkg.pointer.json` from disk, asserting exact 5-point agreement:
      1. `readActive.GenerationID == readPtr.GenerationID`
      2. `readActive.Generation == readPtr.GenerationNumber`
      3. `readActive.AppliedConfigDigest == readPtr.AppliedConfigDigest`
      4. `readActive.AppliedStoreDigest == readPtr.AppliedStoreDigest`
      5. `readActive.BridgeIdentityVersion == CurrentBridgeIdentityVersion` (1) and `readActive.AppliedBridgesDigest == m.TargetBridgesDigest`
    - Advances manifest to `StateMigrationCommitted` via `c.transitionManifestLocked`. Returns error on failure.
    - Unlinks backup files and manifest with error checking; errors are joined and returned, preventing false reporting of successful recovery when durability is uncertain.
- **Tests Implemented:**
  - `TestGateB_Rev11_P0_3_RecoveryWriteFailures`: subtests injecting faults into:
    - `active_write_failure`
    - `pointer_write_failure`
    - `active_verify_failure`
    - `pointer_verify_failure`
    - `manifest_cas_failure`
    - `backup_unlink_failure`
    - `manifest_unlink_failure`
    Asserts each failure mode fails closed without false `StateIdle` or false success.

### 3.4 P0-4: Recovery Marker Precedence & In-Flight Reconciliation

- **Defect:** Injected I/O failures (e.g. pointer advance failure) left a `recovery.marker` on disk. On restart, `RecoverOnStartup` checked `recovery.marker` before checking for in-flight migration manifests, permanently blocking startup in `ErrRecoveryRequired`.
- **Resolution:**
  - `internal/mihomo/coordinator.go` (`RecoverOnStartup`):
    - Checks for an in-flight transaction manifest (`config.yaml.txn.json`) with `m.OperationKind == OperationMigration` and valid schema before blocking on `recovery.marker`.
    - Reconciles the in-flight migration transaction.
    - Upon successful reconciliation, clears the recovery marker if the marker content indicates it was created by the migration transaction (`strings.Contains(content, "migration")` or `strings.Contains(content, m.TxID)`).
    - If the marker was created by a foreign or unrelated subsystem, it is preserved and startup returns `ErrRecoveryRequired`.
- **Tests Implemented:**
  - `TestGateB_Rev8_StartupMigration_RestartAfterInjectedFailures`: verified that after removing the fault injection, restart cleanly recovers to `StateIdle`.
  - `TestGateB_Rev11_P0_4_ForeignRecoveryMarkerBlocksStartup`: verified that a foreign recovery marker continues to block startup with `ErrRecoveryRequired`.

### 3.5 P1-1: Fail-Closed Backup Creation

- **Defect:** `upgradeLegacyAppliedBridgeIdentityLocked` ignored LKG pointer read errors and backup write errors, potentially crossing the commit boundary without a valid rollback source.
- **Resolution:**
  - `upgradeLegacyAppliedBridgeIdentityLocked` requires a present, strictly decoded, schema-valid `lkg.pointer.json`.
  - Verifies that the legacy LKG pointer matches the legacy `verified-active.json` generation number and ID.
  - Atomically writes `migration_backup_active_<txid>.json` and `migration_backup_pointer_<txid>.json`.
  - Verifies the backup files exist on disk, re-reads them, and asserts their SHA-256 digests match the source record digests.
  - Stores the verified backup file paths and digests in the manifest before persisting `StateMigrationIntent`.

### 3.6 P1-2: Explicit Rollback Error Propagation

- **Defect:** `rollbackMigrationBeforeCommitLocked` returned no error and ignored restoration, candidate removal, and active-role cleanup failures.
- **Resolution:**
  - Signature updated to `rollbackMigrationBeforeCommitLocked(m *TransactionManifest) error`.
  - Verifies that restored file digests match `PreviousRecordDigest` and `PreviousLKGPointerDigest`.
  - If restoration fails or restored digests mismatch, keeps manifest/backups, logs error, and returns error without deleting transaction evidence.
  - Propagates errors from `c.syncActiveTransactionRolesLocked(nil)`.

### 3.7 P1-3: Role Reconstruction State Allowlist & Provenance Checks

- **Defect:** `isAuthorizedPortReplacementLocked` in `cmd/awg-manager/mihomo_bridge_runtime.go` only checked `m.ValidateSchema()` and accepted any non-terminal state, including migration states.
- **Resolution:**
  - Enforces `filepath.Base(cleanPath) == "config.yaml.txn.json"`.
  - Enforces `m.ValidateSchema() == nil` and `m.ValidateSchemaForPhase() == nil`.
  - Enforces `m.OperationKind != mihomo.OperationMigration`.
  - Enforces explicit allowlist of states:
    - `StateBridgesReconciling`
    - `StateSwapApplied`
    - `StateSwapVerified`
    - `StateRuntimeIntent`
    - `StateRuntimeApplied`
    - `StateRollbackBridgesVerified`
  - Verifies that `PreviousBridgesDigest` and `TargetBridgesDigest` match `BridgesDigest()` calculations.
- **Tests Implemented:**
  - `TestAuthorizedPortReplacement_P1_3_NegativeCases`:
    - `malformed_previous_digest`: rejected
    - `incomplete_previous_refs`: rejected
    - `migration_operation_kind_rejected`: rejected
    - `disallowed_manifest_state` (`StateCandidatePublished`): rejected
    - `stale_completed_manifest_state` (`StateCommitted`): rejected

---

## 4. Verification Evidence

### 4.1 Full Test Suite

Command:
```bash
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
```

Output:
```text
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	34.317s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	0.306s
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	0.049s
ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	6.503s
```

### 4.2 Race Detector

Command:
```bash
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
```

Output:
```text
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	38.101s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	1.884s
```
Zero data races reported.

### 4.3 Git Whitespace and Formatting Check

Command:
```bash
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Output:
```text
(Clean exit with code 0 - zero formatting or whitespace issues)
```

### 4.4 Patch Validation

Command:
```bash
git apply --check reports/mihomo/GATE_B_REV11_DIFF_2026-09-22.patch --reverse
```

Output:
```text
(Clean exit with code 0 - patch applies cleanly in reverse against the working tree)
```

---

## 5. Constraints & Prohibitions Compliance Confirmation

- **Absolute Ban on `--force-reinstall`:** No command, script, or documentation contains `opkg install --force-reinstall`.
- **Absolute Ban on `--cleanup`:** `/opt/bin/awg-manager --cleanup` is not automated, placed in scripts, or executed.
- **Authoritative Workspace:** All work executed strictly in `E:\AWGM\awg-manager` on branch `feature/mihomo-ai-proxyrt`. `E:\AWGM\awg-manager-mihomo` was untouched.
- **Routing & Server Integrity:** Port 1099, `Wireguard2`, and `telemt` on `0.0.0.0:8443` were preserved.
- **No IPK or Router Deployments:** No packages built; no deployment to router `192.168.90.1`.
- **Strictly Grounded Claims:** All statements in this report reflect actual test executions and command exit codes without overstatement.
