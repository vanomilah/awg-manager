# Mihomo Stage 2 Revised v4 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_REVISED_V3_PLAN_RECHECK_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`

## Verdict

**Do not start implementation from v4 yet.** It closes most v3 design gaps, but two newly visible crash windows can lose the only native-store rollback snapshot or bless an uncommitted target as verified. There are also unresolved integration paths outside the native Mihomo handler.

This is now a focused v5 delta rather than another redesign. The overall coordinator architecture is acceptable once the blockers below are incorporated.

No production code, build, IPK, or router deployment was performed during this review.

## What v4 fixed correctly

1. `internal/mihomonative` owns encoding and decoding of opaque `StoreSnapshot` bytes.
2. Native-store restoration is assigned to the coordinator rather than repeated by `DynamicEngine`.
3. Transaction state is initialized before LKG and runtime operations.
4. The manifest now has an explicit versioned schema and lossless bridge DTOs.
5. First-install, off, and zero-listener transitions are recognized.
6. Existing Stage 1 installations receive an explicit bootstrap path.
7. Process and config generation are separated.
8. Degraded API startup and `recovery_required` are defined.
9. Mixed-port listener derivation is no longer fixed to 1099.

## Blocking findings for v5

### P0-1. There is still an unrecoverable crash window before the first manifest

The proposed order is effectively:

1. write durable native-store snapshot;
2. persist the target native-store mutation;
3. compile, write, and validate candidate;
4. only then write manifest state `preparing`.

If AWG Manager or the router stops between steps 2 and 4, there is no manifest. The startup table says that when no manifest exists, all `store.snapshot.*` files are swept as orphans. This deletes the only rollback snapshot while leaving the target store persisted and the previous active config/runtime in place.

Required correction:

- Introduce durable pre-mutation phases, for example:
  - `snapshot_secured`: snapshot file exists and its digest is in the manifest;
  - `store_mutating`: mutation intent recorded;
  - `store_mutated`: current store digest equals `StoreTargetDigest`.
- Write and fsync the initial manifest **before** invoking `mutation.Mutate()`.
- After mutation, compute/persist the target store digest before compilation.
- Startup must never blindly delete a valid snapshot whose transaction status is unknown.
- For legacy orphan files without a manifest, quarantine/report rather than delete until the current store can be proven unrelated.
- Add process-death failpoints after snapshot write, initial manifest write, each store write, and target-store digest persistence.

### P0-2. Startup calls bootstrap before transaction recovery

The plan wires `BootstrapVerifiedState` followed by `ReconcileStartup`. That order is unsafe. After a crash in `active_swapped`, `runtime_applied`, or `published`, `config.yaml` may contain the uncommitted target. Bootstrap can validate it and create/overwrite `verified-active.json` and LKG before reconciliation sees the unfinished transaction.

Required correction:

- Startup must inspect and reconcile `config.yaml.txn.json` before any legacy bootstrap.
- Recommended single entry point:
  1. if a manifest exists, recover it using existing verified metadata/LKG/snapshot;
  2. if manifest is absent and verified metadata exists, validate normal state;
  3. only if both manifest and verified metadata are absent, run legacy Stage 1 bootstrap.
- Never create verified metadata or replace LKG while an incomplete transaction exists.
- Add a test where metadata is absent and startup begins with `active_swapped`; verify the target is not accepted as the legacy baseline.

### P0-3. `published` crash recovery does not restore or verify the actual runtime and bridges

The recovery table rolls `published` forward by running `mihomo -t`, writing verified metadata, and deleting the snapshot/manifest. Syntax validation only proves the YAML parses. After daemon restart, the managed Mihomo child may be stopped, NDMS bridges may be stale or absent, and the native store may not match `StoreTargetDigest`.

Required correction:

- Before roll-forward from `published`, verify:
  - active digest equals target digest;
  - current store digest equals target store digest;
  - target runtime is started/reloaded and passes process/listener/TUN readiness;
  - the complete target bridge set is reconciled and verified;
  - incrementally persisted publication receipt is consistent with observed NDMS state.
- If target convergence cannot be proven, roll back to stable-old using snapshot/LKG; if that fails, enter `recovery_required`.
- Do not delete snapshot or manifest until stable-new is fully proven.

### P0-4. Recovery of `active_swapped` and `runtime_applied` omits runtime and bridge convergence

Those rows restore LKG and native store, then immediately declare `Stable Old`. They do not specify stopping/restarting the possibly target-configured process, verifying previous listeners/TUN state, withdrawing partially surviving target bridges, or restoring `BeforeBridges`.

Required correction:

