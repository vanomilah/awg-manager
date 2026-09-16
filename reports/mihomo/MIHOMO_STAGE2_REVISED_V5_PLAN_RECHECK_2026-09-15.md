# Mihomo Stage 2 Revised v5 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_REVISED_V4_PLAN_RECHECK_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`  
Branch observed: `feature/mihomo-ai-proxyrt`

## Verdict

**Do not start implementation from revised v5 yet.**

v5 correctly incorporates most of the requested v4 delta: the first manifest is intended to precede native-store mutation, startup reconciliation precedes bootstrap, bridge receipts are incremental, final metadata is separated from cleanup, `apply=false` is acknowledged, and the inventory now mentions non-native inputs.

However, the proposed code does not compile as written, the actual native store still lacks the durability required by the transaction protocol, and several crash-recovery branches remain unsafe. The remaining work is a focused v6 correction, but it must be incorporated before implementation begins.

No production code, build, IPK, or router deployment was performed during this review. The existing dirty working tree was not modified except for this report.

## What v5 fixed correctly

1. `snapshot_secured` is persisted before invoking the native mutation.
2. Startup reconciliation is placed before legacy bootstrap.
3. `published` recovery attempts runtime, store, listener, and bridge convergence.
4. Bridge publication is described as one-at-a-time with a durable receipt after every successful bridge.
5. `verified-active.json`, `committed`, snapshot cleanup, and manifest cleanup are separated.
6. An explicit draft path for the existing `apply=false` API is present.
7. LKG replacement is gated by verified-active metadata.
8. Lock ordering and non-native input sources are at least acknowledged.

These are genuine improvements, but the assertions in section 3 that every P0/P1 item is fully resolved are still premature.

## Blocking findings for v6

### P0-1. The proposed `NativeStoreTx` cannot be implemented by `*mihomonative.Store`

Plan lines 227-235 require:

```go
Snapshot() (any, error)
```

The existing store already declares:

```go
func (s *Store) Snapshot() (StoreSnapshot, error)
```

Go does not support method overloading or covariant return types. The sample implementation in plan lines 387-389 recursively calls `s.Snapshot()` and also attempts to redeclare the same method name with a different signature. It cannot compile.

The boundary also exposes `any` even though the plan requires an opaque, type-safe snapshot.

**Required correction:** do one of the following:

- prefer a file-oriented interface that does not expose the snapshot value at all, for example `CreateSnapshotFile(path) (digest string, err error)`, `RestoreSnapshotFile(path) error`, `CurrentDigest() (string, error)`; or
- introduce an adapter type in the composition root with differently named methods.

Do not add another `Snapshot` method to `*Store`. Digest methods must return errors rather than silently returning an empty string.

### P0-2. The native store itself is still persisted with a non-strict primitive

`mihomonative.Store.saveLocked()` currently calls `storage.AtomicWritePerm`. The planned strict fsync operations cover the transaction manifest and snapshot, but not the target store write performed inside `mutation.Mutate()`.

Consequently, the manifest can durably reach `store_mutated` while the renamed native-store file is not guaranteed durable across a power loss. The central invariant is therefore not established.

**Required correction:** make the authoritative native-store mutation durable using the same strict file-and-directory sync contract, or add a coordinator-owned durable persistence step before writing `StateStoreMutated`. Include injected file-sync, rename, and directory-sync failures in tests. The snapshot file alone is not enough.

### P0-3. Recovery for `snapshot_secured` mishandles the most important crash window

The crash table says that `snapshot_secured` means the mutation was not finished and only verifies that the store matches `StorePreviousDigest` before cleanup.

But a crash can occur after `mutation.Mutate()` has durably saved the target store and before the manifest update to `store_mutated`. In that case the on-disk state remains `snapshot_secured` while the store already contains the target.

**Required correction:** recovery in `snapshot_secured` must be deterministic:

- if current store digest equals previous digest, cleanup may continue;
- otherwise restore the durable snapshot and verify the previous digest;
- any read/digest/restore ambiguity enters `recovery_required` and preserves evidence.

Add a crash-injection test exactly between mutation persistence and the `store_mutated` manifest write.

### P0-4. Corrupt-manifest recovery can still bless an uncommitted target

