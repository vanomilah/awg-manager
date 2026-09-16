# Mihomo Stage 2 Revised v3 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_REVISED_V2_PLAN_RECHECK_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`

## Verdict

**v3 is substantially closer, but it is still not safe to hand to an implementation agent as approved. One more focused revision is required.**

The package-cycle problem, context-aware validator runner, non-consuming LKG restore, phase names, API/package-main boundary, and bridge receipts are now directionally addressed. The remaining blockers are concentrated in transaction ownership, persistent store recovery, crash-phase invariants, and runtime-only mode transitions.

No production code, build artifacts, IPK, or router state were changed during this review.

## Confirmed improvements over v2

1. Shared types are moved to `internal/mihomo`, avoiding the proposed `router <-> mihomo` import cycle.
2. `internal/api.NativeMutationApplier` removes the impossible direct import of `package main`.
3. The plan removes production `GenerateMihomoConfig()` instead of retaining a second apply path.
4. Manifest phases now distinguish `preparing`, `lkg_secured`, and `active_swapped`.
5. First-install rollback and non-consuming LKG restoration are explicitly recognized.
6. Bridge publication now returns a receipt for partial rollback.
7. Context-aware command injection and protocol-specific UDP checks are included.
8. Store restore errors are no longer intentionally discarded.

## Blocking findings for v4

### P0-1. Native-store rollback still has two owners, and the coordinator cannot perform its claimed restore

The coordinator rollback protocol says it restores the native store from `req.StoreSnapshotFile`. However, `ApplyCoordinator` has no store-restorer dependency. The `DynamicEngine.MutateAndApply` sample then calls `mutation.Restore(...)` after `coordinator.Apply` returns an error.

That recreates the split ownership v3 claims to remove:

- the coordinator says store recovery is part of its rollback state machine;
- the outer DynamicEngine performs the actual restore;
- the coordinator may report rollback success before store restoration has even occurred;
- a coordinator manifest may be removed/changed before the outer restore result is known.

Required correction:

- Choose exactly one owner for store restoration.
- Recommended: pass a lower-level `NativeStoreTransaction` capability into the coordinator with durable snapshot, restore, digest, and cleanup methods. Then coordinator rollback is genuinely complete before it returns.
- Alternative: coordinator rolls back only disk/runtime/bridges and returns a typed `RecoveryPendingStore` result; DynamicEngine restores the store and calls one coordinator finalizer. It must not report `stable_old` or remove recovery evidence before that finalizer succeeds.
- The manifest must remain durable until store restoration and old bridge restoration are both verified.

### P0-2. Current `mihomonative.StoreSnapshot` cannot be persisted by `cmd` as proposed

The actual type is:

```go
type StoreSnapshot struct {
    data []byte
}
```

Its contents are deliberately unexported. `cmd/awg-manager` cannot access the bytes, and JSON-marshalling this struct outside `internal/mihomonative` produces an empty object. Therefore `d.writeDurableStoreSnapshot(txID, mutation.Snapshot)` cannot be implemented correctly from the contract shown in v3.

Required correction:

- Put durable snapshot encoding/decoding in `internal/mihomonative`, where the opaque bytes are owned.
- Add methods/interfaces such as `WriteSnapshot(path, snapshot)`, `RestoreSnapshotFile(path)`, and `SnapshotDigest(snapshot)` or an equivalent transaction object.
- Snapshot files contain proxy credentials and must use `0600`, not an unspecified/default mode.
- Use atomic temporary-file write, file sync, rename, directory sync, regular-file/symlink checks, and a content digest stored in the manifest.
- Add a round-trip test using a store containing credentials, providers, rules, groups, and bridge allocations.

### P0-3. Every failure before runtime apply does not restore the already-mutated store

The sample order is:

1. persist old store snapshot;
2. execute `mutation.Mutate()` (current store methods persist immediately);
3. enter coordinator compile/write/validate/LKG/swap.

If compilation, candidate writing, validation, manifest writing, or LKG securing fails, the target store is already persisted. The coordinator protocol often says “remove candidate and return”, while store restoration is described only in the later rollback protocol. This can return with old runtime/config and new store.

Required correction:

