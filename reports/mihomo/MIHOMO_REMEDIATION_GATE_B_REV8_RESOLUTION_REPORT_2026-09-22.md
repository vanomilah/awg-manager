# Mihomo Remediation Gate B Revision 8 Resolution Report

**Date:** 2026-09-22  
**Author:** Antigravity  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Repository:** `e:\AWGM\awg-manager`  
**Target Architecture:** Keenetic MIPS / OpenWrt Linux  
**Patch Artifact:** `reports/mihomo/GATE_B_REV8_DIFF_2026-09-22.patch`  
**Implementation Plan Reference:** `reports/mihomo/MIHOMO_GATE_B_REV8_IMPLEMENTATION_PLAN_FINAL_2026-09-22.md`

---

## 1. Executive Summary

This report provides formal documentation and verification sign-off for the implementation of **Gate B Revision 8** (Durable Bridge Identity, Slot-Based Reconciliation, Split Runtime Resolvers, and Zero-State-Loss Rollback).

All architectural flaws and regression surfaces identified in previous revisions (Revisions 1 through 7) have been systematically resolved:
1. **ListenPort as Durable Identity:** `ListenPort int` was added to `BridgeRef` and integrated into all slot keys, digests, equality predicates, schema validations, and producer pipelines.
2. **Safe Legacy Migration & Pre-Mutation Enrichment:** Legacy bridges lacking `ListenPort` or canonical attributes are enriched against the pre-mutation store snapshot *before* mutations occur, preventing unpopulated identities from reaching candidate manifests.
3. **Persisted Target Bridges Before Side Effects:** Candidate target bridges and their cryptographic digests (`TargetBridgesDigest`) are validated and persisted to the transaction manifest prior to candidate configuration compilation and disk writes.
4. **Split Production Runtime Resolvers:** `mihomoBridgeRuntime` implements distinct resolvers for `resolvePublishIdentity` (strict store match), `resolveInspectIdentity` (durable owner matching, store mutation resilience), and `resolveWithdrawIdentity` (durable owner matching, store absence allowed).
5. **Slot-Based Partitioning & Controlled Replacement:** `syncBridgesLocked` partitions reconciliation by `SlotKey()`. Controlled replacements (different owner or modified port on same slot) execute all withdrawals before any creations (`toWithdraw` precedes `toCreate`).
6. **Manifest-Driven Rollback:** Rollback operations consume durable manifest snapshots (`m.TargetBridges` to undo, `m.PreviousBridges` to restore) without calling `ListActiveBridges()`, eliminating circular store lookups and false `ErrForeignBridgeOwnership` halts.
7. **DurableBridgeRegistry:** Interface implemented by the production runtime to persist and restore durable bridge state across restart cycles without numeric range scanning.
8. **Test Coverage & Verification:** All 22 required Gate B test scenarios pass with zero failures and zero race conditions under `go test -race -count=1`.

---

## 2. Gate B Rev 8 Architecture & Stages Overview

| Stage | Focus Area | Status | Impacted Files |
| :--- | :--- | :--- | :--- |
| **Stage 1** | Types & Producers: `ListenPort`, `DurableBridgeRegistry` | **COMPLETED** | `internal/mihomo/types.go`, `internal/mihomonative/store.go`, `internal/singbox/router/service_mihomo.go`, `cmd/awg-manager/mihomo_bridge_runtime.go` |
| **Stage 2** | Legacy Migration & Pre-Mutation Persistence | **COMPLETED** | `internal/mihomo/coordinator.go`, `internal/mihomo/types.go` |
| **Stage 3** | Split Production Runtime Resolvers | **COMPLETED** | `cmd/awg-manager/mihomo_bridge_runtime.go` |
| **Stage 4** | Slot Reconciliation, Controlled Replacement & Rollback | **COMPLETED** | `internal/mihomo/coordinator.go` |
| **Stage 5** | Test Suite Expansion & Matrix Verification | **COMPLETED** | `internal/mihomo/*_test.go`, `cmd/awg-manager/mihomo_bridge_runtime_test.go` |
| **Stage 6** | Verification, Focused Patch & Documentation | **COMPLETED** | `reports/mihomo/GATE_B_REV8_DIFF_2026-09-22.patch`, `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV8_RESOLUTION_REPORT_2026-09-22.md` |

---

## 3. Concrete Changes by Component

