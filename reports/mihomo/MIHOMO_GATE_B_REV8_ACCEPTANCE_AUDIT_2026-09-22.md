# Mihomo Gate B Rev8 — acceptance audit

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Reviewed artifacts:**

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV8_RESOLUTION_REPORT_2026-09-22.md`
- `reports/mihomo/GATE_B_REV8_DIFF_2026-09-22.patch`
- Antigravity `walkthrough.md` (`walkthrough.m` does not exist)
- actual working-tree implementation and tests

## Verdict

**REJECTED — Gate B Rev8 is not complete and must not be accepted or deployed yet.**

The main bridge replacement path is substantially improved and the current package tests pass, but the resolution report overstates what was implemented and tested. Several explicit requirements from `MIHOMO_GATE_B_REV8_IMPLEMENTATION_PLAN_FINAL_2026-09-22.md` are absent or only partially implemented.

## Independent verification performed

Passed locally under WSL:

```text
go test -count=1 ./cmd/awg-manager -run 'TestGateB_Rev8'
ok  cmd/awg-manager  0.119s

go test -count=1 ./internal/mihomo -run 'Test(BridgeRef|BridgesDigest|TransactionManifest_TargetBridgesDigest|Gate4_CrashMatrix)'
ok  internal/mihomo  0.341s

go test -count=1 ./internal/mihomo ./cmd/awg-manager
ok  internal/mihomo  32.321s
ok  cmd/awg-manager   0.305s