Plan line 280 proposes validating active YAML and runtime health, then writing verified metadata when the transaction manifest is corrupt. Syntax and a running process cannot prove whether active/store/bridges represent stable-old or an uncommitted target.

This contradicts the plan's own rule that an interrupted target must never be adopted as a legacy baseline.

**Required correction:** a corrupt or unsupported-version manifest must fail closed into `recovery_required`. Preserve the manifest, snapshot, active, candidate, and LKG. Automatic recovery is allowed only when transaction identity and all recorded digests are trustworthy. Provide an explicit administrative inspection/recovery path; never infer commit from syntax alone.

### P0-5. `apply=false` is not transactionally safe and needs a real desired/applied model

`MutateOnly` currently means: mutate and persist the store, then write `store.pending.json`. There is another crash window between these operations: the desired store changes but no pending marker exists.

There is a deeper invariant issue. After a valid draft edit:

- `config.yaml` and runtime represent the last applied store;
- the native store represents newer desired state;
- a failed later `apply=true` must normally retain the user's draft, not restore an older applied-store snapshot.

The current single `StorePreviousDigest` model does not distinguish applied state from desired pending state, and the recovery tables assume the store should match active runtime.

**Required correction:** define a durable desired/applied revision model. At minimum:

1. journal draft intent before mutating the store;
2. persist the desired store strictly;
3. atomically publish a pending revision/digest;
4. record both `AppliedStoreDigest` and `DesiredStoreDigest` in verified metadata/manifest;
5. on failed apply, retain a valid pending draft while restoring runtime/config/bridges to the last applied generation;
6. make retry and restart behavior idempotent.

If this is too large for Stage 2, remove `apply=false` and update API/OpenAPI/frontend together rather than implementing an unsafe half-transaction.

### P0-6. Non-native sources are listed but are not part of the transaction

`ApplyRequest{IsNonNativeSource: true}` contains no snapshot, mutation, previous digest, or rollback contract for router settings, orchestrator slots, subscription/tunnel slots, AWG catalogs, and other compiler inputs.

Returning an error from `UpdateSettings` is not rollback: the settings have already been saved, FakeIP/KeenDNS/cloud side effects may already have run, and `Reconcile` follows afterward. Likewise, `OnRoutingSlotsChanged` currently has a `func()` signature and only logs a failed Mihomo sync after slot persistence.

**Required correction:** choose and document one honest model for every source:

- transactional source adapter with snapshot/commit/rollback; or
- durable desired state plus last-applied generation and explicit pending/degraded status.

Inventory every compiler input and its mutation trigger. Do not claim atomic source rollback when a void callback can only log the failure. `IsNonNativeSource` by itself provides no safety.

### P0-7. Snapshot ownership is duplicated between API and coordinator

Section 2 says the coordinator owns native store rollback. Yet `NativeMutation` in lines 477-481 again carries a `mihomonative.StoreSnapshot` and `Restore` callback, while `NativeStoreTx` is also owned by the coordinator.

This recreates the old split ownership and leaves the timing of the API snapshot unclear. If it is captured before `transitionMu`, it is racy; if captured inside the handler, the coordinator no longer owns the full boundary.

The proposed `MutateAndApply(...) error` also has no defined typed result path for create/import handlers that must return the created resource.

**Required correction:** the coordinator must acquire the outer transition lock, create and durably record the snapshot, execute a mutation closure, and return its result. API code should not independently restore the store. Define a concrete result-bearing contract (generic internal helper or closure capture under the synchronous lock) and its error semantics.

### P0-8. The stated lock order conflicts with the current handler order

The plan requires:

```text
transitionMu -> nativeMu -> coordinator.mu -> operator.mu -> store.mu
```

Current `withNativeMutation` locks `nativeMu` before invoking `mutationTx`, which acquires `transitionMu`. Merely adding the coordinator without removing/restructuring this path produces the inverse order and a deadlock opportunity.

**Required correction:** include an explicit migration of `withNativeMutation`, legacy bridge callbacks, and `SetMutationTransaction`. State which locks are removed, which component owns serialization, and prohibit callbacks while `Store.mu` or `Operator.mu` are held. Add a deterministic lock-order test rather than relying only on `-race`.