### 3.1 `internal/mihomo/types.go` & `types_test.go`
- **Field Addition:** Added `ListenPort int` (`json:"listen_port"`) to `BridgeRef`.
- **Identity Predicates:**
  - `SlotKey()`: Returns `"<ProxyIndex>:<ProxyInterface>:<KernelInterface>"`.
  - `SameOwnerAndEndpoint(other)`: Requires identical `SlotKey()`, `OwnerUUID`, and `ListenPort`.
  - `ValidateComplete()`: Enforces `ProxyIndex > 0`, non-empty `ProxyInterface`, non-empty `KernelInterface`, non-empty `OwnerUUID`, and `1 <= ListenPort <= 65535`.
  - `Digest()` and `BridgesDigest()`: Deterministically include `ListenPort`.
  - `AppliedGenerationRecord.Equal()`: Evaluates `ListenPort` across all applied bridges.
- **Manifest Fields:**
  - Added `TargetBridges []BridgeRef` and `TargetBridgesDigest string`.
  - Added `PreviousBridges []BridgeRef` and `PreviousBridgesDigest string`.
  - Added duplicate slot detection in `ValidateSchema()`.
- **Runtime Abstraction:**
  - Defined `DurableBridgeRegistry` interface (`ReplaceDurableBridges([]BridgeRef)`).

### 3.2 Native Store & Producers (`internal/mihomonative`, `internal/singbox/router`)
- **Native Store Adapter (`internal/mihomonative/store.go`):**
  - Updated `TxAdapter().ListBridges()` to populate `ListenPort: b.Bridge.ListenPort` for both `proxy` and `service` bridge kinds.
- **Router Compiler (`internal/singbox/router/service_mihomo.go`):**
  - Updated compiler output to extract `ListenPort` from store bridges and inject it into compiled `TargetBridges`.

### 3.3 Production Runtime Resolvers (`cmd/awg-manager/mihomo_bridge_runtime.go`)
- **Split Resolvers Implemented:**
  - `resolvePublishIdentity(ref)`: Requires exact single-match in current native store (owner, slot, interfaces, and listen port). Rejects ambiguous or inconsistent definitions.
  - `resolveInspectIdentity(ref)`: Requires complete durable ref. Relies on `ref.OwnerUUID` as canonical authority. Does not require store match (permits store mutations during replacement and rollbacks).
  - `resolveWithdrawIdentity(ref)`: Requires complete durable ref. Absence from store is explicitly permitted. Protected by atomic kernel check `RemoveProxyIfOwned`.
- **Durable Registry Integration:**
  - Implemented `DurableBridgeRegistry` on `mihomoBridgeRuntime`.
  - Maintains `durableRefs map[string]mihomo.BridgeRef`.
  - Updated `ListObservedBridges()` to form a validated union of durable registry bridges and native store bridges, eliminating numeric range scanning.

### 3.4 Apply Coordinator & Reconciliation Engine (`internal/mihomo/coordinator.go`)
- **Pre-Mutation Snapshot & Enrichment:**
  - Prior to executing `mutateFn()`, takes store snapshot, enriches legacy previous bridges using `enrichLegacyBridgeRefLocked`, and persists `manifest.PreviousBridges` and `PreviousBridgesDigest` under `StateSnapshotSecured`.
- **Target Bridges Validation & Candidate Write Gating:**
  - Validates all `compileResult.TargetBridges` for completeness and slot uniqueness.
  - Persists `manifest.TargetBridges` and `TargetBridgesDigest` before candidate config writes.
- **Slot-Based Partitioning (`syncBridgesLocked`):**
  - Completely rewritten to partition by `SlotKey()`.
  - **Retained Bridges:** When `oldRef.SameOwnerAndEndpoint(targetRef)`, checks live state. Self-heals if absent from NDMS; migrates legacy owners; skips mutation if matching canonical owner.
  - **Controlled Replacements:** When slot is present in both before and target but owner or port differs:
    - Queues `oldRef` into `toWithdraw`.
    - Queues `targetRef` into `toCreate`.
  - **Pure Withdrawals:** Queues slots present in before but absent in target into `toWithdraw`.
  - **Pure Creations:** Queues slots present in target but absent in before into `toCreate`.
  - **Execution Order Guarantee:** ALL `toWithdraw` operations execute strictly before ANY `toCreate` operations.
- **Deterministic Journal Operation IDs:**
  - Format: `<txID>-<action>-<slotKey>-<digest>`.
  - Replay consistency verification against recorded target digest and ref interface.
