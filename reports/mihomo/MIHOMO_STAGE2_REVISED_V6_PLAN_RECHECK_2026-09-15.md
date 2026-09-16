# Mihomo Stage 2 Revised v6 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_REVISED_V5_PLAN_RECHECK_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`  
Branch observed: `feature/mihomo-ai-proxyrt`

## Verdict

**Revised v6 is improved, but it is still not ready to implement.**

It resolves the original `Snapshot()` collision, adds strict native-store persistence, fixes the intended `snapshot_secured` recovery rule, rejects corrupt manifests, and removes `nativeMu` from the proposed design. Those are substantive corrections.

However, one equivalent compile-time collision remains, several v5 blockers are only restated rather than designed, and the durability protocol does not handle ambiguous outcomes after rename/unlink. Starting implementation from v6 would force architectural decisions to be invented inside the code.

No production code, build, IPK, or router deployment was performed. Existing working-tree changes were preserved; only this review file was added.

## Confirmed improvements over v5

1. `NativeStoreTx` no longer tries to overload the existing typed `Snapshot()` method.
2. `CurrentDigest` now returns an error.
3. `saveLocked()` is explicitly included in the strict durability scope.
4. `snapshot_secured` recovery recognizes a crash after mutation persistence but before the next manifest state.
5. Corrupt and unsupported manifests fail closed.
6. Fixed paths and TxID validation are introduced.
7. `nativeMu` removal and coordinator ownership are stated.
8. Post-commit cleanup is distinguished from an apply failure.

## Blocking findings for v7

### P0-1. `ListBridges()` still has an impossible Go overload

The current store declares:

```go
func (s *Store) ListBridges() []BridgeRef
```

v6 requires:

```go
ListBridges() ([]BridgeRef, error)
```

and proposes:

```go
func (s *Store) ListBridges() ([]BridgeRef, error) {
    return s.ListBridges(), nil
}
```

This is the same failure pattern that v6 fixed for `Snapshot`: Go cannot overload a method by result type, and the body is recursively ambiguous. The plan does not compile as written.

**Required correction:** either keep `ListBridges() []BridgeRef` in the interface, rename the transactional method (for example `CurrentBridges() ([]mihomo.BridgeRef, error)`), or use a separate adapter type. Because this data is in-memory under an `RWMutex`, adding a fake error result provides no value unless disk reload/validation is actually part of the method.

### P0-2. The strict filesystem package boundary is inconsistent

The component list says `internal/mihomo/fs.go`, which normally belongs to package `mihomo`, but the store sample calls `fs.StrictWriteAtomic` and `fs.StrictUnlink`. No `internal/mihomo/fs` subpackage is specified.

The current `mihomonative` package already imports `internal/mihomo`, so an exported helper would be called as `mihomo.StrictWriteAtomic`; alternatively the plan must explicitly create a cycle-free `internal/strictfs` package.

**Required correction:** choose one concrete package and show its import graph. Prefer a small neutral package such as `internal/strictfs` if both Mihomo state-machine and native store own durable files. Add a compile-time import-cycle check to the plan.

### P0-3. Draft journaling has no schema or crash-recovery state machine

Section 8 lists a happy-path sequence for `store.draft.json`, but the document provides no draft schema and no recovery table for crashes:

- after draft intent is durable but before mutation;
- during strict store persistence;
- after store mutation but before `DesiredStoreDigest` is written;
- after desired digest publication but before the HTTP response;
- while an `apply=true` transaction consumes a pre-existing draft;
- after commit but before the draft marker is removed.

The ordinary transaction manifest cannot automatically answer these cases because it models apply generations, not draft intent/revision.

**Required correction:** define a versioned draft journal with TxID, base applied digest, previous desired digest, intended operation identity or rollback snapshot, target desired digest, state, and timestamps. Add a startup reconciliation table and idempotency rules. If arbitrary mutation closures cannot be replayed safely, recovery must use a durable pre-draft snapshot rather than serialized intent.

