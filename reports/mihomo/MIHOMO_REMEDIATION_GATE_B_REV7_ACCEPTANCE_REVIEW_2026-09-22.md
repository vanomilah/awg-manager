# Mihomo Remediation Gate B Revision 7 — acceptance review

**Date:** 2026-09-22  
**Branch/worktree:** `feature/mihomo-ai-proxyrt`, `E:\AWGM\awg-manager`  
**Reviewed artifacts:**

- `reports/mihomo/GATE_B_DIFF_2026-09-21.patch`
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_RESOLUTION_REPORT_2026-09-21.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Verdict

**Gate B Revision 7 is not accepted. One production P0 blocker remains.**

Revision 7 closes the two direct Rev 6 findings: the production bridge runtime now implements `ExactBridgeRuntime`, and RuntimeOff checks active-config absence without treating arbitrary `Stat` errors as `ENOENT`. Retained-bridge inspection and owner postconditions are also present.

However, the production ownership resolver depends exclusively on the **current mutable native store**. `MutateAndApply` mutates that store before bridge reconciliation. Consequently, a bridge belonging to a resource removed by the transaction can no longer be resolved when the coordinator tries to inspect or withdraw it. The same defect affects rollback after the store snapshot is restored: bridges created by the failed target generation may disappear from the restored store before they are withdrawn.

The report's claim that Gate B is fully verified is therefore stronger than the implementation and test evidence.

## Findings

### P0 — bridge deletion and rollback can no longer prove ownership after the store changes

#### Evidence

1. `MutateAndApply` executes `mutateFn()` at `internal/mihomo/coordinator.go:1609-1616`.
2. It compiles the post-mutation target at `internal/mihomo/coordinator.go:1628`.
3. Bridge reconciliation happens only later at `internal/mihomo/coordinator.go:1757-1763` and `1798-1804`.
4. A removed bridge comes from `beforeBridges`, and `syncBridgesLocked` correctly puts it into `toWithdraw` at `2074-2080`.
5. Before withdrawal, both coordinator verification and the production runtime call `InspectBridge`.
6. Production `InspectBridge`, `PublishBridge`, and `WithdrawBridge` all call `resolveOwnedBridge` (`cmd/awg-manager/mihomo_bridge_runtime.go:372-375`, `418-421`, `443-446`).
7. `resolveOwnedBridge` searches only `r.store.ListBridges()` (`329-350`). After deletion, the old resource is no longer in that store, so it returns `ErrForeignBridgeOwnership: bridge ... not found in native store` instead of inspecting/removing the bridge described by the durable `before` record.
8. Rollback restores a store snapshot before syncing bridges (`internal/mihomo/coordinator.go:2921-2933`, bridge sync starts after `3003`). Thus target-only bridges can similarly lose their current-store mapping before rollback withdrawal.

#### User-visible consequence

- deleting a Mihomo native proxy/subscription/server with an NDMS bridge can fail during apply;
- the transaction can enter `recovery_required` while the NDMS `ProxyN` interface remains;
- rollback after a partially applied bridge change can fail for the same reason;
- `ListObservedBridges` cannot discover orphaned applied bridges because it also enumerates only current-store bridges (`cmd/awg-manager/mihomo_bridge_runtime.go:466-483`).

#### Why current tests miss it

`TestMihomoBridgeRuntime_CoordinatorLifecycleSubtests` directly calls `PublishBridge` and `WithdrawBridge` while the resource remains present in the store. It does not run a real production coordinator transaction that deletes the resource before reconciliation. The coordinator retained/enrichment tests use `newMockExactBridgeRuntime`, so they do not exercise production `resolveOwnedBridge`.

#### Required fix

Make the durable `BridgeRef` sufficient for exact inspection/mutation across store transitions, while retaining fail-closed identity checks.

Recommended design:

1. Treat `BridgeRef.OwnerUUID`, `ProxyIndex`, `ProxyInterface`, `KernelInterface`, listen port, and the allowed legacy-owner set as immutable bridge identity captured in the applied generation/transaction manifest.
2. Persist every field required for later publish/withdraw. Do not reconstruct old-generation ownership exclusively from the current store.
3. In production runtime:
   - validate the internal consistency of the supplied `BridgeRef`;
   - use current store lookup as an additional consistency check when a matching resource exists;
   - permit a store-absent **withdraw/inspect** only from a complete, durable, canonical ref supplied by the coordinator;
   - never permit a wildcard/default identity or an empty canonical owner;
   - for publish, require the target identity to agree with the target store.