### P0-9. Manifest-controlled filenames are unsafe as specified

The proposed basename regex `^[a-zA-Z0-9_.-]+$` accepts `.` and `..`. More importantly, recovery must not trust filenames from a corrupt manifest when it performs rename or unlink operations.

**Required correction:** fixed filenames must be constants, and transaction-scoped filenames must be derived from a strictly validated TxID. Reject `.`, `..`, separators, volume prefixes, alternate data stream syntax, and any path whose cleaned parent escapes the Mihomo config directory. Prefer not serializing authoritative filesystem paths at all. Add traversal and corrupt-manifest tests.

### P0-10. Runtime-off and first-install behavior remain assertions rather than a protocol

The schema assumes target/previous config digests and an active file, while Stage 2 also promises:

- `RuntimeOff`;
- last sidecar deletion;
- zero listeners;
- no empty YAML commit;
- first install without an LKG.

The recovery table handles missing previous config only in one row; `runtime_applied` still says to restore an LKG. Startup also initializes `Mode=Off` whenever `config.yaml` is absent, even though a native store may already require an exports sidecar.

**Required correction:** add an explicit file-state enum (`absent`/`present`) for previous and target generations and give RuntimeOff/first-install their own apply and recovery tables. Derive desired mode from settings plus native bridge demand; absence of `config.yaml` alone does not imply Off. Never start a previously stopped runtime during rollback merely because a previous config exists—honor `PreviousProcessState` and `PreviousMode`.

### P0-11. Runtime verification still does not prove target configuration identity

`mihomo -t` validates a file, listener probes validate sockets, and `/configs` only proves that a controller responds. None necessarily proves that the running process loaded exactly `TargetConfigDigest`, especially after a hot reload or stale-controller race.

**Required correction:** define a content-derived runtime fingerprint made observable through the controller, or use a controlled restart for commits where exact hot-reload identity cannot be proven. The receipt must only advance `ConfigGeneration` after matching the target fingerprint. Verify controller ownership by expected PID/process generation before accepting it.

### P0-12. Final cleanup needs post-commit idempotency semantics

Once `StateCommitted` is durable, rollback is no longer appropriate. But unlink may remove a file successfully and then fail on directory fsync; on retry the file is already absent. The plan does not state whether `StrictUnlink` treats `ENOENT` as an idempotent success or how the API reports an uncertain cleanup after a committed target.

**Required correction:** distinguish `CommitPending`, `CommittedCleanupPending`, and rollback failures. Cleanup operations must be idempotent. A cleanup failure after durable commit must never be reported as an ordinary apply failure that encourages the client to repeat the mutation.

## High-priority corrections

### P1-1. Exact bridge convergence must include removal of stale bridges

Checking that every target bridge is active is insufficient. Stable-new also requires that every before-only bridge is absent and that active bridge properties match the full `BridgeRef` identity.

### P1-2. Cross-directory rename must sync both directories

Moving an orphan snapshot into `quarantine/` crosses directory boundaries. A strict rename helper must fsync both the source and destination directory. Snapshot quarantine should use a deterministic collision-safe name and retain `0600`.

### P1-3. Startup ordering relative to schedulers and HTTP serving is unspecified

`DynamicEngine.Startup` must complete before slot callbacks, background schedulers, mutable API endpoints, and readiness success are enabled. Degraded read-only API may start only with mutation and bridge gates closed.

### P1-4. Degraded mode has no operational contract

List exactly which endpoints remain available, which return `503 recovery_required`, how evidence can be exported, and how an administrator chooses stable-old/stable-new. Status fields alone do not provide recovery.

### P1-5. Removal of `GenerateMihomoConfig` needs a migration inventory

The current function is wired from `wiring_server.go` and has a large existing test suite. The plan should map each caller and test to `CompileMihomoConfig` or the coordinator and verify that no direct config writer remains.

### P1-6. API and frontend schema changes are missing

The new `applied`, `pending`, `degraded`, and `recovery_required` states require updates to OpenAPI, generated frontend schemas, API clients, and UI behavior. This is absent from the component list and verification matrix.

### P1-7. Store interface read methods need errors and immutable snapshots