- **Manifest-Driven Rollback:**
  - `rollbackActiveLocked()` uses `m.TargetBridges` as `before` and `m.PreviousBridges` as `target`.
  - `resumeRollbackTransactionLocked()` uses `m.TargetBridges` as `before` and `gm.AppliedBridges` as `target`.
  - Zero calls to `ListActiveBridges()`, eliminating circular dependency on mutated store.

---

## 4. Required Scenario Verification Matrix

The following table maps the 22 required Gate B scenarios to their implementing test cases and verification results:

| # | Scenario Description | Test Function | Result |
| :--- | :--- | :--- | :--- |
| **1** | Normal apply with new bridges | `TestGateB_Rev8_EndToEnd_NormalApplyAndCommit` | **PASS** |
| **2** | Apply with bridge deletions | `TestGateB_Rev8_EndToEnd_ResourceDeletion` | **PASS** |
| **3** | Controlled same-slot replacement (withdraw before publish) | `TestGateB_Rev8_EndToEnd_SameSlotReplacement_WithdrawPrecedesPublish` | **PASS** |
| **4** | Port-only change controlled replacement (same slot, new port) | `TestGateB_Rev8_EndToEnd_PortChangeControlledReplacement` | **PASS** |
| **5** | Fail publication: rollback restores previous bridges | `TestGateB_Rev8_EndToEnd_FailPublication_RollbackRestoresPrevious` | **PASS** |
| **6** | Foreign takeover before withdrawal: fails closed, untouched | `TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection/foreign_takeover_before_withdrawal` | **PASS** |
| **7** | Foreign takeover before publish: fails closed, untouched | `TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection/foreign_takeover_before_publish` | **PASS** |
| **8** | Unmanaged live slot untouched | `TestGateB_Rev8_EndToEnd_UnmanagedLiveSlotUntouched` | **PASS** |
| **9** | Coexisting sing-box and user slots untouched | `TestGateB_Rev8_EndToEnd_CoexistingSingBoxAndUserSlotsUntouched` | **PASS** |
| **10** | Store-absent withdrawal succeeds with valid durable owner | `TestGateB_Rev8_EndToEnd_CurrentStoreB_DoesNotPreventDurableA_Withdrawal` | **PASS** |
| **11** | Empty runtime cache rebuilds from durable registry & store | `TestGateB_Rev8_EndToEnd_EmptyRuntimeCacheAfterRestart` | **PASS** |
| **12** | Corrupt bridge journal digest fails closed | `TestGateB_Rev8_EndToEnd_CorruptBridgeJournalDigestFailsClosed` | **PASS** |
| **13** | Checkpoints on disk in exact persistence order | `TestGateB_Rev8_EndToEnd_PersistenceOrder_CheckpointsOnDisk` | **PASS** |
| **14** | Complete bridge validation & slot keys | `TestGateB_Rev8_BridgeRef_CompleteValidation` | **PASS** |
| **15** | SameOwnerAndEndpoint requires identical port | `TestGateB_Rev8_BridgeRef_SameOwnerAndEndpoint` | **PASS** |
| **16** | Generation record equality includes ListenPort | `TestGateB_Rev8_AppliedGenerationRecord_ListenPortEquality` | **PASS** |
| **17** | Manifest schema validation detects duplicate slots | `TestGateB_Rev8_ManifestValidation_DuplicateSlots` | **PASS** |
| **18** | Pre-mutation snapshot persisted before mutateFn | `TestCoordinator_PreMutationStoreSnapshot_CapturedBeforeMutateFn` | **PASS** |
| **19** | Full crash matrix: crash before/after bridge withdraw & create | `TestGate4_CrashRecoveryMatrix` (all 26 failpoints) | **PASS** |
| **20** | Legacy bridge enrichment from pre-mutation store | `TestGateB_LegacyBridgeEnrichment_PopulatesListenPortAndSlot` | **PASS** |
| **21** | Inspect bridge netlink I/O error halts fail-closed | `TestGate4_CorruptionAndBoundaryFailpoints/TC-CR-P0-3a` | **PASS** |
| **22** | No numeric range scanning in observed bridges | `TestMihomoBridgeRuntime_ListObservedBridges_NoRangeScan` | **PASS** |

---

## 5. Comprehensive Verification Command Logs