- Recovery to stable-old must include disk + store + runtime + bridge convergence and verification.
- Handle `PreviousConfigPresent == false` separately: quarantine target active, stop runtime, withdraw target bridges, and return to clean off state without requiring an LKG.
- Persist or derive `PreviousRequiredListeners`; previous mode alone is insufficient for a verified listener check.
- Add first-install crash cases for `active_swapped`, `runtime_applied`, and partial publication.

### P0-5. Partial bridge publication cannot be persisted by the shown callback contract

The manifest contains `PublishedBridges`, but the proposed `BridgePublishReceipt` is returned only when `bridgePublish(...)` returns. If the process exits after bridge 1 is published but before the callback returns, the in-memory receipt is lost and the manifest still has no knowledge of bridge 1.

Required correction:

- The coordinator must publish bridges one at a time and persist the receipt after each successful side effect; or
- pass a progress callback into the bridge publisher that synchronously updates/fsyncs the manifest before continuing.
- A publisher must not report the next bridge as active before its receipt is durable.
- Startup recovery must compare the durable receipt with observed ProxyN ownership and reconcile discrepancies safely.

### P0-6. The `NativeStoreTx` interface is incomplete for the responsibilities assigned to it

The shown interface contains only:

```go
RestoreSnapshotFile(path string) error
RemoveSnapshotFile(path string) error
ListBridges() []BridgeRef
```

But the transaction requires durable snapshot creation, snapshot digest verification, current-store digest calculation, safe restore, and synced cleanup. Extra concrete methods on `*Store` do not help a coordinator that only receives the narrower interface.

Required correction:

- Define the complete interface used by the actual owner, for example:
  - `WriteSnapshotFile(...)`;
  - `SnapshotDigest(...)` or snapshot metadata returned by the write;
  - `CurrentDigest()`;
  - `RestoreSnapshotFile(...)`;
  - `RemoveSnapshotFile(...)` with directory sync;
  - `ListBridges()`.
- Prefer controlled basenames/transaction directory over arbitrary paths.
- Verify snapshot content digest before restoration.

### P0-7. Snapshot removal is not durable despite the stated guarantee

The sample `RemoveSnapshotFile` only calls `os.Remove`. It does not fsync the containing directory. In contrast, the plan claims file/dir durability and uses snapshot absence as a transaction invariant.

Also, `storage.AtomicWritePerm` uses best-effort directory sync and suppresses sync errors, which prevents strict fault injection and cannot prove the Stage 2 durability contract.

Required correction:

- Implement Stage 2 strict file operations that return directory sync failures.
- Snapshot deletion must unlink then sync the directory.
- Do not rely on a best-effort helper for transaction artifacts unless it is extended with a strict variant and tested.
- Include write, chmod, close, file-sync, rename, unlink, and directory-sync failpoints.

### P0-8. Final metadata failure is incorrectly treated as a warning/success

The phase table groups “final metadata write / cleanup failure” and says to log a warning and retry manifest removal. Failure to write `verified-active.json` is not equivalent to failure to remove an already obsolete snapshot. Without verified metadata, the target is not committed and restart behavior remains transactional.

Required correction:

- Split finalization into durable phases:
  1. write verified metadata;
  2. persist `StateCommitted`;
  3. remove snapshot;
  4. remove manifest last.
- A verified-metadata write failure must not return ordinary success. Keep `published`, retain snapshot/manifest, and retry or return a typed commit-pending error.
- Define which cleanup errors are harmless retryable residue and which prevent success.
- Either use `StateCommitted` in the protocol/recovery table or remove the unused state.

### P0-9. Existing non-native config sources and error-swallowing call sites are not covered

Mihomo YAML is compiled not only from `mihomonative.Store`, but also from router settings, orchestrator slots, subscriptions/tunnels, AWG catalog, rules, and DNS settings. v4 defines a durable source transaction only for native Mihomo HTTP mutations.

Current `router.UpdateSettings` persists settings and, when Mihomo reload fails, logs the error and continues (`settings-reload-mihomo`) instead of returning failure. Other slot-change callbacks also trigger convergence outside the native handler.

Required correction:

- Inventory every production path that can change Mihomo compilation input or invoke `DynamicEngine.Reload/SyncMihomoRuntime`.
- Route all runtime applies through the coordinator.
- For each persisted source, define whether apply failure:
  - rolls that source back;
  - leaves it as explicit pending desired state; or
  - enters recovery/degraded state.
- Do not swallow coordinator errors and report API success while old runtime remains active.
- Add integration tests for router settings, rule/slot changes, subscription/tunnel changes, AWG catalog refresh, and native-resource mutation.

### P0-10. The existing `apply=false` native API behavior is not addressed

