# Mihomo Gate B Revision 8 — final implementation plan

**Status:** ready for implementation  
**Repository:** `E:\AWGM\awg-manager`  
**Scope:** fix durable bridge identity, deletion, same-slot replacement and rollback. Do not build IPK and do not deploy.

## 1. Result required

After this work, Mihomo bridge reconciliation must remain safe and recoverable when the native store changes before NDMS reconciliation:

- deleting a native Mihomo resource removes its old NDMS bridge and commits;
- replacing resource A with B on the same `ProxyN` withdraws A before publishing B;
- rollback removes candidate bridges and restores previous bridges even after the store snapshot has changed;
- restart recovery uses manifests/generation records, not an in-memory cache or the current store, as its source of truth;
- foreign, unmanaged, sing-box and user-created proxy interfaces are never modified;
- incomplete legacy identity fails before config, runtime or NDMS side effects.

## 2. Non-negotiable invariants

1. A bridge mutation is authorized by a **complete durable `BridgeRef`** stored in a transaction manifest or verified generation record.
2. The current native store is authoritative for publishing a target bridge, but is not authoritative for inspecting or withdrawing a bridge from an older/candidate generation.
3. `PublishBridge` requires an exact current-store match.
4. `InspectBridge` and `WithdrawBridge` may operate from a complete durable ref when the resource is absent from the current store or when the same slot now describes the opposite generation in a controlled replacement.
5. NDMS ownership is checked atomically by `LookupProxy`, `EnsureProxyIfOwned` and `RemoveProxyIfOwned`. No foreign/unmanaged bridge may be overwritten or deleted.
6. `PreviousBridges` is persisted before `mutateFn`.
7. `TargetBridges` is persisted before config promotion, runtime restart and any NDMS mutation.
8. Durable manifests and generation records are authoritative. Runtime memory may only cache validated copies.
9. No arbitrary `Proxy1..N` scanning.
10. Every side effect has a durable intent and an idempotent replay path.

## 3. Durable bridge identity

### 3.1 `internal/mihomo/types.go`

Extend `BridgeRef`:

```go
type BridgeRef struct {
    ProxyIndex      int    `json:"proxy_index"`
    ProxyInterface  string `json:"proxy_interface"`
    KernelInterface string `json:"kernel_interface"`
    ListenPort      int    `json:"listen_port,omitempty"`
    LegacyOwner     string `json:"legacy_owner,omitempty"`
    OwnerUUID       string `json:"owner_uuid,omitempty"`
    Generation      uint64 `json:"generation,omitempty"`
}
```

Add:

- `ValidateComplete()`: requires positive index, exact `Proxy%d` name, non-empty kernel interface, canonical non-empty owner and valid port `1..65535`.
- `IsLegacy()`: true when any required identity field is missing or inconsistent.
- `SlotKey()`: stable key containing proxy index, proxy interface and kernel interface.
- `SameOwnerAndEndpoint(other)`: same slot, owner and listen port.

`Digest()` must cover every field, including `ListenPort`.

Update all semantic paths:

- `AppliedGenerationRecord.Equal()` sorting and equality;
- manifest/generation clone and validation;
- bridge list deduplication;
- operation digest validation;
- tests and fixtures.

Two refs differing only by port or owner must not compare equal.

### 3.2 Producers

Populate `ListenPort` and canonical `OwnerUUID` in:

- `internal/mihomonative/store.go` / `StoreTxAdapter.ListBridges`;
- `internal/singbox/router/service_mihomo.go` / `AssembleCompileInput`;
- every other production `BridgeRef` producer found by `rg 'BridgeRef{'`.

## 4. Legacy migration before side effects

### 4.1 Enrichment

Replace permissive wildcard enrichment with a dedicated legacy migration routine:

```go
enrichLegacyBridgeRefLocked(ref BridgeRef, snapshot []BridgeRef) (BridgeRef, error)
```

Rules:

- complete refs pass unchanged;
- only legacy refs may be enriched;
- match by every non-empty legacy field;
- require exactly one candidate;
- candidate must itself be complete;
- ambiguity or absence returns `ErrForeignBridgeOwnership`;
- never invent an index, owner, interface or port.

### 4.2 Before mutation

At transaction start, before `mutateFn`:

1. Read the pre-mutation bridge snapshot once from `StoreTx.ListBridges()`.
2. Enrich every `c.appliedRecord.AppliedBridges` entry against that snapshot.
3. Validate the resulting list and reject duplicate slots.
4. Deep-copy it into `manifest.PreviousBridges`.
5. Persist it together with the secured pre-mutation snapshot.
6. Only then invoke `mutateFn`.

If migration fails, prove that none of these happened: `mutateFn`, candidate write, active-config swap, process restart, NDMS call.

### 4.3 Startup migration

Do **not** rewrite `verified-active.json` alone. It is part of a generation/digest relationship.