### 5.1 End-to-End Suite (`cmd/awg-manager`)
```text
=== RUN   TestGateB_Rev8_EndToEnd_NormalApplyAndCommit
--- PASS: TestGateB_Rev8_EndToEnd_NormalApplyAndCommit (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_ResourceDeletion
--- PASS: TestGateB_Rev8_EndToEnd_ResourceDeletion (0.02s)
=== RUN   TestGateB_Rev8_EndToEnd_SameSlotReplacement_WithdrawPrecedesPublish
--- PASS: TestGateB_Rev8_EndToEnd_SameSlotReplacement_WithdrawPrecedesPublish (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_PortChangeControlledReplacement
--- PASS: TestGateB_Rev8_EndToEnd_PortChangeControlledReplacement (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_FailPublication_RollbackRestoresPrevious
--- PASS: TestGateB_Rev8_EndToEnd_FailPublication_RollbackRestoresPrevious (0.02s)
=== RUN   TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection
=== RUN   TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection/foreign_takeover_before_withdrawal_not_deleted
=== RUN   TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection/foreign_takeover_before_publish_not_overwritten
--- PASS: TestGateB_Rev8_EndToEnd_ForeignTakeoverProtection (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_UnmanagedLiveSlotUntouched
--- PASS: TestGateB_Rev8_EndToEnd_UnmanagedLiveSlotUntouched (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_CoexistingSingBoxAndUserSlotsUntouched
--- PASS: TestGateB_Rev8_EndToEnd_CoexistingSingBoxAndUserSlotsUntouched (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_CurrentStoreB_DoesNotPreventDurableA_Withdrawal
--- PASS: TestGateB_Rev8_EndToEnd_CurrentStoreB_DoesNotPreventDurableA_Withdrawal (0.00s)
=== RUN   TestGateB_Rev8_EndToEnd_EmptyRuntimeCacheAfterRestart
--- PASS: TestGateB_Rev8_EndToEnd_EmptyRuntimeCacheAfterRestart (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_PersistenceOrder_CheckpointsOnDisk
--- PASS: TestGateB_Rev8_EndToEnd_PersistenceOrder_CheckpointsOnDisk (0.01s)
=== RUN   TestGateB_Rev8_EndToEnd_CorruptBridgeJournalDigestFailsClosed
--- PASS: TestGateB_Rev8_EndToEnd_CorruptBridgeJournalDigestFailsClosed (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	0.141s
```

### 5.2 Unit & Crash Test Suite (`internal/mihomo`)
```text
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	32.368s
```

### 5.3 Race Detector Verification (`-race -count=1`)
```text
=== cmd/awg-manager race detection ===
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	1.844s

=== internal/mihomo race detection ===
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	36.342s
```

### 5.4 Cross-Package Regression Suite
```text
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	0.066s
ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	6.072s
ok  	github.com/hoaxisr/awg-manager/internal/api	1.247s
ok  	github.com/hoaxisr/awg-manager/internal/proxyrt	0.395s
```

### 5.5 Git Whitespace & Format Audit
```text
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
[Exit code: 0, zero whitespace or formatting errors]
```

---

## 6. Patch Artifact Reference

The complete, focused diff patch containing all Gate B Revision 8 code modifications has been saved to:
`reports/mihomo/GATE_B_REV8_DIFF_2026-09-22.patch`

Diff statistics:
```text
 cmd/awg-manager/mihomo_bridge_runtime.go      |  339 ++-
 cmd/awg-manager/mihomo_bridge_runtime_test.go | 1097 ++++++++-
 internal/mihomo/coordinator.go                | 3119 +++++++++++++++++++++----
 internal/mihomo/coordinator_legacy_test.go    |  250 +-
 internal/mihomo/types.go                      |  775 +++++-
 internal/mihomo/types_test.go                 |  401 +++-
 internal/mihomonative/store.go                |  165 +-
 internal/singbox/router/service_mihomo.go     |  292 +--
 8 files changed, 5670 insertions(+), 768 deletions(-)
```

---

## 7. Operational Commitments & Constraints Adherence

- **Strict Ban on `--force-reinstall`:** Verified. No command, script, or package directive uses `--force-reinstall`.
- **Strict Ban on `--cleanup`:** Verified. Automated execution of `--cleanup` is completely absent.
- **Repository Integrity:** Verified. All modifications reside exclusively in `e:\AWGM\awg-manager` on branch `feature/mihomo-ai-proxyrt`. `e:\AWGM\awg-manager-mihomo` was completely untouched.
- **Routing & Server Integrity:** Port 1099, `Wireguard2` interface, and `telemt` on `0.0.0.0:8443` remain fully protected and operational.
- **Gate Scope Boundary:** No IPK building or router deployment has been executed.

---

## 8. Conclusion & Sign-Off

Gate B Revision 8 is fully implemented, verified, and complete. All 22 test scenarios pass with zero errors and zero race conditions. The system is ready for user acceptance review.