- Once `mutation.Mutate()` succeeds, **every subsequent error and cancellation** must enter the unified recovery path, even before the YAML commit boundary.
- Recovery must be phase-aware:
  - before active swap: restore store and clean candidate/snapshot; do not touch active runtime/config;
  - after active swap: restore disk, store, runtime, and bridges;
  - after partial publication: additionally withdraw the exact published target set and republish the previous set.
- Add compile, candidate-write, validation, manifest-write, and LKG-write failure tests after a persisted native mutation.

### P0-4. Rollback state is derived too late from `ApplyReceipt`

`PreviousConfigPresent` and previous digests are fields of `ApplyReceipt`, but that receipt is returned by `c.applier` only after the active swap. Failures during `SecureLKG`, `SwapActive`, or their manifest writes occur before a valid receipt exists. A zero-value receipt incorrectly means “first install” and could quarantine an existing active config.

Required correction:

- Create a transaction record before the first durable mutation.
- Determine and persist `PreviousConfigPresent`, previous active digest, verified metadata, previous mode/process state, and previous listener set before securing LKG.
- Runtime applier should enrich this transaction with process/config generation and transition kind; it must not be the source of pre-existing disk state.
- Rollback must use the durable transaction record, never zero-value receipt inference.

### P0-5. Crash recovery still lacks a fully specified durable manifest schema

The plan says `TransactionManifest` contains state, digests, and snapshot paths, but does not define the actual struct. This matters because recovery also requires:

- previous-config presence;
- candidate/LKG/active filenames and digests;
- native-store before and target digests plus snapshot digest;
- previous and target runtime modes/process states;
- old and target bridge snapshots;
- partial bridge publication receipt;
- verified-active metadata generation;
- sanitized failure code/phase.

Without those fields, `ReconcileStartup` cannot prove stable-old or stable-new after a daemon exit.

Required correction:

- Include the complete manifest struct in v4.
- Persist bridge publication receipt incrementally or make bridge publication atomically recoverable; an in-memory receipt is lost on process death halfway through publication.
- Validate every manifest-controlled path as a safe basename under the configured transaction directory; reject traversal and symlinks.
- Define versioning and behavior for unknown/newer/corrupt manifests.

### P0-6. Manifest transition ordering still contains unrecorded crash windows

The invariant “write state only after its condition is durable” is useful, but the previous state does not always describe what happened during the operation. Examples:

- crash after LKG rename but before `StateLKGSecured` write;
- crash after active rename but before `StateActiveSwapped` write;
- crash after bridge 1 is published but before the receipt/state is persisted;
- crash after verified metadata write but before manifest removal;
- crash during snapshot cleanup after manifest removal.

Required correction:

- For non-idempotent side effects, record durable intent before the operation and durable completion after it, or make recovery inspect and hash all artifacts to infer completion safely.
- Describe exact recovery for each window listed above.
- Do not remove the manifest before all cleanup whose failure affects correctness is complete.
- Treat cleanup-only failures separately from state-integrity failures; define which are retryable warnings and which force `recovery_required`.
- Add failpoints immediately before and after every rename, sync, bridge operation, metadata write, and manifest removal.

### P0-7. Previous “verified” state is not established safely for upgrades

`SecureLKG` copies the current `config.yaml` into LKG without first proving it matches `verified-active.json`. Existing installations upgrading from Stage 1 will not have that metadata at all. Copying an unverified/stale active file can overwrite a genuinely usable older LKG.

Required correction:

- Define Stage 2 bootstrap/migration:
  - if verified metadata is absent, validate current config, confirm current runtime/mode/listeners, hash it, and atomically establish verified metadata before the first Stage 2 transaction; or
  - keep the existing LKG and enter a clearly defined unverified/bootstrap state.
- Before replacing LKG, require active digest to equal verified-active digest and the current runtime to be healthy for that mode.
- Never overwrite the only verified LKG with an unverified active file.
- Add upgrade tests for: active only, active + old LKG, corrupt active + valid LKG, missing metadata, corrupt metadata, and stopped runtime.

### P0-8. `BridgeTransition.BeforeBridges` cannot be reconstructed by the shown code

The sample calls `d.currentBridgeTransition()` only after `mutation.Mutate()`. At that point the live store exposes the target bridges. The old bridge set existed only in the opaque pre-mutation snapshot, and the shown `BridgeSpec` lacks several ownership fields from the real `mihomonative.BridgeRef`.