On startup:

- legacy applied refs may be enriched in memory for a recovery attempt;
- persist an upgrade only through the existing authoritative generation/commit mechanism, producing a consistent generation bundle, pointer and verified-active record;
- if an atomic durable upgrade cannot be completed, remain fail-closed with the original files untouched and a recovery marker.

Add an explicit bridge identity schema version to generation/manifest data, or a clearly validated legacy-version predicate. New writes always use the new version.

## 5. Transaction persistence order

### 5.1 Previous generation

Before `mutateFn`, `manifest.PreviousBridges` must already contain complete canonical refs and be on disk.

### 5.2 Target generation

Immediately after compile:

1. Validate all `compileResult.TargetBridges` as complete.
2. Reject duplicate `SlotKey`s.
3. Deep-copy them into `manifest.TargetBridges`.
4. Store `TargetBridgesDigest` in the manifest.
5. Checkpoint the manifest before candidate config write/promotion, runtime restart or bridge reconciliation.

Add `TargetBridgesDigest` to manifest schema validation, clone/CAS validation and strict decoding tests.

Recovery must verify the digest before using target refs.

## 6. Production runtime resolution

Replace `resolveOwnedBridge` with operation-specific helpers.

### 6.1 Publish

`resolvePublishIdentity(ref)`:

- requires a complete ref;
- requires exactly one current-store resource with the same slot;
- requires exact equality of owner, interfaces and listen port;
- returns allowed legacy tokens;
- mismatch fails before any NDMS call.

`PublishBridge` then uses `EnsureProxyIfOwned` and verifies the result through inspection.

### 6.2 Inspect

`resolveInspectIdentity(ref)`:

- requires a complete durable ref;
- canonical owner comes from `ref.OwnerUUID`, never from an unrelated current-store occupant;
- if current store contains the same owner, all fields must agree;
- if current store is absent, inspection is allowed;
- if current store has a different owner in the same slot, inspection of the durable old/candidate ref is still allowed because this is expected during replacement/rollback; NDMS classification remains relative to the supplied durable owner;
- it must not authorize publishing.

### 6.3 Withdraw

`resolveWithdrawIdentity(ref)` follows the same durable identity rule as inspection:

- complete ref required;
- absence from store allowed;
- a different current-store owner on the same slot does not invalidate the durable old ref;
- deletion is still protected by `RemoveProxyIfOwned(ref.OwnerUUID, allowedLegacy...)`;
- if live NDMS owner is target/foreign/unmanaged rather than the supplied ref owner, removal returns false and the operation fails closed.

This distinction is essential: after A→B store mutation, the store describes B while NDMS may still contain A.

### 6.4 Observed bridges

Do not scan a numeric NDMS range.

`ListObservedBridges` inspects only a validated union supplied from:

- durable current applied generation;
- active transaction `PreviousBridges`;
- active transaction `TargetBridges`;
- current native-store target refs.

Introduce an optional interface in `internal/mihomo/types.go` if registration is needed:

```go
type DurableBridgeRegistry interface {
    ReplaceDurableBridges([]BridgeRef) error
}
```

Registration atomically replaces a mutex-protected cache. The cache is never authoritative and recovery must work when it starts empty, after the coordinator registers refs read from durable files.

Unknown sing-box/user proxy slots are never enumerated or mutated.

## 7. Reconciliation algorithm

### 7.1 Classification

Build maps by `SlotKey`, rejecting duplicates.

- only in `before`: pure withdrawal;
- only in `target`: pure creation;
- in both and `SameOwnerAndEndpoint`: retained;
- in both but owner or port differs: controlled replacement.

For retained refs:

- inspect target identity;
- canonical match: no mutation;
- absent: enqueue self-heal publish;
- recognized legacy owner: enqueue canonical migration publish;
- foreign/unmanaged: `StateRecoveryRequired`, no mutation.

For replacement A→B:

- inspect using durable A identity;
- require live A canonical/recognized legacy owner, or absence;
- if live B is already canonical, treat A withdrawal as already complete and continue with verification of B;
- any third-party/unmanaged owner fails closed.

### 7.2 Execution order

Execute all withdrawals before creations:

1. persist withdraw intent;
2. inspect/authorize old durable ref;
3. `WithdrawBridge(old)`;
4. verify absence relative to old identity;
5. persist verified state;
6. persist publish intent;
7. strict store-backed authorization for target ref;
8. `PublishBridge(target)`;
9. verify canonical target owner and endpoint;
10. persist verified state.

Sorting must be deterministic by slot and owner digest.

### 7.3 Journal identity

Use the full digest, not a truncated collision domain, as the logical identity:

```text
operation ID = txID + action + slot key + full BridgeRef digest
```

On replay, require the stored `TargetDigest` and stored `BridgeRef.Digest()` to match the requested operation. A mismatch is corruption and requires recovery.