`MihomoHandler.withNativeMutation` currently accepts an `apply` flag, and several handlers intentionally support store-only mutations. v4 says all calls delegate to `MutateAndApply` but does not decide whether draft/store-only behavior remains supported or is removed.

Required correction:

- Make the product decision explicit.
- If `apply=false` remains: persist the mutation as pending desired state without claiming runtime convergence; define how the next apply consumes it.
- If immediate apply is now mandatory for Mihomo: remove the parameter from API/OpenAPI/frontend and update tests atomically.
- Never silently turn an existing store-only request into a runtime transition.

## High-priority corrections

### P1-1. LKG replacement must require a verified source

The bootstrap section says valid/running active is copied to LKG, but normal `SecureLKG` must also verify that the active digest matches `verified-active.json` before overwriting the prior LKG. Syntax-valid is not the same as last-known-good.

Add explicit checks and tests for active/metadata mismatch, corrupt metadata, and a valid older LKG.

### P1-2. Bootstrap mode and runtime semantics need exact evidence

After a daemon restart, the new `Operator` usually does not own the old child process. “Active valid and running” must define how a stale process is identified and how primary vs exports mode is derived. Restoring a valid LKG must also reconcile runtime before declaring a healthy active state.

### P1-3. Hot-reload verification still does not prove target config identity

`GET /configs` returning the path `config.yaml` does not prove Mihomo loaded the target bytes. Define observable fields to compare with the compiled result (ports, mode, controller settings, TUN state) and keep target digest as AWG Manager's disk/config generation invariant rather than claiming the Mihomo API reports it.

### P1-4. UDP readiness should include ownership and IPv6 tables

Checking only the presence of hex port `0xC847` in `/proc/net/udp` can match an unrelated process and ignores `/proc/net/udp6`. Where possible, associate the socket inode with the expected PID; otherwise document the weaker guarantee and combine it with process/config checks.

### P1-5. Lock ordering remains undocumented

The coordinator is described as single-flight while `DynamicEngine.transitionMu`, handler `nativeMu`, store locks, bridge-runtime locks, startup recovery, process-exit callbacks, and shutdown can interact. Specify lock order and ensure callbacks never re-enter a held non-recursive lock.

### P1-6. Orphan cleanup must be conservative and scoped

Do not delete arbitrary pattern matches merely because the manifest is missing. Validate basenames, ownership, age, transaction IDs, and artifact digests. Secret-bearing quarantined files require `0600`, bounded retention, and explicit observability.

### P1-7. The test list claims 32 coordinator scenarios but spans several packages

Adjust verification wording so it does not imply all 32 cases run under `./internal/mihomo -run TestApplyCoordinator`. Add explicit commands for `internal/mihomonative`, `internal/api`, `internal/singbox/router`, and `cmd/awg-manager`, plus the full affected-package regression.

## Required v5 delta

The next revision does not need another large conceptual rewrite. It must add:

1. pre-mutation manifest phases and recovery;
2. reconciliation-before-bootstrap startup ordering;
3. full runtime/store/bridge verification for stable-old and stable-new recovery;
4. incrementally durable bridge publication receipts;
5. the complete `NativeStoreTx` interface with strict directory-sync semantics;
6. split final metadata/commit/cleanup phases;
7. migration of every Mihomo apply trigger, including non-native sources and swallowed errors;
8. an explicit contract for `apply=false`;
9. verified-source checks before LKG replacement;
10. exact lock ordering and conservative artifact cleanup.

## Additional tests required before approval

1. Crash after snapshot persistence but before first manifest.
2. Crash after initial manifest but during/after native-store mutation.
3. Startup with no verified metadata plus unfinished `active_swapped` transaction.
4. `published` restart with Mihomo stopped and target bridges absent.
5. `runtime_applied` restart with stale target bridges present.
6. First-install restart recovery without LKG.
7. Crash after one bridge publication before publisher return.
8. Verified metadata write failure does not return ordinary success.
9. Directory fsync failure on snapshot deletion and manifest deletion.
10. Settings mutation whose Mihomo apply fails is not reported as success.
11. Orchestrator slot/subscription change whose apply fails has defined source state.
12. `apply=false` behavior matches API/OpenAPI/frontend contract.
13. LKG is not overwritten when active digest differs from verified metadata.
14. UDP socket collision with an unrelated process is rejected where ownership is available.
15. Concurrent handler mutation, slot callback, shutdown, and unexpected-exit paths do not deadlock.

## Approval condition

Approve v5 only when the first durable manifest precedes any persistent source mutation, startup recovery precedes legacy bootstrap, stable-old/stable-new include real runtime and bridge convergence, final metadata failure cannot masquerade as success, and all production Mihomo input/change paths have an explicit coordinator/error contract.

With those changes, the design should be ready to implement without another architectural review cycle.
