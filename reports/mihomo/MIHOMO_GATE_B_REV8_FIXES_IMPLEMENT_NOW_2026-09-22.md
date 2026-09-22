# Mihomo Gate B Rev8 — IMPLEMENT NOW

**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Task type:** direct code implementation, not planning or review  
**Source audit:** `reports/mihomo/MIHOMO_GATE_B_REV8_ACCEPTANCE_AUDIT_2026-09-22.md`

## Mandatory instruction to the implementing agent

Start editing the production code and tests immediately.

Do **not**:

- produce another implementation plan;
- stop after code review or analysis;
- ask for approval of the approach;
- rewrite this task into a different document;
- claim completion based only on existing tests;
- build an IPK or deploy to routers.

Work through all sections below. Completion means: production changes are present, all required new tests exist, commands pass, and a factual resolution report plus focused patch are produced.

## 1. Add complete integrity protection for previous bridges

### Files

- `internal/mihomo/types.go`
- `internal/mihomo/types_test.go`
- `internal/mihomo/coordinator.go`

### Implement

Add to `TransactionManifest`:

```go
PreviousBridgesDigest string `json:"previous_bridges_digest,omitempty"`
```

When the coordinator captures `PreviousBridges` before `mutateFn`:

1. Enrich legacy refs against the single pre-mutation store snapshot.
2. Require `ValidateComplete()` for every resulting ref.
3. Reject duplicate `SlotKey()` values.
4. Deep-copy the list to `manifest.PreviousBridges`.
5. Compute `manifest.PreviousBridgesDigest = BridgesDigest(previousBridges)`.
6. Persist both fields before invoking `mutateFn`.

Update manifest validation so that, once previous bridges have been checkpointed:

- every ref must be complete;
- slots must be unique;
- digest must be present when the list is non-empty;
- digest must equal `BridgesDigest(PreviousBridges)`.

Apply equivalent state-aware validation to `TargetBridges` and `TargetBridgesDigest`. Recovery and rollback must validate these fields before using either list.

Do not silently accept a non-empty list with an empty digest in a newly written transaction. Legacy manifests require an explicit migration branch, not permissive validation.

## 2. Implement atomic startup migration of legacy bridge identity

### Files

- `internal/mihomo/coordinator.go`
- `internal/mihomo/generation_store.go`
- related tests under `internal/mihomo`

### Current defect

`RecoverOnStartup` currently loads `verified-active.json` and ignores errors from:

```go
reg.ReplaceDurableBridges(rec.AppliedBridges)
```

It does not perform the atomic migration required by the approved Rev8 design.

### Implement

Introduce a dedicated startup routine, named clearly, for example:

```go
upgradeLegacyAppliedBridgeIdentityLocked(ctx context.Context, rec AppliedGenerationRecord) (*AppliedGenerationRecord, error)
```

Required behavior:

1. Detect legacy bridge refs using `BridgeRef.IsLegacy()` or an explicit bridge-identity schema version.
2. If all refs are already complete, perform no disk mutation.
3. For legacy refs, read one authoritative store bridge snapshot.
4. Enrich each legacy ref using all available legacy fields.
5. Require exactly one complete match per legacy ref.
6. Reject absent or ambiguous matches.
7. Reject duplicate final slots.
8. Build a new consistent applied record and immutable generation bundle.
9. Validate the new generation completely before publication.
10. Publish through the existing generation commit machinery.
11. Atomically advance the authoritative pointer and `verified-active.json` according to the established commit protocol.
12. Register the upgraded refs in `DurableBridgeRegistry` only after durable state is valid.

If any step fails:

- leave the original authoritative generation and record usable and unchanged;
- persist an appropriate recovery marker when consistency cannot be proven;
- enter `StateRecoveryRequired`;
- return the error;
- do not continue startup as if registration succeeded.

Never rewrite `verified-active.json` in isolation.

## 3. Stop ignoring durable-registry failures

### File

- `internal/mihomo/coordinator.go`

Find every call resembling:

```go
_ = reg.ReplaceDurableBridges(...)
```

Replace it with explicit error handling.

The handling must respect transaction state:

- during startup: fail startup closed;
- after an irreversible commit boundary: retain committed state, write recovery evidence and enter recovery-required rather than rolling back committed state;
- before the commit boundary: abort or rollback through the normal coordinator path;
- during rollback: registry failure means rollback is not fully verified and must remain recovery-required.

Add contextual error messages indicating startup, commit, immediate rollback or resumed rollback.

## 4. Strengthen inspect and withdraw identity resolvers

### Files

- `cmd/awg-manager/mihomo_bridge_runtime.go`
- `cmd/awg-manager/mihomo_bridge_runtime_test.go`

### `resolveInspectIdentity`

### `resolveWithdrawIdentity`

Both must keep these valid cases:

- the old durable resource is absent from the current store;
- the same slot now belongs to the opposite generation during A→B replacement;
- the supplied complete durable owner remains the authority for live NDMS classification.

But when the current store contains a record with the **same canonical owner** as the supplied durable ref:

1. Require exactly one such record.
2. Build its complete `BridgeRef`.
3. Require equality of:
   - `ProxyIndex`;
   - `ProxyInterface`;
   - `KernelInterface`;
   - `ListenPort`;
   - `OwnerUUID`.
4. Fail with `ErrForeignBridgeOwnership` on ambiguity or mismatch.
5. Do not perform NDMS I/O after resolver failure.