## 8. Rollback and crash recovery

### 8.1 Immediate rollback

After restoring the pre-mutation store:

- `before` = validated `manifest.TargetBridges`;
- `target` = validated `manifest.PreviousBridges`;
- do not use `ListActiveBridges()` as the source of identity;
- call the same reconciliation algorithm, which withdraws B then restores A.

Prefer manifest refs over `c.appliedRecord`; the manifest is the transaction-specific durable authority.

### 8.2 Resumed rollback

- `before` = validated `manifest.TargetBridges`;
- `target` = verified LKG generation `AppliedBridges`;
- verify target generation/digests before use;
- reconcile after store restoration using durable refs.

### 8.3 Partial operations

Use `BridgeOperations` states to replay idempotently:

- `intent`: inspect live state, then apply or recognize already-applied state;
- `applied`: verify postcondition without blindly repeating mutation;
- `verified`: skip only after digest/ref consistency validation.

Crash recovery must cover:

- before first withdrawal;
- after A withdrawal but before checkpoint;
- after A withdrawal checkpoint;
- after B publish but before checkpoint;
- after B publish checkpoint;
- during inverse rollback.

## 9. Tests required

Use real `ApplyCoordinator`, real `mihomoBridgeRuntime`, real `mihomonative.Store`, and a stateful ownership-aware NDMS fake for end-to-end tests.

1. Apply bridge normally and commit.
2. Delete its resource: old NDMS bridge is withdrawn and transaction commits.
3. Replace A→B on the same slot: registrar proves withdraw-A precedes publish-B.
4. Change only listen port: controlled replacement occurs.
5. Fail B publication: rollback restores A.
6. Crash after A withdrawal: restart completes B.
7. Crash after B publication: restart verifies B without unsafe duplicate mutation.
8. Crash during inverse rollback: restart finishes restoration of A.
9. Foreign takeover between inspection and removal is not deleted.
10. Foreign takeover between withdrawal and publish is not overwritten.
11. Unmanaged live slot is untouched.
12. Coexisting sing-box and user NDMS slots are untouched across apply/delete/rollback.
13. `PreviousBridges` is on disk before `mutateFn` is invoked.
14. `TargetBridges` and its digest are on disk before the first config/runtime/NDMS side effect.
15. Unresolvable legacy record fails before all side effects.
16. Empty/mismatched owner direct runtime calls make zero registrar mutations.
17. Store-absent inspect/withdraw succeeds only with a complete durable ref.
18. Current store B does not prevent safe withdrawal of durable A when live NDMS owner is A.
19. Empty runtime cache after restart produces the same recovery result.
20. Corrupt bridge digest/journal ref fails closed.
21. Refs differing only by port have unequal digest and applied-record equality.
22. Legacy durable upgrade is atomic; injected write failure leaves old authoritative files mutually consistent.

Coordinator mock tests remain useful but cannot substitute for these production-wired tests.

## 10. Implementation order

1. Add identity fields, schema version, validation, equality and producer updates.
2. Add legacy enrichment and pre-mutation `PreviousBridges` checkpoint.
3. Persist and validate `TargetBridges` plus digest before side effects.
4. Split production runtime resolution paths.
5. Replace reconciliation classification and enforce withdraw-before-create.
6. Make journal IDs digest-specific and replay state-aware.
7. Correct immediate and resumed rollback inputs.
8. Wire optional durable-ref registration without making cache authoritative.
9. Add production-wired tests and crash failpoints.
10. Run verification; only then update resolution report and focused patch.

Do not combine all steps into one opaque edit. Keep each stage compiling and testable.

## 11. Verification commands

Run in WSL Ubuntu:

```bash
go test -count=1 ./internal/mihomo
go test -race -count=1 ./internal/mihomo
go test -count=1 ./cmd/awg-manager
go test -race -count=1 ./cmd/awg-manager
go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./internal/proxyrt
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Also run the new E2E/crash tests individually with `-v -count=1` and record their exact names and output.

## 12. Acceptance criteria

Gate B Revision 8 is complete only when:

- all 22 scenarios above exist and pass;
- both rollback implementations use durable candidate and previous refs;
- no current-store-only dependency remains for old-generation inspection/withdrawal;
- same-slot replacement and inverse rollback are deterministic and crash-resumable;
- arbitrary NDMS slots are never scanned;
- foreign, unmanaged and sing-box interfaces are provably untouched;
- all targeted and race tests pass independently;
- focused `GATE_B_REV8_DIFF_2026-09-22.patch` contains only Revision 8 scope;
- the resolution report states actual evidence and does not mark unchecked work complete.

## 13. Required artifacts

- `reports/mihomo/GATE_B_REV8_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV8_RESOLUTION_REPORT_2026-09-22.md`
- updated `walkthrough.md`

No IPK build and no router deployment in this phase.