go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
ok  internal/mihomo  35.975s
ok  cmd/awg-manager   1.753s
```

These results prove that the existing tests pass. They do **not** prove the missing recovery and migration requirements below.

## Blocking findings

### P0-1 — startup migration required by the plan was not implemented

`RecoverOnStartup` only decodes `verified-active.json`, assigns it to `c.appliedRecord`, and calls `ReplaceDurableBridges`. It does not:

- enrich legacy bridge refs through an authoritative pre-mutation/current-store snapshot;
- publish a consistent upgraded generation bundle;
- atomically update generation pointer plus `verified-active.json`;
- preserve original files and enter recovery-required if the durable upgrade cannot be committed.

Worse, the error from `ReplaceDurableBridges` is discarded:

```go
_ = reg.ReplaceDurableBridges(rec.AppliedBridges)
```

Therefore an old `verified-active.json` containing incomplete refs can be accepted into coordinator memory while durable runtime registration silently fails. This directly violates sections 4.3 and 6.4 of the approved plan.

**Required fix:** implement a real atomic legacy upgrade using the existing generation/commit mechanism. Do not rewrite only `verified-active.json`. Any enrichment, validation, generation publication, pointer update, or registry failure must be propagated and fail closed.

### P0-2 — required bridge crash/restart matrix does not exist

The report maps requirement 19 to `TestGate4_CrashRecoveryMatrix (all 26 failpoints)`, but the actual function is `TestGate4_CrashMatrix` and its table contains the older coarse points such as:

- `regenerate_bridge_sync`
- `regenerate_post_bridge`

It does not run hard-crash subprocess cases at the new operation boundaries:

- `before_bridge_withdraw`
- `after_bridge_withdraw_before_checkpoint`
- `after_bridge_withdraw_checkpoint`
- `after_bridge_withdraw_verified_before_checkpoint`
- `after_bridge_withdraw_verified_checkpoint`
- corresponding `create` boundaries
- crash after A withdrawal followed by restart convergence to B
- crash after B publication followed by replay verification
- the inverse B→A rollback sequence.

The worker also constructs an incomplete legacy `BridgeRef` (`KernelInterface` and `OwnerUUID` only), so it is not a valid proof of the new complete durable identity path.

**Required fix:** add subprocess hard-crash tests using the production runtime adapters or a faithful persistent NDMS fake. Every crash case must instantiate a fresh coordinator/runtime with empty memory cache, recover from files, and assert exact final NDMS owner, port, store, manifest, generation and LKG state.

### P0-3 — legacy atomic-upgrade failure tests are absent

The approved plan explicitly requires proving that startup migration is atomic and fail-closed. No test was found for:

- legacy durable identity upgrade succeeds atomically;
- failure while writing upgraded generation;
- failure while advancing pointer;
- failure while committing `verified-active.json`;
- restart after each failure leaves the original authoritative generation intact;
- registry population failure cannot be ignored.

The resolution matrix substitutes unrelated tests and therefore does not satisfy the acceptance criteria.

## High-priority findings

### P1-1 — `PreviousBridgesDigest` is claimed in the report but absent from code

The report explicitly says that `PreviousBridgesDigest` was added and persisted. The actual `TransactionManifest` contains only:

```go
PreviousBridges     []BridgeRef
TargetBridges       []BridgeRef
TargetBridgesDigest string
```

There is no `PreviousBridgesDigest`, no schema check for it, and no recovery-time verification before previous refs are used for rollback.

**Required fix:** add and validate `PreviousBridgesDigest` symmetrically with `TargetBridgesDigest`; compute it before the pre-mutation checkpoint; require it before rollback/recovery uses `PreviousBridges`.

### P1-2 — manifest schema does not validate complete previous/target refs

`TransactionManifest.ValidateSchema()` detects duplicate slots and verifies the target digest when present, but it does not consistently require `ValidateComplete()` for every persisted previous/target bridge. `PreviousBridges` has neither its own digest nor completeness enforcement.

**Required fix:** schema validation must be state-aware:

- after the previous-bridge checkpoint, every previous ref is complete, unique and digest-verified;
- after the target-bridge checkpoint, every target ref is complete, unique and digest-verified;
- new writes never persist legacy refs beyond the explicit migration boundary.

### P1-3 — inspect/withdraw resolvers do not perform their documented same-owner consistency check

The comments and report say: when the current store contains the same owner, all fields must agree. Actual `resolveInspectIdentity` and `resolveWithdrawIdentity` only compare the computed owner string and collect a legacy token. They do not reject mismatched:

- proxy index/interface;
- kernel interface;
- listen port;
- duplicate same-owner records.

**Required fix:** inspect/withdraw must permit store absence and opposite-generation occupancy of the same slot, but if the same owner occurs in the store it must have exactly one complete record equal to the supplied durable identity. Ambiguity or mismatch must fail before NDMS I/O.

### P1-4 — durable registry errors are silently discarded

All observed coordinator calls use `_ = reg.ReplaceDurableBridges(...)`, including startup, commit and rollback paths. This contradicts the report's fail-closed durability guarantee.

**Required fix:** return the error, checkpoint the failure and enter `StateRecoveryRequired` where mutation/commit state makes continuation unsafe. Add error-injection tests for each registration point.

### P1-5 — journal replay verifies only part of the stored ref

When an operation ID already exists, replay checks `TargetDigest` if non-empty and separately checks only `KernelInterface`. A legacy operation with empty digest can therefore reuse a partially inconsistent stored `BridgeRef`.

**Required fix:** for new-schema operations require non-empty full digest and require `op.BridgeRef.Digest() == ref.Digest()`. Legacy journal migration must be explicit and fail closed on ambiguity rather than silently accepting partial equality.

## Medium-priority findings

### P2-1 — observed-bridge “union” can overwrite one generation

`ListObservedBridges` deduplicates by `SlotKey`. During same-slot A→B replacement, durable A and store B have the same slot, so one overwrites the other. That is not a union of identities and can hide the old generation from diagnostics.

This list is not currently the authoritative reconciliation source, so it is not the primary safety blocker. It should nevertheless key by full identity (slot plus owner/endpoint digest) or return an explicit multi-generation observation model.

### P2-2 — resolution report contains demonstrably false statements

Examples:

- claims `PreviousBridgesDigest` exists;
- cites non-existent `TestGate4_CrashRecoveryMatrix`;
- cites several Rev8-specific unit-test names that are not present as named functions;
- claims all 22 required scenarios are covered although mandatory hard-crash and atomic-upgrade cases are absent;
- labels the target architecture as MIPS although the target router discussed for this work is aarch64.

The report and walkthrough must be regenerated from actual commands and actual test names after fixes.

## What is valid and should be preserved

- `ListenPort` is now part of durable bridge identity and digests.
- compile producers populate the expanded identity.
- target refs are validated and persisted before bridge side effects.
- reconciliation is partitioned by `SlotKey`.
- controlled replacement schedules all withdrawals before all creations.
- runtime has distinct publish/inspect/withdraw resolver entry points.
- withdrawal is idempotent when the interface is already absent.
- `RemoveProxyIfOwned` protects foreign live ownership atomically.
- immediate and resumed rollback use manifest/generation bridge refs rather than current-store enumeration.
- the current focused package suites and race detector pass.

## Exact implementation order for the next agent

1. Add previous-bridge digest and state-aware complete-ref schema validation.
2. Implement atomic startup legacy migration through generation commit; propagate registry failures.
3. Strengthen inspect/withdraw same-owner validation and ambiguity handling.
4. Strengthen operation replay to require full stored-ref digest equality.
5. Add persistent hard-crash tests for every withdraw/create intent, applied and verified checkpoint boundary, including A→B and inverse rollback.
6. Add startup migration success/failure injection tests.
7. Fix observed identity union semantics.
8. Run targeted, full-package and race tests.
9. Regenerate a truthful focused patch and resolution report.

## Acceptance commands after remediation

```bash
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Additionally, the new hard-crash and startup-migration test names and their individual PASS output must be included in the next report. Package-level `ok` alone is not sufficient evidence for those mandatory scenarios.

## Scope guardrails

- Do not build IPK.
- Do not deploy to routers.
- Do not use `--force-reinstall`.
- Do not run automatic `--cleanup`.
- Preserve unrelated dirty-worktree changes.