`CurrentDigest() string` converts storage failure into an empty digest. `ListBridges() []BridgeRef` cannot report read/decode failures. Transactional decisions must never silently collapse I/O errors into valid zero values.

## Required v6 delta

Before implementation, revise the plan with these concrete changes:

1. Replace the impossible `Snapshot() (any, error)` interface with a type-safe adapter or file-oriented snapshot contract.
2. Make actual native-store persistence strictly durable, not only its snapshot and manifest.
3. Restore the snapshot when `snapshot_secured` contains any store digest other than the verified previous digest.
4. Treat corrupt/unsupported manifests as `recovery_required`; never bless active from syntax/runtime health alone.
5. Either design durable desired/applied revisions for `apply=false`, or remove that mode consistently.
6. Give every non-native compiler source a real transaction or durable-pending contract.
7. Make the coordinator the sole snapshot/rollback owner and define mutation result propagation.
8. Migrate current handler/wiring locks to the declared order and remove duplicate bridge/reload callbacks.
9. Derive all transaction paths from fixed constants and validated TxID; do not trust manifest filenames.
10. Specify complete RuntimeOff, first-install, stopped-process, and no-config recovery semantics.
11. Define how runtime identity is proven after hot reload and how stale controllers are rejected.
12. Define idempotent post-commit cleanup and distinct client-visible error classes.
13. Require exact bridge-set convergence, including stale bridge withdrawal.
14. Place startup reconciliation before schedulers, callbacks, mutable API serving, and readiness.
15. Add degraded-mode recovery/export API behavior.
16. Include OpenAPI/frontend migration and all existing `GenerateMihomoConfig` callers/tests.

## Additional acceptance tests required

1. Compile-time assertion for the final store adapter (without duplicate `Snapshot` methods).
2. Native-store directory fsync failure before `store_mutated` is persisted.
3. Crash after durable mutation but before `StateStoreMutated`; snapshot is restored.
4. Corrupt manifest plus healthy uncommitted target enters degraded recovery, not stable-new.
5. Unsupported manifest version never performs path operations or automatic commit.
6. Crash after draft store mutation but before pending marker publication.
7. Failed apply with existing drafts retains desired draft and restores last applied runtime/config.
8. Router settings save followed by Mihomo apply failure has defined persisted/pending state.
9. Slot update followed by apply failure is not silently logged as success.
10. Current `nativeMu -> transitionMu` inversion is removed; concurrent API and slot callback complete without deadlock.
11. Manifest fields containing `.`, `..`, separators, drive prefixes, or alternate streams cannot escape config dir.
12. RuntimeOff recovery with previous config absent/present and previous process stopped/running.
13. First startup with no config but native export bridges derives exports mode correctly.
14. Stale controller from an older PID/generation cannot satisfy readiness.
15. Target bridge verification rejects both missing targets and surviving before-only bridges.
16. Snapshot removed but directory fsync failed; committed cleanup retry is idempotent.
17. Manifest removed but final directory fsync failed; mutation is not executed twice on client retry.
18. HTTP mutation endpoints are blocked in degraded mode while evidence/status export remains available.
19. OpenAPI-generated client correctly represents applied, pending, commit-pending, and recovery-required outcomes.
20. Every former `GenerateMihomoConfig` caller passes through the coordinator; direct writer search is empty.

## Approval condition

Approve v6 for implementation only when:

- its Go interfaces can be implemented without overloads, recursion, or `any` snapshot leakage;
- the native store, snapshot, manifest, candidate, LKG, active config, and verified metadata all have explicit durability guarantees;
- `snapshot_secured`, corrupt-manifest, draft, RuntimeOff, first-install, and cleanup-after-commit branches are deterministic;
- all compiler inputs have an honest transactional or durable-pending contract;
- the coordinator is the only rollback owner and the lock order matches the migrated code;
- stable-old and stable-new prove exact disk, desired/applied store, process, runtime-config, listener, and bridge convergence;
- API/OpenAPI/frontend expose the new states without claiming success on pending or degraded outcomes.

Once this focused delta is incorporated, Stage 2 should be ready for implementation. Starting from the current v5 would likely produce code that either does not compile or needs another architectural rewrite around drafts and non-native sources.