### P0-4. Applied and desired state are still not sufficiently separated

Adding `AppliedStoreDigest` and `DesiredStoreDigest` to prose is not enough. Their actual metadata schema is absent, and the transaction manifest still contains only `StorePreviousDigest` and `StoreTargetDigest`.

With pending drafts, `StorePreviousDigest` represents desired state while the active config/runtime represent an older applied state. Calling that combination “Stable Old” is ambiguous. A rollback must retain valid earlier drafts while removing only the current mutation.

Bridge state makes the problem concrete: after a draft deletes a bridge, `nativeStore.ListBridges()` no longer contains the bridge that is still published by the applied generation. It cannot populate `BeforeBridges` correctly from current desired state.

**Required correction:** persist an applied-generation record containing at least applied store digest, config digest/file state, mode, process expectation, listeners, and complete applied bridge set. Define `BaseDesiredDigest`, `MutationTargetDigest`, and `AppliedStoreDigest` separately in the manifest. Derive `BeforeBridges` from the verified applied generation, never from a possibly drafted current store.

### P0-5. Non-native inputs remain outside any transaction or durable-pending model

Section 9 is materially unchanged from v5. `IsNonNativeSource: true` still carries no source snapshot, revision, rollback, or applied/desired digest.

`UpdateSettings` persists settings and performs other side effects before Mihomo reload. Returning the reload error does not undo that persistence. `OnRoutingSlotsChanged` is currently `func()` and can only log a failed apply after the slot is already changed. Marking the engine degraded is useful observability, but it is not transactional convergence.

**Required correction:** inventory each compiler input and assign it one of two explicit contracts:

1. source-specific transactional adapter with snapshot/rollback; or
2. durable desired revision plus last-applied revision, pending status, retry, and restart reconciliation.

The plan must state what remains persisted after a failed settings/slot apply and how the next startup or retry converges it. Remove the claim of full atomicity if eventual convergence is intentionally chosen.

### P0-6. The compiler currently mutates the native store

Current `GenerateMihomoConfig()` calls `ImportLegacyGroups` and `ImportLegacyRules` while generating configuration. Those functions persist `mihomonative.Store`.

Simply renaming the function to `CompileMihomoConfig` does not make it pure. If these migrations remain inside compile, they occur after the coordinator has recorded `StoreTargetDigest`, invalidating the manifest and rollback invariants.

**Required correction:** move all legacy imports/migrations to an explicit, separately journaled mutation before target digest capture. The pure compiler must only read immutable snapshots and perform zero writes to native store, settings, slots, config files, or bridge state. Add a test that snapshots every relevant file before/after compilation.

### P0-7. Runtime identity is still not proven

v6 says hot reload will compare `GET /configs` values for ports, mode, and log level. Many different configurations share those values. Rules, proxies, providers, DNS policies, groups, and listener routing can still be stale while the check passes.

This does not satisfy the v5 requirement for a content-derived runtime fingerprint.

**Required correction:** either:

- use a controlled process restart for transactional commits and bind readiness to the new PID/process generation plus candidate path; or
- identify an API-observable fingerprint that covers the complete loaded target and prove it with the pinned real Mihomo binary.

If Mihomo cannot expose a complete identity after reload, do not claim exact atomic hot-reload verification.

### P0-8. RuntimeOff and first-install are still not specified by the tables

The manifest gained file-state enums, but the error/recovery tables do not use them consistently:

- runtime failure rows still say “start LKG” without checking `PreviousProcessState`;
- `runtime_applied` recovery still requires restoring an LKG with no first-install branch;
- the startup diagram still treats missing `config.yaml` as `Mode=Off`, even when persisted native bridges require exports mode;
- target `FileAbsent` behavior is not described for swap, digest, validation, verified metadata, or rollback.

**Required correction:** provide separate apply and recovery rows for `FileAbsent`, `RuntimeOff`, first install, previous process stopped, exports sidecar, primary router, and last bridge deletion. Desired runtime mode must be derived from settings and native export demand—not from config-file presence.