The current bridge lifecycle relies on resource kind/ID, label, legacy owner, enabled state, `ProxyIndex`, `ListenPort`, `ProxyInterface`, and `KernelInterface`. The proposed `BridgeSpec{Name, Port, Proxy, Index}` is insufficient for safe removal, ownership checks, legacy migration, or exact restoration.

Required correction:

- Capture `BeforeBridges` before mutation and `TargetBridges` after mutation under the same outer serialization boundary.
- Define a lossless bridge transaction DTO containing the ownership and interface fields required by `BridgeManager` and `mihomoBridgeRuntime`.
- Prefer conversion methods owned by `internal/mihomonative` rather than decoding opaque snapshots in `cmd`.
- Preserve current stale-generation and legacy-owner safety tests.

### P0-9. Runtime-off and sidecar-no-listener changes bypass the transaction

The compiler handles only exports/no-listener as `Noop`; `RuntimeOff` behavior is not specified. `Apply` says a `Noop` stops runtime/bridges and returns cleanly, but that is a real state transition, not a harmless no-op. It can fail after a native mutation, and it needs rollback to the previous running sidecar/primary state.

Required correction:

- Model `RuntimeOff` and “exports with zero listeners” as runtime-only transactions.
- Record previous runtime/mode/bridge state, withdraw bridges, stop Mihomo, verify stopped state, commit target store, and clean transaction evidence.
- On failure, restore old store, restart/reverify the previous runtime, and republish old bridges.
- Decide whether stale `config.yaml` remains intentionally retained while off; expose that as inactive verified config rather than silently treating it as active.
- Add failure/cancellation/crash tests for transitions to off and last-bridge deletion.

### P0-10. Final commit and startup recovery omit `published`/metadata crash decisions

`ReconcileStartup` is summarized as restoring LKG for `active_swapped`/`runtime_applied`, but no policy is stated for `lkg_secured`, `published`, or a crash after `verified-active.json` is written. The test matrix also omits recovery from `published` and commit cleanup windows.

Required correction:

- Define recovery for every state:
  - `preparing`;
  - `lkg_secured`;
  - `active_swapped`;
  - `runtime_applied`;
  - `published`;
  - verified metadata written but manifest still present;
  - `recovery_required`.
- For `published`, either verify target disk/store/runtime/bridges and roll forward to committed, or roll back using durable partial/publication receipts.
- Add tests for all final-commit crash windows.

## High-priority corrections

### P1-1. Process generation and config generation remain incomplete contracts

`ProcessGeneration()` is specified, but `ConfigGeneration` is present only as a receipt field. Hot reload normally preserves PID/process generation, so a successful PUT needs a separately maintained config generation/digest invariant.

Required correction:

- Define who increments config generation and at what point.
- Do not advance the verified config generation until post-reload readiness succeeds.
- Specify how the verifier associates the controller response with the expected child process. Mihomo `/version` does not report AWG Manager's generation.
- Rename the “controller reports old generation” test to an implementable process/socket ownership test unless an explicit controller identity mechanism is added.

### P1-2. Listener derivation still hardcodes optional settings

The plan always lists mixed port `1099`, but current generation uses `settings.MihomoMixedPort`, which may be zero or another configured value. Required listeners must be derived from the generated config, not from assumed defaults. TProxy readiness also needs to account for its actual TCP+UDP socket capabilities while transparent-routing policy may use only selected protocols.

Required correction:

- Build `RequiredListeners` from the final typed config immediately before YAML serialization.
- Include TUN interface/routes/rules readiness when TUN mode is active.
- Use wildcard/listen-address semantics correctly rather than claiming the socket binds specifically to `127.0.0.1` when the core binds globally.

### P1-3. The coordinator mutex is contradictory

`ApplyCoordinator` contains `mu sync.Mutex` but the protocol says it merely assumes the caller holds `DynamicEngine.transitionMu`. Startup reconciliation and future callers can bypass that assumption.

Required correction:

- Make coordinator methods internally single-flight and use DynamicEngine lock for the broader engine state machine; document lock order.
- Add deadlock/order tests for HTTP mutation, routing-slot callback, startup reconciliation, unexpected process exit, and shutdown.

### P1-4. Filesystem safety is path-based and still TOCTOU-prone

