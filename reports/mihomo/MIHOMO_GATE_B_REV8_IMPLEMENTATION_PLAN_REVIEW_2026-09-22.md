# Review: Mihomo Gate B Revision 8 implementation plan

**Date:** 2026-09-22  
**Reviewed file:** `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
**Baseline finding:** `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV7_ACCEPTANCE_REVIEW_2026-09-22.md`

## Verdict

**Revision 8 plan is directionally correct, but must be revised before implementation.**

It correctly identifies the store-transition P0, adds the missing listen port to durable identity, proposes pre-mutation enrichment, and recognizes that rollback must use both generations. Those are the right foundations.

The current plan nevertheless leaves three blocking design gaps and several important acceptance gaps. Implementing it literally can still produce an unrecoverable rollback or interfere with bridge interfaces owned by other routing engines.

## Required plan corrections

### P0-1 — explicitly persist `TargetBridges` before any bridge side effect

The plan proposes rollback using `m.TargetBridges`, but does not explicitly add the assignment that makes this data durable. The current apply path builds `compileResult.TargetBridges`, but does not assign it to the manifest before reconciliation.

The revised plan must require:

1. Validate and canonicalize every target bridge immediately after compile.
2. Assign a deep copy to `manifest.TargetBridges`.
3. Persist/checkpoint that manifest **before runtime restart or bridge mutation**.
4. Assign the already enriched pre-mutation applied refs to `manifest.PreviousBridges` and persist them before calling `mutateFn()`.
5. On recovery, use only the durable manifest/generation values; never rebuild an old or candidate identity from the current store.

Add a crash test immediately after target-bridge manifest persistence and another immediately after the first bridge mutation.

### P0-2 — replacement on the same `ProxyN` is not defined

Current `syncBridgesLocked` keys maps only by `KernelInterface`. If resource A is replaced by resource B using the same `ProxyIndex`/`KernelInterface`, the pair is classified as retained. Inspection then compares the live A owner with target B and fails as a foreign conflict. The proposed test names this case but the plan does not change the reconciliation algorithm.

Define bridge identity and slot identity separately:

- **slot key:** `ProxyIndex + ProxyInterface + KernelInterface`;
- **owner identity:** canonical `OwnerUUID`;
- same slot + same owner = retained;
- same slot + different owner = controlled replacement;
- different slot = ordinary create/withdraw.

For controlled replacement:

1. Inspect and prove that the existing slot is owned by the durable `before` ref.
2. Persist a withdraw intent for A.
3. Withdraw A and verify absence.
4. Persist a publish intent for B.
5. Publish B and verify canonical ownership.
6. Rollback performs the exact inverse sequence from durable refs.

Never call publish-B while A still occupies the slot. Never treat a different owner as a retained bridge.

Add crash points after withdrawing A and after publishing B, with restart/resume tests for both.

### P0-3 — do not scan arbitrary NDMS `Proxy1..30` slots

The proposed `ListObservedBridges` scan of slots 1 through 30 is unsafe and underspecified:

- the range is a hard-coded implementation assumption;
- those slots may belong to sing-box, another AWG Manager subsystem, or a user-created NDMS proxy;
- an unknown slot has no trustworthy `BridgeRef`, owner UUID, kernel interface, or listen port;
- reporting it as a Mihomo orphan risks later mutation of a foreign interface.

Replace this proposal with one of these safe designs:

1. **Preferred:** enumerate the union of durable `PreviousBridges`, `TargetBridges`, current applied generation refs, and current target store refs. Inspect only those exact refs.
2. If true NDMS-wide discovery is required, extend the registrar with an authoritative enumeration API returning index and description, then classify only canonical Mihomo owner descriptions with a strict namespace. All other entries remain foreign observations and must never become mutation candidates.

No hard-coded slot range is acceptable. Coexistence tests must include an active sing-box-owned `ProxyN` and a user-owned `ProxyN`, both unchanged after Mihomo reconciliation/recovery.

## Important design corrections

### P1-1 — separate store-backed publish validation from durable inspect/withdraw validation

A single `resolveOwnedBridge` routine no longer fits all operations. Specify three explicit paths:

- `resolvePublishIdentity(ref)`: complete target ref required; exact current-store match required, including owner, port and interfaces.
- `resolveInspectIdentity(ref)`: complete durable ref required; current-store match checked when present, but absence is allowed.
- `resolveWithdrawIdentity(ref)`: complete durable ref required; store absence allowed; any present contradictory store mapping fails closed.

All paths must reject an empty or mismatched owner before calling NDMS.

### P1-2 — `appliedBridges` must not become a second, non-durable source of truth

An in-memory `map[ProxyIndex]BridgeRef` can be a cache, but it cannot be authoritative because it is lost on crash and can become stale. The authoritative inputs must remain generation records and transaction manifests.

If registration is retained:

- introduce a small optional interface such as `DurableBridgeRegistry` instead of coupling the coordinator to the concrete runtime;
- replace the whole cache atomically from a validated durable snapshot rather than incrementally mutating it;
- guard reads/writes under the runtime mutex;
- define when it is refreshed after commit, rollback and startup recovery;
- prove that behavior is identical with an empty cache after process restart.

Prefer passing durable refs directly into reconciliation/recovery rather than depending on the cache.

### P1-3 — version the durable schema and migration

Adding `ListenPort` changes what counts as a complete bridge identity. `omitempty` alone is not a migration design.

The plan must specify:

- a bridge/generation/manifest schema version or an explicit legacy predicate;
- new records always persist `ListenPort` and all exact identity fields;
- old records are enriched only before side effects and the upgraded durable record is checkpointed;
- an old record that cannot be uniquely enriched stops before `mutateFn`, config promotion, runtime restart and NDMS mutation;
- rollback of an old generation follows the same rule.

### P1-4 — update every equality, digest and copy path

`ListenPort` must participate in all semantic comparisons. The current applied-record comparison in `internal/mihomo/types.go:1024-1057` does not compare it.

Audit and test:

- `BridgeRef.Digest()`;
- applied-record equality/canonical sorting;
- manifest clone/deep-copy paths;
- generation validation;
- JSON strict decoding and old-record compatibility;
- deduplication/map keys;
- compiler input/output equality;
- all producers and test fixtures.

Two refs differing only in `ListenPort` must not compare equal.

### P1-5 — rollback sources must be stated correctly for both rollback implementations

There are two relevant paths:

- immediate `rollbackActiveLocked`, which currently restores the pre-mutation store and then calls `ListActiveBridges`;
- durable `resumeRollbackTransactionLocked`, which restores a generation snapshot before later bridge sync.

For both paths, define explicitly:

- `before` = durable candidate/partially applied target refs from `m.TargetBridges` and operation journal;
- `target` = durable previous/LKG refs from `m.PreviousBridges` or the verified generation bundle;
- behavior when only a subset of candidate bridge operations reached `Applied` or `Verified` state;
- idempotent replay after crash.

Do not use `ListActiveBridges()` as the sole source of rollback identity. It may enumerate only the restored current store and omit candidate-only bridges.

### P1-6 — operation journal IDs must distinguish owner replacements

Current operation IDs are based on transaction, action and kernel interface. For A→B replacement on the same slot, this can alias operations during replay.

Include the canonical owner identity or the full bridge digest in the operation ID, and validate that a replayed journal entry's `BridgeRef` matches its operation digest.

## Test-plan corrections

The proposed “7 tests” section actually lists eight categories. Keep the early-owner tests as a separate unit-test group and add the following mandatory cases:

1. Same-slot A→B successful replacement with verified ordering.
2. Crash after A withdrawal, then restart completes B publication.
3. Crash after B publication, then restart verifies without repeating unsafe mutations.
4. Failure publishing B rolls back by restoring A.
5. Foreign takeover between pre-inspection and withdraw/publish fails closed without deleting the foreign interface.
6. Coexisting sing-box and unmanaged NDMS proxy slots remain untouched.
7. Target bridges are present in the persisted manifest before the first NDMS call.
8. A bridge whose only change is `ListenPort` is treated as a replacement/update, not retained.
9. Recovery works with an empty in-memory bridge cache.
10. Legacy enrichment failure proves `mutateFn` was never called and no candidate/config/runtime/NDMS side effect occurred.

The end-to-end tests must use the real production runtime and registrar adapter. Coordinator-only tests with `mockExactBridgeRuntime` are useful but do not close this P0.

## What the plan already gets right

- Correct diagnosis of the mutable-store boundary.
- Durable `ListenPort` is necessary for store-absent replay.
- Pre-mutation enrichment is the correct time for legacy migration.
- Publish must require exact agreement with the target store.
- Inspect and withdraw may use a complete durable ref when the corresponding store resource no longer exists.
- Rollback must reconcile candidate and restored generations rather than infer everything from current state.
- Focused Revision 8 patch and independent race testing are appropriate acceptance artifacts.

## Required revised-plan checklist

Before implementation starts, the plan should explicitly contain all of these:

- [ ] Persist complete `PreviousBridges` before `mutateFn`.
- [ ] Persist complete `TargetBridges` before runtime/NDMS side effects.
- [ ] Define slot identity versus owner identity.
- [ ] Define ordered same-slot owner replacement and inverse rollback.
- [ ] Remove hard-coded `Proxy1..30` scanning.
- [ ] Keep durable manifest/generation data authoritative; memory map is cache only.
- [ ] Split publish versus inspect/withdraw resolution rules.
- [ ] Version legacy migration and persist upgraded records.
- [ ] Include `ListenPort` in equality, digest, validation and copies.
- [ ] Make operation IDs owner/digest-specific.
- [ ] Cover both immediate and resumed rollback paths.
- [ ] Add production-wired deletion, replacement, crash and coexistence tests.

Only after these corrections should Revision 8 be handed to an implementation agent.