### P0-9. `StrictUnlink(ENOENT)` can falsely claim durable deletion

The plan says `ENOENT` is an idempotent success. Consider:

1. `unlink` succeeds;
2. parent-directory `fsync` fails;
3. retry observes `ENOENT`.

If the retry returns immediately, the directory is never synced, so deletion durability is still unproven.

**Required correction:** on `ENOENT`, `StrictUnlink` must still open and fsync the containing directory before reporting durable success. Treat inability to sync it as cleanup-pending. Add failpoints before unlink, after unlink, and during the retry's directory sync.

### P0-10. Ambiguous write outcomes after rename are not modeled

An atomic write can rename successfully and then fail on directory fsync. The caller receives an error even though the new file may be visible and may survive reboot. This affects store persistence, every manifest transition, verified metadata, candidate/LKG swaps, and draft markers.

The phase table assumes each failed write left the previous manifest state, which is not always true.

**Required correction:** after any post-rename error, re-read and validate the destination. Classify the outcome as old, new, or unknown; then converge or enter `recovery_required`. In particular, failure while persisting `StateCommitted` must never trigger rollback of a target whose verified metadata may already be durable.

### P0-11. Recovery-required persistence is undefined when the manifest cannot be written

For corrupt/unparseable manifests, the system cannot update that manifest to `StateRecoveryRequired`. For manifest I/O failure, the same file may not be writable.

**Required correction:** define a separate fixed recovery marker, written with strict durability when possible, while retaining the original evidence. Degraded status must also be derivable directly from the corrupt/unsupported manifest when the marker cannot be created. Never overwrite the corrupt evidence merely to store the new state.

### P0-12. Startup gating and degraded operation are still missing

The plan only says `Startup()` runs “after dependencies are connected”. It does not guarantee execution before schedulers, slot callbacks, mutable HTTP endpoints, readiness success, or other reload triggers.

It also adds `Degraded` fields without defining which endpoints remain available, how evidence is exported, or how an operator chooses/retries stable-old versus stable-new.

**Required correction:** provide exact wiring order and an API gate. During reconciliation/degraded mode, reject every mutation, engine switch, reload, bridge publish, and background apply. Permit only status/evidence/log export and explicitly authenticated recovery actions.

## High-priority gaps

### P1-1. Existing direct mutations must be exhaustively located

The plan inventories only four broad categories. The current compiler also consumes router config, device-proxy listeners, AWG slot/live catalog fallback, native resources, subscriptions, tunnels, settings, and cloud-related flags. Search-based acceptance must prove that all mutation triggers either enter the coordinator or publish a durable desired revision.

### P1-2. Exact bridge reconciliation needs an authoritative applied snapshot

The test promises rejection of stale before-only bridges, but the plan does not define the runtime API used to list and compare active ProxyN interfaces and their full properties. Specify normalization, ordering, duplicate handling, and ownership checks.

### P1-3. API/OpenAPI/frontend work is still absent

`applied`, `pending`, `commit_pending`, `cleanup_pending`, `degraded`, and `recovery_required` require response schemas, generated clients, UI states, retry semantics, and tests. Merely adding two fields to `MihomoStatusSnapshot` is insufficient.

### P1-4. `GenerateMihomoConfig` migration is not inventoried

The function is used by current wiring and a large existing test suite. The plan must map every caller/test to the pure compiler or coordinator and include a search asserting that no direct config writer remains.

### P1-5. The test matrix does not test most newly claimed guarantees

The matrix still has only one happy-path draft test, one settings error test, no slot-persistence failure test, no startup gate test, no degraded API test, no controller-staleness test, and no compile-side-effect test. Several earlier required v6 tests disappeared rather than being incorporated.

### P1-6. TxID uniqueness and stale artifact ownership are unspecified

Digits-only validation prevents traversal but not collisions. Define generation (cryptographically random digits or durable monotonic sequence), exclusive creation, collision retry, and exact association among candidate, snapshot, draft, and manifest artifacts.