`SafeOpenDir` is opened, but the listed primitives continue using string paths and ordinary `os.Rename`. A directory/file can change between validation and use. If symlink resistance is a requirement, validation alone is insufficient.

Required correction:

- On Linux, anchor sensitive operations to the opened directory descriptor (`openat`/`renameat`/`unlinkat`, no-follow flags) or clearly scope the threat model to trusted root-owned directories.
- Validate transaction IDs before using them in filenames.
- Ensure temporary files use exclusive creation and cannot collide with stale transactions.

### P1-5. Secret-bearing recovery artifacts need lifecycle and permissions

Store snapshots, candidates, LKG, failed/quarantined configs, and manifests may contain or reference sensitive proxy material. v3 specifies `0644` for several config artifacts and no retention limit for failed files.

Required correction:

- Use `0600` for native-store snapshots and any artifact containing raw store JSON.
- Define ownership/mode policy for candidate/LKG/quarantine based on existing runtime requirements.
- Bound retained failed transactions by count/age without automatic destructive cleanup outside the transaction's own known files.
- Ensure status and normal logs expose only sanitized codes, txid, phase, and digests.

### P1-6. “Empirically audited” must point to reproducible evidence

The plan states that the pinned Mihomo `-t` performs zero config-dir writes, but gives no evidence file, binary version/hash, or test procedure. This behavior can change with versions or provider/cache settings.

Required correction:

- Record the tested Mihomo version and SHA-256.
- Make the acceptance test snapshot directory metadata/content before and after validation using a representative config with providers/geodata.
- Treat newly observed side effects as a failed compatibility assertion or isolate validation data.

### P1-7. Explicit startup error policy is missing

The plan correctly moves `ReconcileStartup` after wiring, but does not say what happens when reconciliation returns an error.

Required correction:

- Prefer starting the management API in recovery-only/degraded mode with Mihomo bridge gates closed, rather than publishing readiness or silently continuing.
- Block automatic routing applies until recovery succeeds.
- Surface a stable error code and remediation action in status/API.

## Required v4 structure

Before coding, v4 should include:

1. The complete, versioned `TransactionManifest` struct.
2. A single `NativeStoreTransaction` owner capable of durable snapshot and restore.
3. A lossless `BridgeTransition` mapped to current `mihomonative.BridgeRef` semantics.
4. A transaction record initialized before LKG/swap, independent of runtime apply receipt.
5. A phase-by-phase error table showing what is restored for every failure.
6. A crash-recovery table for every manifest and commit-cleanup window.
7. Explicit runtime-only transactions for `off` and exports with zero listeners.
8. Stage 2 bootstrap rules for legacy installs without verified metadata.
9. Separate process-generation, config-generation/digest, and controller/socket identity contracts.
10. Artifact permissions, path validation, cleanup, and degraded-start policy.

## Minimum tests to add to the v3 matrix

1. Opaque `StoreSnapshot` durable round-trip through APIs owned by `mihomonative`.
2. Store restoration after compiler, candidate-write, validation, preparing-manifest, and LKG failures.
3. Failure before runtime receipt exists does not infer first install from zero values.
4. Crash after LKG rename but before manifest transition.
5. Crash after active rename but before manifest transition.
6. Crash halfway through bridge publication with durable partial receipt.
7. Recovery from `published` and verified-metadata/manifest-removal windows.
8. Legacy Stage 1 upgrade without `verified-active.json`.
9. Corrupt active plus valid existing LKG is never copied over that LKG.
10. Transition to off and deletion of the last sidecar bridge, including rollback.
11. Lossless before/target bridge snapshots including owner/interface fields.
12. Snapshot/store restore error persists `recovery_required` before coordinator returns.
13. Mixed port disabled or changed from 1099.
14. Hot reload with unchanged process generation and changed config generation.
15. Degraded API startup when reconciliation cannot prove a stable state.

## Approval condition

Approve only after v4 removes the coordinator/DynamicEngine split store rollback, introduces a real durable snapshot API in `mihomonative`, initializes rollback state before the runtime receipt, defines a complete manifest and all crash windows, models off/no-listener transitions transactionally, and supplies upgrade/bootstrap semantics for installations that predate verified metadata.

Until then, v3 remains a strong design draft, not an executable implementation contract.