4. For old persisted records lacking the new fields, migrate/enrich them before the store mutation or retain a pre-mutation identity snapshot in the manifest. If identity cannot be proven uniquely, fail closed before any runtime/config side effect.
5. Rollback must use the identities from both generations, not only whichever generation currently occupies the native store.

#### Required regression tests

Use the real `mihomoBridgeRuntime`, stateful ownership-aware NDMS fake, real native store, and real `ApplyCoordinator` wiring:

- create resource + bridge, apply, then delete resource: old NDMS bridge is withdrawn and transaction commits;
- replace resource A with B reusing/differing `ProxyN`: correct ordered ownership behavior, no foreign deletion;
- failure after target bridge publish followed by rollback: target-only bridge is withdrawn after store restore, previous bridge is restored;
- crash/restart in bridge reconciliation after store mutation: resume reaches the same final state;
- old-generation record without sufficient identity fails **before** runtime/config mutation;
- foreign or unmanaged interface at the old index is never removed;
- `ListObservedBridges` or its replacement reports durable applied/orphaned bridges needed for recovery.

### P1 — production runtime does not reject a mismatched non-empty `ref.OwnerUUID` before mutation

`resolveOwnedBridge` calculates `canonicalOwner` from the current store but never verifies that a supplied non-empty `ref.OwnerUUID` equals it. `PublishBridge` can therefore create/update the interface under the store-derived owner and only the coordinator's later postcondition notices that the requested owner differed. That is fail-after-mutation, not fail-before-mutation.

Add an early check:

```go
if ref.OwnerUUID == "" || ref.OwnerUUID != canonicalOwner {
    return ..., fmt.Errorf("%w: bridge owner mismatch", mihomo.ErrForeignBridgeOwnership)
}
```

For the controlled legacy-migration path, enrich the ref to the canonical owner before calling runtime. Add direct runtime tests for empty and mismatched `OwnerUUID`, asserting zero registrar mutations.

### P1 — identity matching still permits partial wildcard references

Both `resolveOwnedBridge` and `enrichBridgeRefLocked` treat empty interface fields as wildcards and primarily match by proxy index. This is safer than guessing when multiple matches exist, but it is not the claimed exact match. A single stale resource sharing an index can be accepted even if the durable ref lacks interface identity.

Define explicit legacy schema versions and migration rules. New records must require all canonical identity fields. Only a narrowly scoped migration routine may accept incomplete old records, and it must run before side effects and persist the upgraded record.

### P2 — artifact/report accuracy

- The supplied path `walkthrough.m` does not exist; the reviewed file is `walkthrough.md`.
- The resolution report says the lifecycle suite has “6” scenarios but lists seven behaviors.
- The patch is a broad cumulative Gate B patch (about 12.8k diff lines), not a small Revision 7-only delta. This is usable as an archive but poor review evidence. Produce a base commit SHA and a focused Rev 7 diff or commit series.
- `git diff --check` passed but emitted an LF/CRLF warning for `internal/mihomo/operator.go`; this is not a functional failure, but the “clean” claim should mention it.

## Confirmed improvements

The following changes are present and technically sound within their covered scope:

- compile-time conformance of `*mihomoBridgeRuntime` to `ExactBridgeRuntime`;
- mutually exclusive canonical/legacy/foreign/unmanaged classification via NDMS lookup;
- early rejection of non-exact runtimes when bridges are present;
- retained-bridge inspection, missing-bridge self-heal, and foreign/unmanaged fail-closed behavior;
- non-empty owner guard before tracked mutations;
- create postcondition checks existence, canonical owner equality, and cleared legacy owner;
- injectable single `Stat` dependency and strict `ENOENT` proof in Apply, regeneration, and rollback;
- stateful production-runtime fake is materially better than the previous always-success fake.

## Independent verification

Executed against the current worktree:

```text
go test -count=1 ./internal/mihomo
PASS  32.270s

go test -count=1 ./cmd/awg-manager
PASS  0.176s

go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./internal/proxyrt
PASS  all packages

go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
PASS  36.790s / 1.313s

git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
PASS, with one CRLF-to-LF warning for internal/mihomo/operator.go
```

Passing tests do not invalidate the P0 because no current test combines production runtime resolution with deletion/rollback across the mutable-store boundary.

## Re-acceptance gate

Do not mark Gate B complete until all of the following are true:

1. Old and target bridge identities survive store mutation and rollback without relying solely on current `store.ListBridges()`.
2. Production runtime rejects empty/mismatched canonical owner before any NDMS mutation.
3. New-format bridge refs are exact and complete; legacy enrichment is explicit, durable, and pre-side-effect.
4. Production-wired end-to-end deletion and rollback tests listed above pass under `-count=1` and `-race`.
5. A focused diff and honest resolution report replace the current “100% verified” claim.