A different current-store owner in the same slot is allowed for inspect/withdraw of the durable opposite generation. Publication remains strictly store-backed.

## 5. Make bridge operation replay validate the complete identity

### Files

- `internal/mihomo/coordinator.go`
- corresponding coordinator tests

For new bridge operations require:

```go
op.TargetDigest != ""
op.TargetDigest == ref.Digest()
op.BridgeRef.ValidateComplete() == nil
op.BridgeRef.Digest() == ref.Digest()
```

Checking only `KernelInterface` is insufficient.

For legacy operation records:

- use a separate explicit migration path;
- enrich only when exactly one complete identity is provable;
- checkpoint the upgraded operation before executing/replaying it;
- otherwise enter recovery-required without NDMS mutation.

Do not silently treat empty `TargetDigest` as acceptable for a new-schema operation.

## 6. Correct observed-bridge union semantics

### Files

- `cmd/awg-manager/mihomo_bridge_runtime.go`
- runtime tests

`ListObservedBridges` must not overwrite durable A with current-store B merely because both share `SlotKey()`.

Build the validated input set from durable refs and current-store refs using full identity, for example:

```text
SlotKey + OwnerUUID + ListenPort
```

or full `BridgeRef.Digest()`.

Required behavior:

- same exact identity appears once;
- A and B on the same slot can both be inspected during replacement/recovery;
- no numeric Proxy range scan;
- foreign/user/sing-box slots are not discovered speculatively;
- store-only target refs still appear;
- durable-only previous refs still appear after deletion from the store.

## 7. Add the missing hard-crash recovery tests

### Files

- add a focused test file such as `internal/mihomo/gate_b_rev8_crash_test.go`
- reuse production coordinator semantics and a persistent fake NDMS state

Do not map these requirements to the existing coarse `TestGate4_CrashMatrix`. Add actual tests for the new bridge operation checkpoints.

Use a subprocess that exits abruptly and then instantiate a completely fresh coordinator and runtime with empty in-memory caches.

### Required A→B crash cases

At minimum:

1. crash before withdraw intent side effect;
2. crash after A withdrawal but before applied checkpoint;
3. crash after A applied checkpoint;
4. crash after A absence verification but before verified checkpoint;
5. crash after A verified checkpoint;
6. crash before B publish side effect;
7. crash after B publication but before applied checkpoint;
8. crash after B applied checkpoint;
9. crash after B verification but before verified checkpoint;
10. crash after B verified checkpoint.

After each restart assert:

- recovery uses persisted `PreviousBridges` and `TargetBridges`;
- empty runtime cache does not change the result;
- final NDMS slot has exactly B with B's port;
- A is not republished after B is proven live;
- no foreign slot was touched;
- operation journal has a consistent final state;
- committed generation/store/config agree.

### Required inverse rollback cases

Repeat the meaningful boundaries for rollback B→A:

- B withdrawal replay;
- A restoration replay;
- final NDMS owner and port are A;
- store, config, applied record and LKG all refer to A;
- a foreign takeover at any boundary is preserved and causes recovery-required.

The fake NDMS state must survive the subprocess exit by using a durable test file or another explicit persistent fixture. An in-memory fake recreated empty after restart is not sufficient proof.

## 8. Add startup migration and registry failure tests

Add individually named tests for:

1. complete current record: startup performs no migration write;
2. legacy record: unique enrichment and atomic generation upgrade succeeds;
3. missing legacy match: fail closed, original generation untouched;
4. ambiguous legacy match: fail closed, original generation untouched;
5. failure publishing upgraded generation;
6. failure advancing the LKG/active pointer;
7. failure committing upgraded verified-active record;
8. restart after every injected migration failure;
9. `ReplaceDurableBridges` failure during startup;
10. registry failure during commit;
11. registry failure during immediate rollback;
12. registry failure during resumed rollback.

Each failure test must assert the exact coordinator state, recovery marker, authoritative generation/pointer and absence of unauthorized NDMS mutations.

## 9. Required existing and new scenario coverage

Keep and pass the existing Rev8 functional cases:

- normal apply;
- deletion;
- same-slot owner replacement;
- port-only replacement;
- publication failure restoring previous bridge;
- foreign takeover before withdraw and publish;
- unmanaged slot protection;
- coexistence with sing-box/user slots;
- store-absent durable withdrawal;
- restart with empty runtime cache;
- persistence ordering;
- corrupt operation digest.

Add tests for findings in sections 1–8. Test names in the final report must exactly match functions present in the repository.

## 10. Verification commands

Run after implementation:

```bash
gofmt -w internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Also run the new crash and migration tests individually with `-v` and preserve their real output.

## 11. Required final artifacts

Only after all implementation and verification is complete, create:

```text
reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV9_RESOLUTION_REPORT_2026-09-22.md
reports/mihomo/GATE_B_REV9_DIFF_2026-09-22.patch
```

The report must contain:

- actual changed files;
- exact test function names verified with `rg`;
- actual command output;
- explicit mapping of every finding in this file to its code and test;
- honest disclosure of anything not run or not completed.

Do not claim “all scenarios pass” by mapping missing tests to older coarse suites.

## 12. Completion condition

This task is complete only when:

- sections 1–8 are implemented in production code;
- all required tests exist and pass;
- race detector passes;
- the focused patch and truthful Rev9 resolution report exist;
- no IPK was built and no router was modified.

If blocked, document the exact blocker and stop. Do not replace implementation with another plan or review document.