### P1-7. “Typed result” is inaccurate

`MutateAndApply(...)(any,error)` is dynamically typed. This may be acceptable at the API boundary, but the plan should not call it typed. Define panic handling for `mutateFn`, nil results, and whether a committed mutation can ever return a result-construction error.

## Required v7 delta

1. Rename or adapt `ListBridges` so the final Go interface actually compiles.
2. Choose one concrete cycle-free strict-filesystem package and show imports.
3. Define the versioned draft journal, its states, crash table, rollback snapshot, and restart reconciliation.
4. Persist a complete verified applied-generation record, including applied bridge/listener state.
5. Separate applied, base desired, and mutation target digests in the transaction manifest.
6. Give settings, slots, subscriptions, tunnels, AWG inputs, and other compiler sources explicit transactional or durable-pending semantics.
7. Remove legacy imports and every other mutation from the pure compiler.
8. Replace partial `/configs` comparison with a provable runtime identity strategy; use restart if necessary.
9. Add complete `FileAbsent`/RuntimeOff/first-install/sidecar/process-stopped transition tables.
10. Make `StrictUnlink` fsync the parent even when the file is already absent.
11. Model post-rename errors as old/new/unknown outcomes and handle committed-state ambiguity safely.
12. Add a separate recovery marker and preserve corrupt manifest evidence.
13. Gate startup, schedulers, callbacks, mutable APIs, and readiness on completed reconciliation.
14. Define degraded-mode inspection, retry, and administrative recovery endpoints.
15. Add OpenAPI/frontend changes and migrate all direct writer callers/tests.
16. Restore all missing fault-injection and concurrency tests from the v5 review.

## Minimum additional tests before approval

1. Compile-time assertion for the renamed/adapted `NativeStoreTx` methods.
2. Package import graph has no cycle.
3. Crash at every draft-journal boundary restores or retains the correct desired revision.
4. Existing draft plus new mutation failure retains only the pre-existing draft.
5. Draft deletion of an applied bridge still restores the applied bridge on failed apply.
6. Compiler performs zero writes, including legacy group/rule imports.
7. Settings save followed by apply failure has a documented pending/retry state.
8. Slot change followed by apply failure survives restart and retries deterministically.
9. Two configs with identical port/mode/log-level but different rules cannot share a verified runtime identity.
10. Stale controller/PID cannot satisfy target readiness.
11. First install with native bridge demand starts exports mode despite absent config.
12. RuntimeOff rollback honors a previously stopped process.
13. `StrictUnlink` retry after post-unlink fsync failure fsyncs the directory.
14. Every strict rename/write failpoint resolves destination as old/new/unknown.
15. Failure persisting `StateCommitted` never repeats or rolls back the mutation incorrectly.
16. Corrupt manifest remains preserved while degraded state survives restart.
17. Mutable endpoints and background callbacks are blocked until startup reconciliation completes.
18. Degraded mode permits evidence export but rejects ordinary apply/reload.
19. OpenAPI-generated frontend handles all transaction outcome classes.
20. Search confirms no production call to the removed direct `GenerateMihomoConfig` writer.

## Approval condition

Approve v7 only when:

- every shown Go interface is implementable against the current store without overloads or recursion;
- draft, applied, and mutation-target revisions are separately durable and recoverable;
- every compiler input has an explicit convergence contract;
- compilation is provably pure;
- hot reload has a complete target-identity proof or is replaced by controlled restart;
- file absence, RuntimeOff, first install, exports mode, and stopped-process rollback are fully tabulated;
- strict filesystem helpers handle post-rename/post-unlink ambiguity rather than treating all errors as pre-operation failure;
- reconciliation gates all mutation and background activity;
- degraded/recovery states are operable through API and visible in OpenAPI/frontend.

v6 is closer, but the statement that all v5 findings are fully resolved is not supported by its own contracts. The focused v7 delta above should be incorporated before coding begins.
